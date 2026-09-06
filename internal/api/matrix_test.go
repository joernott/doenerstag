package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/joernott/doenerstag/internal/api"
)

// The permission matrix from docs/05_auth_and_permissions.md, one case per
// cell.
//
// The matrix has four columns -- anonymous, user, owner/creator, admin -- and
// every row states four answers. A test per row that only checked the happy
// path would leave the three refusals untested, which are the ones that
// matter: a permission bug is almost always something being allowed, not
// something being refused.
//
// Sprint 5 implements the account rows. The rows about orders, restaurants,
// menus and images name endpoints that do not exist yet; they are listed in
// pendingRows below so the count is visible rather than silently short, and
// this file grows as those sprints land.

// caller is one column of the matrix.
type caller int

const (
	anonymous caller = iota
	otherUser        // logged in, but not the owner
	owner            // the subject of the resource
	admin
)

func (c caller) String() string {
	switch c {
	case anonymous:
		return "anonymous"
	case otherUser:
		return "another user"
	case owner:
		return "the owner"
	case admin:
		return "the administrator"
	default:
		return "unknown"
	}
}

// cell is one expected answer.
type cell struct {
	who     caller
	allowed bool
	// code is the error expected when not allowed. Zero means "any error will
	// do", which is never used: knowing which refusal is the point.
	code api.Code
}

// matrixCase is one row of the matrix against one endpoint.
type matrixCase struct {
	row    string
	method string
	// path is built from the fixture, because most of them contain an id.
	path  func(f *matrixFixture) string
	body  any
	cells []cell
}

// matrixFixture holds the four callers and a subject to act on.
type matrixFixture struct {
	*apiFixture
	ownerCookies []*http.Cookie
	otherCookies []*http.Cookie
	adminCookies []*http.Cookie
	subjectID    string
	tokenID      string
}

func newMatrixFixture(t *testing.T) *matrixFixture {
	t.Helper()

	f := newAPIFixture(t)
	m := &matrixFixture{apiFixture: f}

	m.ownerCookies = f.register("subject")
	m.otherCookies = f.register("bystander")
	m.adminCookies = f.loginAsAdmin("chief")
	m.subjectID = f.userID("subject")
	m.tokenID = f.createToken("owned", "subject", m.ownerCookies).ID

	return m
}

func (m *matrixFixture) cookiesFor(who caller) []*http.Cookie {
	switch who {
	case anonymous:
		return nil
	case otherUser:
		return m.otherCookies
	case owner:
		return m.ownerCookies
	case admin:
		return m.adminCookies
	default:
		return nil
	}
}

// accountRows are the matrix rows sprint 5 implements.
func accountRows() []matrixCase {
	return []matrixCase{
		{
			row:    "View a public profile",
			method: http.MethodGet,
			path:   func(m *matrixFixture) string { return "/users/" + m.subjectID },
			cells: []cell{
				{anonymous, true, 0},
				{otherUser, true, 0},
				{owner, true, 0},
				{admin, true, 0},
			},
		},
		{
			row:    "Edit own profile",
			method: http.MethodPatch,
			path:   func(m *matrixFixture) string { return "/users/" + m.subjectID },
			body:   map[string]any{"display_name": "changed"},
			cells: []cell{
				{anonymous, false, api.CodeNotAuthenticated},
				{otherUser, false, api.CodeNotItemOwner},
				{owner, true, 0},
				{admin, true, 0},
			},
		},
		{
			row:    "List all users",
			method: http.MethodGet,
			path:   func(*matrixFixture) string { return "/users" },
			cells: []cell{
				{anonymous, false, api.CodeNotAuthenticated},
				{otherUser, false, api.CodeAdminRequired},
				{owner, false, api.CodeAdminRequired},
				{admin, true, 0},
			},
		},
		{
			row:    "Manage own API tokens: list",
			method: http.MethodGet,
			path:   func(m *matrixFixture) string { return "/users/" + m.subjectID + "/tokens" },
			cells: []cell{
				{anonymous, false, api.CodeNotAuthenticated},
				{otherUser, false, api.CodeNotItemOwner},
				{owner, true, 0},
				{admin, true, 0},
			},
		},
		{
			row:    "Manage own API tokens: create",
			method: http.MethodPost,
			path:   func(m *matrixFixture) string { return "/users/" + m.subjectID + "/tokens" },
			body:   map[string]any{"name": "created-by-the-matrix"},
			cells: []cell{
				{anonymous, false, api.CodeNotAuthenticated},
				{otherUser, false, api.CodeNotItemOwner},
				{owner, true, 0},
				{admin, true, 0},
			},
		},
		{
			row:    "See own deletion impact",
			method: http.MethodGet,
			path: func(m *matrixFixture) string {
				return "/users/" + m.subjectID + "/deletion-impact"
			},
			cells: []cell{
				{anonymous, false, api.CodeNotAuthenticated},
				{otherUser, false, api.CodeNotItemOwner},
				{owner, true, 0},
				{admin, true, 0},
			},
		},
		{
			row:    "View imprint, legal notes, version",
			method: http.MethodGet,
			path:   func(*matrixFixture) string { return "/version" },
			cells: []cell{
				{anonymous, true, 0},
				{otherUser, true, 0},
				{owner, true, 0},
				{admin, true, 0},
			},
		},
	}
}

