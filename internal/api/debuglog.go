package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"runtime"
	"strings"

	"github.com/julienschmidt/httprouter"
	"github.com/rs/zerolog"

	"github.com/joernott/doenerstag/internal/logging"
)

// bodyPeekLimit is how much of a JSON body the DEBUG entry line reads for its
// field names. Every JSON body this API accepts is far smaller; a larger one is
// left unread rather than half-parsed.
const bodyPeekLimit = 1 << 20

// logEntry wraps a handler so that, at DEBUG, its entry is logged as
// docs/08_technologies.md asks: the function, its parameters, and of a request
// body only the names of the fields it contained (20.21).
//
// The parameters are the route, the path parameters and the query, each value
// through the logger's redaction, so a reset token in the path or an API token
// in the query is written as ***. A body's values are never read into the line
// at all. Below DEBUG the wrapper costs one level check per request.
func (r *Router) logEntry(info RouteInfo, name string, next http.Handler) http.Handler {
	logger := r.opts.Logger
	if logger == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if debugEnabled(logger) {
			entry := logger.With().
				Str(logging.FieldComponent, "api").
				Str(logging.FieldRequestID, RequestIDFrom(req.Context())).
				Logger()
			logging.FuncCall(&entry, name, entryArgs(info, req))
		}
		next.ServeHTTP(w, req)
	})
}

// debugEnabled reports whether a DEBUG line from logger would be written.
func debugEnabled(logger *zerolog.Logger) bool {
	return logger.GetLevel() <= zerolog.DebugLevel && zerolog.GlobalLevel() <= zerolog.DebugLevel
}

// entryArgs collects what the entry line reports about a request.
func entryArgs(info RouteInfo, req *http.Request) map[string]any {
	args := map[string]any{"route": info.Method + " " + info.Path}
	for _, param := range httprouter.ParamsFromContext(req.Context()) {
		args[param.Key] = param.Value
	}
	for key, values := range req.URL.Query() {
		if len(values) > 0 {
			args["query."+key] = values[0]
		}
	}
	if fields := bodyFieldNames(req); len(fields) > 0 {
		args["body_fields"] = fields
	}
	return args
}

// bodyFieldNames reads a JSON body's top-level field names and puts the body
// back, untouched, for the handler.
func bodyFieldNames(req *http.Request) []string {
	if req.Body == nil || req.Body == http.NoBody ||
		!strings.HasPrefix(req.Header.Get("Content-Type"), "application/json") {
		return nil
	}

	peeked, err := io.ReadAll(io.LimitReader(req.Body, bodyPeekLimit+1))
	req.Body = struct {
		io.Reader
		io.Closer
	}{io.MultiReader(bytes.NewReader(peeked), req.Body), req.Body}
	if err != nil || len(peeked) > bodyPeekLimit {
		return nil
	}

	var fields map[string]any
	if json.Unmarshal(peeked, &fields) != nil {
		return nil
	}
	return logging.FieldNames(fields)
}

// handlerName is how a handler is named in its entry line: the method it is,
// without the module path -- "api.(*OrderHandlers).get".
func handlerName(handler http.Handler) string {
	fn, ok := handler.(http.HandlerFunc)
	if !ok {
		return fmt.Sprintf("%T", handler)
	}
	name := runtime.FuncForPC(reflect.ValueOf(fn).Pointer()).Name()
	name = strings.TrimSuffix(name, "-fm")
	if slash := strings.LastIndex(name, "/"); slash >= 0 {
		name = name[slash+1:]
	}
	return name
}
