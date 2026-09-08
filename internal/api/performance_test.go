package api_test

import (
	"bytes"
	"fmt"
	stdimage "image"
	"image/color"
	"image/png"
	"net/http"
	"os"
	"sort"
	"sync"
	"testing"
	"time"
)

// The performance targets from docs/11_nonfunctional.md, measured rather than
// assumed.
//
// Off by default: it seeds a few thousand rows, opens a hundred streams and
// spends real time doing it, and a timing assertion on a shared CI runner is a
// flake generator. Run it deliberately:
//
//	DOENERSTAG_PERFORMANCE=1 go test ./internal/api/ -run TestPerformance -v
//
// The targets are deliberately unambitious, as the document says: the point is
// to notice if something goes badly wrong, not to optimize. A failure means an
// operation got slower by a large factor, which is worth looking at.

// perfEnabled reports whether the performance checks should run.
func perfEnabled(t *testing.T) bool {
	t.Helper()

	if os.Getenv("DOENERSTAG_PERFORMANCE") == "" {
		t.Skip("set DOENERSTAG_PERFORMANCE=1 to run the performance checks")
		return false
	}
	return true
}

// samples is how many times each operation is measured.
//
// Enough for a 95th percentile to mean something, few enough that the whole
// file finishes in about a minute against a container database.
const samples = 40

// percentile95 returns the 95th percentile of a set of durations.
func percentile95(d []time.Duration) time.Duration {
	if len(d) == 0 {
		return 0
	}
	sorted := make([]time.Duration, len(d))
	copy(sorted, d)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	// The nearest-rank definition: the smallest value at or above 95% of the
	// sample. Interpolating between two of forty measurements would suggest a
	// precision the measurement does not have.
	index := (len(sorted)*95 + 99) / 100
	if index > len(sorted) {
		index = len(sorted)
	}
	return sorted[index-1]
}

// measure runs op `samples` times and reports the 95th percentile against a
// budget.
func measure(t *testing.T, name string, budget time.Duration, op func()) {
	t.Helper()

	// One untimed pass: the first call through a path pays for a connection,
	// a prepared statement and a cold page cache, and none of that is what the
	// target is about.
	op()

	timings := make([]time.Duration, 0, samples)
	for range samples {
		start := time.Now()
		op()
		timings = append(timings, time.Since(start))
	}

	got := percentile95(timings)
	median := percentile50(timings)
	if got > budget {
		t.Errorf("%s: p95 %v, over the %v budget (median %v)", name, got, budget, median)
		return
	}
	t.Logf("%-46s p95 %8v  median %8v  budget %v", name, got, median, budget)
}

func percentile50(d []time.Duration) time.Duration {
	if len(d) == 0 {
		return 0
	}
	sorted := make([]time.Duration, len(d))
	copy(sorted, d)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return sorted[len(sorted)/2]
}

// perfFixture is a realistically sized database: the data volumes from the
// table in docs/11_nonfunctional.md, at the top of each range.
type perfFixture struct {
	*orderFixture
	bigOrder string
}

func newPerfFixture(t *testing.T) *perfFixture {
	t.Helper()

	o := newOrderFixture(t)
	p := &perfFixture{orderFixture: o}

	// A 300-item menu, which is the number the menu target names.
	category := o.addCategory("Vom Grill", 1)
	for i := 2; i <= 300; i++ {
		o.addItem(map[string]any{
			"name":        fmt.Sprintf("Gericht %d", i),
			"external_id": fmt.Sprintf("%d", i),
			"price_cents": 300 + i,
			"category_id": category.ID,
			"description": "Mit Salat und Soße nach Wahl.",
		})
	}

	// Thirty orders, which is close to the documented ceiling of forty live.
	for range 29 {
		o.createOrder(o.cookies, 3*time.Hour)
	}

	// And one order with fifty items, which the detail and summary targets name.
	p.bigOrder = o.order.ID
	for range 50 {
		o.addOrderItem(o.cookies, map[string]any{"quantity": 1})
	}

	return p
}

