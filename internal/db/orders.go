package db

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// IsParticipant reports whether a user is a participant of an order.
//
// docs/05_auth_and_permissions.md defines a participant as the order's creator,
// or any user holding at least one order_item in it. Participation is derived,
// never stored: adding an item makes you one immediately, and removing your
// last item stops you being one. This query is that definition, so it is the
// only place the rule is written down in code.
//
// The administrator also counts, but that is decided by the caller: it is a
// property of the acting user, not of the order, and folding it in here would
// mean every future caller had to remember that this function quietly answers
// yes for one particular account.
//
// A missing order is ErrNotFound rather than false. "You are not a participant
// of an order that does not exist" is a confusing thing to be told, and the two
// deserve different answers.
func IsParticipant(ctx context.Context, q Querier, orderID, userID uuid.UUID) (bool, error) {
	var participant bool
	err := q.QueryRow(ctx, `
		SELECT o.creator_id = $2
		    OR EXISTS (
		           SELECT 1 FROM order_item i
		           WHERE i.order_id = o.id AND i.user_id = $2
		       )
		FROM food_order o
		WHERE o.id = $1`,
		orderID, userID).Scan(&participant)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, ErrNotFound
		}
		return false, err
	}
	return participant, nil
}
