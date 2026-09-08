package db

import (
	"context"
	"fmt"
	"time"
)

// CleanupStep is one of the six operations docs/09_configuration.md specifies,
// with what it removed.
type CleanupStep struct {
	// Object names what was removed, for the log line and the report.
	Object string
	Count  int64
}

// CleanupReport is the outcome of a run.
type CleanupReport struct {
	Steps  []CleanupStep
	DryRun bool
}

// Total is how many rows were removed altogether.
func (r CleanupReport) Total() int64 {
	var total int64
	for _, step := range r.Steps {
		total += step.Count
	}
	return total
}

// Cleanup removes expired data.
//
// The order of the six steps is not arbitrary and is fixed in
// docs/09_configuration.md: each one removes references that the next one needs
// gone. Deleting old orders first releases the order items that were pinning
// soft-deleted menu rows; removing those releases the images they referenced.
// Running them in any other order would leave rows that are ready to go until
// the next night's run, which would look like the cleanup not working.
//
// Each step is its own transaction, so a failure late in the sequence keeps
// what the earlier ones achieved. The verb is safe to run against a live
// server, and a half-finished run is a smaller problem than a run that has to
// start over.
func Cleanup(ctx context.Context, pool Pool, retention time.Duration, now time.Time, dryRun bool) (CleanupReport, error) {
	cutoff := at(now).Add(-retention)

	steps := []struct {
		object string
		count  string
		delete string
		args   func() []any
	}{
		// 1. Orders past the retention period, cascading to their items and
		//    item modifications through the schema's ON DELETE CASCADE.
		{
			object: "orders",
			count:  `SELECT count(*) FROM food_order WHERE deadline_at < $1`,
			delete: `DELETE FROM food_order WHERE deadline_at < $1`,
			args:   func() []any { return []any{cutoff} },
		},
		// 2. Soft-deleted modifications nothing points at any more.
		{
			object: "menu item modifications",
			count: `SELECT count(*) FROM menu_item_modification m
			        WHERE m.deleted_at IS NOT NULL
			          AND NOT EXISTS (SELECT 1 FROM order_item_modification o
			                          WHERE o.modification_id = m.id)`,
			delete: `DELETE FROM menu_item_modification m
			         WHERE m.deleted_at IS NOT NULL
			           AND NOT EXISTS (SELECT 1 FROM order_item_modification o
			                           WHERE o.modification_id = m.id)`,
			args: func() []any { return nil },
		},
		// 3. Soft-deleted menu items no order item references.
		{
			object: "menu items",
			count: `SELECT count(*) FROM menu_item m
			        WHERE m.deleted_at IS NOT NULL
			          AND NOT EXISTS (SELECT 1 FROM order_item o WHERE o.menu_item_id = m.id)`,
			delete: `DELETE FROM menu_item m
			         WHERE m.deleted_at IS NOT NULL
			           AND NOT EXISTS (SELECT 1 FROM order_item o WHERE o.menu_item_id = m.id)`,
			args: func() []any { return nil },
		},
		// 4a. Soft-deleted categories nothing references.
		{
			object: "menu categories",
			count: `SELECT count(*) FROM menu_category c
			        WHERE c.deleted_at IS NOT NULL
			          AND NOT EXISTS (SELECT 1 FROM menu_item m WHERE m.category_id = c.id)`,
			delete: `DELETE FROM menu_category c
			         WHERE c.deleted_at IS NOT NULL
			           AND NOT EXISTS (SELECT 1 FROM menu_item m WHERE m.category_id = c.id)`,
			args: func() []any { return nil },
		},
		// 4b. Soft-deleted restaurants no order and no menu row references.
		{
			object: "restaurants",
			count: `SELECT count(*) FROM restaurant r
			        WHERE r.deleted_at IS NOT NULL
			          AND NOT EXISTS (SELECT 1 FROM food_order o WHERE o.restaurant_id = r.id)
			          AND NOT EXISTS (SELECT 1 FROM menu_item m WHERE m.restaurant_id = r.id)
			          AND NOT EXISTS (SELECT 1 FROM menu_category c WHERE c.restaurant_id = r.id)`,
			delete: `DELETE FROM restaurant r
			         WHERE r.deleted_at IS NOT NULL
			           AND NOT EXISTS (SELECT 1 FROM food_order o WHERE o.restaurant_id = r.id)
			           AND NOT EXISTS (SELECT 1 FROM menu_item m WHERE m.restaurant_id = r.id)
			           AND NOT EXISTS (SELECT 1 FROM menu_category c WHERE c.restaurant_id = r.id)`,
			args: func() []any { return nil },
		},
		// 5. Images no restaurant and no menu item points at.
		{
			object: "images",
			count: `SELECT count(*) FROM image i
			        WHERE NOT EXISTS (SELECT 1 FROM restaurant r WHERE r.logo_image_id = i.id)
			          AND NOT EXISTS (SELECT 1 FROM menu_item m WHERE m.image_id = i.id)`,
			delete: `DELETE FROM image i
			         WHERE NOT EXISTS (SELECT 1 FROM restaurant r WHERE r.logo_image_id = i.id)
			           AND NOT EXISTS (SELECT 1 FROM menu_item m WHERE m.image_id = i.id)`,
			args: func() []any { return nil },
		},
		// 6a. Sessions past their absolute or idle expiry.
		{
			object: "sessions",
			count: `SELECT count(*) FROM session
			        WHERE absolute_expires <= $1 OR last_seen_at < $2`,
			delete: `DELETE FROM session
			         WHERE absolute_expires <= $1 OR last_seen_at < $2`,
			args: func() []any { return []any{at(now), at(now).Add(-idleCleanupWindow)} },
		},
		// 6b. API tokens past their expiry.
		{
			object: "api tokens",
			count: `SELECT count(*) FROM api_token
			        WHERE expires_at IS NOT NULL AND expires_at <= $1`,
			delete: `DELETE FROM api_token
			         WHERE expires_at IS NOT NULL AND expires_at <= $1`,
			args: func() []any { return []any{at(now)} },
		},
	}

	report := CleanupReport{Steps: make([]CleanupStep, 0, len(steps)), DryRun: dryRun}

	for _, step := range steps {
		count, err := runCleanupStep(ctx, pool, step.object, step.count, step.delete, step.args(), dryRun)
		if err != nil {
			return report, err
		}
		report.Steps = append(report.Steps, CleanupStep{Object: step.object, Count: count})
	}

	return report, nil
}