func TestPermissionMatrix(t *testing.T) {
	for _, tc := range accountRows() {
		t.Run(tc.row, func(t *testing.T) {
			for _, c := range tc.cells {
				t.Run(c.who.String(), func(t *testing.T) {
					// A fresh fixture per cell. The rows are destructive --
					// PATCH changes the subject, POST /tokens consumes a name
					// -- and sharing one would make a cell's result depend on
					// which cells ran before it.
					m := newMatrixFixture(t)

					rec := m.do(request{
						method:  tc.method,
						path:    tc.path(m),
						body:    tc.body,
						cookies: m.cookiesFor(c.who),
					})

					if c.allowed {
						if rec.Code >= 400 {
							t.Errorf("%s was refused: %d %s",
								c.who, rec.Code, rec.Body.String())
						}
						return
					}

					if rec.Code < 400 {
						t.Fatalf("%s was allowed: %d %s",
							c.who, rec.Code, rec.Body.String())
					}
					var body errorBody
					decode(t, rec, &body)
					if api.Code(body.Error.Code) != c.code {
						t.Errorf("%s was refused with %d, want %d",
							c.who, body.Error.Code, c.code)
					}
				})
			}
		})
	}
}

// Deleting an account is its own case, because every allowed cell destroys the
// subject and the row cannot be run against one fixture.
func TestPermissionMatrixAccountDeletion(t *testing.T) {
	cases := []cell{
		{anonymous, false, api.CodeNotAuthenticated},
		{otherUser, false, api.CodeNotItemOwner},
		{owner, true, 0},
		{admin, true, 0},
	}

	for _, c := range cases {
		t.Run(c.who.String(), func(t *testing.T) {
			m := newMatrixFixture(t)

			rec := m.remove("/users/"+m.subjectID, m.cookiesFor(c.who)...)

			if c.allowed {
				if rec.Code != http.StatusNoContent {
					t.Errorf("%s was refused: %d %s", c.who, rec.Code, rec.Body.String())
				}
				return
			}
			expectErrorCode(t, rec, c.code)
		})
	}
}

// Revoking a token is likewise destructive.
func TestPermissionMatrixTokenRevocation(t *testing.T) {
	cases := []cell{
		{anonymous, false, api.CodeNotAuthenticated},
		{otherUser, false, api.CodeNotItemOwner},
		{owner, true, 0},
		{admin, true, 0},
	}

	for _, c := range cases {
		t.Run(c.who.String(), func(t *testing.T) {
			m := newMatrixFixture(t)

			rec := m.remove("/users/"+m.subjectID+"/tokens/"+m.tokenID,
				m.cookiesFor(c.who)...)

			if c.allowed {
				if rec.Code != http.StatusNoContent {
					t.Errorf("%s was refused: %d %s", c.who, rec.Code, rec.Body.String())
				}
				return
			}
			expectErrorCode(t, rec, c.code)
		})
	}
}

