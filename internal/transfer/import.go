package transfer

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/joernott/doenerstag/internal/db"
)

// ErrAlreadyExists is returned when a restaurant with that id is already here
// and --overwrite was not given.
var ErrAlreadyExists = errors.New("a restaurant with this id already exists")

// Beginner is what Import needs from a pool: the ability to start a
// transaction.
//
// An import is all or nothing. A restaurant that arrived with its contacts and
// half its menu, because the twentieth item named a tag this installation does
// not have, is worse than one that did not arrive: somebody has to work out
// what is missing before they can try again.
type Beginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Import writes one restaurant, keeping the ids it came with.
//
// actor is recorded as the creator of everything it writes, which is the
// administrator running the command rather than whoever created the restaurant
// on the machine it came from -- that account may not exist here, and inventing
// a reference to it would be a lie in the audit trail.
func Import(ctx context.Context, pool Beginner, in Restaurant, actor uuid.UUID, overwrite bool) error {
	restaurantID, err := uuid.Parse(in.ID)
	if err != nil {
		return fmt.Errorf("restaurant id %q is not a uuid: %w", in.ID, err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	exists, err := restaurantRowExists(ctx, tx, restaurantID)
	if err != nil {
		return err
	}
	if exists {
		if !overwrite {
			return ErrAlreadyExists
		}
		if err := db.HardDeleteRestaurant(ctx, tx, restaurantID); err != nil {
			return fmt.Errorf("replacing restaurant %s: %w", restaurantID, err)
		}
	}

	// The codes this document uses have to exist here before anything is
	// written, so that an unknown one fails before the first insert rather than
	// twenty rows in.
	codes, err := loadCodes(ctx, tx)
	if err != nil {
		return err
	}
	if err := codes.check(in); err != nil {
		return err
	}

	if err := insertRestaurant(ctx, tx, restaurantID, in, codes, actor); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing the import of %s: %w", restaurantID, err)
	}
	return nil
}

// restaurantRowExists asks about the row, including a soft-deleted one.
//
// Deliberately not db.RestaurantExists, which asks about a *living* restaurant.
// A soft-deleted row still occupies the id, so importing over it without
// noticing would fail on the primary key with a message about a constraint.
func restaurantRowExists(ctx context.Context, q db.Querier, id uuid.UUID) (bool, error) {
	var exists bool
	err := q.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM restaurant WHERE id = $1)`, id).Scan(&exists)
	return exists, err
}

// codeTables are the reference rows a document names by code.
type codeTables struct {
	currencies  map[string]bool
	contactType map[string]uuid.UUID
	tags        map[string]uuid.UUID
	allergens   map[string]uuid.UUID
	additives   map[string]uuid.UUID
}

func loadCodes(ctx context.Context, q db.Querier) (*codeTables, error) {
	c := &codeTables{
		currencies:  map[string]bool{},
		contactType: map[string]uuid.UUID{},
		tags:        map[string]uuid.UUID{},
		allergens:   map[string]uuid.UUID{},
		additives:   map[string]uuid.UUID{},
	}

	rows, err := q.Query(ctx, `SELECT code FROM currency`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			rows.Close()
			return nil, err
		}
		c.currencies[strings.ToUpper(code)] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for _, load := range []struct {
		query string
		into  map[string]uuid.UUID
	}{
		{`SELECT id, code FROM contact_type`, c.contactType},
		{`SELECT id, code FROM tag`, c.tags},
		{`SELECT id, code FROM allergen`, c.allergens},
		{`SELECT id, code FROM additive`, c.additives},
	} {
		rows, err := q.Query(ctx, load.query)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id uuid.UUID
			var code string
			if err := rows.Scan(&id, &code); err != nil {
				rows.Close()
				return nil, err
			}
			load.into[code] = id
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	return c, nil
}

// check refuses a document naming anything this installation does not have.
//
// Every unknown code is collected rather than the first one reported, because
// somebody fixing a file wants the list, not one round trip per mistake.
func (c *codeTables) check(in Restaurant) error {
	var problems []string

	if !c.currencies[strings.ToUpper(in.Currency)] {
		problems = append(problems, fmt.Sprintf("currency %q", in.Currency))
	}
	for _, contact := range in.Contacts {
		if _, ok := c.contactType[contact.Type]; !ok {
			problems = append(problems, fmt.Sprintf("contact type %q", contact.Type))
		}
	}
	for i := range in.Items {
		item := &in.Items[i]
		for _, code := range item.Tags {
			if _, ok := c.tags[code]; !ok {
				problems = append(problems, fmt.Sprintf("tag %q", code))
			}
		}
		for _, code := range item.Allergens {
			if _, ok := c.allergens[code]; !ok {
				problems = append(problems, fmt.Sprintf("allergen %q", code))
			}
		}
		for _, code := range item.Additives {
			if _, ok := c.additives[code]; !ok {
				problems = append(problems, fmt.Sprintf("additive %q", code))
			}
		}
	}

	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf(
		"this installation does not have: %s.\nReference data is seeded per installation; a file can only name what exists here",
		strings.Join(unique(problems), ", "))
}

func unique(values []string) []string {
	seen := map[string]bool{}
	out := values[:0]
	for _, v := range values {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}
