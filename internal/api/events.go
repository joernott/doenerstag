package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/joernott/doenerstag/internal/db"
	"github.com/joernott/doenerstag/internal/model"
	"github.com/joernott/doenerstag/internal/sse"
)

// PingInterval is how often an idle stream sends a comment-only event.
//
// docs/adr/0003 fixes it at 30 seconds, to keep intermediate proxies from
// closing a connection they consider idle.
const PingInterval = 30 * time.Second

// events serves the per-order SSE stream.
//
// Open to anonymous readers like the rest of the order data, but what a
// subscriber receives depends on their authentication state, which is captured
// here when the stream opens. A subscriber who logs in afterwards reconnects to
// get the fuller set; the frontend does that automatically.
func (h *OrderHandlers) events(w http.ResponseWriter, r *http.Request) {
	order, lookupErr := h.lookup(r)
	if lookupErr != nil {
		WriteError(w, r, lookupErr)
		return
	}
	if h.Events == nil {
		// No registry wired in, which only happens in a test that does not care
		// about streams. Better a clear 404 than a nil dereference.
		WriteError(w, r, &Error{Code: CodeNotFound})
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		WriteError(w, r, &Error{
			Code:   CodeInternal,
			Detail: "this server cannot stream events",
		})
		return
	}

	authenticated := PrincipalFrom(r.Context()) != nil
	sub := h.Events.Subscribe(order.ID, authenticated)
	defer h.Events.Unsubscribe(order.ID, sub)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	// Nagle-style buffering in an intermediate proxy breaks live updates
	// entirely. This header asks nginx not to; a proxy that ignores it is what
	// the troubleshooting table in docs/10_operations.md is for.
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// The write deadline has to go, or --http-write-timeout would kill a stream
	// that is meant to stay open for hours. ADR-0003 calls this exemption out
	// and says it must survive refactoring, so it is done here, immediately
	// after the headers, where it is visible rather than buried.
	//
	// A failure is not fatal: without a write timeout configured there is
	// nothing to clear, and the stream works either way.
	if err := http.NewResponseController(w).SetWriteDeadline(time.Time{}); err != nil {
		h.log(r).Debug().Err(err).Msg("could not clear the stream write deadline")
	}

	// An opening comment, so a client knows the stream is live before anything
	// happens on the order.
	if _, err := fmt.Fprint(w, ": open\n\n"); err != nil {
		return
	}
	flusher.Flush()

	ping := time.NewTicker(PingInterval)
	defer ping.Stop()

	// order.expired has no write to hang off: nothing happens at the deadline
	// except that the clock passes it. So the stream watches its own order's
	// deadline and tells the page when it goes by, which is what F7.3 asks for
	// -- the open page switches itself into the read-only state without a
	// reload. A background scheduler would be the alternative, and it would
	// exist solely to serve the pages that are already watching.
	expiry := h.expiryTimer(order)
	defer expiry.Stop()

	for {
		select {
		case <-r.Context().Done():
			// The client went away, or the server is shutting down.
			return

		case <-expiry.C:
			if err := writeEvent(w, sse.Event{
				Name:     sse.EventOrderExpired,
				Audience: sse.Everyone,
				Data:     []byte(`{"id":"` + order.ID.String() + `"}`),
			}); err != nil {
				return
			}
			flusher.Flush()

		case event, ok := <-sub.Events():
			if !ok {
				// The hub dropped us: shutdown, or we fell too far behind.
				return
			}
			if err := writeEvent(w, event); err != nil {
				return
			}
			flusher.Flush()

		case <-ping.C:
			if _, err := fmt.Fprint(w, "event: ping\ndata: {}\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// writeEvent renders one event in the SSE wire format.
func writeEvent(w http.ResponseWriter, e sse.Event) error {
	_, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Name, e.Data)
	return err
}

// publishItemChange announces an item event, and the count that replaces it for
// anonymous subscribers.
//
// The two go together on purpose. An anonymous subscriber must never receive an
// item payload, so the count is sent instead -- and it is sent even when the
// change leaves the count unchanged, so that nothing can be inferred from an
// event failing to arrive (F7.4). Editing a quantity does not alter the number
// of items, and an anonymous subscriber still sees an event.
func (h *OrderHandlers) publishItemChange(orderID uuid.UUID, name string, payload any) {
	if h.Events == nil {
		return
	}

	events := make([]sse.Event, 0, 2)

	if data, err := json.Marshal(payload); err == nil {
		events = append(events, sse.Event{
			Name: name, Audience: sse.AuthenticatedOnly, Data: data,
		})
	}

	count, err := db.CountOrderItems(context.Background(), h.Pool, orderID)
	if err == nil {
		if data, err := json.Marshal(map[string]int{"item_count": count}); err == nil {
			events = append(events, sse.Event{
				Name:     sse.EventOrderItemCount,
				Audience: sse.AnonymousOnly,
				Data:     data,
			})
		}
	}

	h.Events.Publish(orderID, events...)
}

// publishOrderChange announces a change to the order itself.
//
// The header goes to everyone -- but not the same header. An anonymous
// subscriber gets the shape that names no user, exactly as the REST endpoint
// gives them, because a live event must not carry what a fetch would withhold.
func (h *OrderHandlers) publishOrderChange(order model.Order) {
	if h.Events == nil {
		return
	}

	events := make([]sse.Event, 0, 2)

	if data, err := json.Marshal(h.publicHeader(order)); err == nil {
		events = append(events, sse.Event{
			Name: sse.EventOrderUpdated, Audience: sse.AnonymousOnly, Data: data,
		})
	}
	if data, err := json.Marshal(h.listEntry(order)); err == nil {
		events = append(events, sse.Event{
			Name: sse.EventOrderUpdated, Audience: sse.AuthenticatedOnly, Data: data,
		})
	}

	h.Events.Publish(order.ID, events...)
}

// publishOrderGone announces a deletion. The id is all anybody needs, and it is
// the same for every subscriber.
func (h *OrderHandlers) publishOrderGone(orderID uuid.UUID, name string) {
	if h.Events == nil {
		return
	}

	data, err := json.Marshal(map[string]string{"id": orderID.String()})
	if err != nil {
		return
	}
	h.Events.Publish(orderID, sse.Event{
		Name: name, Audience: sse.Everyone, Data: data,
	})
}

// expiryTimer fires when the order's deadline passes.
//
// An already-expired order gets a timer that never fires: the page opened
// knowing it was read-only, and announcing an expiry that happened yesterday
// would be noise. A very distant deadline is capped, because time.Timer takes
// an int64 of nanoseconds and an order dated a few centuries out would
// otherwise overflow it into firing immediately.
func (h *OrderHandlers) expiryTimer(order model.Order) *time.Timer {
	const never = 100 * 365 * 24 * time.Hour

	remaining := order.DeadlineAt.Sub(h.now())
	if remaining <= 0 || remaining > never {
		return time.NewTimer(never)
	}
	return time.NewTimer(remaining)
}

// PingEventName is the keep-alive event, exported so a test can skip it while
// waiting for something else.
const PingEventName = sse.EventPing
