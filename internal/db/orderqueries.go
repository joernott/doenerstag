package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/joernott/doenerstag/internal/model"
)

// orderColumns joins in the restaurant and creator names the header needs, and
// counts the items in the same pass.
//
// The count is a correlated subquery rather than a join with GROUP BY: it is
// the only aggregate wanted, and grouping would have to list every other column
// in the projection.
const orderColumns = `o.id, o.creator_id, coalesce(u.display_name, u.name),
	o.restaurant_id, r.name, r.logo_image_id,
	o.fulfilment, o.fulfilment_at, o.deadline_at,
	coalesce(o.money_collector, ''), coalesce(o.pickup_person, ''),
	o.currency_code, o.min_order_value_cents, o.delivery_fee_cents,
	(SELECT count(*) FROM order_item i WHERE i.order_id = o.id),
	o.created_at, o.updated_at`

const orderFrom = `
	FROM food_order o
	JOIN restaurant r ON r.id = o.restaurant_id
	JOIN app_user u ON u.id = o.creator_id`

func scanOrder(row pgx.Row) (model.Order, error) {
	var o model.Order
	err := row.Scan(&o.ID, &o.CreatorID, &o.CreatorName,
		&o.RestaurantID, &o.RestaurantName, &o.RestaurantLogo,
		&o.Fulfilment, &o.FulfilmentAt, &o.DeadlineAt,
		&o.MoneyCollector, &o.PickupPerson,
		&o.CurrencyCode, &o.MinOrderValueCents, &o.DeliveryFeeCents,
		&o.ItemCount, &o.CreatedAt, &o.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Order{}, ErrNotFound
		}
		return model.Order{}, err
	}
	return o, nil
}

// NewOrder is what creation supplies.
//
// The currency and the two money fields are not here: they are copied from the
// restaurant inside CreateOrder, so a caller cannot supply a currency the
// restaurant does not use.
type NewOrder struct {
	CreatorID    uuid.UUID
	RestaurantID uuid.UUID
	Fulfilment   string
	FulfilmentAt time.Time
	DeadlineAt   time.Time

	MoneyCollector string
	PickupPerson   string
}

// CreateOrder inserts an order, copying the restaurant's money fields.
//
// F5.11: the order stores a copy of the currency, minimum order value and
// delivery fee as they were when the restaurant was selected. Copying them in
// the INSERT's SELECT means the copy is taken atomically with the read, so a
// restaurant edited between two statements cannot produce an order carrying
// half of each.
func CreateOrder(ctx context.Context, q Querier, in NewOrder) (model.Order, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return model.Order{}, fmt.Errorf("generating an order id: %w", err)
	}

	_, err = q.Exec(ctx, `
		INSERT INTO food_order (id, creator_id, restaurant_id, fulfilment,
		                        fulfilment_at, deadline_at, money_collector, pickup_person,
		                        currency_code, min_order_value_cents, delivery_fee_cents,
		                        created_by, updated_by)
		SELECT $1, $2, r.id, $4, $5, $6, nullif($7, ''), nullif($8, ''),
		       r.currency_code, r.min_order_value_cents, r.delivery_fee_cents,
		       $2, $2
		FROM restaurant r
		WHERE r.id = $3 AND r.deleted_at IS NULL`,
		id, in.CreatorID, in.RestaurantID, in.Fulfilment,
		in.FulfilmentAt, in.DeadlineAt, in.MoneyCollector, in.PickupPerson)
	if err != nil {
		return model.Order{}, err
	}

	// A missing restaurant makes the SELECT empty, so nothing is inserted and
	// the read-back finds nothing. Reporting that as ErrNotFound is exactly
	// right: the restaurant the caller named does not exist.
	return OrderByID(ctx, q, id)
}

// OrderByID reads one order's header.
func OrderByID(ctx context.Context, q Querier, id uuid.UUID) (model.Order, error) {
	return scanOrder(q.QueryRow(ctx,
		`SELECT `+orderColumns+orderFrom+` WHERE o.id = $1`, id))
}

