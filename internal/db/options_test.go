package db

import (
	"net/url"
	"strings"
	"testing"
)

// urlParse is net/url.Parse, aliased so the helper below reads clearly.
var urlParse = url.Parse

func validOptions() Options {
	return Options{
		Host:           "localhost",
		Port:           5432,
		Database:       "doenerstag",
		User:           "doener",
		Password:       "hunter2",
		SSLMode:        "prefer",
		MaxConnections: 10,
	}
}

func TestDSNCarriesEverythingNeededToConnect(t *testing.T) {
	dsn := validOptions().DSN()

	for _, want := range []string{
		"postgres://",
		"doener:hunter2@",
		"localhost:5432",
		"/doenerstag",
		"sslmode=prefer",
		"application_name=" + ApplicationName,
	} {
		if !strings.Contains(dsn, want) {
			t.Errorf("DSN %q is missing %q", dsn, want)
		}
	}
}

// The password must survive the round trip through the URL encoding, or a
// perfectly good password becomes an authentication failure nobody can explain.
func TestDSNEscapesAwkwardPasswords(t *testing.T) {
	awkward := []string{
		"p@ssw0rd",
		"with spaces",
		"sla/sh",
		"colon:colon",
		"hash#mark",
		"question?mark",
		"percent%sign",
		"Ünïcøde",
	}

	for _, password := range awkward {
		opts := validOptions()
		opts.Password = password

		dsn := opts.DSN()
		parsed, err := parseDSN(dsn)
		if err != nil {
			t.Errorf("password %q produced an unparseable DSN %q: %v", password, dsn, err)
			continue
		}
		if parsed != password {
			t.Errorf("password %q survived as %q", password, parsed)
		}
	}
}

// parseDSN extracts the password back out of a connection URL.
func parseDSN(dsn string) (string, error) {
	u, err := urlParse(dsn)
	if err != nil {
		return "", err
	}
	password, _ := u.User.Password()
	return password, nil
}

// An empty password must produce no password at all, not an empty one. The two
// are different to libpq: "user:@host" says "authenticate with the empty
// string", which fails differently from "no password supplied".
func TestDSNOmitsAnEmptyPassword(t *testing.T) {
	opts := validOptions()
	opts.Password = ""

	dsn := opts.DSN()
	if strings.Contains(dsn, "doener:@") {
		t.Errorf("an empty password was rendered as a colon: %s", dsn)
	}
	if !strings.Contains(dsn, "doener@") {
		t.Errorf("the user is missing from %s", dsn)
	}
}

// Anything that reaches a log line or an error message goes through
// RedactedDSN. If the password leaked there, the redaction in the logging
// package would never see it.
func TestRedactedDSNHidesThePassword(t *testing.T) {
	opts := validOptions()
	opts.Password = "correct-horse-battery-staple"

	redacted := opts.RedactedDSN()
	if strings.Contains(redacted, opts.Password) {
		t.Errorf("RedactedDSN leaked the password: %s", redacted)
	}
	if !strings.Contains(redacted, passwordMarker) {
		t.Errorf("RedactedDSN does not mark the redaction: %s", redacted)
	}
	// Still useful for diagnosis.
	for _, want := range []string{"localhost:5432", "/doenerstag", "doener"} {
		if !strings.Contains(redacted, want) {
			t.Errorf("RedactedDSN %q lost %q, which is what makes it useful", redacted, want)
		}
	}
}

func TestRedactedDSNWithNoPassword(t *testing.T) {
	opts := validOptions()
	opts.Password = ""

	if strings.Contains(opts.RedactedDSN(), passwordMarker) {
		t.Error("RedactedDSN invented a password that was never set")
	}
}

func TestValidate(t *testing.T) {
	cases := map[string]func(*Options){
		"empty host":        func(o *Options) { o.Host = "" },
		"zero port":         func(o *Options) { o.Port = 0 },
		"negative port":     func(o *Options) { o.Port = -1 },
		"port out of range": func(o *Options) { o.Port = 70000 },
		"empty database":    func(o *Options) { o.Database = "" },
		"empty user":        func(o *Options) { o.User = "" },
		"zero pool":         func(o *Options) { o.MaxConnections = 0 },
		"negative pool":     func(o *Options) { o.MaxConnections = -5 },
		"unknown sslmode":   func(o *Options) { o.SSLMode = "sometimes" },
		"empty sslmode":     func(o *Options) { o.SSLMode = "" },
	}

	for name, break_ := range cases {
		opts := validOptions()
		break_(&opts)
		if err := opts.Validate(); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}

	if err := validOptions().Validate(); err != nil {
		t.Errorf("valid options were rejected: %v", err)
	}
}

func TestEverySSLModeIsAccepted(t *testing.T) {
	for _, mode := range SSLModes {
		opts := validOptions()
		opts.SSLMode = mode
		if err := opts.Validate(); err != nil {
			t.Errorf("documented sslmode %q was rejected: %v", mode, err)
		}
	}

	// The list must match what docs/09_configuration.md documents.
	want := []string{"disable", "allow", "prefer", "require", "verify-ca", "verify-full"}
	if len(SSLModes) != len(want) {
		t.Fatalf("SSLModes is %v, want %v", SSLModes, want)
	}
	for i := range want {
		if SSLModes[i] != want[i] {
			t.Errorf("SSLModes is %v, want %v", SSLModes, want)
			break
		}
	}
}

func TestValidateErrorNamesTheProblem(t *testing.T) {
	opts := validOptions()
	opts.SSLMode = "sometimes"

	err := opts.Validate()
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "sometimes") {
		t.Errorf("error %q does not quote the offending value", err)
	}
	if !strings.Contains(err.Error(), "verify-full") {
		t.Errorf("error %q does not list the valid values", err)
	}
}
