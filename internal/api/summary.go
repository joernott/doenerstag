package api

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/joernott/doenerstag/internal/db"
	"github.com/joernott/doenerstag/internal/model"
)

// summaryBody is the shape docs/04_api.md documents.
type summaryBody struct {
	OrderID      string `json:"order_id"`
	Title        string `json:"title"`
	CurrencyCode string `json:"currency_code"`

	Aggregated []aggregatedLine `json:"aggregated"`
	PerPerson  []personLine     `json:"per_person"`

	ItemTotalCents     int64  `json:"item_total_cents"`
	DeliveryFeeCents   *int64 `json:"delivery_fee_cents"`
	GrandTotalCents    int64  `json:"grand_total_cents"`
	MinOrderValueCents *int64 `json:"min_order_value_cents"`
	BelowMinimum       bool   `json:"below_minimum"`

	// PlainText is what the person on the phone reads out. Rendered here rather
	// than in the frontend so that every client -- including a script -- gets
	// the same wording.
	PlainText string `json:"plain_text"`
}

// aggregatedLine is one line of the order as it would be dictated: three of the
// same thing, counted together.
type aggregatedLine struct {
	ItemName string `json:"item_name"`
	// ExternalID is the restaurant's own item number, read from the menu as it
	// is now rather than snapshotted. It is a dialling aid -- "number 12, three
	// times" -- not part of what was agreed, and it is empty when the item has
	// since left the menu.
	ExternalID     string   `json:"external_id"`
	Modifications  []string `json:"modifications"`
	Note           *string  `json:"note"`
	Count          int      `json:"count"`
	UnitPriceCents int64    `json:"unit_price_cents"`
	TotalCents     int64    `json:"total_cents"`
}

type personLine struct {
	UserID      string          `json:"user_id"`
	DisplayName string          `json:"display_name"`
	Items       []orderItemBody `json:"items"`

	// TotalCents is what this person still owes: every line they ordered that
	// has not been ticked as settled. PaidCents is the rest, so that a page can
	// say "nothing outstanding" rather than showing a zero that might mean the
	// person ordered nothing.
	TotalCents int64 `json:"total_cents"`
	PaidCents  int64 `json:"paid_cents"`
}

// summary serves the aggregated order.
//
// Participants only (F1.3), which is the third visibility tier: the summary
// carries per-person totals, and that is close to a record of what an
// individual spends. RequireParticipant answers 3004 for a logged-in
// non-participant and 2000 for an anonymous caller.
func (h *OrderHandlers) summary(w http.ResponseWriter, r *http.Request) {
	order, lookupErr := h.lookup(r)
	if lookupErr != nil {
		WriteError(w, r, lookupErr)
		return
	}

	if _, authErr := RequireParticipant(r.Context(), r, h.Pool, order.ID); authErr != nil {
		WriteError(w, r, authErr)
		return
	}

	items, err := db.ListOrderItems(r.Context(), h.Pool, order.ID)
	if err != nil {
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: err})
		return
	}

	externalIDs, err := db.ExternalIDsForItems(r.Context(), h.Pool, menuItemIDs(items))
	if err != nil {
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: err})
		return
	}

	_ = WriteJSON(w, http.StatusOK, buildSummary(order, items, externalIDs))
}

func menuItemIDs(items []model.OrderItem) []uuid.UUID {
	seen := make(map[uuid.UUID]bool, len(items))
	ids := make([]uuid.UUID, 0, len(items))
	for i := range items {
		if !seen[items[i].MenuItemID] {
			seen[items[i].MenuItemID] = true
			ids = append(ids, items[i].MenuItemID)
		}
	}
	return ids
}

// buildSummary does the aggregation.
//
// Pure, taking everything it needs as arguments, so the arithmetic can be
// tested without a database or a request.
func buildSummary(
	order model.Order, items []model.OrderItem, externalIDs map[uuid.UUID]string,
) summaryBody {
	body := summaryBody{
		OrderID:            order.ID.String(),
		Title:              order.Title(),
		CurrencyCode:       order.CurrencyCode,
		Aggregated:         make([]aggregatedLine, 0, len(items)),
		PerPerson:          make([]personLine, 0, 8),
		DeliveryFeeCents:   order.DeliveryFeeCents,
		MinOrderValueCents: order.MinOrderValueCents,
	}

	aggregated := aggregate(items, externalIDs)
	body.Aggregated = aggregated
	for _, line := range aggregated {
		body.ItemTotalCents += line.TotalCents
	}

	body.PerPerson = perPerson(items)

	body.GrandTotalCents = body.ItemTotalCents
	if order.DeliveryFeeCents != nil {
		body.GrandTotalCents += *order.DeliveryFeeCents
	}
	if order.MinOrderValueCents != nil {
		body.BelowMinimum = body.ItemTotalCents < *order.MinOrderValueCents
	}

	body.PlainText = plainText(aggregated)
	return body
}

