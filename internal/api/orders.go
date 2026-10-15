package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/joernott/doenerstag/internal/db"
	"github.com/joernott/doenerstag/internal/model"
	"github.com/joernott/doenerstag/internal/sse"
)

// Field limits, matching migration 000007.
const (
	MaxOrderNoteLength   = 500
	MaxOrderItemQuantity = 99
)

// OrderHandlers serves orders and their items.
type OrderHandlers struct {
	Pool *pgxpool.Pool

	// Events carries live updates. Nil disables streaming, which is what a test
	// that does not care about it uses.
	Events *sse.Registry

	// Logger records what must not fail a request. Nil discards.
	Logger *zerolog.Logger

	// Now is the clock. Everything about an order turns on it -- whether it is
	// active, whether it may still be edited -- so a test needs to move it.
	Now func() time.Time
}

// log returns a logger carrying the request's correlation ID.
func (h *OrderHandlers) log(r *http.Request) *zerolog.Logger {
	return requestLogger(h.Logger, r)
}

func (h *OrderHandlers) now() time.Time {
	return nowFrom(h.Now)
}

// Register adds the order routes.
func (h *OrderHandlers) Register(r *Router) {
	r.HandleFunc(http.MethodGet, "/orders", h.list)
	r.HandleFunc(http.MethodPost, "/orders", h.create)
	r.HandleFunc(http.MethodGet, "/orders/:id", h.get)
	r.HandleFunc(http.MethodPatch, "/orders/:id", h.patch)
	r.HandleFunc(http.MethodDelete, "/orders/:id", h.remove)

	r.HandleFunc(http.MethodPost, "/orders/:id/pickup-person", h.claimPickup)
	r.HandleFunc(http.MethodPost, "/orders/:id/money-collector", h.claimMoneyCollection)
	r.HandleFunc(http.MethodDelete, "/orders/:id/pickup-person", h.releasePickup)
	r.HandleFunc(http.MethodDelete, "/orders/:id/money-collector", h.releaseMoneyCollection)

	r.HandleFunc(http.MethodGet, "/orders/:id/summary", h.summary)
	r.HandleFunc(http.MethodGet, "/orders/:id/events", h.events)

	r.HandleFunc(http.MethodPost, "/orders/:id/items", h.addItem)
	r.HandleFunc(http.MethodPatch, "/orders/:id/items/:iid", h.patchItem)
	r.HandleFunc(http.MethodDelete, "/orders/:id/items/:iid", h.deleteItem)
	r.HandleFunc(http.MethodPut, "/orders/:id/items/:iid/paid", h.setItemPaid)
}