// An anonymous caller can change nothing at all, which
// docs/05_auth_and_permissions.md states as a rule in its own right rather than
// as the sum of the matrix's rows.
func TestAnAnonymousCallerCanChangeNothing(t *testing.T) {
	m := newMatrixFixture(t)

	writes := []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodPatch, "/users/" + m.subjectID, map[string]any{"display_name": "x"}},
		{http.MethodDelete, "/users/" + m.subjectID, nil},
		{http.MethodPost, "/users/" + m.subjectID + "/tokens", map[string]any{"name": "x"}},
		{http.MethodDelete, "/users/" + m.subjectID + "/tokens/" + m.tokenID, nil},
		{http.MethodPost, "/auth/logout", nil},
	}

	for _, w := range writes {
		rec := m.do(request{method: w.method, path: w.path, body: w.body})

		// Logout is the one write an anonymous caller may make, because it
		// asks for a state that already holds.
		if w.path == "/auth/logout" {
			if rec.Code != http.StatusOK {
				t.Errorf("anonymous logout answered %d", rec.Code)
			}
			continue
		}
		if rec.Code < 400 {
			t.Errorf("anonymous %s %s was allowed: %d", w.method, w.path, rec.Code)
		}
	}
}

// Every cell where the owner column is ticked, the administrator can act too.
// The matrix says so once, at the bottom, rather than in each row, so it is
// asserted once here.
func TestTheAdministratorCanActWhereverTheOwnerCan(t *testing.T) {
	for _, tc := range accountRows() {
		ownerAllowed := false
		adminAllowed := false
		for _, c := range tc.cells {
			if c.who == owner {
				ownerAllowed = c.allowed
			}
			if c.who == admin {
				adminAllowed = c.allowed
			}
		}
		if ownerAllowed && !adminAllowed {
			t.Errorf("%q allows the owner but not the administrator", tc.row)
		}
	}
}

// The rows this sprint cannot cover, named so the gap is visible rather than
// silent. Each becomes a real case when its sprint lands.
func TestPermissionMatrixCoverageIsRecorded(t *testing.T) {
	pendingRows := map[string]string{
		"View the order list and each order's item count": "sprint 8",
		"View an order's items and totals":                "sprint 8",
		"View an order's summary page":                    "sprint 9",
		"Create an order":                                 "sprint 8",
		"Edit an order's fields":                          "sprint 8",
		"Change an order's restaurant (no items yet)":     "sprint 8",
		"Delete an order":                                 "sprint 8",
		"Add an order item to an active order":            "sprint 8",
		"Edit or delete an order item":                    "sprint 8",
		"Edit or delete an order item after deadline":     "sprint 8",
		"View restaurants, menus, opening hours":          "sprint 6",
		"Create a restaurant":                             "sprint 6",
		"Edit a restaurant, contacts, opening hours":      "sprint 6",
		"Delete a restaurant":                             "sprint 6",
		"Create a menu category or menu item":             "sprint 7",
		"Edit a menu item, mark it unavailable":           "sprint 7",
		"Delete a menu category, item or modification":    "sprint 7",
		"Create a free tag":                               "sprint 7",
		"Upload an image":                                 "sprint 6",
		"Replace imprint / legal notes":                   "sprint 9",
		"Shut the application down":                       "sprint 9",
	}

	if len(pendingRows) == 0 {
		t.Error("this test should be deleted once every row is covered")
	}
	t.Logf("%d matrix rows covered here, %d awaiting their sprint",
		len(accountRows()), len(pendingRows))
}

// expectErrorCode asserts the internal code without pinning the status, for the
// cases where the code is the point.
func expectErrorCode(t *testing.T, rec *httptest.ResponseRecorder, code api.Code) {
	t.Helper()

	if rec.Code < 400 {
		t.Fatalf("the request was allowed: %d %s", rec.Code, rec.Body.String())
	}
	var body errorBody
	decode(t, rec, &body)
	if api.Code(body.Error.Code) != code {
		t.Errorf("refused with %d, want %d", body.Error.Code, code)
	}
}
