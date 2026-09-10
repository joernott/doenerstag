package api_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/joernott/doenerstag/internal/api"
)

// The rest of the permission matrix from docs/05_auth_and_permissions.md: the
// rows about orders, restaurants, menus, images and content pages.
//
// matrix_test.go covers the account rows, and until this file existed the rest
// were listed in a pendingRows map naming the sprint that would deliver them.
// Sprints 6 to 9 delivered every one of those endpoints and none of the cells,
// and the map went on reporting the gap to nobody. The endpoints each have
// their own tests, but a test that a creator may edit their order is not a test
// that a stranger may not, and the refusals are the half that matters.
//
// One row was wrong in the implementation rather than merely untested:
// POST /shutdown checked nothing at all.

// contentMatrix is the four callers plus something of each kind to act on.
//
// Everything is created by the order fixture's own user, so that "owner /
// creator" means one person throughout: the creator of the restaurant, of the
// menu, of the order and of the order item.
type contentMatrix struct {
	*orderFixture

	// bystander is logged in and created none of it.
	bystander []*http.Cookie

	category  string
	orderItem string

	// emptyOrder has no items, because changing an order's restaurant is only
	// allowed while that is true, and the fixture's main order has one.
	emptyOrder string

	// spare is a restaurant nothing refers to. Deleting the main one is refused
	// with 4003 whoever asks, which would hide the permission answer.
	spare string
}

func newContentMatrix(t *testing.T) *contentMatrix {
	t.Helper()

	o := newOrderFixture(t)
	c := &contentMatrix{orderFixture: o}

	c.bystander = o.register("bystander")
	c.category = o.addCategory("Vom Grill", 1).ID
	c.orderItem = o.addOrderItem(o.cookies, map[string]any{"quantity": 1}).ID
	c.emptyOrder = o.createOrder(o.cookies, 2*time.Hour).ID
	c.spare = o.createRestaurant("Zweite Wahl", o.cookies).ID

	return c
}

func (c *contentMatrix) cookiesFor(who caller) []*http.Cookie {
	switch who {
	case anonymous:
		return nil
	case otherUser:
		return c.bystander
	case owner:
		return c.cookies
	case admin:
		return c.admin
	default:
		return nil
	}
}

// contentCase is one row of the matrix against one endpoint.
type contentCase struct {
	row    string
	method string
	path   func(c *contentMatrix) string
	body   func(c *contentMatrix) any

	// send replaces the JSON request for the two rows whose bodies are not
	// JSON: a content page is HTML and an upload is multipart.
	send func(c *contentMatrix, cookies []*http.Cookie) *httptest.ResponseRecorder

	// afterDeadline moves the clock past the order's deadline first.
	afterDeadline bool

	cells []cell
}

