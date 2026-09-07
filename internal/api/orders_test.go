package api_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/joernott/doenerstag/internal/api"
	"github.com/joernott/doenerstag/internal/db"
)

type orderResponse struct {
	ID             string          `json:"id"`
	Title          string          `json:"title"`
	RestaurantID   string          `json:"restaurant_id"`
	RestaurantName string          `json:"restaurant_name"`
	Fulfilment     string          `json:"fulfilment"`
	FulfilmentAt   string          `json:"fulfilment_at"`
	DeadlineAt     string          `json:"deadline_at"`
	Status         string          `json:"status"`
	CreatorID      string          `json:"creator_id"`
	CreatorName    string          `json:"creator_name"`
	MoneyCollector string          `json:"money_collector"`
	PickupPerson   string          `json:"pickup_person"`
	CurrencyCode   string          `json:"currency_code"`
	MinOrderValue  *int64          `json:"min_order_value_cents"`
	DeliveryFee    *int64          `json:"delivery_fee_cents"`
	ItemCount      int             `json:"item_count"`
	Items          []orderItemResp `json:"items"`
	ItemTotalCents int64           `json:"item_total_cents"`
	GrandTotal     int64           `json:"grand_total_cents"`
	BelowMinimum   bool            `json:"below_minimum"`
}

type orderItemResp struct {
	ID             string `json:"id"`
	UserID         string `json:"user_id"`
	UserName       string `json:"user_name"`
	MenuItemID     string `json:"menu_item_id"`
	Quantity       int    `json:"quantity"`
	ItemName       string `json:"item_name"`
	UnitPriceCents int64  `json:"unit_price_cents"`
	Note           string `json:"note"`
	LineTotalCents int64  `json:"line_total_cents"`
	Modifications  []struct {
		ID              string  `json:"id"`
		ModificationID  *string `json:"modification_id"`
		Name            string  `json:"name"`
		PriceDeltaCents int64   `json:"price_delta_cents"`
	} `json:"modifications"`
}

// orderFixture is a restaurant with a menu and an open order.
type orderFixture struct {
	*menuFixture
	doener     menuItemResponse
	noOnions   string
	extraSauce string
	order      orderResponse
}

func newOrderFixture(t *testing.T) *orderFixture {
	t.Helper()

	m := newMenuFixture(t)
	o := &orderFixture{menuFixture: m}

	o.doener = m.addItem(map[string]any{
		"name": "Döner Kebab", "external_id": "1", "price_cents": 650,
	})
	o.noOnions = o.addModification("ohne Zwiebeln", 0)
	o.extraSauce = o.addModification("extra Sauce", 50)

	o.order = o.createOrder(m.cookies, 2*time.Hour)
	return o
}

func (o *orderFixture) addModification(name string, delta int64) string {
	o.t.Helper()

	rec := o.post(o.menuPath("/menu-items/"+o.doener.ID+"/modifications"),
		map[string]any{"name": name, "price_delta_cents": delta}, o.cookies...)
	if rec.Code != http.StatusCreated {
		o.t.Fatalf("adding modification %q: %s", name, rec.Body.String())
	}
	var body struct {
		ID string `json:"id"`
	}
	decode(o.t, rec, &body)
	return body.ID
}

// createOrder makes an order whose deadline is `in` from the fixture clock.
func (o *orderFixture) createOrder(cookies []*http.Cookie, in time.Duration) orderResponse {
	o.t.Helper()

	deadline := o.now.Add(in)
	rec := o.post("/orders", map[string]any{
		"restaurant_id": o.restaurant,
		"fulfilment":    "pickup",
		"fulfilment_at": deadline.Add(30 * time.Minute).UTC().Format(time.RFC3339),
		"deadline_at":   deadline.UTC().Format(time.RFC3339),
	}, cookies...)
	if rec.Code != http.StatusCreated {
		o.t.Fatalf("creating an order: %d %s", rec.Code, rec.Body.String())
	}
	var body orderResponse
	decode(o.t, rec, &body)
	return body
}

