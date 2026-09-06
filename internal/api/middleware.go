package api

import (
	"context"
	"net/http"
	"runtime"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/joernott/doenerstag/internal/logging"
)

// RequestIDHeader carries the correlation ID in and out.
//
// An inbound value is honoured so that a request crossing a reverse proxy keeps
// one identity end to end.
const RequestIDHeader = "X-Request-Id"

// contextKey types this package's context keys so they cannot collide with
// another package's.
type contextKey int

const (
	requestIDKey contextKey = iota
	userNameKey
)

// RequestIDFrom returns the correlation ID for a request, or "-" if there is
// none, which only happens outside the middleware chain.
func RequestIDFrom(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return "-"
}

// actingUser is a box for the acting user's name.
//
// A box rather than the string itself, because of where the two ends sit. The
// name is learned by the authentication middleware, deep inside the chain, and
// it is read by the request log line, which is written by LogRequests after
// next.ServeHTTP has returned -- from the request value it still holds, whose
// context is the one from *before* authentication ran. A context value written
// on the way in is therefore invisible on the way out, and the user field would
// read "-" on every authenticated request.
//
// LogRequests installs the box; authentication fills it in; the log line reads
// it. One request is one goroutine, so the write always happens-before the
// read.
type actingUser struct{ name string }

// UserNameFrom returns the acting user, or the anonymous marker.
func UserNameFrom(ctx context.Context) string {
	if box, ok := ctx.Value(userNameKey).(*actingUser); ok && box.name != "" {
		return box.name
	}
	return logging.AnonymousUser
}

// WithActingUser installs the box. LogRequests calls it; nothing else should.
func WithActingUser(ctx context.Context) context.Context {
	return context.WithValue(ctx, userNameKey, &actingUser{})
}

// SetUserName records who is making the request, for the log line.
//
// It writes into the box installed by LogRequests, so it takes a context rather
// than returning one. A request that never reached LogRequests -- a handler
// driven directly by a test -- silently records nothing, which is the right
// outcome: the log line it would have fed does not exist either.
func SetUserName(ctx context.Context, name string) {
	if box, ok := ctx.Value(userNameKey).(*actingUser); ok {
		box.name = name
	}
}

// Middleware wraps a handler.
type Middleware func(http.Handler) http.Handler

// Chain applies middleware so that the first listed is the outermost.
//
// Order matters: the request ID must exist before anything logs, and the
// recovery handler must be outside the logging one so that a panic still
// produces a completion line.
func Chain(h http.Handler, middleware ...Middleware) http.Handler {
	for i := len(middleware) - 1; i >= 0; i-- {
		h = middleware[i](h)
	}
	return h
}

// RequestID assigns each request a correlation ID and echoes it.
//
// It is the outermost middleware, because every log line inside the request
// carries this value and an error body quotes it.
func RequestID() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get(RequestIDHeader)
			if !plausibleRequestID(id) {
				id = newRequestID()
			}

			w.Header().Set(RequestIDHeader, id)
			ctx := context.WithValue(r.Context(), requestIDKey, id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// plausibleRequestID rejects an inbound value that is empty, unreasonably long
// or contains anything that would be awkward in a log line.
//
// A correlation ID is echoed into a header and into structured logs, so it is
// attacker-influenced data and gets the same scrutiny as any other input.
func plausibleRequestID(id string) bool {
	if id == "" || len(id) > 200 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.', r == ':':
		default:
			return false
		}
	}
	return true
}

func newRequestID() string {
	id, err := uuid.NewV7()
	if err != nil {
		// A request without an ID is worse than one with a fixed marker, and
		// this only fails if the system entropy source has.
		return "unavailable"
	}
	return id.String()
}

// statusRecorder remembers what a handler wrote, so the completion line can
// report it.
type statusRecorder struct {
	http.ResponseWriter
	status  int
	written bool
}

func (s *statusRecorder) WriteHeader(status int) {
	if !s.written {
		s.status = status
		s.written = true
	}
	s.ResponseWriter.WriteHeader(status)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if !s.written {
		s.status = http.StatusOK
		s.written = true
	}
	return s.ResponseWriter.Write(b)
}

