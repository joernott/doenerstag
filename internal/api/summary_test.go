package api_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/joernott/doenerstag/internal/api"
)

type summaryResponse struct {
	OrderID      string `json:"order_id"`
	Title        string `json:"title"`
	CurrencyCode string `json:"currency_code"`
	Aggregated   []struct {
		ItemName       string   `json:"item_name"`
		ExternalID     string   `json:"external_id"`
		Modifications  []string `json:"modifications"`
		Note           *string  `json:"note"`
		Count          int      `json:"count"`
		UnitPriceCents int64    `json:"unit_price_cents"`
		TotalCents     int64    `json:"total_cents"`
	} `json:"aggregated"`
	PerPerson []struct {
		UserID      string          `json:"user_id"`
		DisplayName string          `json:"display_name"`
		Items       []orderItemResp `json:"items"`
		TotalCents  int64           `json:"total_cents"`
	} `json:"per_person"`
	ItemTotalCents     int64  `json:"item_total_cents"`
	DeliveryFeeCents   *int64 `json:"delivery_fee_cents"`
	GrandTotalCents    int64  `json:"grand_total_cents"`
	MinOrderValueCents *int64 `json:"min_order_value_cents"`
	BelowMinimum       bool   `json:"below_minimum"`
	PlainText          string `json:"plain_text"`
}

func (o *orderFixture) readSummary(cookies ...*http.Cookie) summaryResponse {
	o.t.Helper()

	rec := o.get("/orders/"+o.order.ID+"/summary", cookies...)
	if rec.Code != http.StatusOK {
		o.t.Fatalf("reading the summary: %d %s", rec.Code, rec.Body.String())
	}
	var body summaryResponse
	decode(o.t, rec, &body)
	return body
}

// Identical orders aggregate into one line, and differing ones do not.
func TestSummaryAggregation(t *testing.T) {
	o := newOrderFixture(t)
	anna := o.register("anna")
	bert := o.register("bert")

	// Two people, the same dish, the same modification: one line saying 2x.
	o.addOrderItem(anna, map[string]any{
		"quantity": 1, "modification_ids": []string{o.noOnions},
	})
	o.addOrderItem(bert, map[string]any{
		"quantity": 1, "modification_ids": []string{o.noOnions},
	})
	// The same dish with a different modification: its own line.
	o.addOrderItem(anna, map[string]any{
		"quantity": 1, "modification_ids": []string{o.extraSauce},
	})
	// And plain, twice over on one item.
	o.addOrderItem(bert, map[string]any{"quantity": 2})

	summary := o.readSummary(anna...)

	if len(summary.Aggregated) != 3 {
		t.Fatalf("aggregated into %d lines, want 3: %+v", len(summary.Aggregated), summary.Aggregated)
	}

	byMods := map[string]int{}
	for _, line := range summary.Aggregated {
		byMods[strings.Join(line.Modifications, "+")] = line.Count
	}
	if byMods["ohne Zwiebeln"] != 2 {
		t.Errorf("the shared line counts %d, want 2: %v", byMods["ohne Zwiebeln"], byMods)
	}
	if byMods["extra Sauce"] != 1 {
		t.Errorf("the sauce line counts %d", byMods["extra Sauce"])
	}
	if byMods[""] != 2 {
		t.Errorf("the plain line counts %d, want 2 from one item of quantity 2", byMods[""])
	}

	// 2*650 + 700 + 2*650
	if summary.ItemTotalCents != 1300+700+1300 {
		t.Errorf("the item total is %d", summary.ItemTotalCents)
	}
}

