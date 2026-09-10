package api_test

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/joernott/doenerstag/internal/api"
)

// stream is one open SSE connection in a test.
type stream struct {
	t      *testing.T
	cancel context.CancelFunc
	body   *bufio.Reader
	closed chan struct{}
}

// openStream connects to an order's event stream against a real listener.
//
// httptest.ResponseRecorder cannot be used here: it buffers everything until
// the handler returns, and this handler never returns. A real server is the
// only way to test a stream at all.
func (o *orderFixture) openStream(orderID string, cookies []*http.Cookie) *stream {
	o.t.Helper()

	server := httptest.NewServer(o.handler)
	o.t.Cleanup(server.Close)

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		server.URL+api.APIPrefix+"/orders/"+orderID+"/events", http.NoBody)
	if err != nil {
		cancel()
		o.t.Fatalf("building the stream request: %v", err)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}

	resp, err := server.Client().Do(req)
	if err != nil {
		cancel()
		o.t.Fatalf("opening the stream: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		cancel()
		o.t.Fatalf("the stream answered %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		cancel()
		o.t.Fatalf("the stream is %q, want text/event-stream", ct)
	}

	s := &stream{t: o.t, cancel: cancel, body: bufio.NewReader(resp.Body), closed: make(chan struct{})}
	o.t.Cleanup(func() {
		cancel()
		_ = resp.Body.Close()
	})
	return s
}

// waitFor reads until it sees one of the named events, or gives up.
//
// Returns the event name and its data line. Comments and pings are skipped, so
// a test does not have to care that the stream opens with one.
func (s *stream) waitFor(names ...string) (name, data string) {
	s.t.Helper()

	wanted := make(map[string]bool, len(names))
	for _, n := range names {
		wanted[n] = true
	}

	deadline := time.Now().Add(5 * time.Second)
	var current string

	for time.Now().Before(deadline) {
		line, err := s.body.ReadString('\n')
		if err != nil {
			s.t.Fatalf("reading the stream: %v", err)
		}
		line = strings.TrimRight(line, "\r\n")

		switch {
		case strings.HasPrefix(line, "event: "):
			current = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: ") && wanted[current]:
			return current, strings.TrimPrefix(line, "data: ")
		}
	}

	s.t.Fatalf("no %v event arrived within five seconds", names)
	return "", ""
}

// expectNothing asserts no event arrives for a short while.
//
// Bounded rather than instantaneous: the publish happens on another goroutine,
// so checking immediately would pass even when the event was on its way.
func (s *stream) expectNothing(within time.Duration) {
	s.t.Helper()

	done := make(chan string, 1)
	go func() {
		var current string
		for {
			line, err := s.body.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			if strings.HasPrefix(line, "event: ") {
				current = strings.TrimPrefix(line, "event: ")
				if current != api.PingEventName {
					done <- current
					return
				}
			}
		}
	}()

	select {
	case name := <-done:
		s.t.Errorf("an unexpected %s event arrived", name)
	case <-time.After(within):
	}
}

// The exit criterion: two subscribers on one order, one anonymous and one
// authenticated, each receiving only their own event set.
func TestTwoSubscribersReceiveDifferentEvents(t *testing.T) {
	o := newOrderFixture(t)
	hungry := o.register("hungrig")

	anonymous := o.openStream(o.order.ID, nil)
	loggedIn := o.openStream(o.order.ID, hungry)

	// Give both streams a moment to register before publishing.
	waitForSubscribers(t, o, 2)

	o.addOrderItem(hungry, map[string]any{"quantity": 1, "note": "viel Schärfe"})

	// The logged-in subscriber gets the item, with its detail.
	name, data := loggedIn.waitFor("item.created")
	if name != "item.created" {
		t.Fatalf("the logged-in subscriber got %q", name)
	}
	for _, needle := range []string{"hungrig", "Döner Kebab", "viel Schärfe"} {
		if !strings.Contains(data, needle) {
			t.Errorf("the item event is missing %q: %s", needle, data)
		}
	}

	// The anonymous subscriber gets the count instead, and nothing about the
	// item -- which is the whole point of the split.
	name, data = anonymous.waitFor("order.item_count")
	if name != "order.item_count" {
		t.Fatalf("the anonymous subscriber got %q", name)
	}
	if !strings.Contains(data, `"item_count":1`) {
		t.Errorf("the count event reads %s", data)
	}
	for _, needle := range []string{"hungrig", "Döner Kebab", "viel Schärfe", "650"} {
		if strings.Contains(data, needle) {
			t.Errorf("the anonymous event leaks %q: %s", needle, data)
		}
	}
}

// F7.4: the count is republished even when a change leaves it unchanged, so
// nothing can be inferred from an event failing to arrive.
func TestTheCountIsRepublishedEvenWhenUnchanged(t *testing.T) {
	o := newOrderFixture(t)
	hungry := o.register("hungrig")
	item := o.addOrderItem(hungry, map[string]any{"quantity": 1})

	anonymous := o.openStream(o.order.ID, nil)
	waitForSubscribers(t, o, 1)

	// Editing a quantity does not change how many items there are.
	if rec := o.patch("/orders/"+o.order.ID+"/items/"+item.ID,
		map[string]any{"quantity": 5}, hungry...); rec.Code != http.StatusOK {
		t.Fatalf("patching: %s", rec.Body.String())
	}

	name, data := anonymous.waitFor("order.item_count")
	if name != "order.item_count" {
		t.Fatalf("got %q", name)
	}
	if !strings.Contains(data, `"item_count":1`) {
		t.Errorf("the count reads %s, want the unchanged 1", data)
	}
	if strings.Contains(data, "5") && strings.Contains(data, "quantity") {
		t.Errorf("the anonymous event leaked the quantity: %s", data)
	}
}

// The order header goes to everyone, but not the same header: an anonymous
// subscriber must not learn through a live event what a fetch withholds.
func TestTheOrderEventSplitsOnTheCreatorToo(t *testing.T) {
	o := newOrderFixture(t)
	watcher := o.register("zuschauer")

	anonymous := o.openStream(o.order.ID, nil)
	loggedIn := o.openStream(o.order.ID, watcher)
	waitForSubscribers(t, o, 2)

	// One change an anonymous reader may see and one they may not, in the same
	// request: the fulfilment is a fact about the order, and the person fetching
	// the food is a named person.
	if rec := o.patch("/orders/"+o.order.ID, map[string]any{
		"fulfilment":       "delivery",
		"pickup_person_id": o.userID("cook"),
	}, o.cookies...); rec.Code != http.StatusOK {
		t.Fatalf("patching the order: %s", rec.Body.String())
	}

	_, anonymousData := anonymous.waitFor("order.updated")
	if strings.Contains(anonymousData, "creator_name") || strings.Contains(anonymousData, "cook") {
		t.Errorf("the anonymous order event names somebody: %s", anonymousData)
	}
	if strings.Contains(anonymousData, "pickup_person") {
		t.Errorf("the anonymous order event carries the pickup person: %s", anonymousData)
	}
	if !strings.Contains(anonymousData, "delivery") {
		t.Errorf("the anonymous order event is missing the change: %s", anonymousData)
	}

	_, loggedInData := loggedIn.waitFor("order.updated")
	if !strings.Contains(loggedInData, "creator_name") {
		t.Errorf("the logged-in order event omits the creator: %s", loggedInData)
	}
	if !strings.Contains(loggedInData, "pickup_person_name") {
		t.Errorf("the logged-in order event omits the pickup person: %s", loggedInData)
	}
}

// Deleting an order tells everyone, with nothing but the id.
func TestDeletingAnOrderNotifiesEverySubscriber(t *testing.T) {
	o := newOrderFixture(t)
	watcher := o.register("zuschauer")

	anonymous := o.openStream(o.order.ID, nil)
	loggedIn := o.openStream(o.order.ID, watcher)
	waitForSubscribers(t, o, 2)

	if rec := o.remove("/orders/"+o.order.ID, o.cookies...); rec.Code != http.StatusNoContent {
		t.Fatalf("deleting: %s", rec.Body.String())
	}

	for _, s := range []*stream{anonymous, loggedIn} {
		name, data := s.waitFor("order.deleted")
		if name != "order.deleted" {
			t.Errorf("got %q", name)
		}
		if !strings.Contains(data, o.order.ID) {
			t.Errorf("the deletion event is missing the id: %s", data)
		}
	}
}

// F7.3: the page learns that the deadline passed without a reload.
func TestTheStreamAnnouncesTheDeadline(t *testing.T) {
	o := newOrderFixture(t)

	// An order that expires almost immediately, so the timer fires while the
	// test is watching rather than in two hours.
	//
	// Whole seconds, and that matters: the deadline goes over the wire as
	// RFC 3339, which carries no fractional part, so a sub-second offset is
	// truncated away and can land in the past. The first version of this test
	// asked for 700ms, got a deadline that had already passed, and waited for
	// an expiry event the handler had correctly decided not to send.
	soon := o.createOrder(o.cookies, 2*time.Second)
	s := o.openStream(soon.ID, nil)

	name, data := s.waitFor("order.expired")
	if name != "order.expired" {
		t.Fatalf("got %q", name)
	}
	if !strings.Contains(data, soon.ID) {
		t.Errorf("the expiry event is missing the id: %s", data)
	}
}

// An expired order's stream does not announce an expiry that already happened:
// the page opened knowing it was read-only.
func TestAnAlreadyExpiredOrderAnnouncesNothing(t *testing.T) {
	o := newOrderFixture(t)
	past := o.createExpiredOrder(o.cookies)

	s := o.openStream(past.ID, nil)
	s.expectNothing(time.Second)
}

// The stream is public, like the rest of the order data.
func TestTheStreamIsOpenToAnonymousReaders(t *testing.T) {
	o := newOrderFixture(t)
	s := o.openStream(o.order.ID, nil)
	_ = s
}

func TestTheStreamOfAnUnknownOrderIsNotFound(t *testing.T) {
	o := newOrderFixture(t)
	server := httptest.NewServer(o.handler)
	defer server.Close()

	for _, id := range []string{"018f0000-0000-7000-8000-00000000dead", "not-a-uuid"} {
		resp, err := server.Client().Get(
			server.URL + api.APIPrefix + "/orders/" + id + "/events")
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s answered %d, want 404", id, resp.StatusCode)
		}
	}
}

// waitForSubscribers blocks until the registry has registered the expected
// number of streams for the fixture's order.
//
// Opening a stream is asynchronous: the request returns as soon as the headers
// arrive, which can be before the handler has registered with the hub.
// Publishing before then would deliver to nobody, and the test would fail for a
// reason that has nothing to do with what it is testing.
func waitForSubscribers(t *testing.T, o *orderFixture, want int) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if o.registry.Subscribers(mustParseUUID(t, o.order.ID)) >= want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("only %d of %d subscribers registered",
		o.registry.Subscribers(mustParseUUID(t, o.order.ID)), want)
}