// addOrderItem adds an item to the fixture's order.
func (o *orderFixture) addOrderItem(cookies []*http.Cookie, fields map[string]any) orderItemResp {
	o.t.Helper()

	if fields["menu_item_id"] == nil {
		fields["menu_item_id"] = o.doener.ID
	}
	if fields["quantity"] == nil {
		fields["quantity"] = 1
	}

	rec := o.post("/orders/"+o.order.ID+"/items", fields, cookies...)
	if rec.Code != http.StatusCreated {
		o.t.Fatalf("adding an order item: %d %s", rec.Code, rec.Body.String())
	}
	var body orderItemResp
	decode(o.t, rec, &body)
	return body
}

// createExpiredOrder makes an order that has already closed.
//
// Creating one with a deadline in the past is refused (error 1014), so this
// does what a person does: opens the order, then moves its deadline back.
// Closing an order early is deliberately still allowed, and this is the only
// way an expired order comes into existence through the API.
func (o *orderFixture) createExpiredOrder(cookies []*http.Cookie) orderResponse {
	o.t.Helper()

	created := o.createOrder(cookies, time.Hour)
	rec := o.patch("/orders/"+created.ID, map[string]any{
		"deadline_at": o.now.Add(-time.Hour).UTC().Format(time.RFC3339),
	}, cookies...)
	if rec.Code != http.StatusOK {
		o.t.Fatalf("closing the order early: %d %s", rec.Code, rec.Body.String())
	}

	var body orderResponse
	decode(o.t, rec, &body)
	return body
}

func (o *orderFixture) readOrder(cookies ...*http.Cookie) orderResponse {
	o.t.Helper()

	rec := o.get("/orders/"+o.order.ID, cookies...)
	if rec.Code != http.StatusOK {
		o.t.Fatalf("reading the order: %d %s", rec.Code, rec.Body.String())
	}
	var body orderResponse
	decode(o.t, rec, &body)
	return body
}

// F5.11: the order copies the restaurant's currency and money fields.
func TestCreatingAnOrderCopiesTheRestaurantsTerms(t *testing.T) {
	m := newMenuFixture(t)

	// Give the restaurant a minimum and a fee to copy.
	if rec := m.patch("/restaurants/"+m.restaurant, map[string]any{
		"min_order_value_cents": 2000, "delivery_fee_cents": 250,
	}, m.cookies...); rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}

	o := &orderFixture{menuFixture: m}
	order := o.createOrder(m.cookies, time.Hour)

	if order.CurrencyCode != "EUR" {
		t.Errorf("currency is %q", order.CurrencyCode)
	}
	if order.MinOrderValue == nil || *order.MinOrderValue != 2000 {
		t.Errorf("minimum is %v", order.MinOrderValue)
	}
	if order.DeliveryFee == nil || *order.DeliveryFee != 250 {
		t.Errorf("fee is %v", order.DeliveryFee)
	}

	// And the copy is a copy: changing the restaurant afterwards leaves the
	// order alone, which is the point of F5.11.
	if rec := m.patch("/restaurants/"+m.restaurant, map[string]any{
		"delivery_fee_cents": 900,
	}, m.cookies...); rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}

	o.order = order
	reread := o.readOrder(m.cookies...)
	if reread.DeliveryFee == nil || *reread.DeliveryFee != 250 {
		t.Errorf("the order's fee followed the restaurant's: %v", reread.DeliveryFee)
	}
}

// F5.6: the title is computed, not stored.
func TestTheOrderTitleIsComputed(t *testing.T) {
	o := newOrderFixture(t)

	if !strings.HasPrefix(o.order.Title, "Döner Palast — ") {
		t.Errorf("the title is %q", o.order.Title)
	}

	// Renaming the restaurant renames its orders, which is right: unlike a
	// price, the restaurant's name is not part of what was agreed.
	if rec := o.patch("/restaurants/"+o.restaurant,
		map[string]any{"name": "Kebap Haus"}, o.cookies...); rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	if title := o.readOrder(o.cookies...).Title; !strings.HasPrefix(title, "Kebap Haus — ") {
		t.Errorf("after renaming, the title is %q", title)
	}
}

