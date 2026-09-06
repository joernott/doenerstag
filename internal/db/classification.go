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

const modificationColumns = `id, menu_item_id, name, price_delta_cents, sort_order`

func scanModification(row pgx.Row) (model.Modification, error) {
	var m model.Modification
	err := row.Scan(&m.ID, &m.MenuItemID, &m.Name, &m.PriceDeltaCents, &m.SortOrder)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Modification{}, ErrNotFound
		}
		return model.Modification{}, err
	}
	return m, nil
}

// NewModification is one predefined change to an item.
type NewModification struct {
	MenuItemID      uuid.UUID
	Name            string
	PriceDeltaCents int64
	SortOrder       int
}

// CreateModification adds a modification.
func CreateModification(ctx context.Context, q Querier, in NewModification, actor uuid.UUID) (model.Modification, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return model.Modification{}, fmt.Errorf("generating a modification id: %w", err)
	}

	m, err := scanModification(q.QueryRow(ctx, `
		INSERT INTO menu_item_modification (id, menu_item_id, name, price_delta_cents,
		                                    sort_order, created_by, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $6)
		RETURNING `+modificationColumns,
		id, in.MenuItemID, in.Name, in.PriceDeltaCents, in.SortOrder, actor))
	if isUniqueViolation(err) {
		return model.Modification{}, ErrNameTaken
	}
	if isForeignKeyViolation(err) {
		return model.Modification{}, ErrNotFound
	}
	return m, err
}

