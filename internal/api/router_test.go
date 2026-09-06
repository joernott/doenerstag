package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// httprouter panics at registration time on a conflicting route, so simply
// building the production router is the assertion. Catching it here turns a
// wildcard collision into a CI failure rather than a crash at server startup.
func TestBuildingTheRouterDoesNotPanic(t *testing.T) {
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("registering the routes panicked, most likely a route conflict: %v", recovered)
		}
	}()

	r := NewRouter(Options{
		Assets:  http.NotFoundHandler(),
		Index:   http.NotFoundHandler(),
		Swagger: http.NotFoundHandler(),
	})
	(&SystemHandlers{}).Register(r)
}

// The catch-alls that exist must not collide with the API prefix, which is the
// specific conflict httprouter would reject.
func TestCatchAllsCoexistWithTheAPI(t *testing.T) {
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("a catch-all conflicts with an API route: %v", recovered)
		}
	}()

	r := NewRouter(Options{Assets: http.NotFoundHandler(), Swagger: http.NotFoundHandler()})
	r.HandleFunc(http.MethodGet, "/orders", func(http.ResponseWriter, *http.Request) {})
	r.HandleFunc(http.MethodGet, "/orders/:id", func(http.ResponseWriter, *http.Request) {})
	r.HandleFunc(http.MethodGet, "/orders/:id/items/:iid", func(http.ResponseWriter, *http.Request) {})
	r.HandleFunc(http.MethodGet, "/restaurants/:id/menu-items/:mid", func(http.ResponseWriter, *http.Request) {})
}

func testRouter(t *testing.T) *Router {
	t.Helper()

	r := NewRouter(Options{
		Index: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte("<!doctype html><title>doenerstag</title>"))
		}),
	})
	r.HandleFunc(http.MethodGet, "/orders/:id", func(w http.ResponseWriter, req *http.Request) {
		_ = WriteJSON(w, http.StatusOK, map[string]string{"id": Param(req, "id")})
	})
	return r
}

// Path parameters reach the handler through the request context, which is what
// keeps every handler an ordinary http.Handler.
func TestPathParametersReachTheHandler(t *testing.T) {
	r := testRouter(t)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/orders/abc123", http.NoBody))

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if body["id"] != "abc123" {
		t.Errorf("the handler saw id %q, want abc123", body["id"])
	}
}

// The SPA fallback: a deep link must serve the shell so that reloading works,
// while an unknown API path must be a genuine JSON 404.
func TestUnmatchedPathsSplitBetweenTheAppAndTheAPI(t *testing.T) {
	r := testRouter(t)

	cases := map[string]struct {
		wantStatus int
		wantJSON   bool
	}{
		"/":                       {http.StatusOK, false},
		"/orders":                 {http.StatusOK, false},
		"/orders/abc/summary":     {http.StatusOK, false},
		"/restaurants":            {http.StatusOK, false},
		"/api/v1/nonexistent":     {http.StatusNotFound, true},
		"/api/v1/orders/abc/oops": {http.StatusNotFound, true},
		"/api/nonsense":           {http.StatusNotFound, true},
		"/api":                    {http.StatusNotFound, true},
	}

	for path, want := range cases {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, http.NoBody))

		if rec.Code != want.wantStatus {
			t.Errorf("%s: status %d, want %d", path, rec.Code, want.wantStatus)
			continue
		}

		isJSON := strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json")
		if isJSON != want.wantJSON {
			t.Errorf("%s: JSON=%v, want %v (body: %s)", path, isJSON, want.wantJSON, rec.Body)
		}
	}
}

// A 404 under /api must carry the documented envelope, so a client can react to
// the code rather than parsing prose.
func TestAPINotFoundUsesTheErrorEnvelope(t *testing.T) {
	r := testRouter(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/nope", http.NoBody)
	r.ServeHTTP(rec, req)

	var body envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("the 404 body is not the error envelope: %v (%s)", err, rec.Body)
	}
	if body.Error.Code != CodeNotFound {
		t.Errorf("code is %d, want %d", body.Error.Code, CodeNotFound)
	}
	if body.Error.Message == "" {
		t.Error("the envelope carries no message")
	}
}

// A wrong method should say so rather than pretending the path does not exist.
func TestWrongMethodReportsAllow(t *testing.T) {
	r := testRouter(t)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/v1/orders/abc", http.NoBody))

	if allow := rec.Header().Get("Allow"); !strings.Contains(allow, http.MethodGet) {
		t.Errorf("Allow is %q, want it to list GET", allow)
	}
	if rec.Code == http.StatusOK {
		t.Error("a disallowed method succeeded")
	}
}

func TestTrailingSlashRedirects(t *testing.T) {
	r := testRouter(t)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/orders/abc/", http.NoBody))

	if rec.Code != http.StatusMovedPermanently && rec.Code != http.StatusPermanentRedirect {
		t.Errorf("status %d, want a redirect for a trailing slash", rec.Code)
	}
}

// Case-insensitive path matching is deliberately off: /Orders and /orders being
// the same resource is not something an API should promise.
func TestPathsAreCaseSensitive(t *testing.T) {
	r := testRouter(t)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/ORDERS/abc", http.NoBody))

	if rec.Code == http.StatusOK {
		t.Error("an upper-case API path matched")
	}
	if rec.Code == http.StatusMovedPermanently {
		t.Error("an upper-case API path was redirected rather than refused")
	}
}

// ---------------------------------------------------------------------------
// CORS
// ---------------------------------------------------------------------------

// The default is no CORS headers at all. The frontend is same-origin, so none
// are needed, and their absence keeps another origin from calling the API with
// the user's cookies.
func TestNoCORSHeadersByDefault(t *testing.T) {
	r := testRouter(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/orders/abc", http.NoBody)
	req.Header.Set("Origin", "https://elsewhere.invalid")
	r.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("an unconfigured server answered CORS with %q", got)
	}
}

func TestCORSAnswersOnlyConfiguredOrigins(t *testing.T) {
	r := NewRouter(Options{CORSOrigins: []string{"https://tools.example.invalid"}})
	r.HandleFunc(http.MethodGet, "/orders", func(http.ResponseWriter, *http.Request) {})

	preflight := func(origin string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodOptions, "/api/v1/orders", http.NoBody)
		req.Header.Set("Origin", origin)
		req.Header.Set("Access-Control-Request-Method", http.MethodGet)
		r.ServeHTTP(rec, req)
		return rec
	}

	allowed := preflight("https://tools.example.invalid")
	if got := allowed.Header().Get("Access-Control-Allow-Origin"); got != "https://tools.example.invalid" {
		t.Errorf("an allowed origin got %q", got)
	}
	// Credentials are never allowed cross-origin: a cross-origin caller must
	// use an API token, which carries no ambient authority to abuse.
	if got := allowed.Header().Get("Access-Control-Allow-Credentials"); got != "false" {
		t.Errorf("Access-Control-Allow-Credentials is %q, want false", got)
	}
	if !strings.Contains(allowed.Header().Get("Vary"), "Origin") {
		t.Error("the response does not vary on Origin, so a cache could cross origins")
	}

	refused := preflight("https://attacker.invalid")
	if got := refused.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("an unlisted origin got %q", got)
	}
}

func TestParseCORSOrigins(t *testing.T) {
	got := ParseCORSOrigins(" https://a.invalid , https://b.invalid ,, ")
	want := []string{"https://a.invalid", "https://b.invalid"}

	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	if len(ParseCORSOrigins("")) != 0 {
		t.Error("an empty setting produced origins")
	}
}
