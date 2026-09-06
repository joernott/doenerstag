package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/joernott/doenerstag/internal/api"
	"github.com/joernott/doenerstag/internal/auth"
	"github.com/joernott/doenerstag/internal/testdb"
)

func TestMain(m *testing.M) {
	code := m.Run()
	testdb.Shutdown()
	os.Exit(code)
}

// The secret is fixed rather than random so that a failing test prints the same
// token twice in a row and can be compared by eye.
const fixtureSecret = "0123456789abcdef0123456789abcdef"

// apiFixture is a router with the authentication routes wired to a real
// database, driven through httptest.
//
// It exercises the routes as registered, not the handler functions directly:
// a handler that works but is registered on the wrong method or path is a bug
// the test should catch.
type apiFixture struct {
	t             *testing.T
	pool          *pgxpool.Pool
	router        *api.Router
	auth          *api.AuthHandlers
	authenticator *api.Authenticator

	// handler is the router wrapped in the middleware chain, which is what the
	// tests drive. Anything that depends on a resolved principal has to go
	// through the chain to see one.
	handler http.Handler

	// now is the clock the handlers read, so a test can move time forward
	// rather than sleeping through a six-hour idle timeout.
	now time.Time
}

// Session timeouts for the fixture. Long enough that no test lapses by
// accident, short enough that advancing past them is obviously deliberate.
const (
	fixtureIdleTimeout     = 6 * time.Hour
	fixtureAbsoluteTimeout = 7 * 24 * time.Hour
)

func newAPIFixture(t *testing.T) *apiFixture {
	t.Helper()

	pool := testdb.Migrated(t)
	signer, err := auth.NewSigner(fixtureSecret)
	if err != nil {
		t.Fatalf("creating a signer: %v", err)
	}

	f := &apiFixture{t: t, pool: pool, now: time.Now()}
	clock := func() time.Time { return f.now }

	f.auth = &api.AuthHandlers{
		Pool:            pool,
		Signer:          signer,
		AbsoluteTimeout: fixtureAbsoluteTimeout,
		Secure:          true,
		Now:             clock,
	}
	f.authenticator = &api.Authenticator{
		Pool:        pool,
		Signer:      signer,
		IdleTimeout: fixtureIdleTimeout,
		Now:         clock,
	}

	f.router = api.NewRouter(api.Options{})
	f.auth.Register(f.router)
	f.registerProbe()

	logger := zerolog.Nop()
	f.handler = api.Chain(f.router,
		api.RequestID(),
		api.LogRequests(&logger),
		api.Recover(&logger),
		f.authenticator.Middleware(),
	)
	return f
}

// advance moves the fixture's clock forward.
func (f *apiFixture) advance(d time.Duration) { f.now = f.now.Add(d) }

// request is one call through the router.
type request struct {
	method  string
	path    string
	body    any
	cookies []*http.Cookie
	headers map[string]string
}

// do sends a request and returns the recorder.
func (f *apiFixture) do(req request) *httptest.ResponseRecorder {
	f.t.Helper()

	var body *strings.Reader
	if req.body != nil {
		encoded, err := json.Marshal(req.body)
		if err != nil {
			f.t.Fatalf("encoding the request body: %v", err)
		}
		body = strings.NewReader(string(encoded))
	} else {
		body = strings.NewReader("")
	}

	r := httptest.NewRequest(req.method, api.APIPrefix+req.path, body)
	if req.body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	for name, value := range req.headers {
		r.Header.Set(name, value)
	}
	for _, c := range req.cookies {
		r.AddCookie(c)
	}
	// A plausible peer address: the session row records it, and httptest's
	// default already includes a port, but being explicit documents that the
	// handlers split it.
	r.RemoteAddr = "192.0.2.55:41234"

	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, r)
	return rec
}

// post is the common case.
func (f *apiFixture) post(path string, body any, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	f.t.Helper()
	return f.do(request{method: http.MethodPost, path: path, body: body, cookies: cookies})
}

