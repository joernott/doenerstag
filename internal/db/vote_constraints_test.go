package db_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/joernott/doenerstag/internal/db"
	"github.com/joernott/doenerstag/internal/model"
	"github.com/joernott/doenerstag/internal/testdb"
)

// voteFixture holds two users and six restaurants: one more than a vote may
// offer, so the upper bound can be tested.
type voteFixture struct {
	pool        *pgxpool.Pool
	anna, berta string
	restaurants []string
}

func newVoteFixture(t *testing.T) voteFixture {
	t.Helper()
	pool := testdb.Migrated(t)
	ctx := context.Background()

	f := voteFixture{
		pool:  pool,
		anna:  "00000000-0000-7000-8000-0000000000a1",
		berta: "00000000-0000-7000-8000-0000000000a2",
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("building the fixture: %v\n%s", err, sql)
		}
	}
	exec(`INSERT INTO app_user (id, name, password_hash) VALUES ($1, 'anna', 'x'), ($2, 'berta', 'x')`,
		f.anna, f.berta)
	for i := 1; i <= 6; i++ {
		id := fmt.Sprintf("00000000-0000-7000-8000-0000000000b%d", i)
		exec(`INSERT INTO restaurant (id, name, currency_code) VALUES ($1, $2, 'EUR')`,
			id, fmt.Sprintf("Restaurant %d", i))
		f.restaurants = append(f.restaurants, id)
	}
	return f
}

// choiceID is the id of the n-th choice (1-based) of a vote made by
// createVote; fixed, so tests can name them.
func choiceID(n int) string {
	return fmt.Sprintf("00000000-0000-7000-8000-0000000000c%d", n)
}

const testVoteID = "00000000-0000-7000-8000-0000000000d1"

// createVote inserts a vote offering the first n restaurants, in one
// transaction as the API will, and returns the commit's error.
func createVote(t *testing.T, f voteFixture, n int) error {
	t.Helper()
	ctx := context.Background()
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	decision := time.Now().Add(time.Hour)
	if _, err := tx.Exec(ctx, `
		INSERT INTO vote (id, creator_id, decision_at, deadline_at, fulfilment_at)
		VALUES ($1, $2, $3, $4, $5)`,
		testVoteID, f.anna, decision, decision.Add(time.Hour), decision.Add(2*time.Hour)); err != nil {
		return err
	}
	for i := 1; i <= n; i++ {
		if _, err := tx.Exec(ctx, `
			INSERT INTO vote_choice (id, vote_id, restaurant_id, added_by_id) VALUES ($1, $2, $3, $4)`,
			choiceID(i), testVoteID, f.restaurants[i-1], f.anna); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func mustCreateVote(t *testing.T, f voteFixture, n int) {
	t.Helper()
	if err := createVote(t, f, n); err != nil {
		t.Fatalf("creating a vote with %d restaurants: %v", n, err)
	}
}

func castBallot(t *testing.T, f voteFixture, user string, choice int) error {
	t.Helper()
	return execErr(t, f.pool, `
		INSERT INTO vote_ballot (vote_id, user_id, choice_id) VALUES ($1, $2, $3)
		ON CONFLICT (vote_id, user_id) DO UPDATE SET choice_id = excluded.choice_id, cast_at = now()`,
		testVoteID, user, choiceID(choice))
}

func count(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("counting: %v\n%s", err, sql)
	}
	return n
}

func isConstraint(err error, name string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.ConstraintName == name
}

// F12.1: decision, deadline, fulfilment, in that order.
func TestAVotesTimesMustComeInOrder(t *testing.T) {
	f := newVoteFixture(t)
	now := time.Now()

	cases := []struct {
		name                       string
		decision, deadline, fulfil time.Time
		valid                      bool
	}{
		{"in order", now, now.Add(time.Hour), now.Add(2 * time.Hour), true},
		{"decision after deadline", now.Add(time.Hour), now, now.Add(2 * time.Hour), false},
		{"decision at the deadline", now, now, now.Add(2 * time.Hour), false},
		{"deadline after fulfilment", now, now.Add(2 * time.Hour), now.Add(time.Hour), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// The count trigger is deferred, so a vote alone in a statement
			// that is never committed checks only the CHECK.
			ctx := context.Background()
			tx, err := f.pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(ctx) }()
			_, err = tx.Exec(ctx, `
				INSERT INTO vote (id, creator_id, decision_at, deadline_at, fulfilment_at)
				VALUES (gen_random_uuid(), $1, $2, $3, $4)`, f.anna, c.decision, c.deadline, c.fulfil)
			if c.valid && err != nil {
				t.Errorf("a vote with its times in order was refused: %v", err)
			}
			if !c.valid && !isConstraint(err, "vote_times_in_order") {
				t.Errorf("got %v, want the vote_times_in_order check", err)
			}
		})
	}
}