// The note is part of the key, normalised: trimmed and case-insensitive.
func TestTheNoteIsPartOfTheAggregationKey(t *testing.T) {
	o := newOrderFixture(t)
	anna := o.register("anna")

	o.addOrderItem(anna, map[string]any{"quantity": 1, "note": "gut durch"})
	// Same note, differently typed: the same line.
	o.addOrderItem(anna, map[string]any{"quantity": 1, "note": "  Gut Durch  "})
	// A different note: its own line.
	o.addOrderItem(anna, map[string]any{"quantity": 1, "note": "ohne Salat"})

	summary := o.readSummary(anna...)
	if len(summary.Aggregated) != 2 {
		t.Fatalf("aggregated into %d lines, want 2: %+v", len(summary.Aggregated), summary.Aggregated)
	}

	for _, line := range summary.Aggregated {
		if line.Note == nil {
			t.Errorf("a line lost its note: %+v", line)
			continue
		}
		if strings.EqualFold(*line.Note, "gut durch") && line.Count != 2 {
			t.Errorf("the two identical notes did not merge: %+v", line)
		}
	}
}

// Ticking the same boxes in a different order is the same food.
func TestModificationOrderDoesNotSplitALine(t *testing.T) {
	o := newOrderFixture(t)
	anna := o.register("anna")

	o.addOrderItem(anna, map[string]any{
		"quantity": 1, "modification_ids": []string{o.noOnions, o.extraSauce},
	})
	o.addOrderItem(anna, map[string]any{
		"quantity": 1, "modification_ids": []string{o.extraSauce, o.noOnions},
	})

	summary := o.readSummary(anna...)
	if len(summary.Aggregated) != 1 {
		t.Fatalf("the same selection made two lines: %+v", summary.Aggregated)
	}
	if summary.Aggregated[0].Count != 2 {
		t.Errorf("the line counts %d", summary.Aggregated[0].Count)
	}
}

// ADR-0009: the same dish before and after a price correction is two lines at
// two prices, because grouping them would give a line whose count times unit
// price is not its total.
func TestAPriceChangeSplitsTheLine(t *testing.T) {
	o := newOrderFixture(t)
	anna := o.register("anna")

	o.addOrderItem(anna, map[string]any{"quantity": 1})

	if rec := o.patch(o.menuPath("/menu-items/"+o.doener.ID),
		map[string]any{"price_cents": 800}, o.cookies...); rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	o.addOrderItem(anna, map[string]any{"quantity": 1})

	summary := o.readSummary(anna...)
	if len(summary.Aggregated) != 2 {
		t.Fatalf("the price change did not split the line: %+v", summary.Aggregated)
	}

	prices := map[int64]int{}
	for _, line := range summary.Aggregated {
		prices[line.UnitPriceCents] = line.Count
		if line.TotalCents != int64(line.Count)*line.UnitPriceCents {
			t.Errorf("line total %d is not count %d times unit price %d",
				line.TotalCents, line.Count, line.UnitPriceCents)
		}
	}
	if prices[650] != 1 || prices[800] != 1 {
		t.Errorf("the lines are %v, want one at 650 and one at 800", prices)
	}
	if summary.ItemTotalCents != 1450 {
		t.Errorf("the total is %d, want 1450", summary.ItemTotalCents)
	}
}

func TestSummaryPerPersonTotals(t *testing.T) {
	o := newOrderFixture(t)
	anna := o.register("anna")
	bert := o.register("bert")

	o.addOrderItem(anna, map[string]any{"quantity": 2})                                             // 1300
	o.addOrderItem(bert, map[string]any{"quantity": 1, "modification_ids": []string{o.extraSauce}}) // 700
	o.addOrderItem(anna, map[string]any{"quantity": 1})                                             // 650

	summary := o.readSummary(anna...)
	if len(summary.PerPerson) != 2 {
		t.Fatalf("%d people, want 2", len(summary.PerPerson))
	}

	totals := map[string]int64{}
	counts := map[string]int{}
	for _, person := range summary.PerPerson {
		totals[person.DisplayName] = person.TotalCents
		counts[person.DisplayName] = len(person.Items)
	}
	if totals["anna"] != 1950 {
		t.Errorf("anna owes %d, want 1950", totals["anna"])
	}
	if totals["bert"] != 700 {
		t.Errorf("bert owes %d, want 700", totals["bert"])
	}
	if counts["anna"] != 2 || counts["bert"] != 1 {
		t.Errorf("item counts are %v", counts)
	}

	// The per-person totals add up to the item total.
	var sum int64
	for _, person := range summary.PerPerson {
		sum += person.TotalCents
	}
	if sum != summary.ItemTotalCents {
		t.Errorf("the per-person totals sum to %d but the item total is %d",
			sum, summary.ItemTotalCents)
	}
}

