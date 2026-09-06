// Package db owns the PostgreSQL connection pool and the schema migrations.
//
// Only PostgreSQL 18 is supported. See docs/08_technologies.md.
package db

import (
	"fmt"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// ApplicationName identifies the application in pg_stat_activity, so that a
// connection can be attributed when someone is looking at the server.
const ApplicationName = "doenerstag"

// SSLModes are the values --database-sslmode accepts, in increasing order of
// strictness.
var SSLModes = []string{
	"disable",
	"allow",
	"prefer",
	"require",
	"verify-ca",
	"verify-full",
}

// Options describes how to reach the database.
//
// This mirrors config.DatabaseConfig but is declared separately so that this
// package does not depend on the configuration package. The command layer maps
// one to the other.
type Options struct {
	Host     string
	Port     int
	Database string
	User     string
	Password string `log:"-"`
	SSLMode  string

	// MaxConnections caps the pool. Idle connections are inherently bounded by
	// it as well, since a connection cannot be idle without being open.
	MaxConnections int
}

// Validate reports the first problem that would stop a connection from being
// attempted at all.
func (o Options) Validate() error {
	switch {
	case o.Host == "":
		return fmt.Errorf("database host is empty")
	case o.Port <= 0 || o.Port > 65535:
		return fmt.Errorf("database port %d is out of range", o.Port)
	case o.Database == "":
		return fmt.Errorf("database name is empty")
	case o.User == "":
		return fmt.Errorf("database user is empty")
	case o.MaxConnections < 1:
		return fmt.Errorf("max connection pool must be at least 1, got %d", o.MaxConnections)
	case !slices.Contains(SSLModes, o.SSLMode):
		return fmt.Errorf("unknown sslmode %q, expected one of %v", o.SSLMode, SSLModes)
	}
	return nil
}

// DSN builds the connection string. It contains the password, so it must never
// be logged; use RedactedDSN for that.
func (o Options) DSN() string {
	return o.dsn(o.Password)
}

// passwordMarker replaces the password in RedactedDSN.
//
// Deliberately not the logging package's "***": a URL escapes those asterisks
// to %2A%2A%2A inside the userinfo, which is unreadable in exactly the log line
// the redaction exists to make safe. Unreserved characters pass through
// untouched.
const passwordMarker = "REDACTED"

// RedactedDSN is DSN with the password replaced. It is what goes in a log line
// or an error message.
func (o Options) RedactedDSN() string {
	if o.Password == "" {
		return o.dsn("")
	}
	return o.dsn(passwordMarker)
}

func (o Options) dsn(password string) string {
	u := url.URL{
		Scheme: "postgres",
		Host:   net.JoinHostPort(o.Host, strconv.Itoa(o.Port)),
		Path:   "/" + o.Database,
	}

	// url.UserPassword with an empty password renders "user:@host", which is
	// not the same thing as having no password at all.
	if password == "" {
		u.User = url.User(o.User)
	} else {
		u.User = url.UserPassword(o.User, password)
	}

	query := url.Values{}
	query.Set("sslmode", o.SSLMode)
	query.Set("application_name", ApplicationName)
	u.RawQuery = query.Encode()

	return u.String()
}

// OptionsFromDSN parses a PostgreSQL connection URL back into Options.
//
// The installer needs to reach the same server as several different users and
// databases in turn — root to create things, admin to migrate them, runtime to
// verify the result — and this is what lets one connection string be varied
// rather than six settings being threaded around.
func OptionsFromDSN(dsn string) (Options, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return Options{}, fmt.Errorf("parsing the connection string: %w", err)
	}

	port := 5432
	if raw := u.Port(); raw != "" {
		port, err = strconv.Atoi(raw)
		if err != nil {
			return Options{}, fmt.Errorf("connection string has a non-numeric port %q", raw)
		}
	}

	password, _ := u.User.Password()

	sslMode := u.Query().Get("sslmode")
	if sslMode == "" {
		sslMode = "prefer"
	}

	return Options{
		Host:           u.Hostname(),
		Port:           port,
		Database:       strings.TrimPrefix(u.Path, "/"),
		User:           u.User.Username(),
		Password:       password,
		SSLMode:        sslMode,
		MaxConnections: 4,
	}, nil
}

// WithDatabase returns a copy pointing at a different database on the same
// server, keeping the credentials.
func (o Options) WithDatabase(name string) Options {
	o.Database = name
	return o
}

// WithUser returns a copy authenticating as a different user.
func (o Options) WithUser(user, password string) Options {
	o.User = user
	o.Password = password
	return o
}
