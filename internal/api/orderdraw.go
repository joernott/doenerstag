package api

import (
	"context"
	"time"

	"github.com/joernott/doenerstag/internal/db"
)

// DrawInterval is how often the server looks for orders whose deadline has
// passed with a job nobody took (21.3). The order page has already switched
// to closed at the deadline itself; the name arrives within this much after.
const DrawInterval = 15 * time.Second

// DrawJobs makes the draw for every order that is due one and tells the
// pages watching those orders who was picked. Exported so a test can stand at
// a deadline and run it once, rather than waiting for the loop.
func (h *OrderHandlers) DrawJobs(ctx context.Context) error {
	filled, err := db.DrawJobs(ctx, h.Pool, h.now())
	if err != nil {
		return err
	}
	for _, id := range filled {
		order, err := db.OrderByID(ctx, h.Pool, id)
		if err != nil {
			return err
		}
		h.publishOrderChange(order)
	}
	return nil
}

// RunDraws calls DrawJobs now and then every interval until ctx ends.
//
// Run by the server rather than hung off a request, because nothing happens
// at a deadline except that the clock passes it, and the pick must be made
// whether or not anybody has the page open. Running once at start is what
// makes a restart across a deadline harmless: the first pass draws whatever
// fell due while the server was down. A failed pass is logged and the next
// one tries again; the orders it missed are still due.
func (h *OrderHandlers) RunDraws(ctx context.Context, interval time.Duration) {
	if h.Pool == nil {
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		if err := h.DrawJobs(ctx); err != nil && ctx.Err() == nil {
			requestLogger(h.Logger, nil).Warn().Err(err).
				Msg("drawing people for the jobs of closed orders failed; trying again")
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