// F5.5: the status is derived from the clock, with no manual transition.
func TestTheOrderStatusIsDerived(t *testing.T) {
	o := newOrderFixture(t)

	if o.readOrder(o.cookies...).Status != "active" {
		t.Error("a fresh order is not active")
	}

	o.advance(3 * time.Hour)
	if status := o.readOrder(o.cookies...).Status; status != "expired" {
		t.Errorf("after the deadline the status is %q, want expired", status)
	}
}

// F5.3: the deadline must lie strictly before the fulfilment time.
func TestTheDeadlineMustPrecedeFulfilment(t *testing.T) {
	m := newMenuFixture(t)
	at := m.now.Add(time.Hour).UTC().Format(time.RFC3339)

	for _, deadline := range []string{
		at, // equal
		m.now.Add(2 * time.Hour).UTC().Format(time.RFC3339), // after
	} {
		rec := m.post("/orders", map[string]any{
			"restaurant_id": m.restaurant, "fulfilment": "pickup",
			"fulfilment_at": at, "deadline_at": deadline,
		}, m.cookies...)
		expectError(t, rec, http.StatusBadRequest, api.CodeDeadlineAfterFulfil)
	}
}

// An order created with a deadline that has already passed is born closed:
// F6.6 makes an expired order read-only, so nobody could add an item to it and
// its creator could not edit it back into life. The only thing left to do with
// one is delete it, so it is refused instead.
func TestTheDeadlineMustBeInTheFuture(t *testing.T) {
	m := newMenuFixture(t)

	for name, deadline := range map[string]time.Time{
		"an hour ago": m.now.Add(-time.Hour),
		"right now":   m.now,
	} {
		t.Run(name, func(t *testing.T) {
			rec := m.post("/orders", map[string]any{
				"restaurant_id": m.restaurant,
				"fulfilment":    "pickup",
				// Comfortably after the deadline, so the only thing wrong with
				// the request is the deadline itself.
				"fulfilment_at": m.now.Add(6 * time.Hour).UTC().Format(time.RFC3339),
				"deadline_at":   deadline.UTC().Format(time.RFC3339),
			}, m.cookies...)
			expectError(t, rec, http.StatusBadRequest, api.CodeDeadlineInThePast)
		})
	}
}

// Moving an existing order's deadline into the past stays allowed: that is how
// a creator closes one early, and it is a different act from creating an order
// that never had a life.
func TestAnExistingOrderMayBeClosedEarly(t *testing.T) {
	o := newOrderFixture(t)

	rec := o.patch("/orders/"+o.order.ID, map[string]any{
		"deadline_at": o.now.Add(-time.Minute).UTC().Format(time.RFC3339),
	}, o.cookies...)
	if rec.Code != http.StatusOK {
		t.Fatalf("closing an order early: %d %s", rec.Code, rec.Body.String())
	}

	if status := o.readOrder(o.cookies...).Status; status != "expired" {
		t.Errorf("the order is %q after its deadline was moved into the past", status)
	}
}

// F5.2: an order can be created with no items and stays valid while empty.
func TestAnEmptyOrderIsValid(t *testing.T) {
	o := newOrderFixture(t)

	if o.order.ItemCount != 0 {
		t.Errorf("a new order has %d items", o.order.ItemCount)
	}
	if o.order.Items == nil {
		t.Error("the items array is null rather than empty")
	}
	if o.order.ItemTotalCents != 0 {
		t.Errorf("an empty order totals %d", o.order.ItemTotalCents)
	}
}

