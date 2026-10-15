package db

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog"

	"github.com/joernott/doenerstag/internal/logging"
)

// 20.21: every line written for a request carries its request_id, the database's
// query lines included (docs/08_technologies.md), so one request can be
// followed into the database with a single grep.
func TestAQueryLineCarriesTheRequestID(t *testing.T) {
	var out bytes.Buffer
	logger := zerolog.New(&out).Level(zerolog.DebugLevel)
	tracer := newQueryTracer(&logger)

	ctx := logging.WithRequestID(context.Background(), "req-7")
	ctx = tracer.TraceQueryStart(ctx, nil, pgx.TraceQueryStartData{SQL: "SELECT 1"})
	tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})

	if line := out.String(); !strings.Contains(line, `"request_id":"req-7"`) ||
		!strings.Contains(line, `"sql":"SELECT 1"`) {
		t.Errorf("the query line is %q", line)
	}
}
