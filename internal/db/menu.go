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

// menuItemOrder is F4.6, and it is quoted verbatim from docs/03_data_model.md.
//
// A purely numeric external_id sorts numerically, so 2 precedes 10; a mixed one
// sorts as text; items with none come last. This is how a printed menu is laid
// out, which is the order people expect when they read it back.
//
// The CASE yields NULL for a non-numeric id, so NULLS LAST puts "A3" after
// every number rather than interleaving it. The leading "external_id IS NULL"
// is what separates numbered items from unnumbered ones before either of the
// other clauses is considered.
const menuItemOrder = `
	ORDER BY external_id IS NULL,
	         (CASE WHEN external_id ~ '^[0-9]+$'
	               THEN external_id::bigint END) NULLS LAST,
	         external_id,
	         lower(name)`

const categoryColumns = `id, restaurant_id, name, sort_order, created_at, updated_at`

func scanCategory(row pgx.Row) (model.MenuCategory, error) {
	var c model.MenuCategory
	err := row.Scan(&c.ID, &c.RestaurantID, &c.Name, &c.SortOrder, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.MenuCategory{}, ErrNotFound
		}
		return model.MenuCategory{}, err
	}
	return c, nil
}

// CreateCategory adds a category to a restaurant.
func CreateCategory(
	ctx context.Context, q Querier, restaurantID uuid.UUID, name string, sortOrder int, actor uuid.UUID,
) (model.MenuCategory, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return model.MenuCategory{}, fmt.Errorf("generating a category id: %w", err)
	}

	c, err := scanCategory(q.QueryRow(ctx, `
		INSERT INTO menu_category (id, restaurant_id, name, sort_order, created_by, updated_by)
		VALUES ($1, $2, $3, $4, $5, $5)
		RETURNING `+categoryColumns,
		id, restaurantID, name, sortOrder, actor))
	if isUniqueViolation(err) {
		return model.MenuCategory{}, ErrNameTaken
	}
	if isForeignKeyViolation(err) {
		return model.MenuCategory{}, ErrNotFound
	}
	return c, err
}

// ListCategories returns a restaurant's living categories, in editorial order.
func ListCategories(ctx context.Context, q Querier, restaurantID uuid.UUID) ([]model.MenuCategory, error) {
	rows, err := q.Query(ctx, `
		SELECT `+categoryColumns+`
		FROM menu_category
		WHERE restaurant_id = $1 AND deleted_at IS NULL
		ORDER BY sort_order, lower(name)`, restaurantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	categories := make([]model.MenuCategory, 0, 8)
	for rows.Next() {
		c, err := scanCategory(rows)
		if err != nil {
			return nil, err
		}
		categories = append(categories, c)
	}
	return categories, rows.Err()
}

// CategoryByID reads one living category.
func CategoryByID(ctx context.Context, q Querier, id uuid.UUID) (model.MenuCategory, error) {
	return scanCategory(q.QueryRow(ctx,
		`SELECT `+categoryColumns+` FROM menu_category WHERE id = $1 AND deleted_at IS NULL`, id))
}

// CategoryUpdate carries what PATCH may change: a rename or a reorder.
type CategoryUpdate struct {
	Name      *string
	SortOrder *int
}

// UpdateCategory applies the fields that are set.
func UpdateCategory(
	ctx context.Context, q Querier, id, actor uuid.UUID, in CategoryUpdate,
) (model.MenuCategory, error) {
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
	if in.SortOrder != nil {
		add("sort_order = $%d", *in.SortOrder)
	}
	if len(sets) == 0 {
		return CategoryByID(ctx, q, id)
	}

	add("updated_by = $%d", actor)
	args = append(args, id)

	c, err := scanCategory(q.QueryRow(ctx,
		`UPDATE menu_category SET `+strings.Join(sets, ", ")+
			fmt.Sprintf(" WHERE id = $%d AND deleted_at IS NULL RETURNING ", len(args))+
			categoryColumns,
		args...))
	if isUniqueViolation(err) {
		return model.MenuCategory{}, ErrNameTaken
	}
	return c, err
}

// SoftDeleteCategory marks a category deleted.
//
// Its items are not deleted with it. menu_item.category_id is ON DELETE SET
// NULL, and the same reasoning applies to a soft delete: removing a category
// should not remove the food. The items fall back into the uncategorised list,
// which is where an item with no category belongs anyway.
func SoftDeleteCategory(ctx context.Context, q Querier, id, actor uuid.UUID) error {
	tag, err := q.Exec(ctx, `
		WITH released AS (
			UPDATE menu_item SET category_id = NULL, updated_by = $2
			WHERE category_id = $1
		)
		UPDATE menu_category SET deleted_at = now(), updated_by = $2
		WHERE id = $1 AND deleted_at IS NULL`, id, actor)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// menuItemColumns is the shared projection.
const menuItemColumns = `id, restaurant_id, category_id, coalesce(external_id, ''),
	name, coalesce(description, ''), image_id, price_cents, available,
	created_at, updated_at`

func scanMenuItem(row pgx.Row) (model.MenuItem, error) {
	var m model.MenuItem
	err := row.Scan(&m.ID, &m.RestaurantID, &m.CategoryID, &m.ExternalID,
		&m.Name, &m.Description, &m.ImageID, &m.PriceCents, &m.Available,
		&m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.MenuItem{}, ErrNotFound
		}
		return model.MenuItem{}, err
	}
	return m, nil
}

// NewMenuItem is what creation supplies.
type NewMenuItem struct {
	RestaurantID uuid.UUID
	CategoryID   *uuid.UUID
	ExternalID   string
	Name         string
	Description  string
	ImageID      *uuid.UUID
	PriceCents   int64
	Available    bool
}

// CreateMenuItem adds an item.
func CreateMenuItem(ctx context.Context, q Querier, in NewMenuItem, actor uuid.UUID) (model.MenuItem, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return model.MenuItem{}, fmt.Errorf("generating a menu item id: %w", err)
	}

	m, err := scanMenuItem(q.QueryRow(ctx, `
		INSERT INTO menu_item (id, restaurant_id, category_id, external_id, name,
		                       description, image_id, price_cents, available,
		                       created_by, updated_by)
		VALUES ($1, $2, $3, nullif($4, ''), $5, nullif($6, ''), $7, $8, $9, $10, $10)
		RETURNING `+menuItemColumns,
		id, in.RestaurantID, in.CategoryID, in.ExternalID, in.Name,
		in.Description, in.ImageID, in.PriceCents, in.Available, actor))
	if isUniqueViolation(err) {
		return model.MenuItem{}, ErrNameTaken
	}
	if isForeignKeyViolation(err) {
		return model.MenuItem{}, ErrNotFound
	}
	return m, err
}