// postRaw sends a body verbatim, for the cases where the point is that it is
// not valid JSON and so cannot be produced by encoding a Go value.
func (f *apiFixture) postRaw(path, body string) *httptest.ResponseRecorder {
	f.t.Helper()

	r := httptest.NewRequest(http.MethodPost, api.APIPrefix+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.RemoteAddr = "192.0.2.55:41234"

	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, r)
	return rec
}

// decode reads a JSON response body into target, failing the test if it is not
// JSON at all.
func decode(t *testing.T, rec *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), target); err != nil {
		t.Fatalf("the response is not JSON (%d): %s", rec.Code, rec.Body.String())
	}
}

// errorBody mirrors the envelope from docs/04_api.md.
type errorBody struct {
	Error struct {
		Code      int    `json:"code"`
		Message   string `json:"message"`
		Field     string `json:"field"`
		RequestID string `json:"request_id"`
	} `json:"error"`
}

// expectError asserts the status and the internal code together.
//
// Both matter: the status is what an HTTP client branches on and the code is
// what the frontend translates, and a handler that gets one right and the other
// wrong is still wrong.
func expectError(t *testing.T, rec *httptest.ResponseRecorder, status int, code api.Code) {
	t.Helper()

	if rec.Code != status {
		t.Errorf("status %d, want %d (body: %s)", rec.Code, status, rec.Body.String())
	}
	var body errorBody
	decode(t, rec, &body)
	if api.Code(body.Error.Code) != code {
		t.Errorf("error code %d, want %d", body.Error.Code, code)
	}
	if body.Error.Message == "" {
		t.Error("the error envelope has no message")
	}
}

// sessionResponse mirrors what register, login and GET /auth/session return.
type sessionResponse struct {
	User *struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		DisplayName string `json:"display_name"`
		IsAdmin     bool   `json:"is_admin"`
	} `json:"user"`
	ExpiresAt string `json:"expires_at"`
}

// cookie finds a Set-Cookie by name, or nil.
func cookie(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// validPassword satisfies three of the five classes: upper, lower and digit.
const validPassword = "Correct-Horse9"

// probeResponse is what the fixture's own route reports about the caller.
type probeResponse struct {
	Authenticated bool   `json:"authenticated"`
	Name          string `json:"name"`
	IsAdmin       bool   `json:"is_admin"`
	ViaToken      bool   `json:"via_token"`
	SessionID     string `json:"session_id"`
}

// registerProbe adds a route that reports what the authentication middleware
// resolved.
//
// A route of the fixture's own rather than a real endpoint, because the thing
// under test in 5.3 is the middleware, and pinning those assertions to whichever
// endpoint happens to exist would make them fail for unrelated reasons later.
// It is registered on GET and on POST, so the CSRF tests have a state-changing
// method to aim at.
func (f *apiFixture) registerProbe() {
	report := func(w http.ResponseWriter, r *http.Request) {
		var body probeResponse
		if p := api.PrincipalFrom(r.Context()); p != nil {
			body = probeResponse{
				Authenticated: true,
				Name:          p.User.Name,
				IsAdmin:       p.IsAdmin(),
				ViaToken:      p.ViaToken,
				SessionID:     p.SessionID.String(),
			}
		}
		_ = api.WriteJSON(w, http.StatusOK, body)
	}
	f.router.HandleFunc(http.MethodGet, "/probe", report)
	f.router.HandleFunc(http.MethodPost, "/probe", report)
}

// whoami calls the probe with the given cookies and returns what it reported.
func (f *apiFixture) whoami(cookies ...*http.Cookie) probeResponse {
	f.t.Helper()

	rec := f.do(request{method: http.MethodGet, path: "/probe", cookies: cookies})
	if rec.Code != http.StatusOK {
		f.t.Fatalf("the probe answered %d: %s", rec.Code, rec.Body.String())
	}
	var body probeResponse
	decode(f.t, rec, &body)
	return body
}

// sessionCookie picks the session cookie out of a set, failing if it is absent.
func (f *apiFixture) sessionCookie(cookies []*http.Cookie) *http.Cookie {
	f.t.Helper()
	for _, c := range cookies {
		if c.Name == api.SessionCookieName {
			return c
		}
	}
	f.t.Fatal("no session cookie in the set")
	return nil
}