// F12.3: two to five, checked at commit.
func TestAVoteOffersTwoToFiveRestaurants(t *testing.T) {
	for n := 0; n <= 6; n++ {
		t.Run(fmt.Sprintf("%d restaurants", n), func(t *testing.T) {
			f := newVoteFixture(t)
			err := createVote(t, f, n)
			valid := n >= 2 && n <= 5
			if valid && err != nil {
				t.Errorf("a vote with %d restaurants was refused: %v", n, err)
			}
			if !valid && !isConstraint(err, "vote_choice_count") {
				t.Errorf("a vote with %d restaurants: got %v, want vote_choice_count", n, err)
			}
		})
	}
}

func TestRemovingARestaurantMayNotLeaveFewerThanTwo(t *testing.T) {
	f := newVoteFixture(t)
	mustCreateVote(t, f, 3)

	if err := execErr(t, f.pool, `DELETE FROM vote_choice WHERE id = $1`, choiceID(3)); err != nil {
		t.Fatalf("going from three restaurants to two was refused: %v", err)
	}
	err := execErr(t, f.pool, `DELETE FROM vote_choice WHERE id = $1`, choiceID(2))
	if !isConstraint(err, "vote_choice_count") {
		t.Errorf("going from two restaurants to one: got %v, want vote_choice_count", err)
	}
}

// Two people adding a fifth and a sixth restaurant at the same moment: each
// transaction alone sees five, so only the lock the trigger takes stops both
// committing.
func TestTwoRestaurantsAddedAtOnceCannotMakeSix(t *testing.T) {
	f := newVoteFixture(t)
	mustCreateVote(t, f, 4)
	ctx := context.Background()

	// Both transactions insert before either commits.
	txs := make([]interface {
		Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
		Commit(context.Context) error
		Rollback(context.Context) error
	}, 2)
	for i := range txs {
		tx, err := f.pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = tx.Rollback(ctx) })
		if _, err := tx.Exec(ctx, `
			INSERT INTO vote_choice (id, vote_id, restaurant_id, added_by_id) VALUES ($1, $2, $3, $4)`,
			choiceID(5+i), testVoteID, f.restaurants[4+i], f.anna); err != nil {
			t.Fatalf("inserting the %d. restaurant: %v", 5+i, err)
		}
		txs[i] = tx
	}

	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i, tx := range txs {
		wg.Go(func() { errs[i] = tx.Commit(ctx) })
	}
	wg.Wait()

	committed := 0
	for _, err := range errs {
		switch {
		case err == nil:
			committed++
		case !isConstraint(err, "vote_choice_count"):
			t.Errorf("a commit failed for another reason: %v", err)
		}
	}
	if committed != 1 {
		t.Errorf("%d of the two commits went through, want exactly one", committed)
	}
	if n := count(t, f.pool, `SELECT count(*) FROM vote_choice WHERE vote_id = $1`, testVoteID); n != 5 {
		t.Errorf("the vote offers %d restaurants, want 5", n)
	}
}

func TestARestaurantIsOfferedOncePerVote(t *testing.T) {
	f := newVoteFixture(t)
	mustCreateVote(t, f, 2)

	err := execErr(t, f.pool, `
		INSERT INTO vote_choice (id, vote_id, restaurant_id, added_by_id) VALUES ($1, $2, $3, $4)`,
		choiceID(3), testVoteID, f.restaurants[0], f.anna)
	if !isConstraint(err, "vote_choice_once_per_vote") {
		t.Errorf("got %v, want vote_choice_once_per_vote", err)
	}
}