// MenuItemByID reads one living item, without its classification.
func MenuItemByID(ctx context.Context, q Querier, id uuid.UUID) (model.MenuItem, error) {
	return scanMenuItem(q.QueryRow(ctx,
		`SELECT `+menuItemColumns+` FROM menu_item WHERE id = $1 AND deleted_at IS NULL`, id))
}

// MenuItemUpdate carries the fields PATCH may change.
type MenuItemUpdate struct {
	CategoryID  **uuid.UUID
	ExternalID  *string
	Name        *string
	Description *string
	ImageID     **uuid.UUID
	PriceCents  *int64
	Available   *bool
}

// UpdateMenuItem applies the fields that are set.
func UpdateMenuItem(
	ctx context.Context, q Querier, id, actor uuid.UUID, in MenuItemUpdate,
) (model.MenuItem, error) {
	var (
		sets []string
		args []any
	)
	add := func(clause string, value any) {
		args = append(args, value)
		sets = append(sets, fmt.Sprintf(clause, len(args)))
	}

	if in.CategoryID != nil {
		add("category_id = $%d", *in.CategoryID)
	}
	if in.ExternalID != nil {
		add("external_id = nullif($%d, '')", *in.ExternalID)
	}
	if in.Name != nil {
		add("name = $%d", *in.Name)
	}
	if in.Description != nil {
		add("description = nullif($%d, '')", *in.Description)
	}
	if in.ImageID != nil {
		add("image_id = $%d", *in.ImageID)
	}
	if in.PriceCents != nil {
		add("price_cents = $%d", *in.PriceCents)
	}
	if in.Available != nil {
		add("available = $%d", *in.Available)
	}
	if len(sets) == 0 {
		return MenuItemByID(ctx, q, id)
	}

	add("updated_by = $%d", actor)
	args = append(args, id)

	m, err := scanMenuItem(q.QueryRow(ctx,
		`UPDATE menu_item SET `+strings.Join(sets, ", ")+
			fmt.Sprintf(" WHERE id = $%d AND deleted_at IS NULL RETURNING ", len(args))+
			menuItemColumns,
		args...))
	if isUniqueViolation(err) {
		return model.MenuItem{}, ErrNameTaken
	}
	if isForeignKeyViolation(err) {
		return model.MenuItem{}, ErrNotFound
	}
	return m, err
}

// SoftDeleteMenuItem marks an item deleted.
//
// Soft, because order items reference it and F4.5 keeps placed orders readable.
// The row survives until no order item references it and the retention window
// has passed, which is the cleanup verb's job.
func SoftDeleteMenuItem(ctx context.Context, q Querier, id, actor uuid.UUID) error {
	tag, err := q.Exec(ctx, `
		UPDATE menu_item SET deleted_at = now(), updated_by = $2
		WHERE id = $1 AND deleted_at IS NULL`, id, actor)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ExternalIDsForItems maps menu item ids to their restaurant item numbers.
//
// Read from the menu as it is now rather than from a snapshot: the number is a
// dialling aid -- "number 12, three times" -- not part of what was agreed, so
// the current one is the useful one. A soft-deleted item simply has no entry,
// and the summary shows the snapshotted name without a number.
func ExternalIDsForItems(ctx context.Context, q Querier, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	out := make(map[uuid.UUID]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}

	rows, err := q.Query(ctx, `
		SELECT id, coalesce(external_id, '')
		FROM menu_item
		WHERE id = ANY($1) AND deleted_at IS NULL`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id         uuid.UUID
			externalID string
		)
		if err := rows.Scan(&id, &externalID); err != nil {
			return nil, err
		}
		out[id] = externalID
	}
	return out, rows.Err()
}