// orderHeaderBody is what every caller sees, anonymous included.
//
// It carries no item data of any kind. The authenticated shape embeds this and
// adds the items, so the anonymous response cannot accidentally gain a field:
// there is no field to gain, because the struct does not have one. See
// docs/adr/0011-tiered-order-visibility.md.
type orderHeaderBody struct {
	ID    string `json:"id"`
	Title string `json:"title"`

	RestaurantID   string  `json:"restaurant_id"`
	RestaurantName string  `json:"restaurant_name"`
	RestaurantLogo *string `json:"restaurant_logo_image_id"`

	Fulfilment   string `json:"fulfilment"`
	FulfilmentAt string `json:"fulfilment_at"`
	DeadlineAt   string `json:"deadline_at"`
	Status       string `json:"status"`

	// There is deliberately no creator here.
	//
	// An anonymous response names no user at all, the creator included, and
	// this struct is what an anonymous caller is served -- so the rule is
	// enforced by the type rather than by remembering to blank a field. The
	// creator lives on orderDetailBody, which only an authenticated caller
	// receives. See docs/adr/0011-tiered-order-visibility.md.

	// Neither is the money collector nor the person fetching the food: both are
	// accounts, and an account is a named person, so the rule that keeps the
	// creator out of this struct keeps them out of it. They are on
	// orderDetailBody.

	CurrencyCode       string `json:"currency_code"`
	MinOrderValueCents *int64 `json:"min_order_value_cents"`
	DeliveryFeeCents   *int64 `json:"delivery_fee_cents"`

	// ItemCount is in both shapes so the frontend never branches on its
	// absence.
	ItemCount int `json:"item_count"`

	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// orderDetailBody is the authenticated shape: the list entry -- the header and
// the three people -- and the items with their totals.
//
// The list entry is embedded rather than its six fields repeated: they are the
// same fields in the same order, so the JSON is unchanged, and an addition to
// one shape cannot miss the other (20.22).
type orderDetailBody struct {
	orderListEntry

	Items           []orderItemBody `json:"items"`
	ItemTotalCents  int64           `json:"item_total_cents"`
	GrandTotalCents int64           `json:"grand_total_cents"`
	BelowMinimum    bool            `json:"below_minimum"`

	// The item total split by whether each line has been ticked as settled:
	// UnpaidTotalCents + PaidTotalCents == ItemTotalCents, always. Summed here
	// rather than by each page, so the order page and the summary cannot come to
	// disagree about what is still owed.
	UnpaidTotalCents int64 `json:"unpaid_total_cents"`
	PaidTotalCents   int64 `json:"paid_total_cents"`
}

type orderItemBody struct {
	ID       string `json:"id"`
	UserID   string `json:"user_id"`
	UserName string `json:"user_name"`

	MenuItemID     string                  `json:"menu_item_id"`
	Quantity       int                     `json:"quantity"`
	ItemName       string                  `json:"item_name"`
	UnitPriceCents int64                   `json:"unit_price_cents"`
	Note           string                  `json:"note"`
	Modifications  []orderModificationBody `json:"modifications"`
	LineTotalCents int64                   `json:"line_total_cents"`

	// Paid is a tick against a line, not a payment: the application takes none.
	Paid bool `json:"paid"`

	CreatedAt string `json:"created_at"`
}

type orderModificationBody struct {
	ID              string  `json:"id"`
	ModificationID  *string `json:"modification_id"`
	Name            string  `json:"name"`
	PriceDeltaCents int64   `json:"price_delta_cents"`
}

func (h *OrderHandlers) publicHeader(o model.Order) orderHeaderBody {
	body := orderHeaderBody{
		ID:                 o.ID.String(),
		Title:              o.Title(),
		RestaurantID:       o.RestaurantID.String(),
		RestaurantName:     o.RestaurantName,
		Fulfilment:         o.Fulfilment,
		FulfilmentAt:       o.FulfilmentAt.UTC().Format(time.RFC3339),
		DeadlineAt:         o.DeadlineAt.UTC().Format(time.RFC3339),
		Status:             o.Status(h.now()),
		CurrencyCode:       o.CurrencyCode,
		MinOrderValueCents: o.MinOrderValueCents,
		DeliveryFeeCents:   o.DeliveryFeeCents,
		ItemCount:          o.ItemCount,
		CreatedAt:          o.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:          o.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if o.RestaurantLogo != nil {
		id := o.RestaurantLogo.String()
		body.RestaurantLogo = &id
	}
	return body
}

func publicOrderItem(i model.OrderItem) orderItemBody {
	body := orderItemBody{
		ID:             i.ID.String(),
		UserID:         i.UserID.String(),
		UserName:       i.UserName,
		MenuItemID:     i.MenuItemID.String(),
		Quantity:       i.Quantity,
		ItemName:       i.ItemName,
		UnitPriceCents: i.UnitPriceCents,
		Note:           i.Note,
		Modifications:  make([]orderModificationBody, 0, len(i.Modifications)),
		LineTotalCents: i.LineTotalCents(),
		Paid:           i.Paid,
		CreatedAt:      i.CreatedAt.UTC().Format(time.RFC3339),
	}
	for _, m := range i.Modifications {
		mod := orderModificationBody{
			ID: m.ID.String(), Name: m.Name, PriceDeltaCents: m.PriceDeltaCents,
		}
		if m.ModificationID != nil {
			id := m.ModificationID.String()
			mod.ModificationID = &id
		}
		body.Modifications = append(body.Modifications, mod)
	}
	return body
}

// orderListEntry is a list tile for a logged-in caller: the header plus who
// opened the order.
//
// docs/06_ui_ux.md shows the creator on the tile for logged-in visitors, and
// only for them. Still no item detail -- docs/04_api.md says the list never
// carries that for anybody, and this type has nowhere to put one.
type orderListEntry struct {
	orderHeaderBody
	CreatorID   string `json:"creator_id"`
	CreatorName string `json:"creator_name"`

	// The other two people an order has. Here for the same reason the creator
	// is: the tile and the live event both want them, and neither may reach an
	// anonymous subscriber. Null when nobody is doing the job, which is a thing
	// an order says rather than a thing it fails to say: the "Me!" buttons are
	// offered on exactly that.
	MoneyCollectorID   *string `json:"money_collector_id"`
	MoneyCollectorName string  `json:"money_collector_name"`
	PickupPersonID     *string `json:"pickup_person_id"`
	PickupPersonName   string  `json:"pickup_person_name"`
}

// listEntry is the authenticated shape of one order, built in one place so the
// list and the live event cannot come to disagree about what it contains.
func (h *OrderHandlers) listEntry(o model.Order) orderListEntry {
	return orderListEntry{
		orderHeaderBody:    h.publicHeader(o),
		CreatorID:          o.CreatorID.String(),
		CreatorName:        o.CreatorName,
		MoneyCollectorID:   idString(o.MoneyCollectorID),
		MoneyCollectorName: o.MoneyCollectorName,
		PickupPersonID:     idString(o.PickupPersonID),
		PickupPersonName:   o.PickupPersonName,
	}
}

// list serves the order list.
//
// Item detail is absent for everybody, so there is no branch for it at all --
// which is the safest way to implement "never". The creator does branch, on the
// same rule as the detail endpoint: an anonymous response names no user.
func (h *OrderHandlers) list(w http.ResponseWriter, r *http.Request) {
	orders, err := db.ListOrders(r.Context(), h.Pool, h.now())
	if err != nil {
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: err})
		return
	}

	if PrincipalFrom(r.Context()) == nil {
		bodies := make([]orderHeaderBody, 0, len(orders))
		for i := range orders {
			bodies = append(bodies, h.publicHeader(orders[i]))
		}
		_ = WriteJSON(w, http.StatusOK, map[string]any{"orders": bodies})
		return
	}

	bodies := make([]orderListEntry, 0, len(orders))
	for i := range orders {
		bodies = append(bodies, h.listEntry(orders[i]))
	}
	_ = WriteJSON(w, http.StatusOK, map[string]any{"orders": bodies})
}

