package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/joernott/doenerstag/internal/logging"
)

// 20.21: at DEBUG every handler logs its entry, as docs/08_technologies.md
// specifies -- the function, its parameters, and of a body only the names of
// its fields -- with secrets redacted and the request's correlation ID.
func TestAHandlerLogsItsEntryAtDebug(t *testing.T) {
	var out bytes.Buffer
	logger := zerolog.New(&out).Level(zerolog.DebugLevel)
	router := NewRouter(Options{Logger: &logger})
	router.HandleFunc(http.MethodPost, "/things/:id", func(w http.ResponseWriter, r *http.Request) {
		// The handler still gets the whole body.
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r.Body)
		if !strings.Contains(buf.String(), "Geheim-Passwort-99") {
			t.Errorf("the handler received %q", buf.String())
		}
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/things/42?token=abc&sort=name",
		strings.NewReader(`{"name":"Döner","password":"Geheim-Passwort-99"}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(logging.WithRequestID(req.Context(), "req-1"))
	router.ServeHTTP(httptest.NewRecorder(), req)

	line := out.String()
	for _, want := range []string{
		`"message":"call"`, `"request_id":"req-1"`, `"route":"POST /things/:id"`,
		`"id":"42"`, `"query.sort":"name"`, `"query.token":"***"`,
		`"body_fields":["name","password"]`, `"func":"api.TestAHandlerLogsItsEntryAtDebug.func1"`,
	} {
		if !strings.Contains(line, want) {
			t.Errorf("the entry line lacks %s:\n%s", want, line)
		}
	}
	for _, secret := range []string{"Geheim-Passwort-99", "abc", "Döner"} {
		if strings.Contains(line, secret) {
			t.Errorf("the entry line carries the value %q:\n%s", secret, line)
		}
	}
}

func TestNoEntryLineBelowDebug(t *testing.T) {
	var out bytes.Buffer
	logger := zerolog.New(&out).Level(zerolog.InfoLevel)
	router := NewRouter(Options{Logger: &logger})
	router.HandleFunc(http.MethodGet, "/things", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/things", http.NoBody))

	if out.Len() != 0 {
		t.Errorf("an INFO logger received %q", out.String())
	}
}
