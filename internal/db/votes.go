package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/joernott/doenerstag/internal/model"
)

// The vote's own refusals, each the schema's answer to a write that breaks
// one of F12's rules. The API checks the same rules first, for the error
// numbers; these are what is left when two requests race past those checks.
var (
	// ErrChoiceLimit: the write would leave a vote with fewer than two or
	// more than five restaurants.
	ErrChoiceLimit = errors.New("db: a vote offers two to five restaurants")
	// ErrChoiceExists: the restaurant is already in the vote.
	ErrChoiceExists = errors.New("db: the restaurant is already in this vote")
	// ErrChoiceChosen: somebody's ballot names the choice.
	ErrChoiceChosen = errors.New("db: somebody has chosen this restaurant")
	// ErrChoiceWrongVote: a ballot names a choice of another vote.
	ErrChoiceWrongVote = errors.New("db: the choice does not belong to this vote")
)

// constraintName is the name of the constraint a PostgreSQL error is about.
func constraintName(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.ConstraintName
	}
	return ""
}

// voteError maps the schema's refusals onto the sentinels above.
func voteError(err error) error {
	switch constraintName(err) {
	case "vote_choice_count":
		return ErrChoiceLimit
	case "vote_choice_once_per_vote":
		return ErrChoiceExists
	case "vote_ballot_choice_of_vote":
		return ErrChoiceChosen
	}
	return err
}

const voteColumns = `v.id, v.creator_id, coalesce(u.display_name, u.name),
	v.decision_at, v.deadline_at, v.fulfilment_at,
	v.open_choices, v.public_ballots,
	v.decided, v.outcome, v.winner_choice_id, v.order_id,
	v.created_at, v.updated_at`

const voteFrom = `
	FROM vote v
	JOIN app_user u ON u.id = v.creator_id`

func scanVote(row pgx.Row) (model.Vote, error) {
	var v model.Vote
	err := row.Scan(&v.ID, &v.CreatorID, &v.CreatorName,
		&v.DecisionAt, &v.DeadlineAt, &v.FulfilmentAt,
		&v.OpenChoices, &v.PublicBallots,
		&v.Decided, &v.Outcome, &v.WinnerChoiceID, &v.OrderID,
		&v.CreatedAt, &v.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.Vote{}, ErrNotFound
	}
	return v, err
}

// VoteByID reads one vote with its choices, counts and voters.
func VoteByID(ctx context.Context, q Querier, id uuid.UUID) (model.Vote, error) {
	v, err := scanVote(q.QueryRow(ctx, `SELECT `+voteColumns+voteFrom+` WHERE v.id = $1`, id))
	if err != nil {
		return model.Vote{}, err
	}
	votes := []model.Vote{v}
	if err := loadChoices(ctx, q, votes); err != nil {
		return model.Vote{}, err
	}
	return votes[0], nil
}

// ListOpenVotes returns the undecided votes, soonest fulfilment first, as the
// start page sorts the orders beside them (docs/06).
func ListOpenVotes(ctx context.Context, q Querier) ([]model.Vote, error) {
	rows, err := q.Query(ctx, `SELECT `+voteColumns+voteFrom+`
		WHERE NOT v.decided
		ORDER BY v.fulfilment_at, v.id`)
	if err != nil {
		return nil, err
	}
	votes, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.Vote, error) {
		return scanVote(row)
	})
	if err != nil {
		return nil, err
	}
	if err := loadChoices(ctx, q, votes); err != nil {
		return nil, err
	}
	return votes, nil
}

