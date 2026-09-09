package transfer

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/joernott/doenerstag/internal/db"
)

// insertRestaurant writes the whole restaurant inside the caller's transaction.
//
// The statements are written here rather than reusing the db package's Create
// functions, and for one reason: those generate ids, and an import has to keep
// the ones the file carries. That is what makes re-importing the same file
// recognisable as the same restaurant rather than as a duplicate with a new
// identity, and what lets an item name its category without a second scheme.
func insertRestaurant(
	ctx context.Context, q db.Querier, id uuid.UUID, in Restaurant, codes *codeTables, actor uuid.UUID,
) error {
	if _, err := q.Exec(ctx, `
		INSERT INTO restaurant
			(id, name, currency_code, min_order_value_cents, delivery_fee_cents,
			 notes, created_by, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $7)`,
		id, in.Name, strings.ToUpper(in.Currency),
		in.MinOrderValueCents, in.DeliveryFeeCents, in.Notes, actor,
	); err != nil {
		return fmt.Errorf("writing restaurant %s: %w", in.Name, err)
	}

	for i := range in.Contacts {
		contact := &in.Contacts[i]
		if _, err := q.Exec(ctx, `
			INSERT INTO restaurant_contact
				(id, restaurant_id, contact_type_id, value, label, sort_order,
				 created_by, updated_by)
			-- nullif on the label: the column is nullable with a length check that
			-- refuses the empty string, so a contact exported without a label has
			-- to arrive as NULL rather than as "".
			VALUES (gen_random_uuid(), $1, $2, $3, nullif($4, ''), $5, $6, $6)`,
			id, codes.contactType[contact.Type], contact.Value, contact.Label,
			contact.SortOrder, actor,
		); err != nil {
			return fmt.Errorf("writing a contact of %s: %w", in.Name, err)
		}
	}

	for _, hours := range in.OpeningHours {
		if _, err := q.Exec(ctx, `
			INSERT INTO opening_hours
				(id, restaurant_id, day_of_week, start_time, end_time,
				 created_by, updated_by)
			VALUES (gen_random_uuid(), $1, $2, $3::time, $4::time, $5, $5)`,
			id, hours.Day, hours.Start, hours.End, actor,
		); err != nil {
			return fmt.Errorf("writing the opening hours of %s: %w", in.Name, err)
		}
	}

	for _, category := range in.Categories {
		categoryID, err := uuid.Parse(category.ID)
		if err != nil {
			return fmt.Errorf("category id %q is not a uuid: %w", category.ID, err)
		}
		if _, err := q.Exec(ctx, `
			INSERT INTO menu_category
				(id, restaurant_id, name, sort_order, created_by, updated_by)
			VALUES ($1, $2, $3, $4, $5, $5)`,
			categoryID, id, category.Name, category.SortOrder, actor,
		); err != nil {
			return fmt.Errorf("writing category %q of %s: %w", category.Name, in.Name, err)
		}
	}

	for i := range in.Items {
		if err := insertItem(ctx, q, id, &in.Items[i], codes, actor); err != nil {
			return err
		}
	}
	return nil
}

func insertItem(
	ctx context.Context, q db.Querier, restaurantID uuid.UUID, item *Item, codes *codeTables, actor uuid.UUID,
) error {
	itemID, err := uuid.Parse(item.ID)
	if err != nil {
		return fmt.Errorf("item id %q is not a uuid: %w", item.ID, err)
	}

	// A category the document does not declare is left empty rather than
	// refused. It means the file was hand-edited, and an item under no heading
	// is a shape the menu already supports -- better than refusing the import
	// over a heading.
	var categoryID *uuid.UUID
	if item.Category != "" {
		parsed, err := uuid.Parse(item.Category)
		if err != nil {
			return fmt.Errorf("item %q names category %q, which is not a uuid", item.Name, item.Category)
		}
		categoryID = &parsed
	}

	if _, err := q.Exec(ctx, `
		INSERT INTO menu_item
			(id, restaurant_id, category_id, external_id, name, description,
			 price_cents, available, created_by, updated_by)
		VALUES ($1, $2, $3, nullif($4, ''), $5, $6, $7, $8, $9, $9)`,
		itemID, restaurantID, categoryID, item.ExternalID, item.Name,
		item.Description, item.PriceCents, item.Available, actor,
	); err != nil {
		return fmt.Errorf("writing item %q: %w", item.Name, err)
	}

	for _, m := range item.Modifications {
		if _, err := q.Exec(ctx, `
			INSERT INTO menu_item_modification
				(id, menu_item_id, name, price_delta_cents, sort_order,
				 created_by, updated_by)
			VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $5)`,
			itemID, m.Name, m.PriceDeltaCents, m.SortOrder, actor,
		); err != nil {
			return fmt.Errorf("writing a modification of %q: %w", item.Name, err)
		}
	}

	for _, link := range []struct {
		table  string
		column string
		codes  []string
		lookup map[string]uuid.UUID
	}{
		{"menu_item_tag", "tag_id", item.Tags, codes.tags},
		{"menu_item_allergen", "allergen_id", item.Allergens, codes.allergens},
		{"menu_item_additive", "additive_id", item.Additives, codes.additives},
	} {
		for _, code := range link.codes {
			// The table and column names come from this literal list, never
			// from the document: a file must not be able to name a table.
			statement := fmt.Sprintf(
				`INSERT INTO %s (menu_item_id, %s, created_by, updated_by) VALUES ($1, $2, $3, $3)`,
				link.table, link.column)
			if _, err := q.Exec(ctx, statement, itemID, link.lookup[code], actor); err != nil {
				return fmt.Errorf("linking %q to %s %q: %w", item.Name, link.table, code, err)
			}
		}
	}
	return nil
}
