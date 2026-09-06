package db

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/joernott/doenerstag/internal/model"
)

// DeletionImpact is what deleting an account would do, per F2.6.
//
// It is shown before the deletion so the user can cancel, which is the only
// warning they get: the deletion itself is immediate and irreversible.
type DeletionImpact struct {
	// ActiveOrders is how many still-open orders the user participates in.
	ActiveOrders int
	// ActiveItems is how many of their items would be deleted outright.
	ActiveItems int
	// ExpiredItems is how many would be reassigned to the placeholder and shown
	// as "deleted user".
	ExpiredItems int
	// CreatedActiveOrders is how many open orders they created, which pass to
	// the placeholder rather than being removed.
	CreatedActiveOrders int
}

// ImpactOfDeleting counts what would change, without changing anything.
//
// An order is active while now is before its deadline, which is what
// food_order.deadline_at means. The time comes from the caller so that the
// count and the deletion that follows agree about which orders are expired --
// two calls to now() a second apart could otherwise disagree about an order
// whose deadline falls between them.
func ImpactOfDeleting(ctx context.Context, q Querier, userID uuid.UUID, now time.Time) (DeletionImpact, error) {
	var impact DeletionImpact

	err := q.QueryRow(ctx, `
		SELECT
			(SELECT count(DISTINCT o.id)
			   FROM food_order o
			   LEFT JOIN order_item i ON i.order_id = o.id AND i.user_id = $1
			  WHERE o.deadline_at > $2
			    AND (o.creator_id = $1 OR i.id IS NOT NULL)),
			(SELECT count(*)
			   FROM order_item i JOIN food_order o ON o.id = i.order_id
			  WHERE i.user_id = $1 AND o.deadline_at > $2),
			(SELECT count(*)
			   FROM order_item i JOIN food_order o ON o.id = i.order_id
			  WHERE i.user_id = $1 AND o.deadline_at <= $2),
			(SELECT count(*)
			   FROM food_order o
			  WHERE o.creator_id = $1 AND o.deadline_at > $2)`,
		userID, at(now)).
		Scan(&impact.ActiveOrders, &impact.ActiveItems,
			&impact.ExpiredItems, &impact.CreatedActiveOrders)
	if err != nil {
		return DeletionImpact{}, err
	}
	return impact, nil
}

// DeleteAccount applies F2.6 and removes the account.
//
// The three rules, in order:
//
//   - Items in expired orders are reassigned to the placeholder, so the order
//     still reads "1x Döner, no onions" without naming anyone.
//   - Items in active orders are deleted outright. Nobody is going to collect
//     money for a kebab whose owner has left, and leaving them would make the
//     order's total wrong for everyone still in it.
//   - Orders created by the user pass to the placeholder, so they survive and
//     the administrator can still delete them.
//
// All of it in one transaction. A partial deletion -- items reassigned but the
// account still present, or the account gone and its items orphaned -- is worse
// than either outcome, and the account row's foreign keys would produce exactly
// that if the steps were run separately and one failed.
//
// The final delete goes through the same trigger that protects the placeholder
// and the administrator, so this cannot remove either even if a caller asks.
func DeleteAccount(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID, now time.Time) error {
	stamp := at(now)

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("starting the deletion: %w", err)
	}
	// Rollback after a successful commit is a no-op, which is what makes this
	// safe to defer unconditionally.
	defer func() { _ = tx.Rollback(ctx) }()

	if err := remapAndDelete(ctx, tx, userID, stamp); err != nil {
		return err
	}

	if err := commitDeletion(ctx, tx); err != nil {
		return err
	}
	return nil
}

func remapAndDelete(ctx context.Context, tx pgx.Tx, userID uuid.UUID, now time.Time) error {
	// Items in expired orders: reassign.
	if _, err := tx.Exec(ctx, `
		UPDATE order_item SET user_id = $2, updated_by = $2
		WHERE user_id = $1
		  AND order_id IN (SELECT id FROM food_order WHERE deadline_at <= $3)`,
		userID, model.DeletedUserID, now); err != nil {
		return fmt.Errorf("reassigning items in expired orders: %w", err)
	}

	// Items in active orders: delete.
	if _, err := tx.Exec(ctx, `
		DELETE FROM order_item
		WHERE user_id = $1
		  AND order_id IN (SELECT id FROM food_order WHERE deadline_at > $2)`,
		userID, now); err != nil {
		return fmt.Errorf("removing items from active orders: %w", err)
	}

	// Orders created by the user: reassign, active or not.
	if _, err := tx.Exec(ctx, `
		UPDATE food_order SET creator_id = $2, updated_by = $2 WHERE creator_id = $1`,
		userID, model.DeletedUserID); err != nil {
		return fmt.Errorf("reassigning created orders: %w", err)
	}

	tag, err := tx.Exec(ctx, `DELETE FROM app_user WHERE id = $1`, userID)
	if err != nil {
		return fmt.Errorf("deleting the account: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func commitDeletion(ctx context.Context, tx pgx.Tx) error {
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing the deletion: %w", err)
	}
	return nil
}
