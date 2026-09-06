package api_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/joernott/doenerstag/internal/api"
)

// putHTML sends a raw HTML body, which is what the content page endpoint takes.
//
// The fixture's helpers all encode JSON, and encoding HTML as JSON is exactly
// what this endpoint does not want.
func (f *apiFixture) putHTML(path, html string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	f.t.Helper()

	r := httptest.NewRequest(http.MethodPut, api.APIPrefix+path, strings.NewReader(html))
	r.Header.Set("Content-Type", "text/html; charset=utf-8")
	r.RemoteAddr = "192.0.2.55:41234"
	for _, c := range cookies {
		r.AddCookie(c)
		if c.Name == api.CSRFCookieName {
			r.Header.Set(api.CSRFHeaderName, c.Value)
		}
	}

	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, r)
	return rec
}

func TestTheContentPagesAreReadableByAnybody(t *testing.T) {
	f := newAPIFixture(t)

	for _, key := range api.ContentPageKeys {
		rec := f.get("/pages/" + key)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /pages/%s = %d, want 200: %s", key, rec.Code, rec.Body.String())
		}
		// HTML, not JSON: what is stored is a fragment, and wrapping it in a
		// JSON string would only mean unwrapping it again.
		if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
			t.Errorf("Content-Type = %q, want text/html", got)
		}
		if rec.Body.Len() == 0 {
			t.Errorf("the %s page is empty; the migration seeds a placeholder", key)
		}
	}
}

func TestAnUnknownContentPageIsNotFound(t *testing.T) {
	f := newAPIFixture(t)

	rec := f.get("/pages/pricing")
	// 4000 rather than a constraint violation surfacing as 9001.
	expectError(t, rec, http.StatusNotFound, api.CodeNotFound)
}

func TestOnlyTheAdministratorMayReplaceAContentPage(t *testing.T) {
	f := newAPIFixture(t)

	anonymous := f.putHTML("/pages/imprint", "<p>mine now</p>")
	expectError(t, anonymous, http.StatusUnauthorized, api.CodeNotAuthenticated)

	ordinary := f.register("pagesuser")
	rec := f.putHTML("/pages/imprint", "<p>mine now</p>", ordinary...)
	expectError(t, rec, http.StatusForbidden, api.CodeAdminRequired)

	// And the page is untouched.
	if body := f.get("/pages/imprint").Body.String(); strings.Contains(body, "mine now") {
		t.Error("a refused write changed the page anyway")
	}
}

func TestTheAdministratorReplacesAPageAndItIsSanitised(t *testing.T) {
	f := newAPIFixture(t)
	admin := f.loginAsAdmin("pagesadmin")

	const submitted = `<h2>Imprint</h2>` +
		`<p onclick="steal()">Ott Consult</p>` +
		`<script>alert(1)</script>` +
		`<a href="javascript:alert(1)">click</a>` +
		`<a href="https://example.org">home</a>`

	rec := f.putHTML("/pages/imprint", submitted, admin...)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	stored := f.get("/pages/imprint").Body.String()
	for _, forbidden := range []string{"<script", "onclick", "javascript:"} {
		if strings.Contains(stored, forbidden) {
			t.Errorf("the stored page still contains %q: %s", forbidden, stored)
		}
	}
	for _, kept := range []string{"<h2>Imprint</h2>", "Ott Consult", `href="https://example.org"`} {
		if !strings.Contains(stored, kept) {
			t.Errorf("the stored page lost %q: %s", kept, stored)
		}
	}
	// What the write answered with is what was stored, so an administrator can
	// see what survived without fetching again.
	if rec.Body.String() != stored {
		t.Errorf("the response and the stored page differ:\n%s\n%s", rec.Body.String(), stored)
	}
}

func TestAPageThatIsEmptyOnceSanitisedIsRefused(t *testing.T) {
	f := newAPIFixture(t)
	admin := f.loginAsAdmin("pagesadmin2")

	before := f.get("/pages/legal_notes").Body.String()

	// Everything here is disallowed, so the result would be a blank page. That
	// is worth saying out loud rather than storing.
	rec := f.putHTML("/pages/legal_notes", "<script>alert(1)</script>", admin...)
	expectError(t, rec, http.StatusBadRequest, api.CodeMissingField)

	if after := f.get("/pages/legal_notes").Body.String(); after != before {
		t.Error("a refused write changed the page anyway")
	}
}

func TestAnOversizedContentPageIsRefused(t *testing.T) {
	f := newAPIFixture(t)
	admin := f.loginAsAdmin("pagesadmin3")

	huge := "<p>" + strings.Repeat("a", api.MaxContentPageBytes) + "</p>"
	rec := f.putHTML("/pages/imprint", huge, admin...)
	expectError(t, rec, http.StatusBadRequest, api.CodeInvalidField)
}