// aggregate groups identical orders together.
//
// The key is the menu item, the exact set of selected modification names, and
// the normalised note -- trimmed and lower-cased -- exactly as docs/04_api.md
// specifies. Two people who both wanted a Döner without onions are one line
// saying "2x"; one of them wanting extra sauce makes it two lines.
//
// The unit price is part of the key too, though the specification does not say
// so in as many words. ADR-0009 does: the same dish ordered before and after a
// price correction "appears twice in the summary at two prices, because the
// aggregation key includes the item and its modifications but the rows carry
// different unit prices". Grouping them would produce a line whose count times
// unit price is not its total.
func aggregate(items []model.OrderItem, externalIDs map[uuid.UUID]string) []aggregatedLine {
	type key struct {
		menuItemID    uuid.UUID
		modifications string
		note          string
		unitPrice     int64
	}

	lines := make(map[key]*aggregatedLine, len(items))
	order := make([]key, 0, len(items))

	for i := range items {
		item := items[i]

		names := modificationNames(item)
		k := key{
			menuItemID:    item.MenuItemID,
			modifications: strings.Join(names, "\x00"),
			note:          normaliseNote(item.Note),
			unitPrice:     item.UnitPriceCents,
		}

		line, ok := lines[k]
		if !ok {
			line = &aggregatedLine{
				ItemName:       item.ItemName,
				ExternalID:     externalIDs[item.MenuItemID],
				Modifications:  names,
				UnitPriceCents: item.UnitPriceCents,
			}
			if trimmed := strings.TrimSpace(item.Note); trimmed != "" {
				note := trimmed
				line.Note = &note
			}
			lines[k] = line
			order = append(order, k)
		}

		line.Count += item.Quantity

		// The line total is summed from the items rather than multiplied out,
		// so the modification deltas are counted once per item exactly as
		// LineTotalCents computes them.
		line.TotalCents += item.LineTotalCents()
	}

	out := make([]aggregatedLine, 0, len(order))
	for _, k := range order {
		out = append(out, *lines[k])
	}

	// Sorted by item number then name, the same rule the menu uses, so the
	// summary reads in the order the menu is printed and can be dictated
	// straight down the page.
	sort.SliceStable(out, func(i, j int) bool {
		return lessByMenuOrder(out[i], out[j])
	})
	return out
}

// lessByMenuOrder implements F4.6 for two summary lines.
func lessByMenuOrder(a, b aggregatedLine) bool {
	an, aNumeric := numericID(a.ExternalID)
	bn, bNumeric := numericID(b.ExternalID)

	switch {
	case a.ExternalID == "" && b.ExternalID != "":
		return false
	case a.ExternalID != "" && b.ExternalID == "":
		return true
	case aNumeric && bNumeric && an != bn:
		return an < bn
	case aNumeric != bNumeric:
		return aNumeric
	case a.ExternalID != b.ExternalID:
		return a.ExternalID < b.ExternalID
	default:
		return strings.ToLower(a.ItemName) < strings.ToLower(b.ItemName)
	}
}

func numericID(value string) (int64, bool) {
	if value == "" {
		return 0, false
	}
	var n int64
	for _, r := range value {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int64(r-'0')
	}
	return n, true
}

// modificationNames is the sorted set of selected modification names.
//
// Sorted, so that two items differing only in the order the boxes were ticked
// aggregate together -- which they should, being the same food.
func modificationNames(item model.OrderItem) []string {
	names := make([]string, 0, len(item.Modifications))
	for _, m := range item.Modifications {
		names = append(names, m.Name)
	}
	sort.Strings(names)
	return names
}

// normaliseNote trims and lower-cases, per docs/04_api.md.
func normaliseNote(note string) string {
	return strings.ToLower(strings.TrimSpace(note))
}

// perPerson groups the items by who ordered them.
func perPerson(items []model.OrderItem) []personLine {
	lines := make(map[uuid.UUID]*personLine, 8)
	order := make([]uuid.UUID, 0, 8)

	for i := range items {
		item := items[i]

		line, ok := lines[item.UserID]
		if !ok {
			line = &personLine{
				UserID:      item.UserID.String(),
				DisplayName: item.UserName,
				Items:       make([]orderItemBody, 0, 4),
			}
			lines[item.UserID] = line
			order = append(order, item.UserID)
		}

		line.Items = append(line.Items, publicOrderItem(item))
		if item.Paid {
			line.PaidCents += item.LineTotalCents()
			continue
		}
		line.TotalCents += item.LineTotalCents()
	}

	out := make([]personLine, 0, len(order))
	for _, id := range order {
		out = append(out, *lines[id])
	}

	// By name, so the list is stable and readable rather than following
	// whoever happened to order first.
	sort.SliceStable(out, func(i, j int) bool {
		return strings.ToLower(out[i].DisplayName) < strings.ToLower(out[j].DisplayName)
	})
	return out
}

// plainText renders the order for reading down a telephone.
//
// One line per aggregated entry, in the same order as the list, with the
// modifications and note appended in brackets. Deliberately plain: it is meant
// to be read aloud, and it is also what somebody pastes into a chat window.
func plainText(lines []aggregatedLine) string {
	var b strings.Builder
	for _, line := range lines {
		fmt.Fprintf(&b, "%dx ", line.Count)
		if line.ExternalID != "" {
			fmt.Fprintf(&b, "%s ", line.ExternalID)
		}
		b.WriteString(line.ItemName)

		details := make([]string, 0, len(line.Modifications)+1)
		details = append(details, line.Modifications...)
		if line.Note != nil {
			details = append(details, *line.Note)
		}
		if len(details) > 0 {
			fmt.Fprintf(&b, " (%s)", strings.Join(details, ", "))
		}
		b.WriteByte('\n')
	}
	return b.String()
}
