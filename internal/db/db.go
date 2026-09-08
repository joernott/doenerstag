package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

// Component is the value of the component field in this package's log lines.
const Component = "db"

// connectTimeout bounds the initial connection attempt, so that a wrong host
// fails in seconds rather than hanging until the operator gives up.
const connectTimeout = 10 * time.Second

// Connect opens the pool and verifies it with a round trip.
//
// The logger may be nil, in which case nothing is traced.
func Connect(ctx context.Context, opts Options, logger *zerolog.Logger) (*pgxpool.Pool, error) {
	if err := opts.Validate(); err != nil {
		return nil, err
	}

	poolConfig, err := pgxpool.ParseConfig(opts.DSN())
	if err != nil {
		// The parse error can quote the DSN, so it is not passed through.
		return nil, fmt.Errorf("invalid database connection settings for %s", opts.RedactedDSN())
	}

	poolConfig.MaxConns = int32(opts.MaxConnections) //nolint:gosec // validated above

	// Every connection runs in UTC. docs/03_data_model.md stores every instant
	// as timestamptz and converts to local time in the browser; a session in
	// any other zone would make timestamps read differently depending on where
	// the server happens to be installed.
	poolConfig.ConnConfig.RuntimeParams["timezone"] = "UTC"

	if logger != nil {
		poolConfig.ConnConfig.Tracer = newQueryTracer(logger)
	}

	connectCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(connectCtx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("connecting to %s: %w", opts.RedactedDSN(), err)
	}

	if err := pool.Ping(connectCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connecting to %s: %w", opts.RedactedDSN(), err)
	}

	if logger != nil {
		logger.Info().
			Str("server", opts.Host).
			Int("port", opts.Port).
			Str("database", opts.Database).
			Str("user", opts.User).
			Str("sslmode", opts.SSLMode).
			Int32("max_connections", poolConfig.MaxConns).
			Msg("database connected")
	}

	return pool, nil
}

// VerifyServerVersion reports an error unless the server is PostgreSQL 18.
//
// docs/08_technologies.md supports exactly one major version. Finding out at
// startup is far better than finding out when a version-specific statement
// fails somewhere in the middle of a migration.
func VerifyServerVersion(ctx context.Context, pool *pgxpool.Pool) error {
	const wantMajor = 18

	// current_setting rather than SHOW: SHOW returns text, so the cast has to
	// happen somewhere and doing it in SQL keeps the scan honest.
	var version int
	err := pool.QueryRow(ctx,
		"SELECT current_setting('server_version_num')::int").Scan(&version)
	if err != nil {
		return fmt.Errorf("reading server version: %w", err)
	}

	major := version / 10000
	if major != wantMajor {
		return fmt.Errorf(
			"PostgreSQL %d is not supported: doenerstag requires PostgreSQL %d",
			major, wantMajor)
	}
	return nil
}

// SessionTimeZone returns the time zone the pool's connections run in. It
// exists so that a test can assert the UTC guarantee rather than trust it.
func SessionTimeZone(ctx context.Context, pool *pgxpool.Pool) (string, error) {
	var zone string
	if err := pool.QueryRow(ctx, "SHOW TimeZone").Scan(&zone); err != nil {
		return "", fmt.Errorf("reading session time zone: %w", err)
	}
	return zone, nil
}

// Stats reports pool usage for the metrics endpoint in task 4.10.
type Stats struct {
	Open int32
	Idle int32
	Max  int32
}

// PoolStats snapshots the pool.
func PoolStats(pool *pgxpool.Pool) Stats {
	s := pool.Stat()
	return Stats{
		Open: s.AcquiredConns() + s.IdleConns(),
		Idle: s.IdleConns(),
		Max:  s.MaxConns(),
	}
}
