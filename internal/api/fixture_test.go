package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/joernott/doenerstag/internal/api"
	"github.com/joernott/doenerstag/internal/auth"
	"github.com/joernott/doenerstag/internal/db"
	"github.com/joernott/doenerstag/internal/sse"
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
	limiter       *api.LoginLimiter
	users         *api.UserHandlers
	restaurants   *api.RestaurantHandlers
	images        *api.ImageHandlers
	menu          *api.MenuHandlers
	orders        *api.OrderHandlers
	registry      *sse.Registry

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

// fixtureMaxImageBytes is the upload cap the fixture applies.
//
// Far below the shipped 5 MiB, so the too-large test can exceed it with a
// picture rather than with five megabytes of noise.
const fixtureMaxImageBytes = 256 * 1024

// Login limits for the rate limit tests.
//
// Deliberately far below the documented defaults of 10 and 60. Every failed
// login costs a full Argon2id verification at 64 MiB -- on purpose, so that a
// miss takes as long as a hit -- and running the real per-address limit to
// exhaustion meant sixty of them per test, which took the package from thirty
// seconds to two minutes. The limits are configuration, so what these tests
// exercise is the mechanism; that the shipped defaults are 10, 60 and 15
// minutes is asserted in the config package, where checking it costs nothing.
const (
	fixturePerName    = 3
	fixturePerAddress = 6
	fixtureWindow     = 15 * time.Minute
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

	// No limit by default. A limiter in the shared fixture would silently
	// govern every test that logs in more than a handful of times, so a test
	// about something else would start failing with 5000 for reasons that have
	// nothing to do with it. Zero disables a counter; the rate limit tests
	// switch it on with limitLogins.
	f.limiter = &api.LoginLimiter{Window: fixtureWindow, Now: clock}
	f.auth = &api.AuthHandlers{
		Pool:            pool,
		Signer:          signer,
		AbsoluteTimeout: fixtureAbsoluteTimeout,
		Secure:          true,
		Limiter:         f.limiter,
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
	f.users = &api.UserHandlers{Pool: pool, Secure: true, Now: clock}
	f.users.Register(f.router)

	// The system endpoints are here so the permission matrix can assert the
	// public-read rows against a real route rather than a stand-in.
	(&api.SystemHandlers{Pool: pool}).Register(f.router)
	f.restaurants = &api.RestaurantHandlers{Pool: pool}
	f.restaurants.Register(f.router)
	f.images = &api.ImageHandlers{Pool: pool, MaxUploadBytes: fixtureMaxImageBytes}
	f.images.Register(f.router)
	f.menu = &api.MenuHandlers{Pool: pool}
	f.menu.Register(f.router)
	f.registry = sse.NewRegistry()
	f.orders = &api.OrderHandlers{
		Pool: pool, Events: f.registry, Now: clock,
	}
	f.orders.Register(f.router)
	f.registerProbe()

	logger := zerolog.Nop()
	f.handler = api.Chain(f.router,
		api.RequestID(),
		api.LogRequests(&logger),
		api.Recover(&logger),
		f.authenticator.Middleware(),
		api.RequireCSRF(),
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

	// omitCSRF suppresses the X-CSRF-Token header that a well-behaved client
	// sends with every state-changing request. Only the CSRF tests set it: for
	// every other test the header is noise, and forgetting it would turn a
	// meaningful assertion into "2005 again".
	omitCSRF bool
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
		// Behave like the frontend: read the CSRF cookie and echo it. Doing it
		// here rather than in each test means a test that forgets it is not
		// silently testing the CSRF guard instead of what it meant to.
		if c.Name == api.CSRFCookieName && !req.omitCSRF && r.Header.Get(api.CSRFHeaderName) == "" {
			r.Header.Set(api.CSRFHeaderName, c.Value)
		}
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

// limitLogins turns on the login rate limit for a test that is about it.
func (f *apiFixture) limitLogins() {
	f.limiter.PerName = fixturePerName
	f.limiter.PerAddress = fixturePerAddress
}

// loginAsAdmin creates an account and makes it the administrator.
//
// The API cannot do this, on purpose: docs/05_auth_and_permissions.md says
// exactly one administrator exists, created by install, and that no other
// account can be granted is_admin. A test needs one anyway, so it is set
// directly in the database -- which is what install does too.
func (f *apiFixture) loginAsAdmin(name string) []*http.Cookie {
	f.t.Helper()

	cookies := f.register(name)
	if _, err := f.pool.Exec(context.Background(),
		`UPDATE app_user SET is_admin = true WHERE lower(name) = lower($1)`,
		name); err != nil {
		f.t.Fatalf("granting is_admin to %q: %v", name, err)
	}

	// The session predates the flag, but is_admin is re-read from the database
	// on every request, so the existing cookies are already administrator
	// cookies. That is the property being relied on, and it is asserted in
	// TestTheAdmClaimIsNotTrusted.
	return cookies
}

// userID reads an account's id, for building paths.
func (f *apiFixture) userID(name string) string {
	f.t.Helper()

	user, err := db.UserByName(context.Background(), f.pool, name)
	if err != nil {
		f.t.Fatalf("looking up %q: %v", name, err)
	}
	return user.ID.String()
}

// get, patch and remove are the remaining verbs, shaped like post.
func (f *apiFixture) get(path string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	f.t.Helper()
	return f.do(request{method: http.MethodGet, path: path, cookies: cookies})
}

func (f *apiFixture) patch(path string, body any, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	f.t.Helper()
	return f.do(request{method: http.MethodPatch, path: path, body: body, cookies: cookies})
}

func (f *apiFixture) remove(path string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	f.t.Helper()
	return f.do(request{method: http.MethodDelete, path: path, cookies: cookies})
}

// probeWithToken asks the probe who the caller is, authenticating by API token.
func (f *apiFixture) probeWithToken(value string) probeResponse {
	f.t.Helper()

	rec := f.do(request{
		method: http.MethodGet, path: "/probe",
		headers: map[string]string{"Authorization": "Bearer " + value},
	})
	if rec.Code != http.StatusOK {
		f.t.Fatalf("the probe answered %d: %s", rec.Code, rec.Body.String())
	}
	var body probeResponse
	decode(f.t, rec, &body)
	return body
}

// countSessions is how many session rows a user has, which the single-session
// rule keeps at zero or one.
func (f *apiFixture) countSessions(name string) int {
	f.t.Helper()

	var count int
	if err := f.pool.QueryRow(context.Background(), `
		SELECT count(*) FROM session s
		JOIN app_user u ON u.id = s.user_id
		WHERE lower(u.name) = lower($1)`, name).Scan(&count); err != nil {
		f.t.Fatalf("counting sessions for %q: %v", name, err)
	}
	return count
}

// hour is a shorthand for the seed helpers' deadlines.
const hour = time.Hour

// seedOrderAt puts an order against a given restaurant, for the tests that need
// one to exist without going through sprint 8's API.
func (f *apiFixture) seedOrderAt(restaurantID, creator string, deadline time.Time) uuid.UUID {
	f.t.Helper()

	creatorID := uuid.MustParse(f.userID(creator))
	orderID := uuid.Must(uuid.NewV7())

	if _, err := f.pool.Exec(context.Background(), `
		INSERT INTO food_order (id, creator_id, restaurant_id, fulfilment,
		                        fulfilment_at, deadline_at, currency_code,
		                        created_by, updated_by)
		VALUES ($1, $2, $3, 'pickup', $4::timestamptz + interval '1 hour',
		        $4::timestamptz, 'EUR', $2, $2)`,
		orderID, creatorID, uuid.MustParse(restaurantID), deadline); err != nil {
		f.t.Fatalf("seeding an order: %v", err)
	}
	return orderID
}

// ctx is the background context, so a test does not import context just to
// run a query against the fixture's pool.
func (f *apiFixture) ctx() context.Context { return context.Background() }

// mustParseUUID is uuid.MustParse with a test failure instead of a panic.
func mustParseUUID(t *testing.T, s string) uuid.UUID {
	t.Helper()

	id, err := uuid.Parse(s)
	if err != nil {
		t.Fatalf("parsing %q: %v", s, err)
	}
	return id
}
