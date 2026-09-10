package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/joernott/doenerstag/internal/model"
)

// ErrWrongRestaurant is returned when a menu item does not belong to the
// order's restaurant.
var ErrWrongRestaurant = errors.New("db: menu item belongs to another restaurant")

// ErrItemUnavailable is returned when a menu item is marked sold out.
var ErrItemUnavailable = errors.New("db: menu item is unavailable")

const orderItemColumns = `i.id, i.order_id, i.user_id, coalesce(u.display_name, u.name),
	i.menu_item_id, i.quantity, i.item_name, i.unit_price_cents,
	coalesce(i.note, ''), i.paid, i.created_at, i.updated_at`

func scanOrderItem(row pgx.Row) (model.OrderItem, error) {
	var i model.OrderItem
	err := row.Scan(&i.ID, &i.OrderID, &i.UserID, &i.UserName,
		&i.MenuItemID, &i.Quantity, &i.ItemName, &i.UnitPriceCents,
		&i.Note, &i.Paid, &i.CreatedAt, &i.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.OrderItem{}, ErrNotFound
		}
		return model.OrderItem{}, err
	}
	return i, nil
}

// NewOrderItem is what adding an item supplies.
//
// No name and no price: those are snapshotted from the menu inside
// AddOrderItem, so a caller cannot claim a kebab cost fifty cents.
type NewOrderItem struct {
	OrderID    uuid.UUID
	UserID     uuid.UUID
	MenuItemID uuid.UUID
	Quantity   int
	Note       string

	// ModificationIDs are the predefined modifications selected. Their names
	// and price deltas are snapshotted too.
	ModificationIDs []uuid.UUID
}

// AddOrderItem adds an item, snapshotting the menu item and its modifications.
//
// One transaction: an item whose modifications failed to insert would have the
// wrong line total, and would look like a deliberate choice rather than a
// failure.
//
// The snapshot is taken from the menu row inside the same transaction as the
// insert, so a price edited concurrently is either wholly before or wholly
// after this item -- never half-applied across the item and its modifications.
func AddOrderItem(ctx context.Context, pool Pool, in NewOrderItem) (model.OrderItem, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return model.OrderItem{}, fmt.Errorf("starting the item insert: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// The menu item must belong to the order's restaurant. Checked by joining
	// the order to the menu item rather than by comparing two reads, so the
	// answer cannot change between the check and the insert.
	var (
		name      string
		price     int64
		available bool
	)
	err = tx.QueryRow(ctx, `
		SELECT m.name, m.price_cents, m.available
		FROM menu_item m
		JOIN food_order o ON o.id = $1
		WHERE m.id = $2
		  AND m.deleted_at IS NULL
		  AND m.restaurant_id = o.restaurant_id`,
		in.OrderID, in.MenuItemID).Scan(&name, &price, &available)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Either the menu item does not exist, or it belongs to another
			// restaurant. The caller is told the second, because it is the one
			// they can act on and the first is a special case of it.
			return model.OrderItem{}, ErrWrongRestaurant
		}
		return model.OrderItem{}, err
	}
	if !available {
		return model.OrderItem{}, ErrItemUnavailable
	}

	id, err := uuid.NewV7()
	if err != nil {
		return model.OrderItem{}, fmt.Errorf("generating an order item id: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO order_item (id, order_id, user_id, menu_item_id, quantity,
		                        item_name, unit_price_cents, note, created_by, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, nullif($8, ''), $3, $3)`,
		id, in.OrderID, in.UserID, in.MenuItemID, in.Quantity,
		name, price, in.Note); err != nil {
		return model.OrderItem{}, err
	}

	if err := insertModifications(ctx, tx, id, in.MenuItemID, in.ModificationIDs, in.UserID); err != nil {
		return model.OrderItem{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return model.OrderItem{}, fmt.Errorf("committing the order item: %w", err)
	}
	return OrderItemByID(ctx, pool, id)
}

// insertModifications snapshots the selected modifications onto an item.
//
// The names and deltas come from the menu rows, and the modification must
// belong to the menu item being ordered -- otherwise "extra cheese" from a
// different dish could be attached at that dish's price.
func insertModifications(
	ctx context.Context, tx pgx.Tx, orderItemID, menuItemID uuid.UUID,
	modificationIDs []uuid.UUID, actor uuid.UUID,
) error {
	if len(modificationIDs) == 0 {
		return nil
	}

	// Read the menu rows first, then insert. One statement with a SELECT would
	// be shorter, but it would have to mint ids with gen_random_uuid(), and
	// every other primary key in this schema is a UUIDv7 generated in Go.
	// Two statements inside the transaction keep that consistent.
	rows, err := tx.Query(ctx, `
		SELECT id, name, price_delta_cents
		FROM menu_item_modification
		WHERE id = ANY($1) AND menu_item_id = $2 AND deleted_at IS NULL`,
		modificationIDs, menuItemID)
	if err != nil {
		return fmt.Errorf("reading the modifications: %w", err)
	}

	type snapshot struct {
		id    uuid.UUID
		name  string
		delta int64
	}
	found := make([]snapshot, 0, len(modificationIDs))
	for rows.Next() {
		var s snapshot
		if err := rows.Scan(&s.id, &s.name, &s.delta); err != nil {
			rows.Close()
			return err
		}
		found = append(found, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	// Every requested modification must have matched. A silently dropped one
	// would give the item a lower price than the person chose, which is the
	// kind of discrepancy nobody notices until the money is counted.
	if len(found) != len(modificationIDs) {
		return ErrModificationWrongItem
	}

	for _, s := range found {
		id, err := uuid.NewV7()
		if err != nil {
			return fmt.Errorf("generating a modification snapshot id: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO order_item_modification (id, order_item_id, modification_id,
			                                     name, price_delta_cents, created_by, updated_by)
			VALUES ($1, $2, $3, $4, $5, $6, $6)`,
			id, orderItemID, s.id, s.name, s.delta, actor); err != nil {
			return fmt.Errorf("snapshotting the modifications: %w", err)
		}
	}
	return nil
}

