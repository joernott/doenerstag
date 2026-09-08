package db

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/joernott/doenerstag/internal/model"
)

// ListOpeningHours returns a restaurant's opening periods.
//
// Ordered by weekday then start time, so a lunch break reads in the order a
// person would say it: Monday 11:00-14:00, then Monday 17:00-22:00.
func ListOpeningHours(ctx context.Context, q Querier, restaurantID uuid.UUID) ([]model.OpeningPeriod, error) {
	rows, err := q.Query(ctx, `
		SELECT id, restaurant_id, day_of_week,
		       to_char(start_time, 'HH24:MI'), to_char(end_time, 'HH24:MI')
		FROM opening_hours
		WHERE restaurant_id = $1
		ORDER BY day_of_week, start_time`, restaurantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	periods := make([]model.OpeningPeriod, 0, 14)
	for rows.Next() {
		var p model.OpeningPeriod
		if err := rows.Scan(&p.ID, &p.RestaurantID, &p.DayOfWeek, &p.Start, &p.End); err != nil {
			return nil, err
		}
		periods = append(periods, p)
	}
	return periods, rows.Err()
}

// ReplaceOpeningHours swaps the whole set for a restaurant.
//
// The whole set at once, per docs/04_api.md, and it is the right shape for
// this data: opening hours are edited as a weekly pattern, not one interval at
// a time, and a PUT of the lot has no ordering problem between the delete of a
// Tuesday period and the insert of its replacement.
//
// In a transaction, so that a rejected row leaves the previous week's hours
// intact rather than half of them.
func ReplaceOpeningHours(
	ctx context.Context, pool interface {
		Querier
		Begin(ctx context.Context) (pgx.Tx, error)
	},
	restaurantID uuid.UUID, periods []model.OpeningPeriod, actor uuid.UUID,
) ([]model.OpeningPeriod, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("starting the opening hours update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`DELETE FROM opening_hours WHERE restaurant_id = $1`, restaurantID); err != nil {
		return nil, fmt.Errorf("clearing the opening hours: %w", err)
	}

	for _, p := range periods {
		id, err := uuid.NewV7()
		if err != nil {
			return nil, fmt.Errorf("generating an opening hours id: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO opening_hours (id, restaurant_id, day_of_week, start_time, end_time,
			                           created_by, updated_by)
			VALUES ($1, $2, $3, $4::time, $5::time, $6, $6)`,
			id, restaurantID, p.DayOfWeek, p.Start, p.End, actor); err != nil {
			return nil, fmt.Errorf("storing opening hours for day %d: %w", p.DayOfWeek, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("committing the opening hours: %w", err)
	}

	return ListOpeningHours(ctx, pool, restaurantID)
}