// contentRows is the matrix, one entry per row, in the order the document
// lists them.
func contentRows() []contentCase {
	const (
		allowed = true
		refused = false
	)

	return []contentCase{
		{
			row:    "View the order list and each order's item count",
			method: http.MethodGet,
			path:   func(*contentMatrix) string { return "/orders" },
			cells: []cell{
				{anonymous, allowed, 0},
				{otherUser, allowed, 0},
				{owner, allowed, 0},
				{admin, allowed, 0},
			},
		},
		{
			row:    "View an order's summary page",
			method: http.MethodGet,
			path:   func(c *contentMatrix) string { return "/orders/" + c.order.ID + "/summary" },
			cells: []cell{
				{anonymous, refused, api.CodeNotAuthenticated},
				// "participants only": the bystander is logged in and holds no
				// item in this order.
				{otherUser, refused, api.CodeNotParticipant},
				{owner, allowed, 0},
				{admin, allowed, 0},
			},
		},
		{
			// The one row that is anonymous-only. Registering starts a session,
			// and until sprint 14 a logged-in caller who asked was quietly
			// switched to the new account instead of being refused.
			row:    "Register an account",
			method: http.MethodPost,
			path:   func(*contentMatrix) string { return "/auth/register" },
			body: func(*contentMatrix) any {
				return map[string]any{"name": "neuling", "password": validPassword}
			},
			cells: []cell{
				{anonymous, allowed, 0},
				{otherUser, refused, api.CodeAlreadyLoggedIn},
				{owner, refused, api.CodeAlreadyLoggedIn},
				{admin, refused, api.CodeAlreadyLoggedIn},
			},
		},
		{
			row:    "Create an order",
			method: http.MethodPost,
			path:   func(*contentMatrix) string { return "/orders" },
			body: func(c *contentMatrix) any {
				deadline := c.now.Add(90 * time.Minute)
				return map[string]any{
					"restaurant_id": c.restaurant,
					"fulfilment":    "pickup",
					"fulfilment_at": deadline.Add(30 * time.Minute).UTC().Format(time.RFC3339),
					"deadline_at":   deadline.UTC().Format(time.RFC3339),
				}
			},
			cells: []cell{
				{anonymous, refused, api.CodeNotAuthenticated},
				{otherUser, allowed, 0},
				{owner, allowed, 0},
				{admin, allowed, 0},
			},
		},
		{
			row:    "Edit an order's fields",
			method: http.MethodPatch,
			path:   func(c *contentMatrix) string { return "/orders/" + c.order.ID },
			body:   func(*contentMatrix) any { return map[string]any{"fulfilment": "delivery"} },
			cells: []cell{
				{anonymous, refused, api.CodeNotAuthenticated},
				{otherUser, refused, api.CodeNotOrderCreator},
				{owner, allowed, 0},
				{admin, allowed, 0},
			},
		},
		{
			// Deliberately open to anybody signed in, which is the whole point
			// of the row: the person willing to walk to the restaurant should
			// not have to find the creator first.
			row:    "Take on fetching the food, when nobody has",
			method: http.MethodPost,
			path:   func(c *contentMatrix) string { return "/orders/" + c.order.ID + "/pickup-person" },
			body:   func(*contentMatrix) any { return map[string]any{} },
			cells: []cell{
				{anonymous, refused, api.CodeNotAuthenticated},
				{otherUser, allowed, 0},
				{owner, allowed, 0},
				{admin, allowed, 0},
			},
		},
		{
			row:    "Change an order's restaurant (no items yet)",
			method: http.MethodPatch,
			path:   func(c *contentMatrix) string { return "/orders/" + c.emptyOrder },
			body: func(c *contentMatrix) any {
				return map[string]any{"restaurant_id": c.spare}
			},
			cells: []cell{
				{anonymous, refused, api.CodeNotAuthenticated},
				{otherUser, refused, api.CodeNotOrderCreator},
				{owner, allowed, 0},
				{admin, allowed, 0},
			},
		},
		{
			row:    "Delete an order",
			method: http.MethodDelete,
			path:   func(c *contentMatrix) string { return "/orders/" + c.order.ID },
			cells: []cell{
				{anonymous, refused, api.CodeNotAuthenticated},
				{otherUser, refused, api.CodeNotOrderCreator},
				{owner, allowed, 0},
				{admin, allowed, 0},
			},
		},
		{
			row:    "Add an order item to an active order",
			method: http.MethodPost,
			path:   func(c *contentMatrix) string { return "/orders/" + c.order.ID + "/items" },
			body: func(c *contentMatrix) any {
				return map[string]any{"menu_item_id": c.doener.ID, "quantity": 1}
			},
			cells: []cell{
				{anonymous, refused, api.CodeNotAuthenticated},
				{otherUser, allowed, 0},
				{owner, allowed, 0},
				{admin, allowed, 0},
			},
		},
		{
			row:    "Edit or delete an order item",
			method: http.MethodPatch,
			path: func(c *contentMatrix) string {
				return "/orders/" + c.order.ID + "/items/" + c.orderItem
			},
			body: func(*contentMatrix) any { return map[string]any{"quantity": 2} },
			cells: []cell{
				{anonymous, refused, api.CodeNotAuthenticated},
				// The item belongs to the fixture's own user, so the bystander
				// is a stranger to it even though anyone may add their own.
				{otherUser, refused, api.CodeNotItemOwner},
				{owner, allowed, 0},
				{admin, allowed, 0},
			},
		},
		{
			row:    "Edit or delete an order item after deadline",
			method: http.MethodPatch,
			path: func(c *contentMatrix) string {
				return "/orders/" + c.order.ID + "/items/" + c.orderItem
			},
			body:          func(*contentMatrix) any { return map[string]any{"quantity": 2} },
			afterDeadline: true,
			cells: []cell{
				// The only row where the administrator is refused as well. The
				// deadline is what the group agreed, not an access rule, and an
				// administrator who could edit past it could change what
				// somebody owes after the fact.
				{anonymous, refused, api.CodeNotAuthenticated},
				{otherUser, refused, api.CodeOrderClosed},
				{owner, refused, api.CodeOrderClosed},
				{admin, refused, api.CodeOrderClosed},
			},
		},
		{
			row:    "View restaurants, menus, opening hours",
			method: http.MethodGet,
			path:   func(c *contentMatrix) string { return c.menuPath("/menu-items") },
			cells: []cell{
				{anonymous, allowed, 0},
				{otherUser, allowed, 0},
				{owner, allowed, 0},
				{admin, allowed, 0},
			},
		},
		{
			row:    "Create a restaurant",
			method: http.MethodPost,
			path:   func(*contentMatrix) string { return "/restaurants" },
			body: func(c *contentMatrix) any {
				return map[string]any{
					"name":          "Neu am Platz",
					"currency_code": "EUR",
					"contacts": []map[string]any{
						{"contact_type_id": c.contactTypeID("phone"), "value": "+49 30 654321"},
					},
				}
			},
			cells: []cell{
				{anonymous, refused, api.CodeNotAuthenticated},
				{otherUser, allowed, 0},
				{owner, allowed, 0},
				{admin, allowed, 0},
			},
		},
		{
			row:    "Edit a restaurant, contacts, opening hours",
			method: http.MethodPatch,
			path:   func(c *contentMatrix) string { return "/restaurants/" + c.restaurant },
			body:   func(*contentMatrix) any { return map[string]any{"notes": "jetzt mit Terrasse"} },
			cells: []cell{
				{anonymous, refused, api.CodeNotAuthenticated},
				// Deliberately open: menu data is shared, and the asymmetry is
				// that anyone may correct it while only the administrator may
				// destroy it.
				{otherUser, allowed, 0},
				{owner, allowed, 0},
				{admin, allowed, 0},
			},
		},
		{
			row:    "Delete a restaurant",
			method: http.MethodDelete,
			path:   func(c *contentMatrix) string { return "/restaurants/" + c.spare },
			cells: []cell{
				{anonymous, refused, api.CodeNotAuthenticated},
				{otherUser, refused, api.CodeAdminRequired},
				// Creating it is not owning it: the creator is refused too.
				{owner, refused, api.CodeAdminRequired},
				{admin, allowed, 0},
			},
		},
		{
			row:    "Create a menu category or menu item",
			method: http.MethodPost,
			path:   func(c *contentMatrix) string { return c.menuPath("/categories") },
			body: func(*contentMatrix) any {
				return map[string]any{"name": "Getränke", "sort_order": 9}
			},
			cells: []cell{
				{anonymous, refused, api.CodeNotAuthenticated},
				{otherUser, allowed, 0},
				{owner, allowed, 0},
				{admin, allowed, 0},
			},
		},
		{
			row:    "Edit a menu item, mark it unavailable",
			method: http.MethodPatch,
			path:   func(c *contentMatrix) string { return c.menuPath("/menu-items/" + c.doener.ID) },
			body:   func(*contentMatrix) any { return map[string]any{"available": false} },
			cells: []cell{
				{anonymous, refused, api.CodeNotAuthenticated},
				{otherUser, allowed, 0},
				{owner, allowed, 0},
				{admin, allowed, 0},
			},
		},
		{
			row:    "Delete a menu category, item or modification",
			method: http.MethodDelete,
			path:   func(c *contentMatrix) string { return c.menuPath("/categories/" + c.category) },
			cells: []cell{
				{anonymous, refused, api.CodeNotAuthenticated},
				{otherUser, refused, api.CodeAdminRequired},
				{owner, refused, api.CodeAdminRequired},
				{admin, allowed, 0},
			},
		},
		{
			row:    "Create a free tag",
			method: http.MethodPost,
			path:   func(*contentMatrix) string { return "/tags" },
			body: func(*contentMatrix) any {
				return map[string]any{"code": "knoblauchfrei", "name": "ohne Knoblauch"}
			},
			cells: []cell{
				{anonymous, refused, api.CodeNotAuthenticated},
				{otherUser, allowed, 0},
				{owner, allowed, 0},
				{admin, allowed, 0},
			},
		},
		{
			row: "Upload an image",
			send: func(c *contentMatrix, cookies []*http.Cookie) *httptest.ResponseRecorder {
				return c.uploadFile("logo.png", samplePNG(c.t, 60, 40), cookies)
			},
			cells: []cell{
				{anonymous, refused, api.CodeNotAuthenticated},
				{otherUser, allowed, 0},
				{owner, allowed, 0},
				{admin, allowed, 0},
			},
		},
		{
			row: "Replace imprint / legal notes",
			send: func(c *contentMatrix, cookies []*http.Cookie) *httptest.ResponseRecorder {
				return c.putHTML("/pages/imprint", "<p>Angaben nach § 5 TMG</p>", cookies)
			},
			cells: []cell{
				{anonymous, refused, api.CodeNotAuthenticated},
				{otherUser, refused, api.CodeAdminRequired},
				{owner, refused, api.CodeAdminRequired},
				{admin, allowed, 0},
			},
		},
		{
			row:    "Shut the application down",
			method: http.MethodPost,
			path:   func(*contentMatrix) string { return "/shutdown" },
			cells: []cell{
				{anonymous, refused, api.CodeNotAuthenticated},
				{otherUser, refused, api.CodeAdminRequired},
				{owner, refused, api.CodeAdminRequired},
				{admin, allowed, 0},
			},
		},
	}
}

