package db

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog"

	"github.com/joernott/doenerstag/internal/logging"
)

// queryTracer logs every statement at DEBUG with its duration.
//
// docs/08_technologies.md puts database queries at DEBUG alongside function
// entry. It is deliberately verbose and is a diagnostic level, not one to run
// in production.
type queryTracer struct {
	logger *zerolog.Logger
}

func newQueryTracer(logger *zerolog.Logger) pgx.QueryTracer {
	return &queryTracer{logger: logger}
}

// traceKey types the context key, so it cannot collide with another package's.
type traceKey struct{}

type traceState struct {
	started time.Time
	sql     string
}

func (t *queryTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn,
	data pgx.TraceQueryStartData) context.Context {
	if !t.logger.Debug().Enabled() {
		return ctx
	}
	// The SQL text is logged; the arguments are not. Every statement in this
	// application is parameterised, so the text is a fixed string, while the
	// arguments carry password hashes, session tokens and personal data.
	return context.WithValue(ctx, traceKey{}, &traceState{
		started: time.Now(),
		sql:     data.SQL,
	})
}

func (t *queryTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn,
	data pgx.TraceQueryEndData) {
	event := t.logger.Debug()
	if !event.Enabled() {
		return
	}

	state, ok := ctx.Value(traceKey{}).(*traceState)
	if !ok {
		return
	}

	event = event.
		Str(logging.FieldComponent, Component).
		Str("sql", state.sql).
		Dur("duration_ms", time.Since(state.started)).
		Int64("rows", data.CommandTag.RowsAffected())

	if data.Err != nil {
		// A failed query is worth an ERROR line rather than hiding at DEBUG.
		t.logger.Error().
			Str(logging.FieldComponent, Component).
			Str("sql", state.sql).
			Dur("duration_ms", time.Since(state.started)).
			Err(data.Err).
			Msg("query failed")
		return
	}

	event.Msg("query")
}
