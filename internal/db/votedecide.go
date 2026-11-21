package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/joernott/doenerstag/internal/model"
)

// DecidedVote is what one decision produced.
type DecidedVote struct {
	VoteID  uuid.UUID
	Outcome string
	OrderID *uuid.UUID
}

// DecideVotes decides every vote whose decision time has passed (F12.9) and
// returns what each became.
//
// One transaction per vote, so a failure in one leaves the others decided
// and itself due for the next pass. The vote row is taken with SKIP LOCKED:
// a second process or an overlapping pass skips a vote somebody is already
// deciding, and the `WHERE NOT decided` on the final UPDATE means a vote is
// decided once even if two passes did read it.
func DecideVotes(ctx context.Context, pool Pool, now time.Time) ([]DecidedVote, error) {
	stamp := at(now)

	rows, err := pool.Query(ctx, `
		SELECT id FROM vote WHERE NOT decided AND decision_at <= $1 ORDER BY decision_at`, stamp)
	if err != nil {
		return nil, err
	}
	due, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, err
	}

	decided := make([]DecidedVote, 0, len(due))
	for _, id := range due {
		result, ok, err := decideVote(ctx, pool, id, stamp)
		if err != nil {
			return decided, fmt.Errorf("deciding vote %s: %w", id, err)
		}
		if ok {
			decided = append(decided, result)
		}
	}
	return decided, nil
}

func decideVote(ctx context.Context, pool Pool, id uuid.UUID, now time.Time) (DecidedVote, bool, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return DecidedVote{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var locked uuid.UUID
	err = tx.QueryRow(ctx, `
		SELECT id FROM vote WHERE id = $1 AND NOT decided FOR UPDATE SKIP LOCKED`, id).Scan(&locked)
	if errors.Is(err, pgx.ErrNoRows) {
		// Decided by somebody else, deleted, or being decided right now.
		return DecidedVote{}, false, nil
	}
	if err != nil {
		return DecidedVote{}, false, err
	}

	vote, err := VoteByID(ctx, tx, id)
	if err != nil {
		return DecidedVote{}, false, err
	}

	result := DecidedVote{VoteID: id}
	var winner *uuid.UUID
	leader := vote.Leader()

	switch {
	case leader == nil:
		// F12.10: nobody voted, so there is nothing to order.
		result.Outcome = model.VoteNoBallots
	case !vote.DeadlineAt.After(now):
		// F12.10: decided only after the order deadline -- the server was
		// down past both -- and an order born closed is no use (F5.3a).
		result.Outcome = model.VoteTooLate
	default:
		order, err := CreateOrder(ctx, tx, NewOrder{
			CreatorID:    vote.CreatorID,
			RestaurantID: leader.RestaurantID,
			Fulfilment:   model.FulfilmentPickup,
			FulfilmentAt: vote.FulfilmentAt,
			DeadlineAt:   vote.DeadlineAt,
		})
		if err != nil {
			return DecidedVote{}, false, fmt.Errorf("creating the order: %w", err)
		}
		result.Outcome = model.VoteOrdered
		result.OrderID = &order.ID
		winner = &leader.ID
	}

	// The counts the vote was decided with, kept so a voter's later account
	// deletion does not change the record (F12.12).
	if _, err := tx.Exec(ctx, `
		UPDATE vote_choice c
		   SET final_count = (SELECT count(*) FROM vote_ballot b
		                      WHERE b.vote_id = c.vote_id AND b.choice_id = c.id)
		 WHERE c.vote_id = $1`, id); err != nil {
		return DecidedVote{}, false, err
	}

	tag, err := tx.Exec(ctx, `
		UPDATE vote
		   SET decided = true, outcome = $2, winner_choice_id = $3, order_id = $4
		 WHERE id = $1 AND NOT decided`,
		id, result.Outcome, winner, result.OrderID)
	if err != nil {
		return DecidedVote{}, false, err
	}
	if tag.RowsAffected() == 0 {
		return DecidedVote{}, false, nil
	}
	if err := tx.Commit(ctx); err != nil {
		return DecidedVote{}, false, err
	}
	return result, true, nil
}
