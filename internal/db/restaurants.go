package db

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/joernott/doenerstag/internal/model"
)

// ErrStillReferenced is returned when a row cannot be removed because
// something else points at it.
var ErrStillReferenced = errors.New("db: still referenced")

// restaurantColumns is the shared projection. Every query filters
// deleted_at IS NULL: a soft-deleted restaurant is gone as far as the API is
// concerned, and the row survives only so that orders referring to it still
// read correctly.
const restaurantColumns = `id, name, logo_image_id, currency_code,
	min_order_value_cents, delivery_fee_cents, coalesce(notes, ''), created_at, updated_at`

func scanRestaurant(row pgx.Row) (model.Restaurant, error) {
	var r model.Restaurant
	err := row.Scan(&r.ID, &r.Name, &r.LogoImageID, &r.CurrencyCode,
		&r.MinOrderValueCents, &r.DeliveryFeeCents, &r.Notes,
		&r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Restaurant{}, ErrNotFound
		}
		return model.Restaurant{}, err
	}
	return r, nil
}

// NewRestaurant is what creation supplies.
type NewRestaurant struct {
	Name               string
	CurrencyCode       string
	LogoImageID        *uuid.UUID
	MinOrderValueCents *int64
	DeliveryFeeCents   *int64
	Notes              string
}

// CreateRestaurant inserts a restaurant.
func CreateRestaurant(ctx context.Context, q Querier, in NewRestaurant, actor uuid.UUID) (model.Restaurant, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return model.Restaurant{}, fmt.Errorf("generating a restaurant id: %w", err)
	}

	row := q.QueryRow(ctx, `
		INSERT INTO restaurant (id, name, logo_image_id, currency_code,
		                        min_order_value_cents, delivery_fee_cents, notes,
		                        created_by, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6, nullif($7, ''), $8, $8)
		RETURNING `+restaurantColumns,
		id, in.Name, in.LogoImageID, in.CurrencyCode,
		in.MinOrderValueCents, in.DeliveryFeeCents, in.Notes, actor)

	r, err := scanRestaurant(row)
	if isUniqueViolation(err) {
		// The unique index is partial, on lower(name) among the living, so a
		// soft-deleted restaurant does not block re-adding one by that name.
		return model.Restaurant{}, ErrNameTaken
	}
	if isForeignKeyViolation(err) {
		// An unknown currency or a logo image id that does not exist.
		return model.Restaurant{}, ErrNotFound
	}
	return r, err
}

// RestaurantByID reads one living restaurant.
func RestaurantByID(ctx context.Context, q Querier, id uuid.UUID) (model.Restaurant, error) {
	return scanRestaurant(q.QueryRow(ctx,
		`SELECT `+restaurantColumns+` FROM restaurant WHERE id = $1 AND deleted_at IS NULL`, id))
}