// loadChoices fills in the choices of every vote given, with two queries
// however many votes there are.
func loadChoices(ctx context.Context, q Querier, votes []model.Vote) error {
	if len(votes) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(votes))
	index := make(map[uuid.UUID]int, len(votes))
	for i := range votes {
		ids[i] = votes[i].ID
		index[votes[i].ID] = i
		votes[i].Choices = []model.VoteChoice{}
	}

	// A decided vote reports its stored final count, so its numbers do not
	// move when a voter deletes their account (F12.12, F12.14).
	rows, err := q.Query(ctx, `
		SELECT c.vote_id, c.id, c.restaurant_id, r.name, r.logo_image_id,
		       c.added_by_id, coalesce(a.display_name, a.name),
		       coalesce(c.final_count, (SELECT count(*) FROM vote_ballot b WHERE b.choice_id = c.id AND b.vote_id = c.vote_id))::int,
		       (SELECT max(b.cast_at) FROM vote_ballot b WHERE b.choice_id = c.id AND b.vote_id = c.vote_id)
		FROM vote_choice c
		JOIN restaurant r ON r.id = c.restaurant_id
		JOIN app_user a ON a.id = c.added_by_id
		WHERE c.vote_id = ANY($1)
		ORDER BY c.vote_id, c.id`, ids)
	if err != nil {
		return err
	}
	type located struct {
		vote   uuid.UUID
		choice model.VoteChoice
	}
	choices, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (located, error) {
		var l located
		err := row.Scan(&l.vote, &l.choice.ID, &l.choice.RestaurantID, &l.choice.RestaurantName,
			&l.choice.RestaurantLogo, &l.choice.AddedByID, &l.choice.AddedByName,
			&l.choice.Count, &l.choice.LastCastAt)
		l.choice.Voters = []model.Voter{}
		return l, err
	})
	if err != nil {
		return err
	}
	for i := range choices {
		v := &votes[index[choices[i].vote]]
		v.Choices = append(v.Choices, choices[i].choice)
	}
	// Where each choice ended up, for the voters below. Taken after every
	// append, so no pointer is left behind by a slice that grew.
	where := make(map[uuid.UUID]*model.VoteChoice, len(choices))
	for i := range votes {
		list := votes[i].Choices
		for j := range list {
			c := &list[j]
			where[c.ID] = c
		}
	}

	rows, err = q.Query(ctx, `
		SELECT b.choice_id, b.user_id, coalesce(u.display_name, u.name)
		FROM vote_ballot b
		JOIN app_user u ON u.id = b.user_id
		WHERE b.vote_id = ANY($1)
		ORDER BY b.cast_at, b.user_id`, ids)
	if err != nil {
		return err
	}
	type ballot struct {
		choice uuid.UUID
		voter  model.Voter
	}
	ballots, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ballot, error) {
		var b ballot
		err := row.Scan(&b.choice, &b.voter.ID, &b.voter.Name)
		return b, err
	})
	if err != nil {
		return err
	}
	for _, b := range ballots {
		if c, ok := where[b.choice]; ok {
			c.Voters = append(c.Voters, b.voter)
		}
	}
	return nil
}

// NewVote is what creation supplies.
type NewVote struct {
	CreatorID     uuid.UUID
	RestaurantIDs []uuid.UUID
	DecisionAt    time.Time
	DeadlineAt    time.Time
	FulfilmentAt  time.Time
	OpenChoices   bool
	PublicBallots bool
}

