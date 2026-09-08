package db_test

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/joernott/doenerstag/internal/db"
)

// currentDatabase reports the database a pool is connected to.
func currentDatabase(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()

	var name string
	if err := pool.QueryRow(context.Background(), "SELECT current_database()").Scan(&name); err != nil {
		t.Fatalf("reading current_database(): %v", err)
	}
	return name
}

// tableNames lists the tables in the public schema.
func tableNames(t *testing.T, pool *pgxpool.Pool) []string {
	t.Helper()

	rows, err := pool.Query(context.Background(), `
		SELECT tablename
		FROM pg_tables
		WHERE schemaname = 'public'
		ORDER BY tablename`)
	if err != nil {
		t.Fatalf("listing tables: %v", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scanning table name: %v", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("listing tables: %v", err)
	}
	return names
}

// optionsFromDSN turns a connection string back into db.Options, so that
// db.Connect can be exercised against whatever server the harness provided.
func optionsFromDSN(dsn string) (db.Options, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return db.Options{}, err
	}

	port := 5432
	if p := u.Port(); p != "" {
		port, err = strconv.Atoi(p)
		if err != nil {
			return db.Options{}, err
		}
	}

	password, _ := u.User.Password()

	sslMode := u.Query().Get("sslmode")
	if sslMode == "" {
		sslMode = "prefer"
	}

	return db.Options{
		Host:           u.Hostname(),
		Port:           port,
		Database:       strings.TrimPrefix(u.Path, "/"),
		User:           u.User.Username(),
		Password:       password,
		SSLMode:        sslMode,
		MaxConnections: 4,
	}, nil
}