// Flush passes the flush through to the real writer.
//
// Embedding an http.ResponseWriter promotes only that interface's three
// methods, so a wrapper silently hides everything else the real writer can do
// -- Flush among them. A handler that type-asserts for http.Flusher gets a
// failed assertion, and the SSE endpoint is the first thing in this
// application that needs one. Nothing else noticed, because nothing else
// streams.
func (s *statusRecorder) Flush() {
	if flusher, ok := s.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Unwrap lets http.ResponseController reach the underlying writer.
//
// That is how SetWriteDeadline gets through this wrapper, which the event
// stream needs to clear the deadline --http-write-timeout would otherwise
// impose on a connection meant to stay open for hours.
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// LogRequests emits one INFO line per request.
//
// docs/08_technologies.md puts every API request at INFO, and that log is the
// audit trail: it carries the acting user, the correlation ID and what was
// asked for.
func LogRequests(logger *zerolog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			started := time.Now()
			recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

			// The box the authentication middleware fills in. It must be
			// installed here, outside the call, because the log line below
			// reads r -- whose context is the one from before anything inside
			// had a chance to replace it.
			r = r.WithContext(WithActingUser(r.Context()))

			next.ServeHTTP(recorder, r)

			logger.Info().
				Str(logging.FieldRequestID, RequestIDFrom(r.Context())).
				Str(logging.FieldUser, UserNameFrom(r.Context())).
				Str(logging.FieldMethod, r.Method).
				// The path only: a query string can carry filter values and has
				// no place in an audit line.
				Str(logging.FieldPath, r.URL.Path).
				Int(logging.FieldStatus, recorder.status).
				Dur(logging.FieldDurationMs, time.Since(started)).
				Msg("request")
		})
	}
}

// Recover turns a panic into a 500 rather than a dropped connection.
//
// It sits outside the logging middleware so that a panicking request still
// produces its completion line, and logs the panic at ERROR with the
// correlation ID so the two can be tied together.
func Recover(logger *zerolog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				recovered := recover()
				if recovered == nil {
					return
				}

				logger.Error().
					Str(logging.FieldRequestID, RequestIDFrom(r.Context())).
					Str(logging.FieldMethod, r.Method).
					Str(logging.FieldPath, r.URL.Path).
					Interface("panic", recovered).
					Bytes("stack", stack()).
					Msg("handler panicked")

				// The client is told nothing but the request ID: the detail is
				// in the log, where it belongs.
				WriteError(w, r, &Error{Code: CodeInternal})
			}()

			next.ServeHTTP(w, r)
		})
	}
}

// SecurityHeaders sets the headers from docs/05_auth_and_permissions.md on
// every response.
//
// They are applied to everything rather than only to the API, because the CSP
// is what protects the one place the frontend renders stored HTML.
func SecurityHeaders(secure bool) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("Content-Security-Policy", ContentSecurityPolicy)
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Referrer-Policy", "same-origin")
			h.Set("Cross-Origin-Opener-Policy", "same-origin")
			h.Set("Permissions-Policy", PermissionsPolicy)

			// HSTS only means anything over TLS, and asserting it on a plain
			// HTTP deployment would be a promise the server cannot keep.
			if secure {
				h.Set("Strict-Transport-Security", "max-age=31536000")
			}

			next.ServeHTTP(w, r)
		})
	}
}

// ContentSecurityPolicy forbids inline scripts and styles, which is possible
// because Tailwind is compiled to a static stylesheet and TypeScript to static
// bundles at build time. Nothing is generated at runtime.
const ContentSecurityPolicy = "default-src 'self'; " +
	"img-src 'self' data:; " +
	"style-src 'self'; " +
	"script-src 'self'; " +
	"font-src 'self'; " +
	"connect-src 'self'; " +
	"frame-ancestors 'none'; " +
	"base-uri 'none'; " +
	"form-action 'self'"

// PermissionsPolicy switches off capabilities a food ordering application has
// no use for.
const PermissionsPolicy = "geolocation=(), camera=(), microphone=(), payment=()"

// stack captures the goroutine's stack for a panic log line, bounded so that a
// deep recursion cannot produce a megabyte of log.
func stack() []byte {
	const maxStack = 8 * 1024
	buf := make([]byte, maxStack)
	return buf[:runtime.Stack(buf, false)]
}
