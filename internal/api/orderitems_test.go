package api_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/joernott/doenerstag/internal/api"
)

// F6.1: any logged-in user may add items, not only the creator. That is the
// whole point -- colleagues fill in the order themselves.
func TestAnyLoggedInUserMayAddItems(t *testing.T) {
	o := newOrderFixture(t)
	stranger := o.register("kollege")

	item := o.addOrderItem(stranger, map[string]any{"quantity": 1})
	if item.UserName != "kollege" {
		t.Errorf("the item belongs to %q", item.UserName)
	}

	anonymous := o.post("/orders/"+o.order.ID+"/items",
		map[string]any{"menu_item_id": o.doener.ID, "quantity": 1})
	expectError(t, anonymous, http.StatusUnauthorized, api.CodeNotAuthenticated)
}

// F6.5: a user edits and deletes only their own items.
func TestOnlyTheOwnerEditsAnItem(t *testing.T) {
	o := newOrderFixture(t)
	owner := o.register("besitzer")
	intruder := o.register("eindringling")

	item := o.addOrderItem(owner, map[string]any{"quantity": 1})
	path := "/orders/" + o.order.ID + "/items/" + item.ID

	patch := o.patch(path, map[string]any{"quantity": 5}, intruder...)
	expectError(t, patch, http.StatusForbidden, api.CodeNotItemOwner)

	del := o.remove(path, intruder...)
	expectError(t, del, http.StatusForbidden, api.CodeNotItemOwner)

	anonymous := o.patch(path, map[string]any{"quantity": 5})
	expectError(t, anonymous, http.StatusUnauthorized, api.CodeNotAuthenticated)

	// The owner can.
	if rec := o.patch(path, map[string]any{"quantity": 3}, owner...); rec.Code != http.StatusOK {
		t.Errorf("the owner could not edit: %s", rec.Body.String())
	}

	// And so can the administrator, while the order is active: the permission
	// matrix ticks admin wherever it ticks owner.
	if rec := o.patch(path, map[string]any{"quantity": 4}, o.admin...); rec.Code != http.StatusOK {
		t.Errorf("the administrator could not edit: %s", rec.Body.String())
	}
}

// F6.6 is the emphatic one: after the deadline, order items are read-only for
// everyone, "including the administrator". No exemption anywhere.
func TestAfterTheDeadlineNobodyCanChangeAnything(t *testing.T) {
	o := newOrderFixture(t)
	owner := o.register("besitzer")
	item := o.addOrderItem(owner, map[string]any{"quantity": 1})

	o.advance(3 * time.Hour)
	path := "/orders/" + o.order.ID + "/items/" + item.ID

	for _, who := range []struct {
		name    string
		cookies []*http.Cookie
	}{
		{"the owner", owner},
		{"the order's creator", o.cookies},
		{"the administrator", o.admin},
	} {
		add := o.post("/orders/"+o.order.ID+"/items",
			map[string]any{"menu_item_id": o.doener.ID, "quantity": 1}, who.cookies...)
		expectError(t, add, http.StatusConflict, api.CodeOrderClosed)

		patch := o.patch(path, map[string]any{"quantity": 9}, who.cookies...)
		expectError(t, patch, http.StatusConflict, api.CodeOrderClosed)

		del := o.remove(path, who.cookies...)
		expectError(t, del, http.StatusConflict, api.CodeOrderClosed)
	}

	// And the order's own fields are frozen too (F5.7).
	edit := o.patch("/orders/"+o.order.ID,
		map[string]any{"pickup_person": "zu spät"}, o.admin...)
	expectError(t, edit, http.StatusConflict, api.CodeOrderClosed)

	// But it still reads.
	if rec := o.get("/orders/"+o.order.ID, owner...); rec.Code != http.StatusOK {
		t.Errorf("an expired order does not read: %d", rec.Code)
	}
}

