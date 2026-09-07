package api

import (
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/joernott/doenerstag/internal/db"
	"github.com/joernott/doenerstag/internal/logging"
	"github.com/joernott/doenerstag/internal/model"
	"github.com/joernott/doenerstag/internal/sse"
)

// Field limits, matching migration 000007.
const (
	MaxMoneyCollectorLength = 200
	MaxPickupPersonLength   = 200
	MaxOrderNoteLength      = 500
	MaxOrderItemQuantity    = 99
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
	if h.Logger == nil {
		discard := zerolog.Nop()
		return &discard
	}
	logger := h.Logger.With().
		Str(logging.FieldRequestID, RequestIDFrom(r.Context())).
		Logger()
	return &logger
}

func (h *OrderHandlers) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

// Register adds the order routes.
func (h *OrderHandlers) Register(r *Router) {
	r.HandleFunc(http.MethodGet, "/orders", h.list)
	r.HandleFunc(http.MethodPost, "/orders", h.create)
	r.HandleFunc(http.MethodGet, "/orders/:id", h.get)
	r.HandleFunc(http.MethodPatch, "/orders/:id", h.patch)
	r.HandleFunc(http.MethodDelete, "/orders/:id", h.remove)

	r.HandleFunc(http.MethodGet, "/orders/:id/summary", h.summary)
	r.HandleFunc(http.MethodGet, "/orders/:id/events", h.events)

	r.HandleFunc(http.MethodPost, "/orders/:id/items", h.addItem)
	r.HandleFunc(http.MethodPatch, "/orders/:id/items/:iid", h.patchItem)
	r.HandleFunc(http.MethodDelete, "/orders/:id/items/:iid", h.deleteItem)
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

	MoneyCollector string `json:"money_collector"`
	PickupPerson   string `json:"pickup_person"`

	CurrencyCode       string `json:"currency_code"`
	MinOrderValueCents *int64 `json:"min_order_value_cents"`
	DeliveryFeeCents   *int64 `json:"delivery_fee_cents"`

	// ItemCount is in both shapes so the frontend never branches on its
	// absence.
	ItemCount int `json:"item_count"`

	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// orderDetailBody is the authenticated shape: the header, the creator and the
// items.
type orderDetailBody struct {
	orderHeaderBody

	// The creator is here rather than in the header, because naming the person
	// who opened an order is still naming a person.
	CreatorID   string `json:"creator_id"`
	CreatorName string `json:"creator_name"`

	Items           []orderItemBody `json:"items"`
	ItemTotalCents  int64           `json:"item_total_cents"`
	GrandTotalCents int64           `json:"grand_total_cents"`
	BelowMinimum    bool            `json:"below_minimum"`
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
		MoneyCollector:     o.MoneyCollector,
		PickupPerson:       o.PickupPerson,
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
		bodies = append(bodies, orderListEntry{
			orderHeaderBody: h.publicHeader(orders[i]),
			CreatorID:       orders[i].CreatorID.String(),
			CreatorName:     orders[i].CreatorName,
		})
	}
	_ = WriteJSON(w, http.StatusOK, map[string]any{"orders": bodies})
}

// get serves one order in whichever shape the caller is entitled to.
//
// The split is here, in the handler, and it is a choice between two response
// types rather than a set of fields blanked out afterwards. An anonymous caller
// is served orderHeaderBody, which has nowhere to put an item.
func (h *OrderHandlers) get(w http.ResponseWriter, r *http.Request) {
	order, lookupErr := h.lookup(r)
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
		orderHeaderBody: h.publicHeader(order),
		CreatorID:       order.CreatorID.String(),
		CreatorName:     order.CreatorName,
		Items:           make([]orderItemBody, 0, len(items)),
	}
	for i := range items {
		rendered := publicOrderItem(items[i])
		body.ItemTotalCents += rendered.LineTotalCents
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
	RestaurantID   string `json:"restaurant_id"`
	Fulfilment     string `json:"fulfilment"`
	FulfilmentAt   string `json:"fulfilment_at"`
	DeadlineAt     string `json:"deadline_at"`
	MoneyCollector string `json:"money_collector"`
	PickupPerson   string `json:"pickup_person"`
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

	restaurantID, err := requiredUUID(body.RestaurantID, "restaurant_id")
	if err != nil {
		WriteError(w, r, err)
		return
	}

	fulfilment, err := validateFulfilment(body.Fulfilment)
	if err != nil {
		WriteError(w, r, err)
		return
	}

	fulfilmentAt, err := requiredTime(body.FulfilmentAt, "fulfilment_at")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	deadlineAt, err := requiredTime(body.DeadlineAt, "deadline_at")
	if err != nil {
		WriteError(w, r, err)
		return
	}

	// F5.3: the deadline must lie strictly before the fulfilment time. Enforced
	// here as well as in the frontend, because the API is documented for
	// third-party use and cannot rely on the frontend having checked.
	if !deadlineAt.Before(fulfilmentAt) {
		WriteError(w, r, &Error{Code: CodeDeadlineAfterFulfil, Field: "deadline_at"})
		return
	}

	// And it must not already have passed. An order created with a deadline in
	// the past is born closed: F6.6 makes an expired order read-only, so nobody
	// can add an item to it, its creator cannot edit it back into life, and the
	// only thing left to do with it is delete it. Refusing is the only useful
	// answer.
	//
	// Only on creation. Moving an existing order's deadline into the past is a
	// different thing -- it is how a creator closes one early -- and it stays
	// allowed.
	if !deadlineAt.After(h.now()) {
		WriteError(w, r, &Error{Code: CodeDeadlineInThePast, Field: "deadline_at"})
		return
	}

	if err := validateFreeText(body.MoneyCollector, "money_collector", MaxMoneyCollectorLength); err != nil {
		WriteError(w, r, err)
		return
	}
	if err := validateFreeText(body.PickupPerson, "pickup_person", MaxPickupPersonLength); err != nil {
		WriteError(w, r, err)
		return
	}

	created, dbErr := db.CreateOrder(r.Context(), h.Pool, db.NewOrder{
		CreatorID:      principal.User.ID,
		RestaurantID:   restaurantID,
		Fulfilment:     fulfilment,
		FulfilmentAt:   fulfilmentAt,
		DeadlineAt:     deadlineAt,
		MoneyCollector: strings.TrimSpace(body.MoneyCollector),
		PickupPerson:   strings.TrimSpace(body.PickupPerson),
	})
	switch {
	case errors.Is(dbErr, db.ErrNotFound):
		WriteError(w, r, &Error{
			Code:   CodeInvalidField,
			Field:  "restaurant_id",
			Detail: "no restaurant has this id",
		})
		return
	case dbErr != nil:
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: dbErr})
		return
	}

	h.writeDetail(w, r, created, http.StatusCreated)
}