// F12.4: one ballot per voter; changing one's mind replaces it.
func TestOneBallotPerVoter(t *testing.T) {
	f := newVoteFixture(t)
	mustCreateVote(t, f, 2)

	if err := castBallot(t, f, f.anna, 1); err != nil {
		t.Fatalf("casting a ballot: %v", err)
	}
	err := execErr(t, f.pool, `INSERT INTO vote_ballot (vote_id, user_id, choice_id) VALUES ($1, $2, $3)`,
		testVoteID, f.anna, choiceID(2))
	if !isConstraint(err, "vote_ballot_pkey") {
		t.Errorf("a second ballot: got %v, want vote_ballot_pkey", err)
	}
	if err := castBallot(t, f, f.anna, 2); err != nil {
		t.Fatalf("changing a ballot: %v", err)
	}
	if n := count(t, f.pool, `SELECT count(*) FROM vote_ballot WHERE choice_id = $1`, choiceID(2)); n != 1 {
		t.Errorf("after changing her mind anna's ballot is on choice 2 %d times, want 1", n)
	}
}

func TestABallotNamesAChoiceOfItsOwnVote(t *testing.T) {
	f := newVoteFixture(t)
	mustCreateVote(t, f, 2)

	// A second vote, whose choice a ballot in the first must not name.
	ctx := context.Background()
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	decision := time.Now().Add(time.Hour)
	other := "00000000-0000-7000-8000-0000000000d2"
	_, err = tx.Exec(ctx, `
		INSERT INTO vote (id, creator_id, decision_at, deadline_at, fulfilment_at) VALUES ($1, $2, $3, $4, $5);`,
		other, f.anna, decision, decision.Add(time.Hour), decision.Add(2*time.Hour))
	if err == nil {
		_, err = tx.Exec(ctx, `
			INSERT INTO vote_choice (id, vote_id, restaurant_id, added_by_id)
			VALUES ('00000000-0000-7000-8000-0000000000e1', $1, $2, $3),
			       ('00000000-0000-7000-8000-0000000000e2', $1, $4, $3)`,
			other, f.restaurants[2], f.anna, f.restaurants[3])
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		t.Fatalf("creating the second vote: %v", err)
	}

	err = execErr(t, f.pool, `INSERT INTO vote_ballot (vote_id, user_id, choice_id) VALUES ($1, $2, $3)`,
		testVoteID, f.anna, "00000000-0000-7000-8000-0000000000e1")
	if !isConstraint(err, "vote_ballot_choice_of_vote") {
		t.Errorf("got %v, want vote_ballot_choice_of_vote", err)
	}
}

// F12.3: a restaurant somebody chose stays.
func TestAChosenRestaurantCannotBeRemoved(t *testing.T) {
	f := newVoteFixture(t)
	mustCreateVote(t, f, 3)
	if err := castBallot(t, f, f.berta, 3); err != nil {
		t.Fatal(err)
	}

	err := execErr(t, f.pool, `DELETE FROM vote_choice WHERE id = $1`, choiceID(3))
	if !isConstraint(err, "vote_ballot_choice_of_vote") {
		t.Errorf("got %v, want vote_ballot_choice_of_vote", err)
	}
}

// Deleting a vote takes its choices and ballots, although a ballot holds
// each of its choices: the reason the ballot's key is NO ACTION.
func TestDeletingAVoteTakesItsChoicesAndBallots(t *testing.T) {
	f := newVoteFixture(t)
	mustCreateVote(t, f, 2)
	if err := castBallot(t, f, f.anna, 1); err != nil {
		t.Fatal(err)
	}

	if err := execErr(t, f.pool, `DELETE FROM vote WHERE id = $1`, testVoteID); err != nil {
		t.Fatalf("deleting a vote with a ballot: %v", err)
	}
	for _, table := range []string{"vote_choice", "vote_ballot"} {
		if n := count(t, f.pool, `SELECT count(*) FROM `+table); n != 0 {
			t.Errorf("%d rows left in %s", n, table)
		}
	}
}

