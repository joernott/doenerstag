package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// draw stands past the order's deadline and runs one pass of the draw.
func (o *orderFixture) draw() {
	o.t.Helper()

	o.advance(3 * time.Hour)
	if err := o.orders.DrawJobs(context.Background()); err != nil {
		o.t.Fatalf("drawing: %v", err)
	}
}

// 21.3: nobody volunteered, so one of the people who ordered is put in both
// jobs, and the order says it was by lot.
func TestTheDrawPutsOnePersonInBothEmptyJobs(t *testing.T) {
	o := newOrderFixture(t)
	ayse := o.register("ayse")
	ben := o.register("ben")
	o.addOrderItem(ayse, map[string]any{})
	o.addOrderItem(ben, map[string]any{})
	o.addOrderItem(ben, map[string]any{"quantity": 2})

	o.draw()

	got := o.readOrder(o.cookies...)
	if got.PickupPersonID == nil || got.MoneyCollectorID == nil {
		t.Fatalf("after the draw: fetching %v, collecting %v", got.PickupPersonID, got.MoneyCollectorID)
	}
	if *got.PickupPersonID != *got.MoneyCollectorID {
		t.Errorf("two different people were picked: %s and %s", *got.PickupPersonID, *got.MoneyCollectorID)
	}
	if picked := *got.PickupPersonID; picked != o.userID("ayse") && picked != o.userID("ben") {
		t.Errorf("%s was picked, who ordered nothing", picked)
	}
	if !got.PickupPersonDrawn || !got.MoneyCollectorDrawn {
		t.Errorf("the order does not say the jobs were drawn: %+v", got)
	}
}

// Only people with an item are drawn: the creator, who ordered nothing here,
// is never picked, and every one who did order can be.
func TestTheDrawPicksOnlyAmongThoseWhoOrdered(t *testing.T) {
	seen := map[string]bool{}
	for range 12 {
		o := newOrderFixture(t)
		ayse := o.register("ayse")
		ben := o.register("ben")
		o.addOrderItem(ayse, map[string]any{})
		o.addOrderItem(ben, map[string]any{})

		o.draw()

		got := o.readOrder(o.cookies...)
		if got.PickupPersonID == nil {
			t.Fatal("nobody was picked")
		}
		switch *got.PickupPersonID {
		case o.userID("ayse"):
			seen["ayse"] = true
		case o.userID("ben"):
			seen["ben"] = true
		default:
			t.Fatalf("%s was picked, who ordered nothing", *got.PickupPersonID)
		}
		if len(seen) == 2 {
			return
		}
	}
	// Twelve draws between two people all landing on one has odds of 1 in
	// 2048. A failure here is far more likely a draw that is not random.
	t.Errorf("twelve draws all picked %v", seen)
}

// A job somebody took is theirs; only the empty one is filled, and only it
// is marked as drawn.
func TestTheDrawLeavesAVolunteerAlone(t *testing.T) {
	o := newOrderFixture(t)
	ayse := o.register("ayse")
	o.addOrderItem(ayse, map[string]any{})
	if rec := o.post("/orders/"+o.order.ID+"/pickup-person", nil, o.cookies...); rec.Code != http.StatusOK {
		t.Fatalf("volunteering: %d %s", rec.Code, rec.Body.String())
	}

	o.draw()

	got := o.readOrder(o.cookies...)
	if got.PickupPersonID == nil || *got.PickupPersonID != o.userID("cook") || got.PickupPersonDrawn {
		t.Errorf("the volunteer's job is %v, drawn %v", got.PickupPersonID, got.PickupPersonDrawn)
	}
	if got.MoneyCollectorID == nil || *got.MoneyCollectorID != o.userID("ayse") || !got.MoneyCollectorDrawn {
		t.Errorf("the empty job is %v, drawn %v", got.MoneyCollectorID, got.MoneyCollectorDrawn)
	}
}

// The draw is made once. A job left empty afterwards -- the picked person's
// account deleted, say, which empties the column -- is not drawn again.
func TestTheDrawIsMadeOnce(t *testing.T) {
	o := newOrderFixture(t)
	ayse := o.register("ayse")
	o.addOrderItem(ayse, map[string]any{})
	o.draw()

	if _, err := o.pool.Exec(context.Background(),
		`UPDATE food_order SET pickup_person_id = NULL, money_collector_id = NULL`); err != nil {
		t.Fatal(err)
	}
	if err := o.orders.DrawJobs(context.Background()); err != nil {
		t.Fatal(err)
	}

	got := o.readOrder(o.cookies...)
	if got.PickupPersonID != nil || got.PickupPersonDrawn {
		t.Errorf("a second draw filled the job: %v, drawn %v", got.PickupPersonID, got.PickupPersonDrawn)
	}
}

// Nothing is drawn before the deadline, and an order nobody ordered from has
// nobody to draw.
func TestNothingIsDrawnEarlyOrFromNobody(t *testing.T) {
	o := newOrderFixture(t)
	ayse := o.register("ayse")
	o.addOrderItem(ayse, map[string]any{})

	if err := o.orders.DrawJobs(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := o.readOrder(o.cookies...); got.PickupPersonID != nil {
		t.Errorf("the draw ran before the deadline and picked %s", *got.PickupPersonID)
	}

	empty := newOrderFixture(t)
	empty.draw()
	if got := empty.readOrder(empty.cookies...); got.PickupPersonID != nil || got.MoneyCollectorID != nil {
		t.Errorf("an order with no items had somebody picked: %+v", got)
	}
}

// The pages watching the order hear who was picked, without a reload.
func TestTheDrawReachesThePageWatchingTheOrder(t *testing.T) {
	o := newOrderFixture(t)
	ayse := o.register("ayse")
	o.addOrderItem(ayse, map[string]any{})
	watching := o.openStream(o.order.ID, o.cookies)
	waitForSubscribers(t, o, 1)

	o.draw()

	_, data := watching.waitFor("order.updated")
	var event orderResponse
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		t.Fatalf("decoding %s: %v", data, err)
	}
	if event.PickupPersonID == nil || *event.PickupPersonID != o.userID("ayse") || !event.PickupPersonDrawn {
		t.Errorf("the event carried %s", data)
	}
}
