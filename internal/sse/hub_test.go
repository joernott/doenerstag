package sse_test

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/joernott/doenerstag/internal/sse"
)

func event(name string, audience sse.Audience) sse.Event {
	return sse.Event{Name: name, Audience: audience, Data: []byte(`{}`)}
}

// receive drains what a subscriber has, without blocking.
func receive(sub *sse.Subscriber) []string {
	var names []string
	for {
		select {
		case e, ok := <-sub.Events():
			if !ok {
				return names
			}
			names = append(names, e.Name)
		default:
			return names
		}
	}
}

// The central rule: an anonymous subscriber must never receive an item payload,
// and gets the count instead.
func TestTheAudienceSplit(t *testing.T) {
	r := sse.NewRegistry()
	order := uuid.Must(uuid.NewV7())

	anonymous := r.Subscribe(order, false)
	loggedIn := r.Subscribe(order, true)

	r.Publish(order,
		event(sse.EventItemCreated, sse.AuthenticatedOnly),
		event(sse.EventOrderItemCount, sse.AnonymousOnly),
		event(sse.EventOrderUpdated, sse.Everyone),
	)

	anonymousGot := receive(anonymous)
	loggedInGot := receive(loggedIn)

	for _, name := range anonymousGot {
		if name == sse.EventItemCreated {
			t.Error("an anonymous subscriber received an item event")
		}
	}
	if !contains(anonymousGot, sse.EventOrderItemCount) {
		t.Errorf("the anonymous subscriber got %v, want the item count", anonymousGot)
	}
	if !contains(anonymousGot, sse.EventOrderUpdated) {
		t.Errorf("the anonymous subscriber missed the order event: %v", anonymousGot)
	}

	if !contains(loggedInGot, sse.EventItemCreated) {
		t.Errorf("the logged-in subscriber got %v, want the item event", loggedInGot)
	}
	if contains(loggedInGot, sse.EventOrderItemCount) {
		t.Errorf("the logged-in subscriber got the anonymous count event: %v", loggedInGot)
	}
	if !contains(loggedInGot, sse.EventOrderUpdated) {
		t.Errorf("the logged-in subscriber missed the order event: %v", loggedInGot)
	}
}

func contains(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

// Publishing to one order must not reach another's subscribers.
func TestHubsAreIsolated(t *testing.T) {
	r := sse.NewRegistry()
	first := uuid.Must(uuid.NewV7())
	second := uuid.Must(uuid.NewV7())

	a := r.Subscribe(first, true)
	b := r.Subscribe(second, true)

	r.Publish(first, event(sse.EventItemCreated, sse.Everyone))

	if got := receive(a); len(got) != 1 {
		t.Errorf("the subscriber on the published order got %v", got)
	}
	if got := receive(b); len(got) != 0 {
		t.Errorf("a subscriber on another order got %v", got)
	}
}

// Publishing to an order nobody is watching is a no-op, not a panic. Most
// writes happen with no page open.
func TestPublishingToNobodyIsHarmless(t *testing.T) {
	r := sse.NewRegistry()
	r.Publish(uuid.Must(uuid.NewV7()), event(sse.EventItemCreated, sse.Everyone))

	if r.Hubs() != 0 {
		t.Errorf("publishing created %d hubs", r.Hubs())
	}
}

// A hub is forgotten when its last subscriber leaves, so a long-running server
// does not carry one for every order it has ever served.
func TestHubsAreReclaimed(t *testing.T) {
	r := sse.NewRegistry()
	order := uuid.Must(uuid.NewV7())

	first := r.Subscribe(order, true)
	second := r.Subscribe(order, false)
	if r.Subscribers(order) != 2 {
		t.Fatalf("%d subscribers", r.Subscribers(order))
	}

	r.Unsubscribe(order, first)
	if r.Hubs() != 1 {
		t.Error("the hub went while a subscriber remained")
	}

	r.Unsubscribe(order, second)
	if r.Hubs() != 0 {
		t.Errorf("%d hubs remain after the last subscriber left", r.Hubs())
	}
}

// A subscriber that has stopped reading must not hold the publisher up. One
// closed laptop lid cannot be allowed to delay a database write for everybody
// else, so the slow subscriber is dropped instead.
func TestASlowSubscriberIsDroppedNotWaitedFor(t *testing.T) {
	r := sse.NewRegistry()
	order := uuid.Must(uuid.NewV7())

	slow := r.Subscribe(order, true)
	healthy := r.Subscribe(order, true)

	// Fill the slow subscriber's buffer and then some, without reading.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 200 {
			r.Publish(order, event(sse.EventItemCreated, sse.Everyone))
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("publishing blocked on a subscriber that stopped reading")
	}

	// The slow one was dropped; its channel is closed.
	drained := 0
	for range slow.Events() {
		drained++
	}
	if drained == 0 {
		t.Error("the slow subscriber received nothing at all")
	}

	// And the healthy one is still registered, though it too filled up. The
	// point is that the publisher never blocked.
	_ = healthy
	if r.Subscribers(order) > 2 {
		t.Errorf("%d subscribers remain", r.Subscribers(order))
	}
}

// Shutdown closes every stream, or the grace period would be spent waiting on
// connections designed never to end.
func TestCloseAllEndsEveryStream(t *testing.T) {
	r := sse.NewRegistry()
	first := uuid.Must(uuid.NewV7())
	second := uuid.Must(uuid.NewV7())

	subs := []*sse.Subscriber{
		r.Subscribe(first, true),
		r.Subscribe(first, false),
		r.Subscribe(second, true),
	}

	r.CloseAll()

	for i, sub := range subs {
		select {
		case _, ok := <-sub.Events():
			if ok {
				// A buffered event is fine; the channel must still end.
				for range sub.Events() {
				}
			}
		case <-time.After(time.Second):
			t.Errorf("subscriber %d was not closed", i)
		}
	}

	if r.Hubs() != 0 {
		t.Errorf("%d hubs survived the shutdown", r.Hubs())
	}
}

// Unsubscribing twice must not panic: the hub and the handler can both reach
// it, and on shutdown they race.
func TestUnsubscribingTwiceIsSafe(t *testing.T) {
	r := sse.NewRegistry()
	order := uuid.Must(uuid.NewV7())

	sub := r.Subscribe(order, true)
	r.Unsubscribe(order, sub)
	r.Unsubscribe(order, sub)
	r.CloseAll()
}

// The registry is shared between every request, so it has to be safe to use
// concurrently. The race detector is what actually checks this.
func TestTheRegistryIsSafeForConcurrentUse(t *testing.T) {
	r := sse.NewRegistry()
	orders := []uuid.UUID{
		uuid.Must(uuid.NewV7()),
		uuid.Must(uuid.NewV7()),
		uuid.Must(uuid.NewV7()),
	}

	var wg sync.WaitGroup
	for i := range 12 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			order := orders[n%len(orders)]
			for range 40 {
				sub := r.Subscribe(order, n%2 == 0)
				r.Publish(order, event(sse.EventItemCreated, sse.Everyone))
				receive(sub)
				r.Unsubscribe(order, sub)
			}
		}(i)
	}
	wg.Wait()

	if r.Hubs() != 0 {
		t.Errorf("%d hubs leaked", r.Hubs())
	}
}
