// Package sse carries live order updates to connected browsers.
//
// One hub per order, holding its subscribers in memory. A write that changes an
// order publishes to that order's hub after its transaction commits. See
// docs/adr/0003-sse-for-order-updates.md.
//
// In-memory means the application cannot run as several instances without a
// shared bus. Multi-instance deployment is already out of scope, and this is
// one of the reasons.
package sse

import (
	"sync"

	"github.com/google/uuid"
)

// Event names, from docs/04_api.md.
const (
	EventItemCreated    = "item.created"
	EventItemUpdated    = "item.updated"
	EventItemDeleted    = "item.deleted"
	EventOrderItemCount = "order.item_count"
	EventOrderUpdated   = "order.updated"
	EventOrderDeleted   = "order.deleted"
	EventOrderExpired   = "order.expired"
	EventPing           = "ping"
)

// Audience says who an event is for.
//
// The split exists because docs/adr/0011-tiered-order-visibility.md gives
// anonymous and authenticated callers different views of an order, and a live
// stream that ignored that would leak exactly what the REST endpoint withholds.
type Audience int

const (
	// Everyone receives the event, whatever their authentication state.
	Everyone Audience = iota
	// AuthenticatedOnly is for item detail.
	AuthenticatedOnly
	// AnonymousOnly is for the item count, which replaces the item events for
	// subscribers who may not see them.
	AnonymousOnly
)

// Event is one thing that happened to an order.
type Event struct {
	Name     string
	Audience Audience

	// Data is the JSON payload, already encoded. Encoding once in the publisher
	// rather than per subscriber matters when a busy order has a dozen open
	// pages.
	Data []byte
}

// subscriberBuffer is how many events a slow subscriber may fall behind before
// being disconnected.
//
// A browser that has stopped reading -- a laptop lid closed mid-order -- must
// not hold the publisher up. Sixteen is generous for a stream whose events are
// a few hundred bytes and whose natural rate is a few per minute; a subscriber
// that exceeds it is not going to catch up, and dropping it is better than
// blocking everyone else. The client reconnects and re-fetches, so nothing is
// lost that mattered.
const subscriberBuffer = 16

// Subscriber is one open stream.
type Subscriber struct {
	// Authenticated is fixed when the stream opens. A subscriber who logs in
	// afterwards has to reconnect to get the fuller event set, which the
	// frontend does automatically.
	Authenticated bool

	events chan Event
	closed chan struct{}
	once   sync.Once
}

// Events is the channel to read from. It is closed when the subscriber is
// dropped.
func (s *Subscriber) Events() <-chan Event { return s.events }

// wants reports whether this subscriber should receive an event.
func (s *Subscriber) wants(e Event) bool {
	switch e.Audience {
	case Everyone:
		return true
	case AuthenticatedOnly:
		return s.Authenticated
	case AnonymousOnly:
		return !s.Authenticated
	default:
		return false
	}
}

// close drops the subscriber. Safe to call twice, because both the hub and the
// handler may reach it.
func (s *Subscriber) close() {
	s.once.Do(func() {
		close(s.closed)
		close(s.events)
	})
}

// Hub is the set of subscribers for one order.
type Hub struct {
	mu          sync.Mutex
	subscribers map[*Subscriber]struct{}
}

// Registry holds one hub per order.
//
// Hubs are created on demand and removed when their last subscriber leaves, so
// an application that has been running for a year does not carry a hub for
// every order it has ever served.
type Registry struct {
	mu   sync.Mutex
	hubs map[uuid.UUID]*Hub
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{hubs: make(map[uuid.UUID]*Hub, 8)}
}

// Subscribe registers a new subscriber for an order.
func (r *Registry) Subscribe(orderID uuid.UUID, authenticated bool) *Subscriber {
	r.mu.Lock()
	hub, ok := r.hubs[orderID]
	if !ok {
		hub = &Hub{subscribers: make(map[*Subscriber]struct{}, 4)}
		r.hubs[orderID] = hub
	}
	r.mu.Unlock()

	sub := &Subscriber{
		Authenticated: authenticated,
		events:        make(chan Event, subscriberBuffer),
		closed:        make(chan struct{}),
	}

	hub.mu.Lock()
	hub.subscribers[sub] = struct{}{}
	hub.mu.Unlock()

	return sub
}

// Unsubscribe removes a subscriber and forgets the hub if it was the last one.
func (r *Registry) Unsubscribe(orderID uuid.UUID, sub *Subscriber) {
	r.mu.Lock()
	hub, ok := r.hubs[orderID]
	r.mu.Unlock()
	if !ok {
		return
	}

	hub.mu.Lock()
	delete(hub.subscribers, sub)
	empty := len(hub.subscribers) == 0
	hub.mu.Unlock()

	sub.close()

	if empty {
		// Re-check under the registry lock: another goroutine may have
		// subscribed between the two, and dropping a hub that has just gained a
		// subscriber would silently stop delivering to it.
		r.mu.Lock()
		if current, ok := r.hubs[orderID]; ok {
			current.mu.Lock()
			stillEmpty := len(current.subscribers) == 0
			current.mu.Unlock()
			if stillEmpty {
				delete(r.hubs, orderID)
			}
		}
		r.mu.Unlock()
	}
}

// Publish sends an event to every subscriber of an order that wants it.
//
// Never blocks. A subscriber whose buffer is full is dropped rather than
// allowed to hold up the publisher, which would mean one stalled browser
// delaying a database write for everybody else.
func (r *Registry) Publish(orderID uuid.UUID, events ...Event) {
	r.mu.Lock()
	hub, ok := r.hubs[orderID]
	r.mu.Unlock()
	if !ok {
		return
	}

	hub.mu.Lock()
	defer hub.mu.Unlock()

	for sub := range hub.subscribers {
		for _, e := range events {
			if !sub.wants(e) {
				continue
			}
			select {
			case sub.events <- e:
			default:
				delete(hub.subscribers, sub)
				sub.close()
			}
		}
	}
}

// CloseAll drops every subscriber, for a graceful shutdown.
//
// Without it a shutdown would wait out the grace period on connections that are
// designed never to end.
func (r *Registry) CloseAll() {
	r.mu.Lock()
	hubs := make([]*Hub, 0, len(r.hubs))
	for id, hub := range r.hubs {
		hubs = append(hubs, hub)
		delete(r.hubs, id)
	}
	r.mu.Unlock()

	for _, hub := range hubs {
		hub.mu.Lock()
		for sub := range hub.subscribers {
			delete(hub.subscribers, sub)
			sub.close()
		}
		hub.mu.Unlock()
	}
}

// Subscribers is how many are connected to an order, for tests and metrics.
func (r *Registry) Subscribers(orderID uuid.UUID) int {
	r.mu.Lock()
	hub, ok := r.hubs[orderID]
	r.mu.Unlock()
	if !ok {
		return 0
	}

	hub.mu.Lock()
	defer hub.mu.Unlock()
	return len(hub.subscribers)
}

// Hubs is how many orders have subscribers.
func (r *Registry) Hubs() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.hubs)
}