func TestPerformanceOfTheReadPaths(t *testing.T) {
	if !perfEnabled(t) {
		return
	}
	p := newPerfFixture(t)

	measure(t, "GET /orders", 50*time.Millisecond, func() {
		if rec := p.get("/orders", p.cookies...); rec.Code != http.StatusOK {
			t.Fatalf("listing orders: %d", rec.Code)
		}
	})

	measure(t, "GET /orders/{id} with 50 items", 100*time.Millisecond, func() {
		if rec := p.get("/orders/"+p.bigOrder, p.cookies...); rec.Code != http.StatusOK {
			t.Fatalf("reading the order: %d", rec.Code)
		}
	})

	measure(t, "GET /orders/{id}/summary", 100*time.Millisecond, func() {
		rec := p.get("/orders/"+p.bigOrder+"/summary", p.cookies...)
		if rec.Code != http.StatusOK {
			t.Fatalf("reading the summary: %d", rec.Code)
		}
	})

	measure(t, "GET /restaurants/{id}/menu-items, 300 items", 150*time.Millisecond, func() {
		rec := p.get(p.menuPath("/menu-items"), p.cookies...)
		if rec.Code != http.StatusOK {
			t.Fatalf("reading the menu: %d", rec.Code)
		}
	})
}

func TestPerformanceOfTheWritePaths(t *testing.T) {
	if !perfEnabled(t) {
		return
	}
	p := newPerfFixture(t)

	measure(t, "POST /orders/{id}/items", 100*time.Millisecond, func() {
		rec := p.post("/orders/"+p.bigOrder+"/items",
			map[string]any{"menu_item_id": p.doener.ID, "quantity": 1}, p.cookies...)
		if rec.Code != http.StatusCreated {
			t.Fatalf("adding an item: %d %s", rec.Code, rec.Body.String())
		}
	})
}

