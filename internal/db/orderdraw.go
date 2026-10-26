package db

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/joernott/doenerstag/internal/model"
)

// DrawJobs makes the draw for every order whose deadline has passed and whose
// draw has not been made (21.3), and returns the orders whose jobs it filled.
//
// For each such order, one person is picked at random from those with an item
// in it and put in every job nobody has: one person for both when both are
// empty, because that person is going to the restaurant either way. A job
// somebody took is left alone, and an order with no items -- or only the
// deleted-user placeholder's -- is marked drawn with nobody picked.
//
// The pick is uniform over people, not items: the distinct people are
// gathered first, so somebody who ordered five things is not five times as
// likely to be sent. One statement, and SKIP LOCKED with the jobs_drawn flag,
// so two processes or two ticks never draw the same order twice.
func DrawJobs(ctx context.Context, q Querier, now time.Time) ([]uuid.UUID, error) {
	rows, err := q.Query(ctx, `
		WITH due AS (
			SELECT id, money_collector_id, pickup_person_id
			FROM food_order
			WHERE NOT jobs_drawn AND deadline_at <= $1
			FOR UPDATE SKIP LOCKED
		),
		people AS (
			SELECT DISTINCT i.order_id, i.user_id
			FROM order_item i
			JOIN due d ON d.id = i.order_id
			WHERE i.user_id <> $2
			  AND (d.money_collector_id IS NULL OR d.pickup_person_id IS NULL)
		),
		picked AS (
			SELECT DISTINCT ON (order_id) order_id, user_id
			FROM people
			ORDER BY order_id, random()
		)
		UPDATE food_order o
		   SET jobs_drawn            = true,
		       money_collector_drawn = (o.money_collector_id IS NULL AND p.user_id IS NOT NULL),
		       pickup_person_drawn   = (o.pickup_person_id IS NULL AND p.user_id IS NOT NULL),
		       money_collector_id    = coalesce(o.money_collector_id, p.user_id),
		       pickup_person_id      = coalesce(o.pickup_person_id, p.user_id)
		  FROM due d
		  LEFT JOIN picked p ON p.order_id = d.id
		 WHERE o.id = d.id
		RETURNING o.id, p.user_id IS NOT NULL`,
		at(now), model.DeletedUserID)
	if err != nil {
		return nil, err
	}

	type outcome struct {
		id     uuid.UUID
		picked bool
	}
	outcomes, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (outcome, error) {
		var o outcome
		err := row.Scan(&o.id, &o.picked)
		return o, err
	})
	if err != nil {
		return nil, err
	}

	filled := make([]uuid.UUID, 0, len(outcomes))
	for _, o := range outcomes {
		if o.picked {
			filled = append(filled, o.id)
		}
	}
	return filled, nil
}