// F5.8: once the order has items, the restaurant is locked. The snapshots on
// those items came from the old menu, and switching underneath them would
// leave an order full of food the new restaurant does not sell.
func TestTheRestaurantLocksOnceItemsExist(t *testing.T) {
	o := newOrderFixture(t)
	other := o.createRestaurant("Anderes Lokal", o.cookies)

	// While empty, the restaurant may be changed.
	if rec := o.patch("/orders/"+o.order.ID,
		map[string]any{"restaurant_id": other.ID}, o.cookies...); rec.Code != http.StatusOK {
		t.Fatalf("changing the restaurant of an empty order: %s", rec.Body.String())
	}

	// Change it back so the fixture's menu applies again, then add an item.
	if rec := o.patch("/orders/"+o.order.ID,
		map[string]any{"restaurant_id": o.restaurant}, o.cookies...); rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	o.addOrderItem(o.cookies, map[string]any{"quantity": 1})

	locked := o.patch("/orders/"+o.order.ID,
		map[string]any{"restaurant_id": other.ID}, o.cookies...)
	expectError(t, locked, http.StatusConflict, api.CodeRestaurantLocked)

	// Everything else about the order is still editable.
	if rec := o.patch("/orders/"+o.order.ID,
		map[string]any{"pickup_person": "Anna"}, o.cookies...); rec.Code != http.StatusOK {
		t.Errorf("an unrelated field was locked too: %s", rec.Body.String())
	}
}

// Changing the restaurant re-copies its terms, or the order would be priced in
// one currency against a restaurant quoting another.
func TestChangingTheRestaurantRecopiesItsTerms(t *testing.T) {
	o := newOrderFixture(t)

	other := o.createRestaurant("Teures Lokal", o.cookies)
	if rec := o.patch("/restaurants/"+other.ID,
		map[string]any{"delivery_fee_cents": 500, "min_order_value_cents": 3000},
		o.cookies...); rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}

	rec := o.patch("/orders/"+o.order.ID,
		map[string]any{"restaurant_id": other.ID}, o.cookies...)
	if rec.Code != http.StatusOK {
		t.Fatalf("changing the restaurant: %s", rec.Body.String())
	}

	var updated orderResponse
	decode(t, rec, &updated)
	if updated.DeliveryFee == nil || *updated.DeliveryFee != 500 {
		t.Errorf("the fee is %v, want the new restaurant's 500", updated.DeliveryFee)
	}
	if updated.MinOrderValue == nil || *updated.MinOrderValue != 3000 {
		t.Errorf("the minimum is %v", updated.MinOrderValue)
	}
}

// Only the creator (or the administrator) edits the order's fields.
func TestOnlyTheCreatorEditsTheOrder(t *testing.T) {
	o := newOrderFixture(t)
	stranger := o.register("fremder")

	rec := o.patch("/orders/"+o.order.ID,
		map[string]any{"pickup_person": "ich"}, stranger...)
	expectError(t, rec, http.StatusForbidden, api.CodeNotOrderCreator)

	if ok := o.patch("/orders/"+o.order.ID,
		map[string]any{"pickup_person": "Anna"}, o.cookies...); ok.Code != http.StatusOK {
		t.Errorf("the creator could not edit: %s", ok.Body.String())
	}
	if admin := o.patch("/orders/"+o.order.ID,
		map[string]any{"money_collector": "Bert"}, o.admin...); admin.Code != http.StatusOK {
		t.Errorf("the administrator could not edit: %s", admin.Body.String())
	}
}

// A menu item from another restaurant must not be orderable, and the snapshot
// arrangement makes that check load-bearing: nothing later would notice.
func TestAnItemFromAnotherRestaurantIsRefused(t *testing.T) {
	o := newOrderFixture(t)

	other := o.createRestaurant("Anderes Lokal", o.cookies)
	rec := o.post("/restaurants/"+other.ID+"/menu-items",
		map[string]any{"name": "Fremdes Gericht", "price_cents": 400}, o.cookies...)
	var foreign menuItemResponse
	decode(t, rec, &foreign)

	refused := o.post("/orders/"+o.order.ID+"/items",
		map[string]any{"menu_item_id": foreign.ID, "quantity": 1}, o.cookies...)
	expectError(t, refused, http.StatusBadRequest, api.CodeItemWrongRestaurant)
}

// An unavailable item is on the menu but not orderable (F4.4).
func TestAnUnavailableItemCannotBeOrdered(t *testing.T) {
	o := newOrderFixture(t)

	if rec := o.patch(o.menuPath("/menu-items/"+o.doener.ID),
		map[string]any{"available": false}, o.cookies...); rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}

	rec := o.post("/orders/"+o.order.ID+"/items",
		map[string]any{"menu_item_id": o.doener.ID, "quantity": 1}, o.cookies...)
	expectError(t, rec, http.StatusConflict, api.CodeItemUnavailable)
}

