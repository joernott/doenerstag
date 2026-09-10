package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/joernott/doenerstag/internal/db"
	"github.com/joernott/doenerstag/internal/model"
	"github.com/joernott/doenerstag/internal/sse"
)

type addItemRequest struct {
	MenuItemID      string   `json:"menu_item_id"`
	Quantity        int      `json:"quantity"`
	Note            string   `json:"note"`
	ModificationIDs []string `json:"modification_ids"`
}

// addItem adds an order item, snapshotting the menu.
//
// F6.1: any logged-in user may add items to an active order. Not just the
// creator -- the whole point is that colleagues fill in the order themselves.
func (h *OrderHandlers) addItem(w http.ResponseWriter, r *http.Request) {
	order, lookupErr := h.lookup(r)
	if lookupErr != nil {
		WriteError(w, r, lookupErr)
		return
	}
	principal, authErr := RequireAuthenticated(r)
	if authErr != nil {
		WriteError(w, r, authErr)
		return
	}
	if err := h.requireActive(order); err != nil {
		WriteError(w, r, err)
		return
	}

	var body addItemRequest
	if !decodeJSON(w, r, &body) {
		return
	}

	menuItemID, err := requiredUUID(body.MenuItemID, "menu_item_id")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	if err := validateQuantity(body.Quantity); err != nil {
		WriteError(w, r, err)
		return
	}
	if err := validateFreeText(body.Note, "note", MaxOrderNoteLength); err != nil {
		WriteError(w, r, err)
		return
	}
	modificationIDs, err := parseIDList(body.ModificationIDs, "modification_ids")
	if err != nil {
		WriteError(w, r, err)
		return
	}

	created, dbErr := db.AddOrderItem(r.Context(), h.Pool, db.NewOrderItem{
		OrderID:         order.ID,
		UserID:          principal.User.ID,
		MenuItemID:      menuItemID,
		Quantity:        body.Quantity,
		Note:            strings.TrimSpace(body.Note),
		ModificationIDs: modificationIDs,
	})
	if err := itemWriteError(dbErr); err != nil {
		WriteError(w, r, err)
		return
	}

	h.publishItemChange(order.ID, sse.EventItemCreated, publicOrderItem(created))

	_ = WriteJSON(w, http.StatusCreated, publicOrderItem(created))
}

type patchItemRequest struct {
	Quantity        *int     `json:"quantity"`
	Note            *string  `json:"note"`
	ModificationIDs []string `json:"modification_ids"`

	raw rawPatch `json:"-"`
}

// patchItem changes quantity, note or modifications. Owner or administrator.
func (h *OrderHandlers) patchItem(w http.ResponseWriter, r *http.Request) {
	order, item, lookupErr := h.lookupItem(r)
	if lookupErr != nil {
		WriteError(w, r, lookupErr)
		return
	}

	// Authentication, then the deadline, then ownership -- in that order, and
	// the middle one matters. Once the order is closed nobody may change an
	// item, so telling the order's creator "you are not the owner" would be
	// misleading: the owner cannot either. Anonymous stays 2000, because that
	// is the answer everywhere else in the API.
	if _, authErr := RequireAuthenticated(r); authErr != nil {
		WriteError(w, r, authErr)
		return
	}
	if err := h.requireActive(order); err != nil {
		WriteError(w, r, err)
		return
	}
	principal, authErr := h.requireItemOwner(r, item)
	if authErr != nil {
		WriteError(w, r, authErr)
		return
	}

	var body patchItemRequest
	if !decodeJSONWithRaw(w, r, &body, &body.raw) {
		return
	}

	update := db.OrderItemUpdate{}
	if body.Quantity != nil {
		if err := validateQuantity(*body.Quantity); err != nil {
			WriteError(w, r, err)
			return
		}
		update.Quantity = body.Quantity
	}
	if body.Note != nil {
		if err := validateFreeText(*body.Note, "note", MaxOrderNoteLength); err != nil {
			WriteError(w, r, err)
			return
		}
		note := strings.TrimSpace(*body.Note)
		update.Note = &note
	}
	if body.raw.has("modification_ids") {
		ids, err := parseIDList(body.ModificationIDs, "modification_ids")
		if err != nil {
			WriteError(w, r, err)
			return
		}
		update.ModificationIDs = ids
		update.SetModifications = true
	}

	updated, dbErr := db.UpdateOrderItem(r.Context(), h.Pool, item.ID, update, principal.User.ID)
	if err := itemWriteError(dbErr); err != nil {
		WriteError(w, r, err)
		return
	}

	h.publishItemChange(order.ID, sse.EventItemUpdated, publicOrderItem(updated))

	_ = WriteJSON(w, http.StatusOK, publicOrderItem(updated))
}

// deleteItem removes an order item. Owner or administrator, while active.
func (h *OrderHandlers) deleteItem(w http.ResponseWriter, r *http.Request) {
	order, item, lookupErr := h.lookupItem(r)
	if lookupErr != nil {
		WriteError(w, r, lookupErr)
		return
	}
	// Same order as patchItem, for the same reason.
	if _, authErr := RequireAuthenticated(r); authErr != nil {
		WriteError(w, r, authErr)
		return
	}
	if err := h.requireActive(order); err != nil {
		WriteError(w, r, err)
		return
	}
	if _, authErr := h.requireItemOwner(r, item); authErr != nil {
		WriteError(w, r, authErr)
		return
	}

	if err := db.DeleteOrderItem(r.Context(), h.Pool, item.ID); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			WriteError(w, r, &Error{Code: CodeNotFound})
			return
		}
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: err})
		return
	}

	h.publishItemChange(order.ID, sse.EventItemDeleted,
		map[string]string{"id": item.ID.String()})

	w.WriteHeader(http.StatusNoContent)
}