// get serves one order in whichever shape the caller is entitled to.
//
// The split is here, in the handler, and it is a choice between two response
// types rather than a set of fields blanked out afterwards. An anonymous caller
// is served orderHeaderBody, which has nowhere to put an item.
func (h *OrderHandlers) get(w http.ResponseWriter, r *http.Request) {
	order, lookupErr := h.lookupOrder(r)
	if lookupErr != nil {
		WriteError(w, r, lookupErr)
		return
	}

	if PrincipalFrom(r.Context()) == nil {
		_ = WriteJSON(w, http.StatusOK, h.publicHeader(order))
		return
	}

	h.writeDetail(w, r, order, http.StatusOK)
}

// writeDetail serves the authenticated shape.
func (h *OrderHandlers) writeDetail(w http.ResponseWriter, r *http.Request, order model.Order, status int) {
	items, err := db.ListOrderItems(r.Context(), h.Pool, order.ID)
	if err != nil {
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: err})
		return
	}

	body := orderDetailBody{
		orderListEntry: h.listEntry(order),
		Items:          make([]orderItemBody, 0, len(items)),
	}
	for i := range items {
		rendered := publicOrderItem(items[i])
		body.ItemTotalCents += rendered.LineTotalCents
		if rendered.Paid {
			body.PaidTotalCents += rendered.LineTotalCents
		} else {
			body.UnpaidTotalCents += rendered.LineTotalCents
		}
		body.Items = append(body.Items, rendered)
	}

	body.GrandTotalCents = body.ItemTotalCents
	if order.DeliveryFeeCents != nil {
		body.GrandTotalCents += *order.DeliveryFeeCents
	}
	if order.MinOrderValueCents != nil {
		body.BelowMinimum = body.ItemTotalCents < *order.MinOrderValueCents
	}

	// The count in the header comes from the query; recomputing it here from
	// the rows just read keeps the two halves of the response consistent even
	// if an item is added between the two queries.
	body.ItemCount = len(items)

	_ = WriteJSON(w, status, body)
}

