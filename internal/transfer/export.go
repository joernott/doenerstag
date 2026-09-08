package transfer

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/joernott/doenerstag/internal/db"
)

// Export reads one restaurant into a portable form.
func Export(ctx context.Context, q db.Querier, id uuid.UUID) (Restaurant, error) {
	restaurant, err := db.RestaurantByID(ctx, q, id)
	if err != nil {
		return Restaurant{}, fmt.Errorf("reading restaurant %s: %w", id, err)
	}

	out := Restaurant{
		ID:                 restaurant.ID.String(),
		Name:               restaurant.Name,
		Currency:           restaurant.CurrencyCode,
		MinOrderValueCents: restaurant.MinOrderValueCents,
		DeliveryFeeCents:   restaurant.DeliveryFeeCents,
		Notes:              restaurant.Notes,
	}

	contacts, err := db.ListContacts(ctx, q, id)
	if err != nil {
		return Restaurant{}, fmt.Errorf("reading the contacts of %s: %w", id, err)
	}
	for _, c := range contacts {
		out.Contacts = append(out.Contacts, Contact{
			// The code, not ContactTypeID: the target installation seeds its
			// own contact types with their own ids.
			Type:      c.ContactTypeCode,
			Value:     c.Value,
			Label:     c.Label,
			SortOrder: c.SortOrder,
		})
	}

	hours, err := db.ListOpeningHours(ctx, q, id)
	if err != nil {
		return Restaurant{}, fmt.Errorf("reading the opening hours of %s: %w", id, err)
	}
	for _, h := range hours {
		out.OpeningHours = append(out.OpeningHours, OpeningHours{
			Day: h.DayOfWeek, Start: h.Start, End: h.End,
		})
	}

	categories, err := db.ListCategories(ctx, q, id)
	if err != nil {
		return Restaurant{}, fmt.Errorf("reading the categories of %s: %w", id, err)
	}
	for _, c := range categories {
		out.Categories = append(out.Categories, Category{
			ID: c.ID.String(), Name: c.Name, SortOrder: c.SortOrder,
		})
	}

	items, err := db.ListMenuItems(ctx, q, id, db.MenuItemFilter{})
	if err != nil {
		return Restaurant{}, fmt.Errorf("reading the menu of %s: %w", id, err)
	}
	for i := range items {
		item := &items[i]
		exported := Item{
			ID:          item.ID.String(),
			ExternalID:  item.ExternalID,
			Name:        item.Name,
			Description: item.Description,
			PriceCents:  item.PriceCents,
			Available:   item.Available,
		}
		if item.CategoryID != nil {
			exported.Category = item.CategoryID.String()
		}
		// Codes throughout, for the reason in the package comment.
		for _, tag := range item.Tags {
			exported.Tags = append(exported.Tags, tag.Code)
		}
		for _, allergen := range item.Allergens {
			exported.Allergens = append(exported.Allergens, allergen.Code)
		}
		for _, additive := range item.Additives {
			exported.Additives = append(exported.Additives, additive.Code)
		}

		modifications, err := db.ListModifications(ctx, q, item.ID)
		if err != nil {
			return Restaurant{}, fmt.Errorf("reading the modifications of %s: %w", item.ID, err)
		}
		for _, m := range modifications {
			exported.Modifications = append(exported.Modifications, Modification{
				Name: m.Name, PriceDeltaCents: m.PriceDeltaCents, SortOrder: m.SortOrder,
			})
		}

		out.Items = append(out.Items, exported)
	}

	return out, nil
}

// ExportAll reads every living restaurant.
func ExportAll(ctx context.Context, q db.Querier) ([]Restaurant, error) {
	restaurants, err := db.ListRestaurants(ctx, q)
	if err != nil {
		return nil, err
	}

	out := make([]Restaurant, 0, len(restaurants))
	for i := range restaurants {
		exported, err := Export(ctx, q, restaurants[i].ID)
		if err != nil {
			return nil, err
		}
		out = append(out, exported)
	}
	return out, nil
}
