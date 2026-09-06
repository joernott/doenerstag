package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

// chainFor wraps a handler in the production middleware and captures the log.
func chainFor(t *testing.T, secure bool, h http.Handler) (http.Handler, *bytes.Buffer) {
	t.Helper()

	logs := &bytes.Buffer{}
	logger := zerolog.New(logs)

	return Chain(h,
		RequestID(),
		LogRequests(&logger),
		Recover(&logger),
		SecurityHeaders(secure),
	), logs
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
}

// Every response carries the headers from docs/05_auth_and_permissions.md.
func TestSecurityHeadersOnEveryResponse(t *testing.T) {
	handler, _ := chainFor(t, true, okHandler())

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/anything", http.NoBody))

	want := map[string]string{
		"Content-Security-Policy":    ContentSecurityPolicy,
		"X-Content-Type-Options":     "nosniff",
		"Referrer-Policy":            "same-origin",
		"Cross-Origin-Opener-Policy": "same-origin",
		"Permissions-Policy":         PermissionsPolicy,
		"Strict-Transport-Security":  "max-age=31536000",
	}
	for header, value := range want {
		if got := rec.Header().Get(header); got != value {
			t.Errorf("%s is %q, want %q", header, got, value)
		}
	}
}

// HSTS over plain HTTP would be a promise the server cannot keep, and would
// pin a browser to a scheme this deployment does not serve.
func TestHSTSIsOnlySentOverTLS(t *testing.T) {
	handler, _ := chainFor(t, false, okHandler())

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/anything", http.NoBody))

	if got := rec.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("HSTS was sent over plain HTTP: %q", got)
	}
	// The rest still apply.
	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Error("the CSP is missing from a plain HTTP response")
	}
}

// The CSP is what protects the one place the frontend renders stored HTML, so
// its two most important clauses are asserted rather than assumed.
func TestCSPForbidsInlineScriptAndFraming(t *testing.T) {
	if !strings.Contains(ContentSecurityPolicy, "script-src 'self'") {
		t.Error("the CSP does not restrict scripts to same-origin files")
	}
	if strings.Contains(ContentSecurityPolicy, "unsafe-inline") {
		t.Error("the CSP allows inline scripts or styles")
	}
	if !strings.Contains(ContentSecurityPolicy, "frame-ancestors 'none'") {
		t.Error("the CSP allows framing")
	}
}

// The correlation ID ties a response a user can see to the log line that has
// the detail. It must appear in the header, the log and any error body.
func TestRequestIDAppearsEverywhere(t *testing.T) {
	var seen string
	handler, logs := chainFor(t, true, http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			seen = RequestIDFrom(r.Context())
			WriteError(w, r, &Error{Code: CodeNotFound})
		}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/missing", http.NoBody))

	header := rec.Header().Get(RequestIDHeader)
	if header == "" || header == "-" {
		t.Fatalf("no request ID in the response header: %q", header)
	}
	if seen != header {
		t.Errorf("the handler saw %q but the header says %q", seen, header)
	}

	var body envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("error body is not the envelope: %v", err)
	}
	if body.Error.RequestID != header {
		t.Errorf("the error body says %q, want %q", body.Error.RequestID, header)
	}
	if !strings.Contains(logs.String(), header) {
		t.Errorf("the log line does not carry the request ID:\n%s", logs)
	}
}

// An inbound ID is honoured so a request crossing a proxy keeps one identity.
func TestInboundRequestIDIsHonoured(t *testing.T) {
	handler, _ := chainFor(t, true, okHandler())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", http.NoBody)
	req.Header.Set(RequestIDHeader, "upstream-abc-123")
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get(RequestIDHeader); got != "upstream-abc-123" {
		t.Errorf("request ID is %q, want the inbound value", got)
	}
}

