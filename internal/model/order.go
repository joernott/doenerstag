package model

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Fulfilment is how the food arrives.
const (
	FulfilmentPickup   = "pickup"
	FulfilmentDelivery = "delivery"
)

// Order is one group food order.
//
// The currency, minimum order value and delivery fee are copies taken from the
// restaurant when it was chosen (F5.11), not references. A later edit to the
// restaurant does not change an order that is already open, for the same reason
// item prices are snapshotted: the order records what was agreed at the time.
type Order struct {
	ID        uuid.UUID
	CreatorID uuid.UUID

	// CreatorName is the creator's display label, joined in for the header.
	CreatorName string

	RestaurantID   uuid.UUID
	RestaurantName string
	RestaurantLogo *uuid.UUID

	Fulfilment   string
	FulfilmentAt time.Time
	DeadlineAt   time.Time

	MoneyCollector string
	PickupPerson   string

	CurrencyCode       string
	MinOrderValueCents *int64
	DeliveryFeeCents   *int64

	// ItemCount is present in every shape, anonymous included, so the frontend
	// does not have to branch on its absence.
	ItemCount int

	CreatedAt time.Time
	UpdatedAt time.Time
}

// Active reports whether items may still be added or changed.
//
// F5.5: an order is active while now is before the deadline and expired
// afterwards. There is no manual state transition, so this is the only place
// the question is answered.
func (o Order) Active(now time.Time) bool { return now.Before(o.DeadlineAt) }

// Status is the derived state, for the API.
func (o Order) Status(now time.Time) string {
	if o.Active(now) {
		return "active"
	}
	return "expired"
}

// Title is the computed display title (F5.6).
//
// An order has no stored title: it is the restaurant and the fulfilment time,
// which is what people call it anyway. Computing it means a renamed restaurant
// renames its orders, which is right -- unlike a price, the restaurant's name
// is not part of what was agreed.
func (o Order) Title() string {
	return fmt.Sprintf("%s — %s", o.RestaurantName,
		o.FulfilmentAt.Format("2006-01-02 15:04"))
}

// OrderItem is one person's line in an order.
//
// ItemName and UnitPriceCents are snapshots taken when the item was added, and
// are authoritative for display and arithmetic. MenuItemID is kept for grouping
// in the summary and for the "still on the menu?" check, and is never read for
// a name or a price. See docs/adr/0009-snapshot-prices-on-order-items.md.
type OrderItem struct {
	ID      uuid.UUID
	OrderID uuid.UUID

	UserID uuid.UUID
	// UserName is the owner's display label, joined in for the item list.
	UserName string

	MenuItemID     uuid.UUID
	Quantity       int
	ItemName       string
	UnitPriceCents int64
	Note           string

	Modifications []OrderItemModification

	CreatedAt time.Time
	UpdatedAt time.Time
}

// LineTotalCents is quantity times the unit price plus every price delta.
//
// Computed entirely from the snapshots, per the ADR. Nothing here reads the
// current menu.
func (i OrderItem) LineTotalCents() int64 {
	unit := i.UnitPriceCents
	for _, m := range i.Modifications {
		unit += m.PriceDeltaCents
	}
	return int64(i.Quantity) * unit
}

// OrderItemModification is one selected modification, snapshotted.
//
// ModificationID is nullable: removing a modification from a menu must not
// damage a historical order, and the snapshot is what matters.
type OrderItemModification struct {
	ID              uuid.UUID
	OrderItemID     uuid.UUID
	ModificationID  *uuid.UUID
	Name            string
	PriceDeltaCents int64
}