// CreateVote inserts a vote and its restaurants in one transaction.
//
// A restaurant that does not exist or has been deleted inserts nothing and
// answers ErrUnknownReference; the API has already checked, so this is the
// race with a deletion.
func CreateVote(ctx context.Context, pool Pool, in NewVote) (model.Vote, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return model.Vote{}, fmt.Errorf("generating a vote id: %w", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return model.Vote{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		INSERT INTO vote (id, creator_id, decision_at, deadline_at, fulfilment_at,
		                  open_choices, public_ballots, created_by, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $2, $2)`,
		id, in.CreatorID, in.DecisionAt, in.DeadlineAt, in.FulfilmentAt,
		in.OpenChoices, in.PublicBallots); err != nil {
		return model.Vote{}, err
	}
	for _, restaurant := range in.RestaurantIDs {
		if err := insertChoice(ctx, tx, id, restaurant, in.CreatorID); err != nil {
			return model.Vote{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return model.Vote{}, voteError(err)
	}
	return VoteByID(ctx, pool, id)
}

func insertChoice(ctx context.Context, q Querier, voteID, restaurantID, actor uuid.UUID) error {
	choiceID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("generating a choice id: %w", err)
	}
	tag, err := q.Exec(ctx, `
		INSERT INTO vote_choice (id, vote_id, restaurant_id, added_by_id, created_by, updated_by)
		SELECT $1, $2, r.id, $4, $4, $4
		FROM restaurant r
		WHERE r.id = $3 AND r.deleted_at IS NULL`,
		choiceID, voteID, restaurantID, actor)
	if err != nil {
		return voteError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrUnknownReference
	}
	return nil
}

// VoteUpdate carries the fields a PATCH may change; nil leaves one alone.
type VoteUpdate struct {
	DecisionAt    *time.Time
	DeadlineAt    *time.Time
	FulfilmentAt  *time.Time
	OpenChoices   *bool
	PublicBallots *bool
}

// UpdateVote changes an undecided vote. A vote decided meanwhile answers
// ErrNotFound, which the API has already ruled out with its own check.
func UpdateVote(ctx context.Context, q Querier, id, actor uuid.UUID, in VoteUpdate) (model.Vote, error) {
	sets := []string{"updated_by = $2"}
	args := []any{id, actor}
	// Each clause is a fixed literal with only its placeholder number
	// formatted in, as UpdateOrder's are (sqlsafety_test.go).
	add := func(clause string, value any) {
		args = append(args, value)
		sets = append(sets, fmt.Sprintf(clause, len(args)))
	}
	if in.DecisionAt != nil {
		add("decision_at = $%d", *in.DecisionAt)
	}
	if in.DeadlineAt != nil {
		add("deadline_at = $%d", *in.DeadlineAt)
	}
	if in.FulfilmentAt != nil {
		add("fulfilment_at = $%d", *in.FulfilmentAt)
	}
	if in.OpenChoices != nil {
		add("open_choices = $%d", *in.OpenChoices)
	}
	if in.PublicBallots != nil {
		add("public_ballots = $%d", *in.PublicBallots)
	}

	tag, err := q.Exec(ctx, `UPDATE vote SET `+strings.Join(sets, ", ")+
		` WHERE id = $1 AND NOT decided`, args...)
	if err != nil {
		return model.Vote{}, err
	}
	if tag.RowsAffected() == 0 {
		return model.Vote{}, ErrNotFound
	}
	return VoteByID(ctx, q, id)
}

// DeleteVote removes a vote with its choices and ballots.
func DeleteVote(ctx context.Context, q Querier, id uuid.UUID) error {
	tag, err := q.Exec(ctx, `DELETE FROM vote WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// AddVoteChoice adds a restaurant to an undecided vote.
//
// The vote is locked first, so a decision in progress finishes before this
// looks, and a vote decided meanwhile answers ErrNotFound.
func AddVoteChoice(ctx context.Context, pool Pool, voteID, restaurantID, actor uuid.UUID) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var decided bool
	err = tx.QueryRow(ctx, `SELECT decided FROM vote WHERE id = $1 FOR NO KEY UPDATE`, voteID).Scan(&decided)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && decided) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if err := insertChoice(ctx, tx, voteID, restaurantID, actor); err != nil {
		return err
	}
	return voteError(tx.Commit(ctx))
}

// RemoveVoteChoice removes a restaurant from an undecided vote.
func RemoveVoteChoice(ctx context.Context, pool Pool, voteID, choiceID uuid.UUID) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `
		DELETE FROM vote_choice c
		USING vote v
		WHERE c.id = $2 AND c.vote_id = $1 AND v.id = c.vote_id AND NOT v.decided`,
		voteID, choiceID)
	if err != nil {
		return voteError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return voteError(tx.Commit(ctx))
}

// CastBallot records the user's choice, replacing any earlier one.
//
// cast_at moves only when the choice does: clicking the same restaurant
// again is not a new ballot, and must not lose the place in a tie (F12.7).
func CastBallot(ctx context.Context, q Querier, voteID, userID, choiceID uuid.UUID, now time.Time) error {
	_, err := q.Exec(ctx, `
		INSERT INTO vote_ballot (vote_id, user_id, choice_id, cast_at, created_by, updated_by)
		SELECT $1, $2, $3, $4, $2, $2
		FROM vote WHERE id = $1 AND NOT decided
		ON CONFLICT (vote_id, user_id) DO UPDATE
		   SET cast_at = CASE WHEN vote_ballot.choice_id = excluded.choice_id
		                      THEN vote_ballot.cast_at ELSE excluded.cast_at END,
		       choice_id = excluded.choice_id,
		       updated_by = excluded.updated_by`,
		voteID, userID, choiceID, at(now))
	if constraintName(err) == "vote_ballot_choice_of_vote" {
		return ErrChoiceWrongVote
	}
	return err
}

// WithdrawBallot removes the user's ballot from an undecided vote, if they
// have one.
func WithdrawBallot(ctx context.Context, q Querier, voteID, userID uuid.UUID) error {
	_, err := q.Exec(ctx, `
		DELETE FROM vote_ballot b
		USING vote v
		WHERE b.vote_id = $1 AND b.user_id = $2 AND v.id = b.vote_id AND NOT v.decided`,
		voteID, userID)
	return err
}

// CountBallots is the number of ballots a vote has, for the rule that keeps
// a secret vote from being made public once anybody has voted in it.
func CountBallots(ctx context.Context, q Querier, voteID uuid.UUID) (int, error) {
	var n int
	err := q.QueryRow(ctx, `SELECT count(*) FROM vote_ballot WHERE vote_id = $1`, voteID).Scan(&n)
	return n, err
}
