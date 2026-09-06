package db

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/joernott/doenerstag/internal/model"
)

// ListCurrencies returns the seeded currency table.
//
// No display name: seeded reference data is identified by a stable code and
// named by the frontend's i18n catalog. See docs/07_i18n.md.
func ListCurrencies(ctx context.Context, q Querier) ([]model.Currency, error) {
	rows, err := q.Query(ctx,
		`SELECT code, symbol, minor_unit, sort_order FROM currency ORDER BY sort_order, code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	currencies := make([]model.Currency, 0, 16)
	for rows.Next() {
		var c model.Currency
		if err := rows.Scan(&c.Code, &c.Symbol, &c.MinorUnit, &c.SortOrder); err != nil {
			return nil, err
		}
		currencies = append(currencies, c)
	}
	return currencies, rows.Err()
}

// CurrencyExists reports whether a code is seeded.
//
// Used to answer an unknown currency with 1013 rather than letting the foreign
// key fail: 1013 names the field, and a constraint violation does not.
func CurrencyExists(ctx context.Context, q Querier, code string) (bool, error) {
	var exists bool
	err := q.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM currency WHERE code = $1)`, code).Scan(&exists)
	return exists, err
}

// ListContactTypes returns the seeded contact types.
func ListContactTypes(ctx context.Context, q Querier) ([]model.ContactType, error) {
	rows, err := q.Query(ctx,
		`SELECT id, code, render_as, sort_order FROM contact_type ORDER BY sort_order, code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	types := make([]model.ContactType, 0, 8)
	for rows.Next() {
		var t model.ContactType
		if err := rows.Scan(&t.ID, &t.Code, &t.RenderAs, &t.SortOrder); err != nil {
			return nil, err
		}
		types = append(types, t)
	}
	return types, rows.Err()
}

// ContactTypeByID reads one contact type.
func ContactTypeByID(ctx context.Context, q Querier, id uuid.UUID) (model.ContactType, error) {
	var t model.ContactType
	err := q.QueryRow(ctx,
		`SELECT id, code, render_as, sort_order FROM contact_type WHERE id = $1`, id).
		Scan(&t.ID, &t.Code, &t.RenderAs, &t.SortOrder)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.ContactType{}, ErrNotFound
		}
		return model.ContactType{}, err
	}
	return t, nil
}