func TestSummaryTotalsAndMinimum(t *testing.T) {
	o := newOrderFixture(t)

	if rec := o.patch("/restaurants/"+o.restaurant, map[string]any{
		"min_order_value_cents": 2000, "delivery_fee_cents": 250,
	}, o.cookies...); rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	o.order = o.createOrder(o.cookies, 2*hour)

	anna := o.register("anna")
	o.addOrderItem(anna, map[string]any{"quantity": 2}) // 1300

	below := o.readSummary(anna...)
	if below.ItemTotalCents != 1300 {
		t.Errorf("item total %d", below.ItemTotalCents)
	}
	if below.GrandTotalCents != 1550 {
		t.Errorf("grand total %d, want the fee added", below.GrandTotalCents)
	}
	if !below.BelowMinimum {
		t.Error("1300 is below the 2000 minimum but is not flagged")
	}

	o.addOrderItem(anna, map[string]any{"quantity": 2})
	above := o.readSummary(anna...)
	if above.BelowMinimum {
		t.Error("2600 is above the minimum but is still flagged")
	}
}

// The plain text is what somebody reads down the telephone.
func TestThePlainTextRendering(t *testing.T) {
	o := newOrderFixture(t)
	anna := o.register("anna")

	o.addOrderItem(anna, map[string]any{"quantity": 2})
	o.addOrderItem(anna, map[string]any{
		"quantity": 1, "modification_ids": []string{o.noOnions}, "note": "gut durch",
	})

	summary := o.readSummary(anna...)
	text := summary.PlainText

	if !strings.Contains(text, "2x") {
		t.Errorf("the plain text has no count: %q", text)
	}
	if !strings.Contains(text, "Döner Kebab") {
		t.Errorf("the plain text has no item name: %q", text)
	}
	if !strings.Contains(text, "ohne Zwiebeln") || !strings.Contains(text, "gut durch") {
		t.Errorf("the plain text omits the modifications or the note: %q", text)
	}
	// The item number is included, because it is what gets read out.
	if !strings.Contains(text, "1 Döner Kebab") {
		t.Errorf("the plain text omits the item number: %q", text)
	}
	if lines := strings.Count(strings.TrimSpace(text), "\n") + 1; lines != 2 {
		t.Errorf("the plain text has %d lines, want 2:\n%s", lines, text)
	}
}

// 9.2: the summary is for participants only.
func TestSummaryIsForParticipantsOnly(t *testing.T) {
	o := newOrderFixture(t)
	participant := o.register("teilnehmer")
	bystander := o.register("zuschauer")

	o.addOrderItem(participant, map[string]any{"quantity": 1})

	anonymous := o.get("/orders/" + o.order.ID + "/summary")
	expectError(t, anonymous, http.StatusUnauthorized, api.CodeNotAuthenticated)

	outsider := o.get("/orders/"+o.order.ID+"/summary", bystander...)
	expectError(t, outsider, http.StatusForbidden, api.CodeNotParticipant)

	if rec := o.get("/orders/"+o.order.ID+"/summary", participant...); rec.Code != http.StatusOK {
		t.Errorf("a participant was refused: %d %s", rec.Code, rec.Body.String())
	}
	if rec := o.get("/orders/"+o.order.ID+"/summary", o.admin...); rec.Code != http.StatusOK {
		t.Errorf("the administrator was refused: %d %s", rec.Code, rec.Body.String())
	}
}

// The creator is a participant even with no items, because the creator is
// usually the person phoning the restaurant and the summary is what they read
// from. Omitting them would break the main workflow.
func TestTheCreatorSeesTheSummaryWithoutOrderingAnything(t *testing.T) {
	o := newOrderFixture(t)
	other := o.register("anderer")
	o.addOrderItem(other, map[string]any{"quantity": 1})

	if rec := o.get("/orders/"+o.order.ID+"/summary", o.cookies...); rec.Code != http.StatusOK {
		t.Errorf("the creator was refused their own order's summary: %d %s",
			rec.Code, rec.Body.String())
	}
}

