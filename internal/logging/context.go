package logging

import "context"

// requestIDKey types the context key, so it cannot collide with another
// package's.
type requestIDKey struct{}

// WithRequestID returns ctx carrying a request's correlation ID.
//
// The key lives here rather than in the api package that assigns the ID,
// because every line written while handling a request carries it
// (docs/08_technologies.md) -- the database's query lines included, and the db
// package cannot import api.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

// RequestIDFrom returns the correlation ID ctx carries, or "-" outside a
// request.
func RequestIDFrom(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey{}).(string); ok {
		return id
	}
	return "-"
}