// The exit criterion, and the leak test docs/12_testing.md asks for: an
// anonymous GET must contain no user name and no menu item name anywhere in
// the response body. Scanned as raw text, not by inspecting fields -- a field
// that leaked under an unexpected name would pass a structural check.
func TestAnAnonymousReadLeaksNothing(t *testing.T) {
	o := newOrderFixture(t)
	hungry := o.register("hungrig")

	o.addOrderItem(hungry, map[string]any{
		"note":             "viel Schärfe",
		"modification_ids": []string{o.noOnions},
	})
	o.addOrderItem(o.cookies, map[string]any{"quantity": 2})

	rec := o.get("/orders/" + o.order.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	// Nothing about who ordered, who opened the order, what was ordered, or
	// what it cost. No user is named at all -- the creator included, which is
	// the rule docs/adr/0011 settled on rather than an omission.
	forbidden := map[string]string{
		"the ordering user's name": "hungrig",
		"the creator's name":       "cook",
		"the creator's id":         "creator_id",
		"the menu item's name":     "Döner Kebab",
		"a modification's name":    "ohne Zwiebeln",
		"the free-text note":       "viel Schärfe",
		"an item price":            "650",
		"the items key":            `"items"`,
		"a line total":             "line_total",
		"an order total":           "item_total",
	}
	for what, needle := range forbidden {
		if strings.Contains(body, needle) {
			t.Errorf("the anonymous response leaks %s (%q):\n%s", what, needle, body)
		}
	}

	// But the count is there, in both shapes, so the frontend never branches on
	// its absence.
	var anonymous orderResponse
	decode(t, rec, &anonymous)
	if anonymous.ItemCount != 2 {
		t.Errorf("the anonymous item count is %d, want 2", anonymous.ItemCount)
	}
	if anonymous.Items != nil {
		t.Errorf("the anonymous shape carries items: %+v", anonymous.Items)
	}
	if anonymous.CreatorName != "" || anonymous.CreatorID != "" {
		t.Errorf("the anonymous shape names the creator: %q / %q",
			anonymous.CreatorName, anonymous.CreatorID)
	}

	// What is left still identifies the order: the restaurant and the times,
	// which is what a passer-by needs to know whether lunch is still open.
	if anonymous.RestaurantName == "" || anonymous.Title == "" || anonymous.DeadlineAt == "" {
		t.Errorf("the anonymous header is incomplete: %+v", anonymous)
	}

	// A logged-in caller does see the creator, which is the tier boundary.
	if named := o.readOrder(hungry...); named.CreatorName == "" {
		t.Error("a logged-in caller cannot see who opened the order")
	}
}

// The same for the list, which never carries item detail for anybody.
func TestTheOrderListNeverCarriesItemDetail(t *testing.T) {
	o := newOrderFixture(t)
	hungry := o.register("hungrig")
	o.addOrderItem(hungry, map[string]any{"note": "geheim"})

	for _, who := range []struct {
		name       string
		cookies    []*http.Cookie
		seeCreator bool
	}{
		{"anonymous", nil, false},
		{"a logged-in user", hungry, true},
		{"the administrator", o.admin, true},
	} {
		rec := o.get("/orders", who.cookies...)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %s", who.name, rec.Body.String())
		}
		body := rec.Body.String()
		for _, needle := range []string{"hungrig", "Döner Kebab", "geheim", `"items"`} {
			if strings.Contains(body, needle) {
				t.Errorf("the list leaks %q to %s", needle, who.name)
			}
		}

		// The creator is on the tile for a logged-in visitor and for nobody
		// else, per docs/06_ui_ux.md.
		if named := strings.Contains(body, "creator_name"); named != who.seeCreator {
			t.Errorf("%s sees the creator: %v, want %v", who.name, named, who.seeCreator)
		}
		if !who.seeCreator && strings.Contains(body, "cook") {
			t.Errorf("the list names the creator to %s", who.name)
		}

		var list struct {
			Orders []orderResponse `json:"orders"`
		}
		decode(t, rec, &list)
		if len(list.Orders) != 1 || list.Orders[0].ItemCount != 1 {
			t.Errorf("%s sees %+v", who.name, list.Orders)
		}
	}
}