// ListRestaurants returns every living restaurant, by name.
//
// Ordered by lower(name) rather than name, so that "Ali's" and "ali's" do not
// end up in different halves of the list depending on capitalisation.
func ListRestaurants(ctx context.Context, q Querier) ([]model.Restaurant, error) {
	rows, err := q.Query(ctx,
		`SELECT `+restaurantColumns+` FROM restaurant WHERE deleted_at IS NULL ORDER BY lower(name)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	restaurants := make([]model.Restaurant, 0, 16)
	for rows.Next() {
		r, err := scanRestaurant(rows)
		if err != nil {
			return nil, err
		}
		restaurants = append(restaurants, r)
	}
	return restaurants, rows.Err()
}

// RestaurantUpdate carries the fields PATCH may change.
//
// Pointers throughout, so absent and cleared are different requests. The
// double pointers are not perverse: LogoImageID has three states -- leave it,
// set it, and remove it -- and one level of indirection can only express two.
type RestaurantUpdate struct {
	Name               *string
	CurrencyCode       *string
	LogoImageID        **uuid.UUID
	MinOrderValueCents **int64
	DeliveryFeeCents   **int64
	Notes              *string
}

// UpdateRestaurant applies the fields that are set.
func UpdateRestaurant(
	ctx context.Context, q Querier, id, actor uuid.UUID, in RestaurantUpdate,
) (model.Restaurant, error) {
	var (
		sets []string
		args []any
	)
	add := func(clause string, value any) {
		args = append(args, value)
		sets = append(sets, fmt.Sprintf(clause, len(args)))
	}

	if in.Name != nil {
		add("name = $%d", *in.Name)
	}
	if in.CurrencyCode != nil {
		add("currency_code = $%d", *in.CurrencyCode)
	}
	if in.LogoImageID != nil {
		add("logo_image_id = $%d", *in.LogoImageID)
	}
	if in.MinOrderValueCents != nil {
		add("min_order_value_cents = $%d", *in.MinOrderValueCents)
	}
	if in.DeliveryFeeCents != nil {
		add("delivery_fee_cents = $%d", *in.DeliveryFeeCents)
	}
	if in.Notes != nil {
		add("notes = nullif($%d, '')", *in.Notes)
	}
	if len(sets) == 0 {
		return RestaurantByID(ctx, q, id)
	}

	add("updated_by = $%d", actor)
	args = append(args, id)

	row := q.QueryRow(ctx,
		`UPDATE restaurant SET `+strings.Join(sets, ", ")+
			fmt.Sprintf(" WHERE id = $%d AND deleted_at IS NULL RETURNING ", len(args))+
			restaurantColumns,
		args...)

	r, err := scanRestaurant(row)
	if isUniqueViolation(err) {
		return model.Restaurant{}, ErrNameTaken
	}
	if isForeignKeyViolation(err) {
		return model.Restaurant{}, ErrNotFound
	}
	return r, err
}

// SoftDeleteRestaurant marks a restaurant deleted.
//
// Soft, because orders point at it and F3.5 keeps those readable. The row is
// removed physically by the cleanup verb once nothing references it.
//
// A restaurant still referenced by any order cannot be deleted at all, which is
// F3.5's second half. The check and the update are one statement so that an
// order created between them cannot slip through.
func SoftDeleteRestaurant(ctx context.Context, q Querier, id, actor uuid.UUID) error {
	tag, err := q.Exec(ctx, `
		UPDATE restaurant
		SET deleted_at = now(), updated_by = $2
		WHERE id = $1
		  AND deleted_at IS NULL
		  AND NOT EXISTS (SELECT 1 FROM food_order WHERE restaurant_id = $1)`,
		id, actor)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 1 {
		return nil
	}

	// Nothing was updated, and the two reasons get different answers: 409 while
	// an order still points at it, 404 otherwise. Working out which costs a
	// second query, but only on the path that is already failing.
	var inUse bool
	if err := q.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM food_order WHERE restaurant_id = $1)`, id).
		Scan(&inUse); err != nil {
		return err
	}
	if inUse {
		return ErrStillReferenced
	}
	return ErrNotFound
}

// RestaurantExists reports whether a living restaurant has this id, for the
// subresource handlers to check before touching contacts or opening hours.
func RestaurantExists(ctx context.Context, q Querier, id uuid.UUID) (bool, error) {
	var exists bool
	err := q.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM restaurant WHERE id = $1 AND deleted_at IS NULL)`, id).
		Scan(&exists)
	return exists, err
}

// CreateRestaurantWithContacts creates a restaurant and its first contacts
// together.
//
// F3.1 says a restaurant has at least one contact. Creating the restaurant and
// then adding contacts in a second call would leave a window -- and, if the
// second call failed, a permanent record -- in which a restaurant violates its
// own rule. One transaction means either both exist or neither does.
func CreateRestaurantWithContacts(
	ctx context.Context, pool interface {
		Querier
		Begin(ctx context.Context) (pgx.Tx, error)
	},
	in NewRestaurant, contacts []NewContact, actor uuid.UUID,
) (model.Restaurant, error) {
	if len(contacts) == 0 {
		return model.Restaurant{}, ErrLastContact
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return model.Restaurant{}, fmt.Errorf("starting the restaurant creation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	created, err := CreateRestaurant(ctx, tx, in, actor)
	if err != nil {
		return model.Restaurant{}, err
	}

	for _, c := range contacts {
		c.RestaurantID = created.ID
		if _, err := CreateContact(ctx, tx, c, actor); err != nil {
			return model.Restaurant{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return model.Restaurant{}, fmt.Errorf("committing the restaurant: %w", err)
	}
	return created, nil
}
