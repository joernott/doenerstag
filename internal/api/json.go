package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// MaxJSONBodyBytes bounds a request body.
//
// Every JSON body this API accepts is a handful of short fields; image uploads
// go through a different path with its own limit. Without a bound, a single
// request could ask the server to buffer as much memory as it likes.
const MaxJSONBodyBytes = 1 << 20 // 1 MiB

// decodeJSON reads a JSON request body into target.
//
// It writes the error response itself and reports whether the caller should
// continue, because every call site would otherwise repeat the same four-way
// switch on the same four failures.
//
// The decoder is strict in two ways that matter:
//
//   - Unknown fields are refused. A client sending "diplay_name" has made a
//     mistake, and silently ignoring it means their change appears to succeed
//     and does nothing.
//   - Exactly one JSON value is required. Trailing content after the object is
//     a sign of a confused client or a smuggling attempt, not of a body that
//     happens to have something after it.
func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	if !hasJSONContentType(r) {
		WriteError(w, r, &Error{
			Code:   CodeInvalidField,
			Detail: "the request body must be application/json",
		})
		return false
	}

	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxJSONBodyBytes))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(target); err != nil {
		WriteError(w, r, jsonError(err))
		return false
	}

	// io.EOF here means the body held exactly one value, which is what is
	// wanted. Anything else is a second value or trailing junk.
	if err := decoder.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		WriteError(w, r, &Error{
			Code:   CodeMalformedJSON,
			Detail: "the request body must contain exactly one JSON object",
		})
		return false
	}
	return true
}

// jsonError turns a decoder failure into the documented envelope.
//
// The distinctions are the ones a client can act on: which field was wrong,
// whether the body was too big, or whether it was not JSON at all.
func jsonError(err error) *Error {
	var (
		syntax        *json.SyntaxError
		unmarshalType *json.UnmarshalTypeError
		tooLarge      *http.MaxBytesError
	)

	switch {
	case errors.Is(err, io.EOF):
		return &Error{Code: CodeMalformedJSON, Detail: "the request body is empty"}

	case errors.As(err, &tooLarge):
		return &Error{
			Code:   CodeInvalidField,
			Detail: fmt.Sprintf("the request body may be at most %d bytes", MaxJSONBodyBytes),
		}

	case errors.As(err, &syntax):
		return &Error{
			Code:   CodeMalformedJSON,
			Detail: fmt.Sprintf("invalid JSON at byte %d", syntax.Offset),
		}

	case errors.As(err, &unmarshalType):
		return &Error{
			Code:   CodeInvalidField,
			Field:  unmarshalType.Field,
			Detail: fmt.Sprintf("expected a %s", unmarshalType.Type),
		}

	case strings.HasPrefix(err.Error(), "json: unknown field "):
		// The standard library offers no typed error for this one. The field
		// name is quoted in the message and is echoed back so the client can
		// see which one it got wrong.
		field := strings.Trim(strings.TrimPrefix(err.Error(), "json: unknown field "), `"`)
		return &Error{
			Code:   CodeInvalidField,
			Field:  field,
			Detail: "unknown field",
		}

	default:
		return &Error{Code: CodeMalformedJSON, Cause: err}
	}
}

// hasJSONContentType reports whether the request declares a JSON body.
//
// A missing Content-Type is accepted: curl sends none by default, and this is
// an intranet tool whose users will reach for curl. A declared type that is not
// JSON is refused, because it means the client believes it is sending something
// else.
func hasJSONContentType(r *http.Request) bool {
	declared := r.Header.Get("Content-Type")
	if declared == "" {
		return true
	}
	// Compare only the media type: "application/json; charset=utf-8" is JSON.
	if i := strings.IndexByte(declared, ';'); i >= 0 {
		declared = declared[:i]
	}
	return strings.EqualFold(strings.TrimSpace(declared), "application/json")
}

// decodeJSONWithRaw decodes a body into target and also records which top-level
// keys were present.
//
// encoding/json collapses "absent" and "null" to the same nil pointer, and for
// a nullable column the difference is "leave it alone" versus "clear it". A
// second pass over the same bytes into a map is the cheapest way to tell them
// apart; the alternative is a custom type per nullable field, which is a lot of
// machinery for three columns.
func decodeJSONWithRaw(w http.ResponseWriter, r *http.Request, target any, keys *rawPatch) bool {
	if !hasJSONContentType(r) {
		WriteError(w, r, &Error{
			Code:   CodeInvalidField,
			Detail: "the request body must be application/json",
		})
		return false
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxJSONBodyBytes))
	if err != nil {
		WriteError(w, r, jsonError(err))
		return false
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		WriteError(w, r, jsonError(err))
		return false
	}
	if err := decoder.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		WriteError(w, r, &Error{
			Code:   CodeMalformedJSON,
			Detail: "the request body must contain exactly one JSON object",
		})
		return false
	}

	// The strict pass above has already accepted the body, so this one cannot
	// fail on anything the caller can influence.
	present := make(rawPatch)
	if err := json.Unmarshal(body, &present); err != nil {
		WriteError(w, r, &Error{Code: CodeMalformedJSON, Cause: err})
		return false
	}
	*keys = present
	return true
}