// A modification belonging to a different dish must not be attachable, or
// "extra cheese" from another item would ride along at that item's price.
func TestAModificationMustBelongToTheItem(t *testing.T) {
	o := newOrderFixture(t)

	other := o.addItem(map[string]any{"name": "Lahmacun", "price_cents": 450})
	rec := o.post(o.menuPath("/menu-items/"+other.ID+"/modifications"),
		map[string]any{"name": "mit Käse", "price_delta_cents": 100}, o.cookies...)
	var foreign struct {
		ID string `json:"id"`
	}
	decode(t, rec, &foreign)

	refused := o.post("/orders/"+o.order.ID+"/items", map[string]any{
		"menu_item_id": o.doener.ID, "quantity": 1,
		"modification_ids": []string{foreign.ID},
	}, o.cookies...)
	expectError(t, refused, http.StatusBadRequest, api.CodeModificationWrongItem)

	// And nothing was created: the whole insert is one transaction.
	if count := o.readOrder(o.cookies...).ItemCount; count != 0 {
		t.Errorf("the refused item was created anyway: %d items", count)
	}
}

// An unknown modification id is refused rather than silently dropped, which
// would give the item a lower price than the person chose.
func TestAnUnknownModificationIsRefused(t *testing.T) {
	o := newOrderFixture(t)

	rec := o.post("/orders/"+o.order.ID+"/items", map[string]any{
		"menu_item_id": o.doener.ID, "quantity": 1,
		"modification_ids": []string{"018f0000-0000-7000-8000-00000000dead"},
	}, o.cookies...)
	expectError(t, rec, http.StatusBadRequest, api.CodeModificationWrongItem)
}

func TestQuantityValidation(t *testing.T) {
	o := newOrderFixture(t)

	for _, quantity := range []int{0, -1} {
		rec := o.post("/orders/"+o.order.ID+"/items",
			map[string]any{"menu_item_id": o.doener.ID, "quantity": quantity}, o.cookies...)
		expectError(t, rec, http.StatusBadRequest, api.CodeQuantityTooSmall)
	}

	tooMany := o.post("/orders/"+o.order.ID+"/items",
		map[string]any{"menu_item_id": o.doener.ID, "quantity": 1000}, o.cookies...)
	expectError(t, tooMany, http.StatusBadRequest, api.CodeInvalidField)
}

// Editing an item re-snapshots its modifications from the current menu, which
// is correct: the person is choosing again, now.
func TestEditingAnItemResnapshotsItsModifications(t *testing.T) {
	o := newOrderFixture(t)
	item := o.addOrderItem(o.cookies, map[string]any{
		"quantity": 1, "modification_ids": []string{o.noOnions},
	})
	if len(item.Modifications) != 1 {
		t.Fatalf("the item has %d modifications", len(item.Modifications))
	}

	rec := o.patch("/orders/"+o.order.ID+"/items/"+item.ID, map[string]any{
		"modification_ids": []string{o.extraSauce},
	}, o.cookies...)
	if rec.Code != http.StatusOK {
		t.Fatalf("editing: %s", rec.Body.String())
	}

	var updated orderItemResp
	decode(t, rec, &updated)
	if len(updated.Modifications) != 1 || updated.Modifications[0].Name != "extra Sauce" {
		t.Errorf("the modifications are %+v", updated.Modifications)
	}
	if updated.LineTotalCents != 700 {
		t.Errorf("the line total is %d, want 700", updated.LineTotalCents)
	}

	// An empty array clears them.
	cleared := o.patch("/orders/"+o.order.ID+"/items/"+item.ID, map[string]any{
		"modification_ids": []string{},
	}, o.cookies...)
	var bare orderItemResp
	decode(t, cleared, &bare)
	if len(bare.Modifications) != 0 {
		t.Errorf("an empty array left %d modifications", len(bare.Modifications))
	}
	if bare.LineTotalCents != 650 {
		t.Errorf("the line total is %d, want 650", bare.LineTotalCents)
	}
}

// An omitted modification_ids leaves them alone, so changing a quantity does
// not silently drop somebody's "no onions".
func TestPatchingAQuantityLeavesModificationsAlone(t *testing.T) {
	o := newOrderFixture(t)
	item := o.addOrderItem(o.cookies, map[string]any{
		"quantity": 1, "modification_ids": []string{o.noOnions, o.extraSauce},
	})

	rec := o.patch("/orders/"+o.order.ID+"/items/"+item.ID,
		map[string]any{"quantity": 3}, o.cookies...)
	if rec.Code != http.StatusOK {
		t.Fatalf("patching: %s", rec.Body.String())
	}

	var updated orderItemResp
	decode(t, rec, &updated)
	if len(updated.Modifications) != 2 {
		t.Errorf("the modifications were dropped: %+v", updated.Modifications)
	}
	// 3 * (650 + 0 + 50)
	if updated.LineTotalCents != 2100 {
		t.Errorf("the line total is %d, want 2100", updated.LineTotalCents)
	}
}

