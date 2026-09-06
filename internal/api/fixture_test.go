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
	t      *testing.T
	pool   *pgxpool.Pool
	router *api.Router
	auth   *api.AuthHandlers

	// now is the clock the handlers read, so a test can move time forward
	// rather than sleeping through a six-hour idle timeout.
	now time.Time
}

func newAPIFixture(t *testing.T) *apiFixture {
	t.Helper()

	pool := testdb.Migrated(t)
	signer, err := auth.NewSigner(fixtureSecret)
	if err != nil {
		t.Fatalf("creating a signer: %v", err)
	}

	f := &apiFixture{t: t, pool: pool, now: time.Now()}
	f.auth = &api.AuthHandlers{
		Pool:            pool,
		Signer:          signer,
		AbsoluteTimeout: 7 * 24 * time.Hour,
		Secure:          true,
		Now:             func() time.Time { return f.now },
	}

	f.router = api.NewRouter(api.Options{})
	f.auth.Register(f.router)
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
	f.router.ServeHTTP(rec, r)
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
	f.router.ServeHTTP(rec, r)
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
