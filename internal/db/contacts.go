package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/joernott/doenerstag/internal/model"
)

// ErrLastContact is returned when removing a contact would leave a restaurant
// with none, which F3.1 forbids.
var ErrLastContact = errors.New("db: a restaurant needs at least one contact")

// contactColumns joins the type in, so a client rendering one entry does not
// have to fetch /contact-types to find out whether it is a phone number.
const contactColumns = `c.id, c.restaurant_id, c.contact_type_id, t.code, t.render_as,
	c.value, coalesce(c.label, ''), c.sort_order`

func scanContact(row pgx.Row) (model.Contact, error) {
	var c model.Contact
	err := row.Scan(&c.ID, &c.RestaurantID, &c.ContactTypeID, &c.ContactTypeCode,
		&c.RenderAs, &c.Value, &c.Label, &c.SortOrder)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.Contact{}, ErrNotFound
		}
		return model.Contact{}, err
	}
	return c, nil
}

// NewContact is one contact entry.
type NewContact struct {
	RestaurantID  uuid.UUID
	ContactTypeID uuid.UUID
	Value         string
	Label         string
	SortOrder     int
}

// CreateContact adds a contact entry.
func CreateContact(ctx context.Context, q Querier, in NewContact, actor uuid.UUID) (model.Contact, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return model.Contact{}, fmt.Errorf("generating a contact id: %w", err)
	}

	// The insert and the read-back are one statement: a plain INSERT ...
	// RETURNING cannot reach contact_type, and a second query would be a second
	// round trip for a row that has just been written.
	row := q.QueryRow(ctx, `
		WITH inserted AS (
			INSERT INTO restaurant_contact (id, restaurant_id, contact_type_id, value,
			                                label, sort_order, created_by, updated_by)
			VALUES ($1, $2, $3, $4, nullif($5, ''), $6, $7, $7)
			RETURNING id, restaurant_id, contact_type_id, value, label, sort_order
		)
		SELECT `+contactColumns+`
		FROM inserted c JOIN contact_type t ON t.id = c.contact_type_id`,
		id, in.RestaurantID, in.ContactTypeID, in.Value, in.Label, in.SortOrder, actor)

	c, err := scanContact(row)
	if isForeignKeyViolation(err) {
		// An unknown contact type, or a restaurant that does not exist.
		return model.Contact{}, ErrNotFound
	}
	return c, err
}

// ListContacts returns a restaurant's contacts, in their configured order.
func ListContacts(ctx context.Context, q Querier, restaurantID uuid.UUID) ([]model.Contact, error) {
	rows, err := q.Query(ctx, `
		SELECT `+contactColumns+`
		FROM restaurant_contact c JOIN contact_type t ON t.id = c.contact_type_id
		WHERE c.restaurant_id = $1
		ORDER BY c.sort_order, t.sort_order, c.value`, restaurantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	contacts := make([]model.Contact, 0, 4)
	for rows.Next() {
		c, err := scanContact(rows)
		if err != nil {
			return nil, err
		}
		contacts = append(contacts, c)
	}
	return contacts, rows.Err()
}

// ContactByID reads one contact entry.
func ContactByID(ctx context.Context, q Querier, id uuid.UUID) (model.Contact, error) {
	return scanContact(q.QueryRow(ctx, `
		SELECT `+contactColumns+`
		FROM restaurant_contact c JOIN contact_type t ON t.id = c.contact_type_id
		WHERE c.id = $1`, id))
}

// ReplaceContact overwrites a contact entry.
//
// PUT rather than PATCH, per docs/04_api.md: a contact is four short fields,
// and replacing the lot is simpler to reason about than merging.
func ReplaceContact(ctx context.Context, q Querier, id uuid.UUID, in NewContact, actor uuid.UUID) (model.Contact, error) {
	row := q.QueryRow(ctx, `
		WITH updated AS (
			UPDATE restaurant_contact
			SET contact_type_id = $2, value = $3, label = nullif($4, ''),
			    sort_order = $5, updated_by = $6
			WHERE id = $1
			RETURNING id, restaurant_id, contact_type_id, value, label, sort_order
		)
		SELECT `+contactColumns+`
		FROM updated c JOIN contact_type t ON t.id = c.contact_type_id`,
		id, in.ContactTypeID, in.Value, in.Label, in.SortOrder, actor)

	c, err := scanContact(row)
	if isForeignKeyViolation(err) {
		return model.Contact{}, ErrNotFound
	}
	return c, err
}

// DeleteContact removes a contact entry, refusing to remove the last one.
//
// F3.1 says a restaurant has at least one contact. The schema deliberately does
// not enforce it -- a table constraint cannot express "at least one row in
// another table" without a deferred trigger, whose cost would be paid on every
// insert -- so it is enforced here, in the one statement that could break it.
//
// The count and the delete are one statement so that two concurrent deletes
// cannot each see two contacts and each remove one.
func DeleteContact(ctx context.Context, q Querier, id uuid.UUID) error {
	tag, err := q.Exec(ctx, `
		DELETE FROM restaurant_contact c
		WHERE c.id = $1
		  AND (SELECT count(*) FROM restaurant_contact o
		        WHERE o.restaurant_id = c.restaurant_id) > 1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 1 {
		return nil
	}

	var exists bool
	if err := q.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM restaurant_contact WHERE id = $1)`, id).
		Scan(&exists); err != nil {
		return err
	}
	if exists {
		return ErrLastContact
	}
	return ErrNotFound
}

// CountContacts is how many a restaurant has.
func CountContacts(ctx context.Context, q Querier, restaurantID uuid.UUID) (int, error) {
	var count int
	err := q.QueryRow(ctx,
		`SELECT count(*) FROM restaurant_contact WHERE restaurant_id = $1`,
		restaurantID).Scan(&count)
	return count, err
}