// putHTML sends a content page, whose body is HTML rather than JSON.
func (c *contentMatrix) putHTML(path, html string, cookies []*http.Cookie) *httptest.ResponseRecorder {
	c.t.Helper()

	r := httptest.NewRequest(http.MethodPut, api.APIPrefix+path, bytes.NewReader([]byte(html)))
	r.Header.Set("Content-Type", "text/html; charset=utf-8")
	r.RemoteAddr = "192.0.2.55:41234"
	for _, cookie := range cookies {
		r.AddCookie(cookie)
		if cookie.Name == api.CSRFCookieName {
			r.Header.Set(api.CSRFHeaderName, cookie.Value)
		}
	}

	rec := httptest.NewRecorder()
	c.handler.ServeHTTP(rec, r)
	return rec
}

func TestPermissionMatrixContent(t *testing.T) {
	for _, tc := range contentRows() {
		t.Run(tc.row, func(t *testing.T) {
			for _, want := range tc.cells {
				t.Run(want.who.String(), func(t *testing.T) {
					// A fresh fixture per cell, for the same reason the account
					// matrix uses one: DELETE and PATCH change the subject, and
					// sharing a fixture would make a cell's answer depend on
					// which cells ran before it.
					c := newContentMatrix(t)
					if tc.afterDeadline {
						c.advance(3 * time.Hour)
					}

					rec := tc.request(c, want.who)

					if want.allowed {
						if rec.Code >= 400 {
							t.Errorf("%s was refused: %d %s",
								want.who, rec.Code, rec.Body.String())
						}
						return
					}
					expectErrorCode(t, rec, want.code)
				})
			}
		})
	}
}