// A logged-in caller sees the items. That is the second tier.
func TestALoggedInCallerSeesTheItems(t *testing.T) {
	o := newOrderFixture(t)
	hungry := o.register("hungrig")
	o.addOrderItem(hungry, map[string]any{"quantity": 2, "note": "gut durch"})

	// Read by somebody who is neither the creator nor a participant: any
	// logged-in user sees the items.
	bystander := o.register("zuschauer")
	body := o.readOrder(bystander...)

	if len(body.Items) != 1 {
		t.Fatalf("a logged-in caller sees %d items", len(body.Items))
	}
	item := body.Items[0]
	if item.UserName != "hungrig" || item.ItemName != "Döner Kebab" {
		t.Errorf("the item reads %+v", item)
	}
	if item.Quantity != 2 || item.LineTotalCents != 1300 {
		t.Errorf("quantity %d, line total %d", item.Quantity, item.LineTotalCents)
	}
	if body.ItemTotalCents != 1300 {
		t.Errorf("the order totals %d", body.ItemTotalCents)
	}
}

// The exit criterion about snapshots, and the test ADR-0009 asks for by name.
func TestEditingTheMenuLeavesPlacedOrdersUntouched(t *testing.T) {
	o := newOrderFixture(t)
	item := o.addOrderItem(o.cookies, map[string]any{
		"quantity": 2, "modification_ids": []string{o.extraSauce},
	})

	if item.UnitPriceCents != 650 || item.ItemName != "Döner Kebab" {
		t.Fatalf("the snapshot is wrong at the start: %+v", item)
	}
	// 2 * (650 + 50)
	if item.LineTotalCents != 1400 {
		t.Fatalf("the line total is %d, want 1400", item.LineTotalCents)
	}

	// Now change everything the item copied.
	if rec := o.patch(o.menuPath("/menu-items/"+o.doener.ID), map[string]any{
		"name": "Döner XXL", "price_cents": 900,
	}, o.cookies...); rec.Code != http.StatusOK {
		t.Fatalf("renaming the menu item: %s", rec.Body.String())
	}
	if rec := o.patch(o.menuPath("/menu-items/"+o.doener.ID+"/modifications/"+o.extraSauce),
		map[string]any{"name": "extra Sauce XL", "price_delta_cents": 300},
		o.cookies...); rec.Code != http.StatusOK {
		t.Fatalf("editing the modification: %s", rec.Body.String())
	}

	reread := o.readOrder(o.cookies...)
	if len(reread.Items) != 1 {
		t.Fatalf("the order has %d items", len(reread.Items))
	}
	after := reread.Items[0]

	if after.ItemName != "Döner Kebab" {
		t.Errorf("the item name followed the menu: %q", after.ItemName)
	}
	if after.UnitPriceCents != 650 {
		t.Errorf("the unit price followed the menu: %d", after.UnitPriceCents)
	}
	if len(after.Modifications) != 1 {
		t.Fatalf("the item has %d modifications", len(after.Modifications))
	}
	if after.Modifications[0].Name != "extra Sauce" {
		t.Errorf("the modification name followed the menu: %q", after.Modifications[0].Name)
	}
	if after.Modifications[0].PriceDeltaCents != 50 {
		t.Errorf("the price delta followed the menu: %d", after.Modifications[0].PriceDeltaCents)
	}
	if after.LineTotalCents != 1400 {
		t.Errorf("the line total changed to %d", after.LineTotalCents)
	}

	// A soft-deleted menu item leaves the history readable too.
	if rec := o.remove(o.menuPath("/menu-items/"+o.doener.ID), o.admin...); rec.Code != http.StatusNoContent {
		t.Fatalf("deleting the menu item: %s", rec.Body.String())
	}
	final := o.readOrder(o.cookies...)
	if len(final.Items) != 1 || final.Items[0].ItemName != "Döner Kebab" {
		t.Errorf("deleting the menu item damaged the order: %+v", final.Items)
	}
}

