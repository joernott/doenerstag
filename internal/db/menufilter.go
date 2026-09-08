package db

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/joernott/doenerstag/internal/model"
)

// MenuItemFilter is the query-parameter set from docs/04_api.md.
//
// Combined with AND across parameter names and OR within one name: an item
// matching ?tag=vegan&tag=spicy carries either tag, and adding
// ?exclude_allergen=nuts additionally requires it not to carry nuts. That is
// what a person filtering a menu means -- "vegan or spicy, but nothing with
// nuts" -- and it is why tags include while allergens and additives exclude.
type MenuItemFilter struct {
	CategoryIDs []uuid.UUID

	// TagCodes, ExcludeAllergenCodes and ExcludeAdditiveCodes are codes rather
	// than ids. A filter is built from a URL a person can type, and
	// ?tag=vegan is usable where ?tag=<uuid> is not.
	TagCodes             []string
	ExcludeAllergenCodes []string
	ExcludeAdditiveCodes []string

	// Available restricts to orderable items when set. Unset returns both,
	// because an unavailable item is still on the menu.
	Available *bool
}

// Empty reports whether the filter selects everything.
func (f MenuItemFilter) Empty() bool {
	return len(f.CategoryIDs) == 0 && len(f.TagCodes) == 0 &&
		len(f.ExcludeAllergenCodes) == 0 && len(f.ExcludeAdditiveCodes) == 0 &&
		f.Available == nil
}

// ListMenuItems returns a restaurant's living items in F4.6 order.
//
// The classification is loaded in three further queries rather than by joining
// it into this one. A join would multiply rows by tags times allergens times
// additives and need collapsing in Go; four queries whose sizes are bounded by
// the result set are simpler and, at menu scale, faster.
func ListMenuItems(
	ctx context.Context, q Querier, restaurantID uuid.UUID, filter MenuItemFilter,
) ([]model.MenuItem, error) {
	where := []string{"restaurant_id = $1", "deleted_at IS NULL"}
	args := []any{restaurantID}

	next := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}

	if len(filter.CategoryIDs) > 0 {
		where = append(where, "category_id = ANY("+next(filter.CategoryIDs)+")")
	}
	if filter.Available != nil {
		where = append(where, "available = "+next(*filter.Available))
	}

	// Tags include: the item carries at least one of the requested tags.
	if len(filter.TagCodes) > 0 {
		where = append(where, `EXISTS (
			SELECT 1 FROM menu_item_tag mt JOIN tag t ON t.id = mt.tag_id
			WHERE mt.menu_item_id = menu_item.id AND t.code = ANY(`+next(filter.TagCodes)+`))`)
	}

	// Allergens and additives exclude: the item carries none of them.
	//
	// NOT EXISTS rather than a NOT IN over a subquery, because NOT IN with a
	// NULL anywhere in the subquery yields no rows at all -- a classic way for
	// an exclusion filter to silently return nothing.
	if len(filter.ExcludeAllergenCodes) > 0 {
		where = append(where, `NOT EXISTS (
			SELECT 1 FROM menu_item_allergen ma JOIN allergen a ON a.id = ma.allergen_id
			WHERE ma.menu_item_id = menu_item.id AND a.code = ANY(`+next(filter.ExcludeAllergenCodes)+`))`)
	}
	if len(filter.ExcludeAdditiveCodes) > 0 {
		where = append(where, `NOT EXISTS (
			SELECT 1 FROM menu_item_additive mad JOIN additive ad ON ad.id = mad.additive_id
			WHERE mad.menu_item_id = menu_item.id AND ad.code = ANY(`+next(filter.ExcludeAdditiveCodes)+`))`)
	}

	rows, err := q.Query(ctx,
		`SELECT `+menuItemColumns+` FROM menu_item WHERE `+
			strings.Join(where, " AND ")+menuItemOrder, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.MenuItem, 0, 32)
	ids := make([]uuid.UUID, 0, 32)
	for rows.Next() {
		m, err := scanMenuItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, m)
		ids = append(ids, m.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if err := loadClassification(ctx, q, items, ids); err != nil {
		return nil, err
	}
	return items, nil
}

// loadClassification fills in tags, allergens and additives for a set of items.
func loadClassification(ctx context.Context, q Querier, items []model.MenuItem, ids []uuid.UUID) error {
	if len(items) == 0 {
		return nil
	}

	index := make(map[uuid.UUID]*model.MenuItem, len(items))
	for i := range items {
		index[items[i].ID] = &items[i]
	}

	tags, err := q.Query(ctx, `
		SELECT mt.menu_item_id, t.id, t.code, t.name, t.sort_order
		FROM menu_item_tag mt JOIN tag t ON t.id = mt.tag_id
		WHERE mt.menu_item_id = ANY($1)
		ORDER BY t.sort_order, t.code`, ids)
	if err != nil {
		return err
	}
	defer tags.Close()
	for tags.Next() {
		var (
			itemID uuid.UUID
			t      model.Tag
		)
		if err := tags.Scan(&itemID, &t.ID, &t.Code, &t.Name, &t.SortOrder); err != nil {
			return err
		}
		if item, ok := index[itemID]; ok {
			item.Tags = append(item.Tags, t)
		}
	}
	if err := tags.Err(); err != nil {
		return err
	}

	allergens, err := q.Query(ctx, `
		SELECT ma.menu_item_id, a.id, a.code, a.reference, a.sort_order
		FROM menu_item_allergen ma JOIN allergen a ON a.id = ma.allergen_id
		WHERE ma.menu_item_id = ANY($1)
		ORDER BY a.sort_order, a.code`, ids)
	if err != nil {
		return err
	}
	defer allergens.Close()
	for allergens.Next() {
		var (
			itemID uuid.UUID
			a      model.Allergen
		)
		if err := allergens.Scan(&itemID, &a.ID, &a.Code, &a.Reference, &a.SortOrder); err != nil {
			return err
		}
		if item, ok := index[itemID]; ok {
			item.Allergens = append(item.Allergens, a)
		}
	}
	if err := allergens.Err(); err != nil {
		return err
	}

	additives, err := q.Query(ctx, `
		SELECT mad.menu_item_id, ad.id, ad.code, coalesce(ad.reference, ''), ad.sort_order
		FROM menu_item_additive mad JOIN additive ad ON ad.id = mad.additive_id
		WHERE mad.menu_item_id = ANY($1)
		ORDER BY ad.sort_order, ad.code`, ids)
	if err != nil {
		return err
	}
	defer additives.Close()
	for additives.Next() {
		var (
			itemID uuid.UUID
			ad     model.Additive
		)
		if err := additives.Scan(&itemID, &ad.ID, &ad.Code, &ad.Reference, &ad.SortOrder); err != nil {
			return err
		}
		if item, ok := index[itemID]; ok {
			item.Additives = append(item.Additives, ad)
		}
	}
	return additives.Err()
}

// MenuItemWithDetail reads one item with its classification and modifications.
func MenuItemWithDetail(ctx context.Context, q Querier, id uuid.UUID) (model.MenuItem, error) {
	item, err := MenuItemByID(ctx, q, id)
	if err != nil {
		return model.MenuItem{}, err
	}

	items := []model.MenuItem{item}
	if err := loadClassification(ctx, q, items, []uuid.UUID{id}); err != nil {
		return model.MenuItem{}, err
	}

	modifications, err := ListModifications(ctx, q, id)
	if err != nil {
		return model.MenuItem{}, err
	}
	items[0].Modifications = modifications
	return items[0], nil
}