type patchOrderRequest struct {
	RestaurantID   *string `json:"restaurant_id"`
	Fulfilment     *string `json:"fulfilment"`
	FulfilmentAt   *string `json:"fulfilment_at"`
	DeadlineAt     *string `json:"deadline_at"`
	MoneyCollector *string `json:"money_collector"`
	PickupPerson   *string `json:"pickup_person"`
}

// patch updates an order. Creator or administrator, and only while active.
func (h *OrderHandlers) patch(w http.ResponseWriter, r *http.Request) {
	order, lookupErr := h.lookup(r)
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
	switch {
	case errors.Is(dbErr, db.ErrNotFound):
		WriteError(w, r, &Error{Code: CodeNotFound})
		return
	case dbErr != nil:
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: dbErr})
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
	if !deadlineAt.Before(fulfilmentAt) {
		return update, &Error{Code: CodeDeadlineAfterFulfil, Field: "deadline_at"}
	}

	if body.MoneyCollector != nil {
		if err := validateFreeText(*body.MoneyCollector, "money_collector", MaxMoneyCollectorLength); err != nil {
			return update, err
		}
		value := strings.TrimSpace(*body.MoneyCollector)
		update.MoneyCollector = &value
	}
	if body.PickupPerson != nil {
		if err := validateFreeText(*body.PickupPerson, "pickup_person", MaxPickupPersonLength); err != nil {
			return update, err
		}
		value := strings.TrimSpace(*body.PickupPerson)
		update.PickupPerson = &value
	}

	return update, nil
}

// remove deletes an order and everything below it.
//
// F5.9: the creator and the administrator may delete an order, and unlike
// editing this is allowed after the deadline -- an expired order is read-only
// for its contents, but the administrator can still clear it away.
func (h *OrderHandlers) remove(w http.ResponseWriter, r *http.Request) {
	order, lookupErr := h.lookup(r)
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

// lookup resolves the :id parameter to an order.
func (h *OrderHandlers) lookup(r *http.Request) (model.Order, *Error) {
	id, err := uuid.Parse(Param(r, "id"))
	if err != nil {
		return model.Order{}, &Error{Code: CodeNotFound}
	}

	order, dbErr := db.OrderByID(r.Context(), h.Pool, id)
	switch {
	case errors.Is(dbErr, db.ErrNotFound):
		return model.Order{}, &Error{Code: CodeNotFound}
	case dbErr != nil:
		return model.Order{}, &Error{Code: CodeDatabaseUnavailable, Cause: dbErr}
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

func requiredUUID(value, field string) (uuid.UUID, *Error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return uuid.Nil, &Error{Code: CodeMissingField, Field: field}
	}
	id, err := uuid.Parse(trimmed)
	if err != nil {
		return uuid.Nil, &Error{
			Code:   CodeInvalidField,
			Field:  field,
			Detail: "this is not a valid identifier",
		}
	}
	return id, nil
}

// requiredTime parses an RFC 3339 timestamp.
//
// RFC 3339 rather than a looser format, because an order's times are the one
// thing everybody has to agree on and an ambiguous "12:30" would be read
// differently by two clients in two time zones.
func requiredTime(value, field string) (time.Time, *Error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Time{}, &Error{Code: CodeMissingField, Field: field}
	}
	parsed, err := time.Parse(time.RFC3339, trimmed)
	if err != nil {
		return time.Time{}, &Error{
			Code:   CodeInvalidField,
			Field:  field,
			Detail: "the time must be an RFC 3339 timestamp",
		}
	}
	return parsed.UTC(), nil
}

func validateFreeText(value, field string, limit int) *Error {
	if utf8.RuneCountInString(strings.TrimSpace(value)) > limit {
		return &Error{
			Code:   CodeInvalidField,
			Field:  field,
			Detail: "this value is too long",
		}
	}
	return nil
}