// The ID is echoed into a header and into structured logs, so it is
// attacker-influenced data and a hostile value must be replaced, not passed on.
func TestImplausibleInboundRequestIDIsReplaced(t *testing.T) {
	hostile := map[string]string{
		"newline":    "abc\ndef",
		"quote":      `abc"def`,
		"brace":      "abc{def}",
		"space":      "abc def",
		"too long":   strings.Repeat("a", 300),
		"empty":      "",
		"json break": `","level":"fatal","message":"forged`,
	}

	for name, value := range hostile {
		handler, logs := chainFor(t, true, okHandler())

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/x", http.NoBody)
		req.Header.Set(RequestIDHeader, value)
		handler.ServeHTTP(rec, req)

		got := rec.Header().Get(RequestIDHeader)
		if got == value && value != "" {
			t.Errorf("%s: the hostile value was echoed back: %q", name, got)
		}
		if got == "" {
			t.Errorf("%s: no request ID was assigned", name)
		}

		// The log must still be one parseable line per request.
		for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
			if line == "" {
				continue
			}
			var decoded map[string]any
			if err := json.Unmarshal([]byte(line), &decoded); err != nil {
				t.Errorf("%s: a hostile ID broke the log format: %v (%s)", name, err, line)
			}
		}
	}
}

// Every request produces one INFO line: that log is the audit trail.
func TestEveryRequestIsLoggedOnce(t *testing.T) {
	handler, logs := chainFor(t, true, okHandler())

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/orders", http.NoBody))

	lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected one log line, got %d:\n%s", len(lines), logs)
	}

	var decoded map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &decoded); err != nil {
		t.Fatalf("the log line is not JSON: %v", err)
	}
	for field, want := range map[string]any{
		"method": "POST",
		"path":   "/api/v1/orders",
		"status": float64(http.StatusOK),
		"user":   "-",
	} {
		if decoded[field] != want {
			t.Errorf("%s is %v, want %v", field, decoded[field], want)
		}
	}
	if _, present := decoded["duration_ms"]; !present {
		t.Error("the log line carries no duration")
	}
}

// A query string can carry filter values and has no place in an audit line.
func TestTheQueryStringIsNotLogged(t *testing.T) {
	handler, logs := chainFor(t, true, okHandler())

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec,
		httptest.NewRequest(http.MethodGet, "/api/v1/menu-items?tag=vegan&secret=leak", http.NoBody))

	if strings.Contains(logs.String(), "secret=leak") {
		t.Errorf("the query string reached the log:\n%s", logs)
	}
	if !strings.Contains(logs.String(), "/api/v1/menu-items") {
		t.Errorf("the path is missing from the log:\n%s", logs)
	}
}

// A panic must become a JSON 500 with a correlation ID, never a dropped
// connection, and never a stack trace in the response.
func TestPanicBecomesAJSONInternalError(t *testing.T) {
	handler, logs := chainFor(t, true, http.HandlerFunc(
		func(http.ResponseWriter, *http.Request) {
			panic("something went badly wrong in a way that mentions /etc/secret")
		}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/boom", http.NoBody))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500", rec.Code)
	}

	var body envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("the panic response is not the envelope: %v (%s)", err, rec.Body)
	}
	if body.Error.Code != CodeInternal {
		t.Errorf("code is %d, want %d", body.Error.Code, CodeInternal)
	}
	if body.Error.RequestID == "" || body.Error.RequestID == "-" {
		t.Error("the panic response carries no request ID to trace it by")
	}

	// The detail belongs in the log, not the response.
	if strings.Contains(rec.Body.String(), "/etc/secret") {
		t.Errorf("the panic message leaked into the response: %s", rec.Body)
	}
	if !strings.Contains(logs.String(), "/etc/secret") {
		t.Errorf("the panic message is missing from the log:\n%s", logs)
	}
	if !strings.Contains(logs.String(), "stack") {
		t.Errorf("the panic was logged without a stack:\n%s", logs)
	}
}

// The completion line must still be written when the handler panicked, or a
// crash would be invisible in the request log.
func TestAPanickingRequestIsStillLogged(t *testing.T) {
	handler, logs := chainFor(t, true, http.HandlerFunc(
		func(http.ResponseWriter, *http.Request) { panic("boom") }))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/boom", http.NoBody))

	if !strings.Contains(logs.String(), `"message":"request"`) {
		t.Errorf("no completion line for a panicking request:\n%s", logs)
	}
	if !strings.Contains(logs.String(), `"status":500`) {
		t.Errorf("the completion line does not report the 500:\n%s", logs)
	}
}