func TestARestaurantOfferedByAVoteCannotBeDeleted(t *testing.T) {
	f := newVoteFixture(t)
	mustCreateVote(t, f, 2)

	if err := execErr(t, f.pool, `DELETE FROM restaurant WHERE id = $1`, f.restaurants[0]); err == nil {
		t.Error("a restaurant a vote offers was deleted")
	}
}

// F12.14: ballots go, votes and choices pass to the placeholder.
func TestDeletingAnAccountRemovesItsBallotsAndKeepsItsVotes(t *testing.T) {
	f := newVoteFixture(t)
	mustCreateVote(t, f, 2)
	if err := castBallot(t, f, f.anna, 1); err != nil {
		t.Fatal(err)
	}

	if err := execErr(t, f.pool, `DELETE FROM app_user WHERE id = $1`, f.anna); err != nil {
		t.Fatalf("deleting anna: %v", err)
	}
	if n := count(t, f.pool, `SELECT count(*) FROM vote_ballot`); n != 0 {
		t.Errorf("%d ballots survived their voter", n)
	}
	placeholder := model.DeletedUserID.String()
	if n := count(t, f.pool, `SELECT count(*) FROM vote WHERE creator_id = $1`, placeholder); n != 1 {
		t.Errorf("the vote did not pass to the placeholder")
	}
	if n := count(t, f.pool, `SELECT count(*) FROM vote_choice WHERE added_by_id = $1`, placeholder); n != 2 {
		t.Errorf("%d of 2 choices passed to the placeholder", n)
	}
}

// The outcome, the order and the winner are set together or not at all.
func TestADecidedVoteRecordsAConsistentOutcome(t *testing.T) {
	f := newVoteFixture(t)
	mustCreateVote(t, f, 2)

	cases := []struct {
		name, set, constraint string
	}{
		{"decided without an outcome", `decided = true`, "vote_decided_has_outcome"},
		{"an outcome without being decided", `outcome = 'no_ballots'`, "vote_decided_has_outcome"},
		{"an unknown outcome", `decided = true, outcome = 'maybe'`, "vote_outcome_known"},
		{"ordered without an order", `decided = true, outcome = 'ordered', winner_choice_id = '` + choiceID(1) + `'`, "vote_ordered_has_order"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := execErr(t, f.pool, `UPDATE vote SET `+c.set+` WHERE id = $1`, testVoteID)
			if !isConstraint(err, c.constraint) {
				t.Errorf("got %v, want %s", err, c.constraint)
			}
		})
	}

	if err := execErr(t, f.pool, `UPDATE vote SET decided = true, outcome = 'no_ballots' WHERE id = $1`,
		testVoteID); err != nil {
		t.Errorf("a vote decided without ballots was refused: %v", err)
	}
}

// The winner is one of the vote's own choices, and the vote goes with the
// order it became.
func TestAVoteGoesWithTheOrderItBecame(t *testing.T) {
	f := newVoteFixture(t)
	mustCreateVote(t, f, 2)
	ctx := context.Background()

	var orderID string
	decision := time.Now().Add(time.Hour)
	if err := f.pool.QueryRow(ctx, `
		INSERT INTO food_order (id, creator_id, restaurant_id, fulfilment, fulfilment_at, deadline_at, currency_code)
		VALUES (gen_random_uuid(), $1, $2, 'pickup', $3, $4, 'EUR') RETURNING id::text`,
		f.anna, f.restaurants[0], decision.Add(2*time.Hour), decision.Add(time.Hour)).Scan(&orderID); err != nil {
		t.Fatalf("creating the order: %v", err)
	}

	err := execErr(t, f.pool, `
		UPDATE vote SET decided = true, outcome = 'ordered', order_id = $2, winner_choice_id = gen_random_uuid()
		WHERE id = $1`, testVoteID, orderID)
	if !isConstraint(err, "vote_winner_is_own_choice") {
		t.Errorf("a winner from nowhere: got %v, want vote_winner_is_own_choice", err)
	}

	if err := execErr(t, f.pool, `
		UPDATE vote SET decided = true, outcome = 'ordered', order_id = $2, winner_choice_id = $3
		WHERE id = $1`, testVoteID, orderID, choiceID(1)); err != nil {
		t.Fatalf("deciding the vote: %v", err)
	}
	if err := execErr(t, f.pool, `DELETE FROM food_order WHERE id = $1`, orderID); err != nil {
		t.Fatalf("deleting the order: %v", err)
	}
	if n := count(t, f.pool, `SELECT count(*) FROM vote`); n != 0 {
		t.Errorf("the vote outlived its order")
	}
}