// requireActive refuses a write after the deadline.
//
// F6.6 is unusually emphatic: after the deadline all order items are read-only
// for everyone, "including the administrator". So this is checked before the
// ownership rules rather than folded into them, and there is no administrator
// exemption anywhere in it. The restaurant has been phoned by then; changing
// what was ordered would make the summary disagree with the food that arrives.
func (h *OrderHandlers) requireActive(order model.Order) *Error {
	if order.Active(h.now()) {
		return nil
	}
	return &Error{Code: CodeOrderClosed}
}

// requireItemOwner allows the item's owner or the administrator.
//
// F6.5: a user may edit and delete only their own order items. The
// administrator is included because the permission matrix ticks the admin
// column wherever the owner column is ticked -- but only while the order is
// active, which requireActive enforces separately.
func (h *OrderHandlers) requireItemOwner(r *http.Request, item model.OrderItem) (*Principal, *Error) {
	principal, err := RequireAuthenticated(r)
	if err != nil {
		return nil, err
	}
	if !principal.Is(item.UserID) && !principal.IsAdmin() {
		return nil, &Error{Code: CodeNotItemOwner}
	}
	return principal, nil
}

// lookupItem resolves both path parameters and checks the item is in the order.
func (h *OrderHandlers) lookupItem(r *http.Request) (model.Order, model.OrderItem, *Error) {
	order, err := h.lookup(r)
	if err != nil {
		return model.Order{}, model.OrderItem{}, err
	}

	id, parseErr := uuid.Parse(Param(r, "iid"))
	if parseErr != nil {
		return model.Order{}, model.OrderItem{}, &Error{Code: CodeNotFound}
	}

	item, dbErr := db.OrderItemByID(r.Context(), h.Pool, id)
	switch {
	case errors.Is(dbErr, db.ErrNotFound):
		return model.Order{}, model.OrderItem{}, &Error{Code: CodeNotFound}
	case dbErr != nil:
		return model.Order{}, model.OrderItem{}, &Error{Code: CodeDatabaseUnavailable, Cause: dbErr}
	}
	if item.OrderID != order.ID {
		// One order's item must not be reachable through another's URL.
		return model.Order{}, model.OrderItem{}, &Error{Code: CodeNotFound}
	}
	return order, item, nil
}

// itemWriteError maps the database layer's item errors onto documented codes.
func itemWriteError(err error) *Error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, db.ErrWrongRestaurant):
		return &Error{Code: CodeItemWrongRestaurant, Field: "menu_item_id"}
	case errors.Is(err, db.ErrItemUnavailable):
		return &Error{Code: CodeItemUnavailable, Field: "menu_item_id"}
	case errors.Is(err, db.ErrModificationWrongItem):
		return &Error{Code: CodeModificationWrongItem, Field: "modification_ids"}
	case errors.Is(err, db.ErrNotFound):
		return &Error{Code: CodeNotFound}
	default:
		return &Error{Code: CodeDatabaseUnavailable, Cause: err}
	}
}

func validateQuantity(quantity int) *Error {
	if quantity < 1 {
		return &Error{Code: CodeQuantityTooSmall, Field: "quantity"}
	}
	if quantity > MaxOrderItemQuantity {
		return &Error{
			Code:   CodeInvalidField,
			Field:  "quantity",
			Detail: "that is more than anybody orders at lunch",
		}
	}
	return nil
}

// setItemPaid records that a line has been settled, or that it has not.
//
// A route of its own, for two reasons that both come from the same place: the
// tick is not a property of the food, it is a property of the debt.
//
//  1. **Three people may set it.** The person whose item it is, the person who
//     opened the order, and whoever is collecting the money -- because those
//     are the three people who plausibly know. That is a wider rule than
//     "owner", and threading it through patchItem as an exception for one field
//     would put it somewhere nobody would find it.
//
//  2. **The deadline does not apply.** Everything else about an item freezes
//     when the order closes (F6.6), and it has to: the order has been placed
//     and the prices are agreed. But the money changes hands when the food
//     arrives, which is always after the deadline. A tick that could only be
//     made while the order was open could never be made at all.
func (h *OrderHandlers) setItemPaid(w http.ResponseWriter, r *http.Request) {
	order, item, lookupErr := h.lookupItem(r)
	if lookupErr != nil {
		WriteError(w, r, lookupErr)
		return
	}

	principal, authErr := RequireAuthenticated(r)
	if authErr != nil {
		WriteError(w, r, authErr)
		return
	}

	me := principal.User.ID
	mayTick := principal.User.IsAdmin ||
		item.UserID == me ||
		order.CreatorID == me ||
		(order.MoneyCollectorID != nil && *order.MoneyCollectorID == me)
	if !mayTick {
		WriteError(w, r, &Error{Code: CodeNotItemOwner})
		return
	}

	var body struct {
		Paid *bool `json:"paid"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Paid == nil {
		WriteError(w, r, &Error{Code: CodeMissingField, Field: "paid"})
		return
	}

	if err := db.SetOrderItemPaid(r.Context(), h.Pool, item.ID, me, *body.Paid); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			WriteError(w, r, &Error{Code: CodeNotFound})
			return
		}
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: err})
		return
	}

	updated, err := db.OrderItemByID(r.Context(), h.Pool, item.ID)
	if err != nil {
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: err})
		return
	}

	// The same event an edit publishes: a second browser watching the summary
	// should see the tick appear.
	h.publishItemChange(order.ID, sse.EventItemUpdated, publicOrderItem(updated))

	_ = WriteJSON(w, http.StatusOK, publicOrderItem(updated))
}