// ListModifications returns an item's living modifications.
func ListModifications(ctx context.Context, q Querier, menuItemID uuid.UUID) ([]model.Modification, error) {
	rows, err := q.Query(ctx, `
		SELECT `+modificationColumns+`
		FROM menu_item_modification
		WHERE menu_item_id = $1 AND deleted_at IS NULL
		ORDER BY sort_order, lower(name)`, menuItemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	modifications := make([]model.Modification, 0, 8)
	for rows.Next() {
		m, err := scanModification(rows)
		if err != nil {
			return nil, err
		}
		modifications = append(modifications, m)
	}
	return modifications, rows.Err()
}

// ModificationByID reads one living modification.
func ModificationByID(ctx context.Context, q Querier, id uuid.UUID) (model.Modification, error) {
	return scanModification(q.QueryRow(ctx,
		`SELECT `+modificationColumns+
			` FROM menu_item_modification WHERE id = $1 AND deleted_at IS NULL`, id))
}

// ModificationUpdate carries what PATCH may change.
type ModificationUpdate struct {
	Name            *string
	PriceDeltaCents *int64
	SortOrder       *int
}

// UpdateModification applies the fields that are set.
func UpdateModification(
	ctx context.Context, q Querier, id, actor uuid.UUID, in ModificationUpdate,
) (model.Modification, error) {
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
	if in.PriceDeltaCents != nil {
		add("price_delta_cents = $%d", *in.PriceDeltaCents)
	}
	if in.SortOrder != nil {
		add("sort_order = $%d", *in.SortOrder)
	}
	if len(sets) == 0 {
		return ModificationByID(ctx, q, id)
	}

	add("updated_by = $%d", actor)
	args = append(args, id)

	m, err := scanModification(q.QueryRow(ctx,
		`UPDATE menu_item_modification SET `+strings.Join(sets, ", ")+
			fmt.Sprintf(" WHERE id = $%d AND deleted_at IS NULL RETURNING ", len(args))+
			modificationColumns,
		args...))
	if isUniqueViolation(err) {
		return model.Modification{}, ErrNameTaken
	}
	return m, err
}

// SoftDeleteModification marks a modification deleted.
//
// Soft, because an order item's selected modifications are snapshots by name
// and price, but the row is still what a summary joins back to when it needs
// the current definition.
func SoftDeleteModification(ctx context.Context, q Querier, id, actor uuid.UUID) error {
	tag, err := q.Exec(ctx, `
		UPDATE menu_item_modification SET deleted_at = now(), updated_by = $2
		WHERE id = $1 AND deleted_at IS NULL`, id, actor)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListTags returns every tag, seeded and user-created alike.
func ListTags(ctx context.Context, q Querier) ([]model.Tag, error) {
	rows, err := q.Query(ctx,
		`SELECT id, code, name, sort_order FROM tag ORDER BY sort_order, code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tags := make([]model.Tag, 0, 16)
	for rows.Next() {
		var t model.Tag
		if err := rows.Scan(&t.ID, &t.Code, &t.Name, &t.SortOrder); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

// CreateTag adds a user-created tag.
//
// The only reference table users may add to. A seeded tag is named by the
// frontend's catalog from its code; a user-created one has no catalog entry, so
// it carries the name that was typed.
func CreateTag(ctx context.Context, q Querier, code, name string, actor uuid.UUID) (model.Tag, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return model.Tag{}, fmt.Errorf("generating a tag id: %w", err)
	}

	var t model.Tag
	err = q.QueryRow(ctx, `
		INSERT INTO tag (id, code, name, sort_order, created_by, updated_by)
		VALUES ($1, $2, $3, 1000, $4, $4)
		RETURNING id, code, name, sort_order`,
		id, code, name, actor).Scan(&t.ID, &t.Code, &t.Name, &t.SortOrder)
	if isUniqueViolation(err) {
		return model.Tag{}, ErrNameTaken
	}
	return t, err
}

// TagByCode reads one tag.
func TagByCode(ctx context.Context, q Querier, code string) (model.Tag, error) {
	var t model.Tag
	err := q.QueryRow(ctx,
		`SELECT id, code, name, sort_order FROM tag WHERE code = $1`, code).
		Scan(&t.ID, &t.Code, &t.Name, &t.SortOrder)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Tag{}, ErrNotFound
		}
		return model.Tag{}, err
	}
	return t, nil
}

// ListAllergens returns the seeded Annex II list. Read-only.
func ListAllergens(ctx context.Context, q Querier) ([]model.Allergen, error) {
	rows, err := q.Query(ctx,
		`SELECT id, code, reference, sort_order FROM allergen ORDER BY sort_order, code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	allergens := make([]model.Allergen, 0, 16)
	for rows.Next() {
		var a model.Allergen
		if err := rows.Scan(&a.ID, &a.Code, &a.Reference, &a.SortOrder); err != nil {
			return nil, err
		}
		allergens = append(allergens, a)
	}
	return allergens, rows.Err()
}

// ListAdditives returns the seeded additive list. Read-only.
func ListAdditives(ctx context.Context, q Querier) ([]model.Additive, error) {
	rows, err := q.Query(ctx,
		`SELECT id, code, coalesce(reference, ''), sort_order FROM additive ORDER BY sort_order, code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	additives := make([]model.Additive, 0, 16)
	for rows.Next() {
		var a model.Additive
		if err := rows.Scan(&a.ID, &a.Code, &a.Reference, &a.SortOrder); err != nil {
			return nil, err
		}
		additives = append(additives, a)
	}
	return additives, rows.Err()
}

// SetMenuItemTags replaces an item's tags.
//
// Replace rather than add and remove, for the same reason opening hours are a
// whole-set PUT: classification is edited as a set of checkboxes, and sending
// the resulting set avoids any ordering question between an add and a remove.
func SetMenuItemTags(ctx context.Context, q Querier, itemID uuid.UUID, tagIDs []uuid.UUID, actor uuid.UUID) error {
	return replaceLinks(ctx, q, replacement{
		table:  "menu_item_tag",
		column: "tag_id",
		itemID: itemID,
		linked: tagIDs,
		actor:  actor,
	})
}

// SetMenuItemAllergens replaces an item's allergens.
func SetMenuItemAllergens(ctx context.Context, q Querier, itemID uuid.UUID, allergenIDs []uuid.UUID, actor uuid.UUID) error {
	return replaceLinks(ctx, q, replacement{
		table:  "menu_item_allergen",
		column: "allergen_id",
		itemID: itemID,
		linked: allergenIDs,
		actor:  actor,
	})
}

// SetMenuItemAdditives replaces an item's additives.
func SetMenuItemAdditives(ctx context.Context, q Querier, itemID uuid.UUID, additiveIDs []uuid.UUID, actor uuid.UUID) error {
	return replaceLinks(ctx, q, replacement{
		table:  "menu_item_additive",
		column: "additive_id",
		itemID: itemID,
		linked: additiveIDs,
		actor:  actor,
	})
}

// replacement describes one join table to rewrite.
//
// The three classification tables differ only in their name and their second
// column, so they share one implementation. The table and column names are
// constants supplied by the three wrappers above and never come from a request,
// which is what makes interpolating them into the statement safe -- the ids
// themselves are bound as parameters.
type replacement struct {
	table  string
	column string
	itemID uuid.UUID
	linked []uuid.UUID
	actor  uuid.UUID
}

func replaceLinks(ctx context.Context, q Querier, in replacement) error {
	if _, err := q.Exec(ctx,
		`DELETE FROM `+in.table+` WHERE menu_item_id = $1 AND `+in.column+` <> ALL($2)`,
		in.itemID, in.linked); err != nil {
		return fmt.Errorf("clearing %s: %w", in.table, err)
	}

	if len(in.linked) == 0 {
		return nil
	}

	// ON CONFLICT DO NOTHING rather than deleting everything and re-inserting:
	// a row that is staying keeps its created_at, so "when was this item marked
	// vegan" survives an unrelated edit to its allergens.
	if _, err := q.Exec(ctx, `
		INSERT INTO `+in.table+` (menu_item_id, `+in.column+`, created_by, updated_by)
		SELECT $1, unnest($2::uuid[]), $3, $3
		ON CONFLICT DO NOTHING`,
		in.itemID, in.linked, in.actor); err != nil {
		return fmt.Errorf("setting %s: %w", in.table, err)
	}
	return nil
}