type createOrderRequest struct {
	RestaurantID string `json:"restaurant_id"`
	Fulfilment   string `json:"fulfilment"`
	FulfilmentAt string `json:"fulfilment_at"`
	DeadlineAt   string `json:"deadline_at"`
	// Accounts, as identifiers. The empty string is "nobody", which is what a
	// form that offers "nobody" as its first option submits.
	MoneyCollectorID string `json:"money_collector_id"`
	PickupPersonID   string `json:"pickup_person_id"`
}

func (h *OrderHandlers) create(w http.ResponseWriter, r *http.Request) {
	principal, authErr := RequireAuthenticated(r)
	if authErr != nil {
		WriteError(w, r, authErr)
		return
	}

	var body createOrderRequest
	if !decodeJSON(w, r, &body) {
		return
	}

	order, err := h.validateNewOrder(r, body, principal.User.ID)
	if err != nil {
		WriteError(w, r, err)
		return
	}

	created, dbErr := db.CreateOrder(r.Context(), h.Pool, order)
	switch {
	case errors.Is(dbErr, db.ErrNotFound):
		// CreateOrder copies the money terms from the restaurant in the same
		// statement, so a restaurant that does not exist inserts nothing.
		WriteError(w, r, &Error{
			Code:   CodeInvalidField,
			Field:  "restaurant_id",
			Detail: "no restaurant has this id",
		})
		return
	case dbErr != nil:
		WriteError(w, r, dbError(dbErr))
		return
	}

	h.writeDetail(w, r, created, http.StatusCreated)
}

// validateNewOrder turns a creation request into a new order, or says what is
// wrong with it. The counterpart of validateOrderPatch, which does the same
// for a change to an existing one.
func (h *OrderHandlers) validateNewOrder(
	r *http.Request, body createOrderRequest, creator uuid.UUID,
) (db.NewOrder, *Error) {
	restaurantID, err := requiredUUID(body.RestaurantID, "restaurant_id")
	if err != nil {
		return db.NewOrder{}, err
	}
	fulfilment, err := validateFulfilment(body.Fulfilment)
	if err != nil {
		return db.NewOrder{}, err
	}
	fulfilmentAt, err := requiredTime(body.FulfilmentAt, "fulfilment_at")
	if err != nil {
		return db.NewOrder{}, err
	}
	deadlineAt, err := requiredTime(body.DeadlineAt, "deadline_at")
	if err != nil {
		return db.NewOrder{}, err
	}
	if err := checkDeadline(deadlineAt, fulfilmentAt); err != nil {
		return db.NewOrder{}, err
	}

	// And the deadline must not already have passed. An order created with a
	// deadline in the past is born closed: F6.6 makes an expired order
	// read-only, so nobody can add an item to it, its creator cannot edit it
	// back into life, and the only thing left to do with it is delete it.
	// Refusing is the only useful answer.
	//
	// Only on creation. Moving an existing order's deadline into the past is a
	// different thing -- it is how a creator closes one early -- and it stays
	// allowed.
	if !deadlineAt.After(h.now()) {
		return db.NewOrder{}, &Error{Code: CodeDeadlineInThePast, Field: "deadline_at"}
	}

	moneyCollector, err := h.optionalUserRef(r.Context(), body.MoneyCollectorID, "money_collector_id")
	if err != nil {
		return db.NewOrder{}, err
	}
	pickupPerson, err := h.optionalUserRef(r.Context(), body.PickupPersonID, "pickup_person_id")
	if err != nil {
		return db.NewOrder{}, err
	}

	return db.NewOrder{
		CreatorID:        creator,
		RestaurantID:     restaurantID,
		Fulfilment:       fulfilment,
		FulfilmentAt:     fulfilmentAt,
		DeadlineAt:       deadlineAt,
		MoneyCollectorID: moneyCollector,
		PickupPersonID:   pickupPerson,
	}, nil
}