// A restaurant an undecided vote offers may be about to become an order, so
// the administrator cannot soft-delete it; once the vote is decided, the
// choice is history and the restaurant can go like any other.
func TestARestaurantInAnUndecidedVoteCannotBeSoftDeleted(t *testing.T) {
	f := newVoteFixture(t)
	mustCreateVote(t, f, 2)
	ctx := context.Background()
	restaurant := uuid.MustParse(f.restaurants[0])
	actor := uuid.MustParse(f.anna)

	err := db.SoftDeleteRestaurant(ctx, f.pool, restaurant, actor)
	if !errors.Is(err, db.ErrStillReferenced) {
		t.Fatalf("soft-deleting a restaurant an open vote offers: got %v, want ErrStillReferenced", err)
	}

	if err := execErr(t, f.pool, `UPDATE vote SET decided = true, outcome = 'no_ballots' WHERE id = $1`,
		testVoteID); err != nil {
		t.Fatal(err)
	}
	if err := db.SoftDeleteRestaurant(ctx, f.pool, restaurant, actor); err != nil {
		t.Errorf("soft-deleting a restaurant only a decided vote offered: %v", err)
	}
}

// F12.12: cleanup takes a vote that became no order once it is past
// retention, and leaves a younger one.
func TestCleanupRemovesOldVotesThatBecameNoOrder(t *testing.T) {
	f := newVoteFixture(t)
	mustCreateVote(t, f, 2)
	if err := execErr(t, f.pool, `UPDATE vote SET decided = true, outcome = 'no_ballots' WHERE id = $1`,
		testVoteID); err != nil {
		t.Fatal(err)
	}

	report, err := db.Cleanup(context.Background(), f.pool, 14*24*time.Hour, time.Now(), false)
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if stepCount(report, "votes") != 0 {
		t.Errorf("a vote decided an hour ago was removed")
	}

	report, err = db.Cleanup(context.Background(), f.pool, 14*24*time.Hour, time.Now().Add(15*24*time.Hour), false)
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if stepCount(report, "votes") != 1 {
		t.Errorf("an old vote that became no order was kept: %+v", report.Steps)
	}
}

// 23.2: the hard delete behind `doenerstag restaurant delete` and a replacing
// import removes the votes that offered the restaurant, rather than being
// refused by vote_choice's foreign key.
func TestAHardDeleteTakesTheVotesThatOfferedTheRestaurant(t *testing.T) {
	f := newVoteFixture(t)
	mustCreateVote(t, f, 2)
	if err := castBallot(t, f, f.anna, 0+1); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	counts, err := db.CountRestaurantDependents(ctx, f.pool, uuid.MustParse(f.restaurants[0]))
	if err != nil || counts.Votes != 1 {
		t.Fatalf("the dependents count %d votes (%v), want 1", counts.Votes, err)
	}

	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := db.HardDeleteRestaurant(ctx, tx, uuid.MustParse(f.restaurants[0])); err != nil {
		t.Fatalf("hard-deleting a restaurant a vote offered: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if n := count(t, f.pool, `SELECT count(*) FROM vote`); n != 0 {
		t.Errorf("%d votes survived the restaurant they offered", n)
	}
	if n := count(t, f.pool, `SELECT count(*) FROM restaurant WHERE id = $1`, f.restaurants[1]); n != 1 {
		t.Errorf("the other restaurant in the vote went too")
	}
}