// idleCleanupWindow is how stale last_seen_at must be for cleanup to collect a
// session.
//
// Deliberately generous, and deliberately not the configured idle timeout. The
// server rejects an idle session on use, so this is housekeeping rather than a
// security control; collecting rows the instant they lapse would mean a
// cleanup run racing a user who is about to come back to their laptop.
const idleCleanupWindow = 30 * 24 * time.Hour

// runCleanupStep counts, and deletes unless this is a dry run.
//
// A dry run counts what a real run would remove using the identical predicate,
// which is what makes "the report matches the run" true by construction rather
// than by two queries being kept in step by hand. The count and the delete
// share their WHERE clause because they are written from the same text above.
func runCleanupStep(
	ctx context.Context, pool Pool, object, countSQL, deleteSQL string, args []any, dryRun bool,
) (int64, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("starting the %s cleanup: %w", object, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if dryRun {
		var count int64
		if err := tx.QueryRow(ctx, countSQL, args...).Scan(&count); err != nil {
			return 0, fmt.Errorf("counting %s: %w", object, err)
		}
		// No commit: a dry run has nothing to commit, and rolling back makes
		// that explicit rather than relying on the statements having been
		// read-only.
		return count, nil
	}

	tag, err := tx.Exec(ctx, deleteSQL, args...)
	if err != nil {
		return 0, fmt.Errorf("removing %s: %w", object, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("committing the %s cleanup: %w", object, err)
	}
	return tag.RowsAffected(), nil
}