// checkDeadline is F5.3: the deadline lies strictly before the fulfilment
// time. Enforced here as well as in the frontend, because the API is
// documented for third-party use and cannot rely on the frontend having
// checked; creating and changing an order both ask.
func checkDeadline(deadlineAt, fulfilmentAt time.Time) *Error {
	if !deadlineAt.Before(fulfilmentAt) {
		return &Error{Code: CodeDeadlineAfterFulfil, Field: "deadline_at"}
	}
	return nil
}

type patchOrderRequest struct {
	RestaurantID     *string `json:"restaurant_id"`
	Fulfilment       *string `json:"fulfilment"`
	FulfilmentAt     *string `json:"fulfilment_at"`
	DeadlineAt       *string `json:"deadline_at"`
	MoneyCollectorID *string `json:"money_collector_id"`
	PickupPersonID   *string `json:"pickup_person_id"`
}

// patch updates an order. Creator or administrator, and only while active.
func (h *OrderHandlers) patch(w http.ResponseWriter, r *http.Request) {
	order, lookupErr := h.lookupOrder(r)
	if lookupErr != nil {
		WriteError(w, r, lookupErr)
		return
	}

	principal, authErr := RequireCreator(r, order.CreatorID)
	if authErr != nil {
		WriteError(w, r, authErr)
		return
	}

	// F5.7: the creator edits the order while it is active. After the deadline
	// it is read-only, and 4001 is the documented code.
	if !order.Active(h.now()) {
		WriteError(w, r, &Error{Code: CodeOrderClosed})
		return
	}

	var body patchOrderRequest
	if !decodeJSON(w, r, &body) {
		return
	}

	update, err := h.validateOrderPatch(r, order, body)
	if err != nil {
		WriteError(w, r, err)
		return
	}

	updated, dbErr := db.UpdateOrder(r.Context(), h.Pool, order.ID, principal.User.ID, update)
	if dbErr != nil {
		WriteError(w, r, dbError(dbErr))
		return
	}

	h.publishOrderChange(updated)

	h.writeDetail(w, r, updated, http.StatusOK)
}

func (h *OrderHandlers) validateOrderPatch(
	r *http.Request, order model.Order, body patchOrderRequest,
) (db.OrderUpdate, *Error) {
	var update db.OrderUpdate

	if body.RestaurantID != nil {
		// F5.8: once the order has at least one item, the restaurant can no
		// longer be changed. The snapshots on those items came from the old
		// restaurant's menu, and switching underneath them would leave an order
		// full of food the new restaurant does not sell.
		count, err := db.CountOrderItems(r.Context(), h.Pool, order.ID)
		if err != nil {
			return update, &Error{Code: CodeDatabaseUnavailable, Cause: err}
		}
		if count > 0 {
			return update, &Error{Code: CodeRestaurantLocked}
		}

		id, parseErr := requiredUUID(*body.RestaurantID, "restaurant_id")
		if parseErr != nil {
			return update, parseErr
		}
		exists, err := db.RestaurantExists(r.Context(), h.Pool, id)
		if err != nil {
			return update, &Error{Code: CodeDatabaseUnavailable, Cause: err}
		}
		if !exists {
			return update, &Error{
				Code:   CodeInvalidField,
				Field:  "restaurant_id",
				Detail: "no restaurant has this id",
			}
		}
		update.RestaurantID = &id
	}

	if body.Fulfilment != nil {
		fulfilment, err := validateFulfilment(*body.Fulfilment)
		if err != nil {
			return update, err
		}
		update.Fulfilment = &fulfilment
	}

	// The deadline and the fulfilment time are checked against each other using
	// whichever of the two is being changed, falling back to the stored value.
	// Checking only the supplied field would let a caller move the deadline
	// past a fulfilment time they did not send.
	fulfilmentAt := order.FulfilmentAt
	deadlineAt := order.DeadlineAt

	if body.FulfilmentAt != nil {
		parsed, err := requiredTime(*body.FulfilmentAt, "fulfilment_at")
		if err != nil {
			return update, err
		}
		fulfilmentAt = parsed
		update.FulfilmentAt = &parsed
	}
	if body.DeadlineAt != nil {
		parsed, err := requiredTime(*body.DeadlineAt, "deadline_at")
		if err != nil {
			return update, err
		}
		deadlineAt = parsed
		update.DeadlineAt = &parsed
	}
	if err := checkDeadline(deadlineAt, fulfilmentAt); err != nil {
		return update, err
	}

	// Present-and-empty clears the field; absent leaves it alone. The double
	// pointer is what carries that distinction as far as the UPDATE.
	if body.MoneyCollectorID != nil {
		ref, err := h.optionalUserRef(r.Context(), *body.MoneyCollectorID, "money_collector_id")
		if err != nil {
			return update, err
		}
		update.MoneyCollectorID = &ref
	}
	if body.PickupPersonID != nil {
		ref, err := h.optionalUserRef(r.Context(), *body.PickupPersonID, "pickup_person_id")
		if err != nil {
			return update, err
		}
		update.PickupPersonID = &ref
	}

	return update, nil
}