// Login is the slowest operation by design: Argon2id at 64 MiB and three
// iterations is what the 500 ms budget is mostly spent on.
func TestPerformanceOfLogin(t *testing.T) {
	if !perfEnabled(t) {
		return
	}
	f := newAPIFixture(t)
	f.register("anna")

	measure(t, "POST /auth/login", 500*time.Millisecond, func() {
		rec := f.post("/auth/login", map[string]string{
			"name": "anna", "password": validPassword,
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("logging in: %d %s", rec.Code, rec.Body.String())
		}
	})
}

// A 5 MiB upload, decoded, downscaled and re-encoded.
//
// The picture is generated at a size that encodes to roughly five megabytes,
// which is what --max-image-size defaults to and what the target names.
func TestPerformanceOfAnImageUpload(t *testing.T) {
	if !perfEnabled(t) {
		return
	}
	f := newAPIFixtureWithUploadLimit(t, 8*1024*1024)
	f.register("anna")
	cookies := f.register("photographer")

	picture := noisyPNG(t, 1500, 1100)
	t.Logf("the sample encodes to %d bytes", len(picture))
	if len(picture) < 4*1024*1024 {
		t.Fatalf("the sample is only %d bytes; the target is a 5 MiB upload", len(picture))
	}

	// Fewer samples: each one is a full decode and re-encode of several
	// megapixels, and forty of them would dominate the file's runtime for a
	// budget that is twenty times the next largest.
	timings := make([]time.Duration, 0, 5)
	for i := range 6 {
		start := time.Now()
		rec := f.uploadFile(fmt.Sprintf("gross-%d.png", i), picture, cookies)
		elapsed := time.Since(start)
		if rec.Code != http.StatusCreated {
			t.Fatalf("uploading: %d %s", rec.Code, rec.Body.String())
		}
		if i > 0 { // the first is the warm-up
			timings = append(timings, elapsed)
		}
	}

	const budget = 2 * time.Second
	if got := percentile95(timings); got > budget {
		t.Errorf("image upload: p95 %v, over the %v budget", got, budget)
	} else {
		t.Logf("%-46s p95 %8v  budget %v", "image upload, 5 MiB with downscale", got, budget)
	}
}

// An event has to reach a subscriber within half a second of the write that
// caused it.
func TestPerformanceOfEventDelivery(t *testing.T) {
	if !perfEnabled(t) {
		return
	}
	o := newOrderFixture(t)
	stream := o.openStream(o.order.ID, o.cookies)
	waitForSubscribers(t, o, 1)

	timings := make([]time.Duration, 0, 10)
	for range 10 {
		start := time.Now()
		o.addOrderItem(o.cookies, map[string]any{"quantity": 1})
		stream.waitFor("item.created")
		timings = append(timings, time.Since(start))
	}

	const budget = 500 * time.Millisecond
	if got := percentile95(timings); got > budget {
		t.Errorf("event delivery: p95 %v, over the %v budget", got, budget)
	} else {
		t.Logf("%-46s p95 %8v  budget %v", "SSE event after the causing write", got, budget)
	}
}

// The headroom check from docs/11: fifty simultaneous users and a hundred
// concurrent event streams, without degradation.
//
// "Without degradation" is read as: the read targets still hold while the load
// is on. A server that answers in 50 ms when idle and 5 s under fifty users has
// not met the requirement, and measuring it idle would not say so.
func TestConcurrencyHeadroom(t *testing.T) {
	if !perfEnabled(t) {
		return
	}
	p := newPerfFixture(t)

	// A hundred streams, held open for the duration.
	streams := make([]*stream, 0, 100)
	for range 100 {
		streams = append(streams, p.openStream(p.bigOrder, p.cookies))
	}
	// Opening a stream returns once the headers arrive, which can be before the
	// handler has registered with the hub. Load applied in that window would be
	// load with fewer streams than the check claims.
	waitForSubscribers(t, p.orderFixture, len(streams))
	t.Logf("%d streams open", len(streams))

	// Fifty callers reading at once, each doing what a person on the order
	// page does: list the orders, then open one.
	const users = 50
	var wg sync.WaitGroup
	slowest := make([]time.Duration, users)

	for i := range users {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 5 {
				start := time.Now()
				if rec := p.get("/orders", p.cookies...); rec.Code != http.StatusOK {
					t.Errorf("listing orders under load: %d", rec.Code)
					return
				}
				if rec := p.get("/orders/"+p.bigOrder, p.cookies...); rec.Code != http.StatusOK {
					t.Errorf("reading the order under load: %d", rec.Code)
					return
				}
				if elapsed := time.Since(start); elapsed > slowest[i] {
					slowest[i] = elapsed
				}
			}
		}()
	}
	wg.Wait()

	// The pair of reads has a combined budget of 150 ms when idle. Under fifty
	// concurrent users on a container database, allowing four times that is
	// still a long way from "degradation" and leaves the check meaningful.
	const budget = 600 * time.Millisecond
	got := percentile95(slowest)
	if got > budget {
		t.Errorf("under %d users and %d streams: p95 of the slowest round trip %v, over %v",
			users, len(streams), got, budget)
		return
	}
	t.Logf("%-46s p95 %8v  budget %v",
		fmt.Sprintf("list+detail under %d users, %d streams", users, len(streams)),
		got, budget)

	// And the streams are all still connected, which is the other half of
	// "without degradation": a server that shed them would look fast.
	if n := p.registry.Subscribers(mustParseUUID(t, p.bigOrder)); n != len(streams) {
		t.Errorf("%d of %d streams are still subscribed", n, len(streams))
	}
}

// noisyPNG builds a picture PNG cannot compress.
//
// samplePNG paints a smooth gradient, which is the right shape for the upload
// tests and the wrong one here: 2400x1600 of it encodes to fifteen kilobytes,
// so timing it would measure nothing like a five megabyte upload. Noise leaves
// the filters and the deflate pass nothing to find, so the encoded size is
// close to three bytes per pixel.
//
// The generator is a fixed-seed linear congruential sequence rather than
// math/rand: the same bytes every run means a slow run can be compared with a
// fast one.
func noisyPNG(t *testing.T, width, height int) []byte {
	t.Helper()

	img := stdimage.NewRGBA(stdimage.Rect(0, 0, width, height))
	state := uint32(0x9e3779b9)
	next := func() uint8 {
		state = state*1664525 + 1013904223
		return uint8(state >> 24) //nolint:gosec // the top byte of a uint32 is a byte
	}

	for y := range height {
		for x := range width {
			img.Set(x, y, color.RGBA{R: next(), G: next(), B: next(), A: 255})
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encoding the sample: %v", err)
	}
	return buf.Bytes()
}