// Several users filling one order, which is the whole point of the application.
func TestSeveralUsersFillOneOrder(t *testing.T) {
	o := newOrderFixture(t)
	anna := o.register("anna")
	bert := o.register("bert")

	o.addOrderItem(anna, map[string]any{"quantity": 1})
	o.addOrderItem(bert, map[string]any{
		"quantity": 2, "modification_ids": []string{o.extraSauce},
	})
	// F6.3: the same person may add the same dish twice, once with and once
	// without a modification. There is deliberately no unique constraint.
	o.addOrderItem(anna, map[string]any{
		"quantity": 1, "modification_ids": []string{o.noOnions},
	})

	body := o.readOrder(anna...)
	if len(body.Items) != 3 {
		t.Fatalf("the order has %d items, want 3", len(body.Items))
	}
	if body.ItemCount != 3 {
		t.Errorf("the count says %d", body.ItemCount)
	}

	// 650 + 2*(650+50) + 650
	want := int64(650 + 1400 + 650)
	if body.ItemTotalCents != want {
		t.Errorf("the total is %d, want %d", body.ItemTotalCents, want)
	}

	owners := map[string]int{}
	for _, item := range body.Items {
		owners[item.UserName]++
	}
	if owners["anna"] != 2 || owners["bert"] != 1 {
		t.Errorf("the items belong to %v", owners)
	}
}

func TestTheOrderListIsActiveFirst(t *testing.T) {
	o := newOrderFixture(t)

	// A second order, further out, and a third that has already expired.
	later := o.createOrder(o.cookies, 5*time.Hour)
	past := o.createExpiredOrder(o.cookies)

	rec := o.get("/orders")
	var list struct {
		Orders []orderResponse `json:"orders"`
	}
	decode(t, rec, &list)
	if len(list.Orders) != 3 {
		t.Fatalf("listed %d orders", len(list.Orders))
	}

	// Active first, soonest first; expired last.
	if list.Orders[0].ID != o.order.ID {
		t.Errorf("the soonest active order is not first: %+v", list.Orders[0].Title)
	}
	if list.Orders[1].ID != later.ID {
		t.Error("the later active order is not second")
	}
	if list.Orders[2].ID != past.ID {
		t.Error("the expired order is not last")
	}
	if list.Orders[2].Status != "expired" || list.Orders[0].Status != "active" {
		t.Errorf("statuses are %q and %q", list.Orders[0].Status, list.Orders[2].Status)
	}
}

func TestOrderDeletionCascades(t *testing.T) {
	o := newOrderFixture(t)
	item := o.addOrderItem(o.cookies, map[string]any{
		"modification_ids": []string{o.extraSauce},
	})

	if rec := o.remove("/orders/"+o.order.ID, o.cookies...); rec.Code != http.StatusNoContent {
		t.Fatalf("deleting: %d %s", rec.Code, rec.Body.String())
	}

	if rec := o.get("/orders/" + o.order.ID); rec.Code != http.StatusNotFound {
		t.Errorf("the order still reads: %d", rec.Code)
	}

	// The items and their modifications went with it.
	ctx := context.Background()
	var items, modifications int
	if err := o.pool.QueryRow(ctx,
		`SELECT count(*) FROM order_item WHERE id = $1`, item.ID).Scan(&items); err != nil {
		t.Fatal(err)
	}
	if err := o.pool.QueryRow(ctx,
		`SELECT count(*) FROM order_item_modification WHERE order_item_id = $1`,
		item.ID).Scan(&modifications); err != nil {
		t.Fatal(err)
	}
	if items != 0 || modifications != 0 {
		t.Errorf("%d items and %d modifications survived", items, modifications)
	}
}