// The line total is quantity times unit price plus every delta, computed
// entirely from the snapshots.
func TestLineTotalArithmetic(t *testing.T) {
	o := newOrderFixture(t)

	cheaper := o.addModification("ohne Fleisch", -200)

	cases := []struct {
		name          string
		quantity      int
		modifications []string
		want          int64
	}{
		{"plain", 1, nil, 650},
		{"quantity multiplies", 3, nil, 1950},
		{"a positive delta", 1, []string{o.extraSauce}, 700},
		{"a negative delta", 1, []string{cheaper}, 450},
		{"deltas accumulate", 2, []string{o.extraSauce, cheaper}, 1000},
		{"a zero delta", 2, []string{o.noOnions}, 1300},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fields := map[string]any{"quantity": tc.quantity}
			if tc.modifications != nil {
				fields["modification_ids"] = tc.modifications
			}
			item := o.addOrderItem(o.cookies, fields)
			if item.LineTotalCents != tc.want {
				t.Errorf("line total is %d, want %d", item.LineTotalCents, tc.want)
			}
		})
	}
}

// The order's totals add up the lines, and the delivery fee is on top.
func TestOrderTotals(t *testing.T) {
	o := newOrderFixture(t)

	if rec := o.patch("/restaurants/"+o.restaurant, map[string]any{
		"min_order_value_cents": 2000, "delivery_fee_cents": 250,
	}, o.cookies...); rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	// A fresh order, so it copies the new terms.
	o.order = o.createOrder(o.cookies, 2*time.Hour)

	o.addOrderItem(o.cookies, map[string]any{"quantity": 2}) // 1300
	body := o.readOrder(o.cookies...)

	if body.ItemTotalCents != 1300 {
		t.Errorf("the item total is %d", body.ItemTotalCents)
	}
	if body.GrandTotal != 1550 {
		t.Errorf("the grand total is %d, want 1550 with the fee", body.GrandTotal)
	}
	if !body.BelowMinimum {
		t.Error("1300 is below the 2000 minimum but is not flagged")
	}

	o.addOrderItem(o.cookies, map[string]any{"quantity": 2}) // another 1300
	after := o.readOrder(o.cookies...)
	if after.BelowMinimum {
		t.Errorf("2600 is above the minimum but is still flagged: %+v", after)
	}
}

// One order's item must not be reachable through another order's URL.
func TestAnItemCannotBeReachedThroughAnotherOrder(t *testing.T) {
	o := newOrderFixture(t)
	item := o.addOrderItem(o.cookies, map[string]any{"quantity": 1})

	other := o.createOrder(o.cookies, 3*time.Hour)

	patch := o.patch("/orders/"+other.ID+"/items/"+item.ID,
		map[string]any{"quantity": 9}, o.cookies...)
	if patch.Code != http.StatusNotFound {
		t.Errorf("editing across orders answered %d, want 404", patch.Code)
	}

	del := o.remove("/orders/"+other.ID+"/items/"+item.ID, o.cookies...)
	if del.Code != http.StatusNotFound {
		t.Errorf("deleting across orders answered %d, want 404", del.Code)
	}
}

func TestDeletingAnItemUpdatesTheCount(t *testing.T) {
	o := newOrderFixture(t)
	first := o.addOrderItem(o.cookies, map[string]any{"quantity": 1})
	o.addOrderItem(o.cookies, map[string]any{"quantity": 1})

	if count := o.readOrder(o.cookies...).ItemCount; count != 2 {
		t.Fatalf("the count is %d", count)
	}

	if rec := o.remove("/orders/"+o.order.ID+"/items/"+first.ID, o.cookies...); rec.Code != http.StatusNoContent {
		t.Fatalf("deleting: %s", rec.Body.String())
	}

	after := o.readOrder(o.cookies...)
	if after.ItemCount != 1 || len(after.Items) != 1 {
		t.Errorf("after deleting one item: count %d, items %d",
			after.ItemCount, len(after.Items))
	}

	// And the anonymous shape agrees.
	rec := o.get("/orders/" + o.order.ID)
	var anonymous orderResponse
	decode(t, rec, &anonymous)
	if anonymous.ItemCount != 1 {
		t.Errorf("the anonymous count is %d", anonymous.ItemCount)
	}
}