// request sends the row's call as the given caller.
func (tc contentCase) request(c *contentMatrix, who caller) *httptest.ResponseRecorder {
	cookies := c.cookiesFor(who)
	if tc.send != nil {
		return tc.send(c, cookies)
	}

	var body any
	if tc.body != nil {
		body = tc.body(c)
	}
	return c.do(request{
		method:  tc.method,
		path:    tc.path(c),
		body:    body,
		cookies: cookies,
	})
}

// Nobody may shut the server down by asking nicely.
//
// The matrix row above asserts the status codes. This asserts the consequence,
// which is the part that matters: a refusal that still ran the hook would look
// identical from outside and would still stop the server.
func TestARefusedShutdownDoesNotShutAnythingDown(t *testing.T) {
	c := newContentMatrix(t)

	for _, who := range []caller{anonymous, otherUser, owner} {
		rec := c.do(request{
			method:  http.MethodPost,
			path:    "/shutdown",
			cookies: c.cookiesFor(who),
		})
		if rec.Code < 400 {
			t.Fatalf("%s was allowed to shut the server down: %d", who, rec.Code)
		}
	}

	if n := c.shutdowns.Load(); n != 0 {
		t.Errorf("the shutdown hook ran %d times after three refusals", n)
	}
}

// The administrator's shutdown reaches the hook. Without this the check above
// would pass just as well if the endpoint refused everybody.
func TestTheAdministratorCanShutTheServerDown(t *testing.T) {
	c := newContentMatrix(t)

	rec := c.do(request{
		method:  http.MethodPost,
		path:    "/shutdown",
		cookies: c.admin,
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("the administrator was refused: %d %s", rec.Code, rec.Body.String())
	}

	// The handler answers first and calls the hook from its own goroutine, so
	// the count is not necessarily up yet when the response arrives.
	deadline := time.Now().Add(2 * time.Second)
	for c.shutdowns.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if n := c.shutdowns.Load(); n != 1 {
		t.Errorf("the shutdown hook ran %d times, want 1", n)
	}
}

// An order's detail is the one row the matrix cannot express as allowed or
// refused: everybody gets an answer, and what differs is what is in it.
//
// docs/05 writes "View an order's items and totals: – ✓ ✓ ✓" and ADR-0011 says
// the anonymous tier is the header and the item count, never the items and
// never anybody's name.
func TestAnOrdersDetailIsTieredRatherThanRefused(t *testing.T) {
	c := newContentMatrix(t)

	t.Run("anonymous", func(t *testing.T) {
		rec := c.get("/orders/" + c.order.ID)
		if rec.Code != http.StatusOK {
			t.Fatalf("an anonymous caller was refused the header: %d %s",
				rec.Code, rec.Body.String())
		}

		var body struct {
			ItemCount *int             `json:"item_count"`
			Items     []map[string]any `json:"items"`
			Creator   *string          `json:"creator_name"`
			Raw       map[string]any   `json:"-"`
		}
		decode(t, rec, &body)

		if body.ItemCount == nil {
			t.Error("the anonymous tier has no item count")
		}
		if len(body.Items) != 0 {
			t.Errorf("the anonymous tier carries %d items", len(body.Items))
		}
		if body.Creator != nil && *body.Creator != "" {
			t.Errorf("the anonymous tier names the creator: %q", *body.Creator)
		}
	})

	t.Run("a logged-in stranger", func(t *testing.T) {
		rec := c.get("/orders/"+c.order.ID, c.bystander...)
		if rec.Code != http.StatusOK {
			t.Fatalf("a logged-in caller was refused: %d %s", rec.Code, rec.Body.String())
		}

		var body struct {
			Items []map[string]any `json:"items"`
		}
		decode(t, rec, &body)
		if len(body.Items) == 0 {
			t.Error("a logged-in caller sees no items")
		}
	})
}