// remove deletes an order and everything below it.
//
// F5.9: the creator and the administrator may delete an order, and unlike
// editing this is allowed after the deadline -- an expired order is read-only
// for its contents, but the administrator can still clear it away.
func (h *OrderHandlers) remove(w http.ResponseWriter, r *http.Request) {
	order, lookupErr := h.lookupOrder(r)
	if lookupErr != nil {
		WriteError(w, r, lookupErr)
		return
	}
	if _, authErr := RequireCreator(r, order.CreatorID); authErr != nil {
		WriteError(w, r, authErr)
		return
	}

	if err := db.DeleteOrder(r.Context(), h.Pool, order.ID); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			WriteError(w, r, &Error{Code: CodeNotFound})
			return
		}
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: err})
		return
	}

	h.publishOrderGone(order.ID, sse.EventOrderDeleted)

	w.WriteHeader(http.StatusNoContent)
}

// lookupOrder resolves the :id parameter to an order.
func (h *OrderHandlers) lookupOrder(r *http.Request) (model.Order, *Error) {
	order, err := loadParam(r, h.Pool, "id", db.OrderByID)
	if err != nil {
		return model.Order{}, err
	}
	return order, nil
}

func validateFulfilment(value string) (string, *Error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case model.FulfilmentPickup:
		return model.FulfilmentPickup, nil
	case model.FulfilmentDelivery:
		return model.FulfilmentDelivery, nil
	case "":
		return "", &Error{Code: CodeMissingField, Field: "fulfilment"}
	default:
		return "", &Error{
			Code:   CodeInvalidField,
			Field:  "fulfilment",
			Detail: "the fulfilment must be pickup or delivery",
		}
	}
}

// optionalUserRef parses an account reference that may be absent or cleared.
//
// The empty string is "nobody", not a bad identifier: it is what a select whose
// first option is "nobody" submits, and what clearing the field means. A
// non-empty value must parse and must name an account that exists, because an
// order pointing at an account that does not is a foreign key violation the
// caller would see as a database error rather than as the mistake it is.
func (h *OrderHandlers) optionalUserRef(
	ctx context.Context, value, field string,
) (*uuid.UUID, *Error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, nil
	}

	id, err := uuid.Parse(trimmed)
	if err != nil {
		return nil, invalidID(field)
	}

	// The placeholder that owns what deleted accounts left behind is a row in
	// the table and not a person. Nobody fetches food.
	if id == model.DeletedUserID {
		return nil, &Error{Code: CodeInvalidField, Field: field, Detail: "this is not a person"}
	}

	switch _, dbErr := db.UserByID(ctx, h.Pool, id); {
	case errors.Is(dbErr, db.ErrNotFound):
		return nil, &Error{Code: CodeInvalidField, Field: field, Detail: "no account has this id"}
	case dbErr != nil:
		return nil, &Error{Code: CodeDatabaseUnavailable, Cause: dbErr}
	}
	return &id, nil
}
