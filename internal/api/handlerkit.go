package api

import (
	"net/http"
	"time"

	"github.com/rs/zerolog"

	"github.com/joernott/doenerstag/internal/logging"
)

// nowFrom is the current time, or a test's fixed one. Handlers carry an
// optional Now so a test can stand at a deadline; each one's now() is this.
func nowFrom(now func() time.Time) time.Time {
	if now != nil {
		return now()
	}
	return time.Now()
}

// requestLogger is base with the request's correlation ID, so every line a
// handler writes can be found with one grep. A handler with no logger, as in
// most tests, gets one that discards; outside a request, base as it is.
func requestLogger(base *zerolog.Logger, r *http.Request) *zerolog.Logger {
	if base == nil {
		discard := zerolog.Nop()
		return &discard
	}
	if r == nil {
		return base
	}
	logger := base.With().
		Str(logging.FieldRequestID, RequestIDFrom(r.Context())).
		Logger()
	return &logger
}