// ErrModificationWrongItem is returned when a selected modification does not
// belong to the menu item being ordered.
var ErrModificationWrongItem = errors.New("db: modification does not belong to this menu item")

// OrderItemByID reads one item with its modifications.
func OrderItemByID(ctx context.Context, q Querier, id uuid.UUID) (model.OrderItem, error) {
	item, err := scanOrderItem(q.QueryRow(ctx, `
		SELECT `+orderItemColumns+`
		FROM order_item i JOIN app_user u ON u.id = i.user_id
		WHERE i.id = $1`, id))
	if err != nil {
		return model.OrderItem{}, err
	}

	items := []model.OrderItem{item}
	if err := loadModifications(ctx, q, items); err != nil {
		return model.OrderItem{}, err
	}
	return items[0], nil
}

// ListOrderItems returns an order's items with their modifications.
//
// Ordered by creation, so the list reads as the order was filled in.
func ListOrderItems(ctx context.Context, q Querier, orderID uuid.UUID) ([]model.OrderItem, error) {
	rows, err := q.Query(ctx, `
		SELECT `+orderItemColumns+`
		FROM order_item i JOIN app_user u ON u.id = i.user_id
		WHERE i.order_id = $1
		ORDER BY i.created_at, i.id`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.OrderItem, 0, 32)
	for rows.Next() {
		item, err := scanOrderItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if err := loadModifications(ctx, q, items); err != nil {
		return nil, err
	}
	return items, nil
}

// loadModifications fills in the modifications for a set of items.
func loadModifications(ctx context.Context, q Querier, items []model.OrderItem) error {
	if len(items) == 0 {
		return nil
	}

	ids := make([]uuid.UUID, 0, len(items))
	index := make(map[uuid.UUID]*model.OrderItem, len(items))
	for i := range items {
		ids = append(ids, items[i].ID)
		index[items[i].ID] = &items[i]
	}

	rows, err := q.Query(ctx, `
		SELECT id, order_item_id, modification_id, name, price_delta_cents
		FROM order_item_modification
		WHERE order_item_id = ANY($1)
		ORDER BY name`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var m model.OrderItemModification
		if err := rows.Scan(&m.ID, &m.OrderItemID, &m.ModificationID,
			&m.Name, &m.PriceDeltaCents); err != nil {
			return err
		}
		if item, ok := index[m.OrderItemID]; ok {
			item.Modifications = append(item.Modifications, m)
		}
	}
	return rows.Err()
}

// OrderItemUpdate carries what PATCH may change.
//
// Not the menu item: changing which dish this is would make the snapshots
// wrong, and "I meant the other one" is a delete and an add.
type OrderItemUpdate struct {
	Quantity *int
	Note     *string

	// ModificationIDs replaces the whole set when non-nil. Re-snapshotted from
	// the current menu, which is correct: the person is choosing again now.
	ModificationIDs  []uuid.UUID
	SetModifications bool
}

// UpdateOrderItem changes quantity, note and modifications.
func UpdateOrderItem(ctx context.Context, pool Pool, id uuid.UUID, in OrderItemUpdate, actor uuid.UUID) (model.OrderItem, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return model.OrderItem{}, fmt.Errorf("starting the item update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var menuItemID uuid.UUID
	if err := tx.QueryRow(ctx,
		`SELECT menu_item_id FROM order_item WHERE id = $1`, id).Scan(&menuItemID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.OrderItem{}, ErrNotFound
		}
		return model.OrderItem{}, err
	}

	if in.Quantity != nil {
		if _, err := tx.Exec(ctx,
			`UPDATE order_item SET quantity = $2, updated_by = $3 WHERE id = $1`,
			id, *in.Quantity, actor); err != nil {
			return model.OrderItem{}, err
		}
	}
	if in.Note != nil {
		if _, err := tx.Exec(ctx,
			`UPDATE order_item SET note = nullif($2, ''), updated_by = $3 WHERE id = $1`,
			id, *in.Note, actor); err != nil {
			return model.OrderItem{}, err
		}
	}

	if in.SetModifications {
		if _, err := tx.Exec(ctx,
			`DELETE FROM order_item_modification WHERE order_item_id = $1`, id); err != nil {
			return model.OrderItem{}, err
		}
		if err := insertModifications(ctx, tx, id, menuItemID, in.ModificationIDs, actor); err != nil {
			return model.OrderItem{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return model.OrderItem{}, fmt.Errorf("committing the item update: %w", err)
	}
	return OrderItemByID(ctx, pool, id)
}

// DeleteOrderItem removes an item and its modifications, which cascade.
func DeleteOrderItem(ctx context.Context, q Querier, id uuid.UUID) error {
	tag, err := q.Exec(ctx, `DELETE FROM order_item WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetOrderItemPaid records, or unrecords, that a line has been settled.
//
// Its own function rather than a field on OrderItemUpdate, because it is the
// only thing about an item that somebody other than its owner may change and
// the only thing that may change after the deadline. Keeping it separate keeps
// both of those rules in one place instead of as conditions inside a general
// update.
func SetOrderItemPaid(ctx context.Context, q Querier, id, actor uuid.UUID, paid bool) error {
	tag, err := q.Exec(ctx,
		`UPDATE order_item SET paid = $2, updated_by = $3 WHERE id = $1`, id, paid, actor)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
