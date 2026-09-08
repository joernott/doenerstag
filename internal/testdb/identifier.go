package testdb

import (
	"net/url"
	"strings"
	"sync/atomic"
)

// quoteIdentifier renders a SQL identifier safely.
//
// CREATE DATABASE and DROP DATABASE cannot take the database name as a bind
// parameter, so it is interpolated. Every name this package builds is
// constructed from a Go test name and a counter, but quoting is done anyway
// rather than relying on that remaining true.
func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// replaceDatabase points a connection string at a different database on the
// same server, preserving credentials and parameters.
func replaceDatabase(dsn, database string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return dsn
	}
	u.Path = "/" + database
	return u.String()
}

// atomic64 hands out unique suffixes for database names.
type atomic64 struct {
	n atomic.Int64
}

func (a *atomic64) next() int64 { return a.n.Add(1) }
