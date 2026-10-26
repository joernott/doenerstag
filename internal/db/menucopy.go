package db

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// maxCopyNameLength is the name limit both menu tables share.
const maxCopyNameLength = 200

// CopyName is the name a copy of base is given: "Döner (copy)", and then
// "Döner (copy 2)" when that is taken. label is the word in the language of
// whoever is copying, which is why it arrives from the caller.
//
// taken holds the lower-cased names already in use. The base is shortened
// rather than the suffix, so a long name still says it is a copy.
func CopyName(base, label string, taken map[string]bool) string {
	for n := 1; ; n++ {
		suffix := " (" + label + ")"
		if n > 1 {
			suffix = fmt.Sprintf(" (%s %d)", label, n)
		}
		room := maxCopyNameLength - utf8.RuneCountInString(suffix)
		trimmed := base
		if utf8.RuneCountInString(trimmed) > room {
			trimmed = strings.TrimSpace(string([]rune(trimmed)[:room]))
		}
		name := trimmed + suffix
		if !taken[strings.ToLower(name)] {
			taken[strings.ToLower(name)] = true
			return name
		}
	}
}

// DuplicateMenuItem copies a living item with everything it carries --
// classification, modifications and availability -- under CopyName (21.1).
// The copy stays in the original's category and returns its id.
func DuplicateMenuItem(
	ctx context.Context, pool txQuerier, id uuid.UUID, label string, actor uuid.UUID,
) (uuid.UUID, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	item, err := MenuItemByID(ctx, tx, id)
	if err != nil {
		return uuid.Nil, err
	}
	taken, err := takenNames(ctx, tx, "menu_item", item.RestaurantID)
	if err != nil {
		return uuid.Nil, err
	}

	copyID, err := copyMenuItem(ctx, tx, id, item.CategoryID, CopyName(item.Name, label, taken), actor)
	if err != nil {
		return uuid.Nil, err
	}
	return copyID, tx.Commit(ctx)
}

// DuplicateCategory copies a living category with its availability and a
// copy of every living item in it, as DuplicateMenuItem makes them (21.2).
// The copy keeps the original's sort order, so its name places it directly
// after the original. Returns the new category's id.
func DuplicateCategory(
	ctx context.Context, pool txQuerier, id uuid.UUID, label string, actor uuid.UUID,
) (uuid.UUID, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	category, err := CategoryByID(ctx, tx, id)
	if err != nil {
		return uuid.Nil, err
	}
	takenCategories, err := takenNames(ctx, tx, "menu_category", category.RestaurantID)
	if err != nil {
		return uuid.Nil, err
	}

	created, err := CreateCategory(ctx, tx, category.RestaurantID,
		CopyName(category.Name, label, takenCategories), category.SortOrder, actor)
	if err != nil {
		return uuid.Nil, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO menu_category_availability (category_id, filter_id, created_by, updated_by)
		SELECT $2, filter_id, $3, $3 FROM menu_category_availability WHERE category_id = $1`,
		id, created.ID, actor); err != nil {
		return uuid.Nil, err
	}

	items, err := categoryItems(ctx, tx, id)
	if err != nil {
		return uuid.Nil, err
	}
	takenItems, err := takenNames(ctx, tx, "menu_item", category.RestaurantID)
	if err != nil {
		return uuid.Nil, err
	}
	for _, item := range items {
		if _, err := copyMenuItem(ctx, tx, item.id, &created.ID,
			CopyName(item.name, label, takenItems), actor); err != nil {
			return uuid.Nil, err
		}
	}

	return created.ID, tx.Commit(ctx)
}

// takenNames reads the lower-cased names of a restaurant's living categories
// or items, which is what the unique indexes on both compare.
func takenNames(ctx context.Context, q Querier, table string, restaurantID uuid.UUID) (map[string]bool, error) {
	// The table name comes from the two callers above, never from a request.
	rows, err := q.Query(ctx,
		`SELECT lower(name) FROM `+table+` WHERE restaurant_id = $1 AND deleted_at IS NULL`,
		restaurantID)
	if err != nil {
		return nil, err
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}

	taken := make(map[string]bool, len(names))
	for _, name := range names {
		taken[name] = true
	}
	return taken, nil
}

type namedItem struct {
	id   uuid.UUID
	name string
}

// categoryItems lists the living items in a category, in menu order.
func categoryItems(ctx context.Context, q Querier, categoryID uuid.UUID) ([]namedItem, error) {
	rows, err := q.Query(ctx, `
		SELECT id, name FROM menu_item
		WHERE category_id = $1 AND deleted_at IS NULL`+menuItemOrder, categoryID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (namedItem, error) {
		var item namedItem
		err := row.Scan(&item.id, &item.name)
		return item, err
	})
}

// copyMenuItem inserts a copy of one item under a new name and category, and
// copies what hangs off it. The caller owns the transaction.
func copyMenuItem(
	ctx context.Context, tx pgx.Tx, sourceID uuid.UUID, categoryID *uuid.UUID, name string, actor uuid.UUID,
) (uuid.UUID, error) {
	copyID, err := uuid.NewV7()
	if err != nil {
		return uuid.Nil, fmt.Errorf("generating a menu item id: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO menu_item (id, restaurant_id, category_id, external_id, name,
		                       description, image_id, price_cents, available,
		                       created_by, updated_by)
		SELECT $2, restaurant_id, $3, external_id, $4,
		       description, image_id, price_cents, available, $5, $5
		FROM menu_item WHERE id = $1`,
		sourceID, copyID, categoryID, name, actor)
	if isUniqueViolation(err) {
		return uuid.Nil, ErrNameTaken
	}
	if err != nil {
		return uuid.Nil, err
	}

	// The link tables all share one shape. Their names are constants here.
	for _, link := range []struct{ table, column string }{
		{"menu_item_tag", "tag_id"},
		{"menu_item_allergen", "allergen_id"},
		{"menu_item_additive", "additive_id"},
		{"menu_item_availability", "filter_id"},
	} {
		if _, err := tx.Exec(ctx, `
			INSERT INTO `+link.table+` (menu_item_id, `+link.column+`, created_by, updated_by)
			SELECT $2, `+link.column+`, $3, $3 FROM `+link.table+` WHERE menu_item_id = $1`,
			sourceID, copyID, actor); err != nil {
			return uuid.Nil, fmt.Errorf("copying %s: %w", link.table, err)
		}
	}

	modifications, err := ListModifications(ctx, tx, sourceID)
	if err != nil {
		return uuid.Nil, err
	}
	for _, m := range modifications {
		modID, err := uuid.NewV7()
		if err != nil {
			return uuid.Nil, fmt.Errorf("generating a modification id: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO menu_item_modification (id, menu_item_id, name, price_delta_cents,
			                                    sort_order, created_by, updated_by)
			VALUES ($1, $2, $3, $4, $5, $6, $6)`,
			modID, copyID, m.Name, m.PriceDeltaCents, m.SortOrder, actor); err != nil {
			return uuid.Nil, fmt.Errorf("copying a modification: %w", err)
		}
	}

	return copyID, nil
}