// ListOrders returns every order, active first.
//
// docs/06_ui_ux.md: active orders first by fulfilment time ascending -- the
// soonest lunch is the one you care about -- then expired ones descending, so
// the most recent history is at the top of the second group.
func ListOrders(ctx context.Context, q Querier, now time.Time) ([]model.Order, error) {
	rows, err := q.Query(ctx, `
		SELECT `+orderColumns+orderFrom+`
		ORDER BY (o.deadline_at <= $1),
		         CASE WHEN o.deadline_at > $1 THEN o.fulfilment_at END ASC,
		         CASE WHEN o.deadline_at <= $1 THEN o.fulfilment_at END DESC`,
		at(now))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	orders := make([]model.Order, 0, 32)
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}
		orders = append(orders, o)
	}
	return orders, rows.Err()
}

// OrderUpdate carries the fields PATCH may change.
//
// The restaurant is here because F5.8 allows changing it while the order has no
// items; the handler enforces that condition.
type OrderUpdate struct {
	RestaurantID   *uuid.UUID
	Fulfilment     *string
	FulfilmentAt   *time.Time
	DeadlineAt     *time.Time
	MoneyCollector *string
	PickupPerson   *string
}

// UpdateOrder applies the fields that are set.
//
// Changing the restaurant re-copies its money fields, because the copy exists
// to record the terms of the restaurant actually being ordered from. Leaving
// the old currency behind would produce an order priced in one currency against
// a restaurant that quotes another.
func UpdateOrder(ctx context.Context, q Querier, id, actor uuid.UUID, in OrderUpdate) (model.Order, error) {
	var (
		sets []string
		args []any
	)
	add := func(clause string, value any) {
		args = append(args, value)
		sets = append(sets, fmt.Sprintf(clause, len(args)))
	}

	if in.RestaurantID != nil {
		add("restaurant_id = $%d", *in.RestaurantID)
		add(`currency_code = (SELECT currency_code FROM restaurant WHERE id = $%d)`, *in.RestaurantID)
		add(`min_order_value_cents = (SELECT min_order_value_cents FROM restaurant WHERE id = $%d)`, *in.RestaurantID)
		add(`delivery_fee_cents = (SELECT delivery_fee_cents FROM restaurant WHERE id = $%d)`, *in.RestaurantID)
	}
	if in.Fulfilment != nil {
		add("fulfilment = $%d", *in.Fulfilment)
	}
	if in.FulfilmentAt != nil {
		add("fulfilment_at = $%d", *in.FulfilmentAt)
	}
	if in.DeadlineAt != nil {
		add("deadline_at = $%d", *in.DeadlineAt)
	}
	if in.MoneyCollector != nil {
		add("money_collector = nullif($%d, '')", *in.MoneyCollector)
	}
	if in.PickupPerson != nil {
		add("pickup_person = nullif($%d, '')", *in.PickupPerson)
	}
	if len(sets) == 0 {
		return OrderByID(ctx, q, id)
	}

	add("updated_by = $%d", actor)
	args = append(args, id)

	tag, err := q.Exec(ctx,
		`UPDATE food_order SET `+strings.Join(sets, ", ")+
			fmt.Sprintf(" WHERE id = $%d", len(args)), args...)
	if err != nil {
		if isForeignKeyViolation(err) {
			return model.Order{}, ErrNotFound
		}
		return model.Order{}, err
	}
	if tag.RowsAffected() == 0 {
		return model.Order{}, ErrNotFound
	}
	return OrderByID(ctx, q, id)
}

// DeleteOrder removes an order and everything below it.
//
// The cascade is the schema's: order_item is ON DELETE CASCADE from food_order,
// and order_item_modification from order_item. Deleting the order row is
// therefore the whole operation.
func DeleteOrder(ctx context.Context, q Querier, id uuid.UUID) error {
	tag, err := q.Exec(ctx, `DELETE FROM food_order WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// CountOrderItems is how many items an order holds.
//
// Used for the restaurant lock (F5.8) and for the anonymous shape, which
// carries the count and nothing else about the items.
func CountOrderItems(ctx context.Context, q Querier, orderID uuid.UUID) (int, error) {
	var count int
	err := q.QueryRow(ctx,
		`SELECT count(*) FROM order_item WHERE order_id = $1`, orderID).Scan(&count)
	return count, err
}

// Pool is the subset of *pgxpool.Pool that the transactional order writes need.
type Pool interface {
	Querier
	Begin(ctx context.Context) (pgx.Tx, error)
}

// assert that a real pool satisfies it.
var _ Pool = (*pgxpool.Pool)(nil)