// F5.9: the creator and the administrator may delete; nobody else.
func TestOnlyTheCreatorOrAdministratorDeletesAnOrder(t *testing.T) {
	o := newOrderFixture(t)
	stranger := o.register("fremder")

	anonymous := o.remove("/orders/" + o.order.ID)
	expectError(t, anonymous, http.StatusUnauthorized, api.CodeNotAuthenticated)

	other := o.remove("/orders/"+o.order.ID, stranger...)
	expectError(t, other, http.StatusForbidden, api.CodeNotOrderCreator)

	if rec := o.remove("/orders/"+o.order.ID, o.admin...); rec.Code != http.StatusNoContent {
		t.Errorf("the administrator could not delete: %s", rec.Body.String())
	}
}

// An expired order can still be deleted, unlike its contents, which are
// read-only. Clearing away old orders is housekeeping, not editing.
func TestAnExpiredOrderCanStillBeDeleted(t *testing.T) {
	o := newOrderFixture(t)
	o.advance(3 * time.Hour)

	if rec := o.remove("/orders/"+o.order.ID, o.cookies...); rec.Code != http.StatusNoContent {
		t.Errorf("an expired order could not be deleted: %d %s", rec.Code, rec.Body.String())
	}
}

func TestUnknownOrderIsNotFound(t *testing.T) {
	o := newOrderFixture(t)

	for _, id := range []string{"018f0000-0000-7000-8000-00000000dead", "not-a-uuid"} {
		if rec := o.get("/orders/" + id); rec.Code != http.StatusNotFound {
			t.Errorf("%s answered %d, want 404", id, rec.Code)
		}
	}
}

// 8.10: account deletion, re-verified against orders created through the API
// rather than the direct fixtures sprint 5 used.
func TestAccountDeletionAgainstRealOrders(t *testing.T) {
	o := newOrderFixture(t)
	leaver := o.register("scheidend")

	// An item in the active order, which F2.6 deletes outright.
	active := o.addOrderItem(leaver, map[string]any{"quantity": 1})

	// An order the leaver created, which passes to the placeholder.
	theirOrder := o.createOrder(leaver, 4*time.Hour)

	// And an item in an order that will have expired by deletion time, which
	// is reassigned to the placeholder so the history still reads.
	expiring := o.createOrder(o.cookies, time.Hour)
	saved := o.order
	o.order = expiring
	expiredItem := o.addOrderItem(leaver, map[string]any{"quantity": 3})
	o.order = saved

	// Move past that order's deadline but not the others'.
	o.advance(90 * time.Minute)

	if rec := o.remove("/users/"+o.userID("scheidend"), leaver...); rec.Code != http.StatusNoContent {
		t.Fatalf("deleting the account: %d %s", rec.Code, rec.Body.String())
	}

	ctx := context.Background()

	// The active order's item is gone.
	var stillThere int
	if err := o.pool.QueryRow(ctx,
		`SELECT count(*) FROM order_item WHERE id = $1`, active.ID).Scan(&stillThere); err != nil {
		t.Fatal(err)
	}
	if stillThere != 0 {
		t.Error("an item in an active order survived the account deletion")
	}

	// The expired order's item is reassigned, not removed.
	var owner string
	if err := o.pool.QueryRow(ctx,
		`SELECT user_id::text FROM order_item WHERE id = $1`, expiredItem.ID).
		Scan(&owner); err != nil {
		t.Fatalf("the expired order's item was deleted: %v", err)
	}
	if owner != "00000000-0000-7000-8000-000000000000" {
		t.Errorf("the expired item belongs to %s, want the placeholder", owner)
	}

	// The order they created survives, owned by the placeholder, and still
	// reads through the API. Read as a logged-in caller, because the creator is
	// only named to those.
	rec := o.get("/orders/"+theirOrder.ID, o.cookies...)
	if rec.Code != http.StatusOK {
		t.Fatalf("the created order did not survive: %d %s", rec.Code, rec.Body.String())
	}
	var survivor orderResponse
	decode(t, rec, &survivor)
	if survivor.CreatorName != "deleted user" {
		t.Errorf("the surviving order's creator reads %q", survivor.CreatorName)
	}

	// And the account itself is gone.
	if _, err := db.UserByName(ctx, o.pool, "scheidend"); err == nil {
		t.Error("the account still exists")
	}
}
