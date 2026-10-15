package api

// Taking on and giving up the two jobs an order has: fetching the food and
// collecting the money (17.8, 19.3, 19.5). Its own file because it is its own
// feature, with its own permission rule -- anybody signed in may take a job
// nobody has -- that belongs to neither creating nor editing an order.

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/joernott/doenerstag/internal/db"
	"github.com/joernott/doenerstag/internal/model"
)

// orderJob is one of the two jobs an order has, which anybody signed in may
// take on while nobody has.
type orderJob struct {
	field  string
	holder func(model.Order) *uuid.UUID
	assign func(*db.OrderUpdate, **uuid.UUID)
}

var (
	fetchingTheFood = orderJob{
		field:  "pickup_person_id",
		holder: func(o model.Order) *uuid.UUID { return o.PickupPersonID },
		assign: func(u *db.OrderUpdate, ref **uuid.UUID) { u.PickupPersonID = ref },
	}
	collectingTheMoney = orderJob{
		field:  "money_collector_id",
		holder: func(o model.Order) *uuid.UUID { return o.MoneyCollectorID },
		assign: func(u *db.OrderUpdate, ref **uuid.UUID) { u.MoneyCollectorID = ref },
	}
)

// claimPickup puts the caller down as the person fetching the food (17.8).
func (h *OrderHandlers) claimPickup(w http.ResponseWriter, r *http.Request) {
	h.claimJob(w, r, fetchingTheFood)
}

// claimMoneyCollection puts the caller down as the person collecting the money
// (19.3), on the same terms as fetching the food.
func (h *OrderHandlers) claimMoneyCollection(w http.ResponseWriter, r *http.Request) {
	h.claimJob(w, r, collectingTheMoney)
}

// claimJob puts the caller down for one of the order's two jobs.
//
// Deliberately not part of PATCH. PATCH belongs to the creator (F5.7), and the
// point of this is that it does not: whoever is willing to walk to the
// restaurant, or to hold the money, should be able to say so without going
// through whoever opened the order. Making it a route of its own means the rule
// is one sentence at one place rather than an exception threaded through the
// general edit path.
//
// It only fills a vacancy. Taking the job off somebody who already has it is a
// different act with a different social meaning, and the creator can do it from
// the editor.
func (h *OrderHandlers) claimJob(w http.ResponseWriter, r *http.Request, job orderJob) {
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

	if !order.Active(h.now()) {
		WriteError(w, r, &Error{Code: CodeOrderClosed})
		return
	}
	if job.holder(order) != nil {
		WriteError(w, r, &Error{Code: CodeJobTaken, Field: job.field})
		return
	}

	me := principal.User.ID
	ref := &me
	var update db.OrderUpdate
	job.assign(&update, &ref)
	updated, dbErr := db.UpdateOrder(r.Context(), h.Pool, order.ID, me, update)
	if dbErr != nil {
		WriteError(w, r, dbError(dbErr))
		return
	}

	h.publishOrderChange(updated)
	h.writeDetail(w, r, updated, http.StatusOK)
}

// releasePickup takes the person fetching the food off the order (19.5).
func (h *OrderHandlers) releasePickup(w http.ResponseWriter, r *http.Request) {
	h.releaseJob(w, r, fetchingTheFood)
}

// releaseMoneyCollection takes the person collecting the money off the order
// (19.5).
func (h *OrderHandlers) releaseMoneyCollection(w http.ResponseWriter, r *http.Request) {
	h.releaseJob(w, r, collectingTheMoney)
}

// releaseJob leaves one of the order's two jobs vacant.
//
// The person doing it may give it up, which is the case the button is for:
// somebody who said "Me!" and then cannot go should not have to find the
// creator to undo it. The creator and the administrator may clear it too, as
// the creator already could from the editor. Nobody else: taking somebody off
// a job is not a stranger's to do, and is 3006 rather than the creator-only
// 3001, which would tell the person doing the job something untrue.
//
// While the order is open, like taking the job: after the deadline the order
// is read-only (F6.6). Clearing a job nobody has is not an error; the result
// is what was asked for.
func (h *OrderHandlers) releaseJob(w http.ResponseWriter, r *http.Request, job orderJob) {
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

	me := principal.User.ID
	holder := job.holder(order)
	allowed := principal.User.IsAdmin || order.CreatorID == me || (holder != nil && *holder == me)
	if !allowed {
		WriteError(w, r, &Error{Code: CodeNotJobHolder, Field: job.field})
		return
	}

	if !order.Active(h.now()) {
		WriteError(w, r, &Error{Code: CodeOrderClosed})
		return
	}
	if holder == nil {
		h.writeDetail(w, r, order, http.StatusOK)
		return
	}

	var nobody *uuid.UUID
	var update db.OrderUpdate
	job.assign(&update, &nobody)
	updated, dbErr := db.UpdateOrder(r.Context(), h.Pool, order.ID, me, update)
	if dbErr != nil {
		WriteError(w, r, dbError(dbErr))
		return
	}

	h.publishOrderChange(updated)
	h.writeDetail(w, r, updated, http.StatusOK)
}