// Losing your last item loses you the summary. Correct and slightly
// surprising, which is why it is tested rather than assumed.
func TestRemovingYourLastItemLosesYouTheSummary(t *testing.T) {
	o := newOrderFixture(t)
	person := o.register("gast")

	item := o.addOrderItem(person, map[string]any{"quantity": 1})
	if rec := o.get("/orders/"+o.order.ID+"/summary", person...); rec.Code != http.StatusOK {
		t.Fatalf("a participant was refused: %s", rec.Body.String())
	}

	if rec := o.remove("/orders/"+o.order.ID+"/items/"+item.ID, person...); rec.Code != http.StatusNoContent {
		t.Fatalf("removing the item: %s", rec.Body.String())
	}

	after := o.get("/orders/"+o.order.ID+"/summary", person...)
	expectError(t, after, http.StatusForbidden, api.CodeNotParticipant)
}

// An empty order's summary is empty rather than an error: the creator opens it
// before anybody has ordered.
func TestTheSummaryOfAnEmptyOrder(t *testing.T) {
	o := newOrderFixture(t)

	summary := o.readSummary(o.cookies...)
	if len(summary.Aggregated) != 0 || len(summary.PerPerson) != 0 {
		t.Errorf("an empty order summarises as %+v", summary)
	}
	if summary.ItemTotalCents != 0 {
		t.Errorf("the total is %d", summary.ItemTotalCents)
	}
	if summary.PlainText != "" {
		t.Errorf("the plain text is %q", summary.PlainText)
	}
	if summary.Title == "" || summary.CurrencyCode == "" {
		t.Errorf("the header is incomplete: %+v", summary)
	}
}

// The summary reads in menu order, so it can be dictated straight down.
func TestTheSummaryIsInMenuOrder(t *testing.T) {
	o := newOrderFixture(t)
	anna := o.register("anna")

	second := o.addItem(map[string]any{"name": "Lahmacun", "external_id": "10", "price_cents": 450})
	third := o.addItem(map[string]any{"name": "Ayran", "external_id": "2", "price_cents": 200})

	// Added in an order that is neither the menu's nor the reverse.
	o.addOrderItem(anna, map[string]any{"menu_item_id": second.ID, "quantity": 1})
	o.addOrderItem(anna, map[string]any{"quantity": 1}) // the Döner, number 1
	o.addOrderItem(anna, map[string]any{"menu_item_id": third.ID, "quantity": 1})

	summary := o.readSummary(anna...)
	got := make([]string, 0, len(summary.Aggregated))
	for _, line := range summary.Aggregated {
		got = append(got, line.ExternalID)
	}
	// 1, 2, 10 -- numerically, not as text.
	if !equal(got, []string{"1", "2", "10"}) {
		t.Errorf("the summary is ordered %v, want [1 2 10]", got)
	}
}

// A soft-deleted menu item leaves the summary readable, with the snapshotted
// name and no item number.
func TestASummaryOfDeletedMenuItemsStillReads(t *testing.T) {
	o := newOrderFixture(t)
	anna := o.register("anna")
	o.addOrderItem(anna, map[string]any{"quantity": 2})

	if rec := o.remove(o.menuPath("/menu-items/"+o.doener.ID), o.admin...); rec.Code != http.StatusNoContent {
		t.Fatalf("deleting the menu item: %s", rec.Body.String())
	}

	summary := o.readSummary(anna...)
	if len(summary.Aggregated) != 1 {
		t.Fatalf("the summary lost its line: %+v", summary.Aggregated)
	}
	line := summary.Aggregated[0]
	if line.ItemName != "Döner Kebab" {
		t.Errorf("the snapshotted name is %q", line.ItemName)
	}
	if line.ExternalID != "" {
		t.Errorf("a deleted item still reports an item number: %q", line.ExternalID)
	}
	if line.TotalCents != 1300 {
		t.Errorf("the total is %d", line.TotalCents)
	}
}
