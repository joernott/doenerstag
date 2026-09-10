package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/joernott/doenerstag/internal/model"
)

// availabilityColumns is the projection every read of a filter uses.
//
// The times come back as strings rather than as a pgtype: the column is a bare
// `time` with no date and no zone, and turning it into a time.Time would attach
// a 1 January year 0 that means nothing and invites somebody to compare it with
// a real instant. The model carries minutes since midnight, which is what the
// rule actually needs.
const availabilityColumns = `f.id, f.restaurant_id, f.name, f.on_date, f.weekdays,
	to_char(f.start_time, 'HH24:MI'), to_char(f.end_time, 'HH24:MI'), f.sort_order`

func scanAvailabilityFilter(row pgx.Row) (model.AvailabilityFilter, error) {
	var (
		f          model.AvailabilityFilter
		onDate     *time.Time
		weekdays   []int16
		start, end *string
	)
	if err := row.Scan(&f.ID, &f.RestaurantID, &f.Name, &onDate, &weekdays,
		&start, &end, &f.SortOrder); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.AvailabilityFilter{}, ErrNotFound
		}
		return model.AvailabilityFilter{}, err
	}

	f.OnDate = onDate
	f.Weekdays = make([]int, 0, len(weekdays))
	for _, day := range weekdays {
		f.Weekdays = append(f.Weekdays, int(day))
	}

	minutes, err := parseClockPair(start, end)
	if err != nil {
		return model.AvailabilityFilter{}, err
	}
	f.StartTime, f.EndTime = minutes[0], minutes[1]

	return f, nil
}

// parseClockPair turns "HH:MM" into minutes since midnight, keeping the
// both-or-neither rule the schema enforces.
func parseClockPair(start, end *string) ([2]*int, error) {
	var out [2]*int
	for i, value := range []*string{start, end} {
		if value == nil {
			continue
		}
		var h, m int
		if _, err := fmt.Sscanf(*value, "%d:%d", &h, &m); err != nil {
			return out, fmt.Errorf("reading the time %q: %w", *value, err)
		}
		minutes := h*60 + m
		out[i] = &minutes
	}
	return out, nil
}

// ListAvailabilityFilters returns a restaurant's filters.
func ListAvailabilityFilters(
	ctx context.Context, q Querier, restaurantID uuid.UUID,
) ([]model.AvailabilityFilter, error) {
	rows, err := q.Query(ctx, `
		SELECT `+availabilityColumns+`
		FROM availability_filter f
		WHERE f.restaurant_id = $1
		ORDER BY f.sort_order, lower(f.name)`, restaurantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	filters := make([]model.AvailabilityFilter, 0, 8)
	for rows.Next() {
		f, err := scanAvailabilityFilter(rows)
		if err != nil {
			return nil, err
		}
		filters = append(filters, f)
	}
	return filters, rows.Err()
}

// AvailabilityFilterByID reads one filter.
func AvailabilityFilterByID(
	ctx context.Context, q Querier, id uuid.UUID,
) (model.AvailabilityFilter, error) {
	return scanAvailabilityFilter(q.QueryRow(ctx,
		`SELECT `+availabilityColumns+` FROM availability_filter f WHERE f.id = $1`, id))
}

// NewAvailabilityFilter is what creating one supplies.
type NewAvailabilityFilter struct {
	RestaurantID uuid.UUID
	Name         string
	OnDate       *time.Time
	Weekdays     []int
	StartTime    *string // "HH:MM"
	EndTime      *string
	SortOrder    int
}

// CreateAvailabilityFilter inserts a filter.
func CreateAvailabilityFilter(
	ctx context.Context, q Querier, actor uuid.UUID, in NewAvailabilityFilter,
) (model.AvailabilityFilter, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return model.AvailabilityFilter{}, fmt.Errorf("generating a filter id: %w", err)
	}

	if _, err := q.Exec(ctx, `
		INSERT INTO availability_filter
			(id, restaurant_id, name, on_date, weekdays, start_time, end_time,
			 sort_order, created_by, updated_by)
		VALUES ($1, $2, $3, $4::date, $5::smallint[], $6::time, $7::time, $8, $9, $9)`,
		id, in.RestaurantID, in.Name, in.OnDate, weekdayArray(in.Weekdays),
		in.StartTime, in.EndTime, in.SortOrder, actor); err != nil {
		return model.AvailabilityFilter{}, err
	}

	return AvailabilityFilterByID(ctx, q, id)
}

// AvailabilityFilterUpdate carries the fields PATCH may change. Every field is
// optional; a set pointer replaces, and for the nullable ones a set pointer to
// nil clears.
type AvailabilityFilterUpdate struct {
	Name      *string
	OnDate    **time.Time
	Weekdays  *[]int
	StartTime **string
	EndTime   **string
	SortOrder *int
}

// UpdateAvailabilityFilter applies the fields that are set.
func UpdateAvailabilityFilter(
	ctx context.Context, q Querier, id, actor uuid.UUID, in AvailabilityFilterUpdate,
) (model.AvailabilityFilter, error) {
	sets := make([]string, 0, 6)
	args := make([]any, 0, 8)
	add := func(clause string, value any) {
		args = append(args, value)
		sets = append(sets, fmt.Sprintf(clause, len(args)))
	}

	if in.Name != nil {
		add("name = $%d", *in.Name)
	}
	if in.OnDate != nil {
		add("on_date = $%d::date", *in.OnDate)
	}
	if in.Weekdays != nil {
		add("weekdays = $%d::smallint[]", weekdayArray(*in.Weekdays))
	}
	if in.StartTime != nil {
		add("start_time = $%d::time", *in.StartTime)
	}
	if in.EndTime != nil {
		add("end_time = $%d::time", *in.EndTime)
	}
	if in.SortOrder != nil {
		add("sort_order = $%d", *in.SortOrder)
	}
	if len(sets) == 0 {
		return AvailabilityFilterByID(ctx, q, id)
	}

	add("updated_by = $%d", actor)
	args = append(args, id)

	tag, err := q.Exec(ctx,
		`UPDATE availability_filter SET `+strings.Join(sets, ", ")+
			fmt.Sprintf(" WHERE id = $%d", len(args)), args...)
	if err != nil {
		return model.AvailabilityFilter{}, err
	}
	if tag.RowsAffected() == 0 {
		return model.AvailabilityFilter{}, ErrNotFound
	}
	return AvailabilityFilterByID(ctx, q, id)
}

// DeleteAvailabilityFilter removes a filter and every attachment of it.
//
// A hard delete: the attachments cascade, and what is lost is a rule rather
// than a record of anything that happened. An order already placed carries
// snapshots and does not consult this.
func DeleteAvailabilityFilter(ctx context.Context, q Querier, id uuid.UUID) error {
	tag, err := q.Exec(ctx, `DELETE FROM availability_filter WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// weekdayArray converts to the int16 the smallint[] column wants, and never
// returns nil: the column is NOT NULL and defaults to an empty array.
func weekdayArray(days []int) []int16 {
	out := make([]int16, 0, len(days))
	for _, day := range days {
		out = append(out, int16(day)) //nolint:gosec // 1..7, checked by the caller and the schema
	}
	return out
}

// FiltersByCategory returns the filters attached to each of a restaurant's
// categories, keyed by category.
//
// One query for the whole restaurant rather than one per category: a menu page
// asks about every category at once, and the alternative is the N+1 that
// ADR-0006 says this application is small enough to avoid by asking properly.
func FiltersByCategory(
	ctx context.Context, q Querier, restaurantID uuid.UUID,
) (map[uuid.UUID][]model.AvailabilityFilter, error) {
	return filtersByOwner(ctx, q, `
		SELECT a.category_id, `+availabilityColumns+`
		FROM menu_category_availability a
		JOIN availability_filter f ON f.id = a.filter_id
		JOIN menu_category c ON c.id = a.category_id
		WHERE c.restaurant_id = $1
		ORDER BY f.sort_order, lower(f.name)`, restaurantID)
}

// FiltersByMenuItem is the same for items.
func FiltersByMenuItem(
	ctx context.Context, q Querier, restaurantID uuid.UUID,
) (map[uuid.UUID][]model.AvailabilityFilter, error) {
	return filtersByOwner(ctx, q, `
		SELECT a.menu_item_id, `+availabilityColumns+`
		FROM menu_item_availability a
		JOIN availability_filter f ON f.id = a.filter_id
		JOIN menu_item i ON i.id = a.menu_item_id
		WHERE i.restaurant_id = $1
		ORDER BY f.sort_order, lower(f.name)`, restaurantID)
}

// filtersByOwner runs a query whose first column is the owning id and whose
// rest is availabilityColumns.
func filtersByOwner(
	ctx context.Context, q Querier, query string, restaurantID uuid.UUID,
) (map[uuid.UUID][]model.AvailabilityFilter, error) {
	rows, err := q.Query(ctx, query, restaurantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[uuid.UUID][]model.AvailabilityFilter, 8)
	for rows.Next() {
		var (
			owner      uuid.UUID
			f          model.AvailabilityFilter
			onDate     *time.Time
			weekdays   []int16
			start, end *string
		)
		if err := rows.Scan(&owner, &f.ID, &f.RestaurantID, &f.Name, &onDate, &weekdays,
			&start, &end, &f.SortOrder); err != nil {
			return nil, err
		}

		f.OnDate = onDate
		f.Weekdays = make([]int, 0, len(weekdays))
		for _, day := range weekdays {
			f.Weekdays = append(f.Weekdays, int(day))
		}
		minutes, err := parseClockPair(start, end)
		if err != nil {
			return nil, err
		}
		f.StartTime, f.EndTime = minutes[0], minutes[1]

		out[owner] = append(out[owner], f)
	}
	return out, rows.Err()
}

// SetCategoryFilters replaces the filters attached to a category.
//
// Replace rather than add-and-remove, because that is what an editor does: the
// form shows a set of ticked boxes and submits the set it now has. Doing it in
// one transaction means a failure leaves the previous set rather than half of
// the new one.
func SetCategoryFilters(
	ctx context.Context, pool txQuerier, categoryID, actor uuid.UUID, filterIDs []uuid.UUID,
) error {
	return replaceAttachments(ctx, pool, attachment{
		table: "menu_category_availability", column: "category_id",
		owner: categoryID, actor: actor, filterIDs: filterIDs,
	})
}

// SetMenuItemFilters is the same for an item.
func SetMenuItemFilters(
	ctx context.Context, pool txQuerier, menuItemID, actor uuid.UUID, filterIDs []uuid.UUID,
) error {
	return replaceAttachments(ctx, pool, attachment{
		table: "menu_item_availability", column: "menu_item_id",
		owner: menuItemID, actor: actor, filterIDs: filterIDs,
	})
}

type attachment struct {
	table     string
	column    string
	owner     uuid.UUID
	actor     uuid.UUID
	filterIDs []uuid.UUID
}

// txQuerier is a pool that can also start a transaction, which is what
// replacing a set of attachments needs.
type txQuerier interface {
	Querier
	Begin(ctx context.Context) (pgx.Tx, error)
}

func replaceAttachments(ctx context.Context, pool txQuerier, in attachment) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// The table and column names come from the two callers above and never from
	// a request; there is no user input in this string.
	if _, err := tx.Exec(ctx,
		`DELETE FROM `+in.table+` WHERE `+in.column+` = $1`, in.owner); err != nil {
		return err
	}

	for _, filterID := range in.filterIDs {
		if _, err := tx.Exec(ctx,
			`INSERT INTO `+in.table+` (`+in.column+`, filter_id, created_by, updated_by)
			 VALUES ($1, $2, $3, $3)`, in.owner, filterID, in.actor); err != nil {
			if isForeignKeyViolation(err) {
				return ErrNotFound
			}
			return err
		}
	}

	return tx.Commit(ctx)
}

// FiltersForCategory returns the filters attached to one category.
func FiltersForCategory(
	ctx context.Context, q Querier, categoryID uuid.UUID,
) ([]model.AvailabilityFilter, error) {
	return filtersForOne(ctx, q, `
		SELECT `+availabilityColumns+`
		FROM menu_category_availability a
		JOIN availability_filter f ON f.id = a.filter_id
		WHERE a.category_id = $1
		ORDER BY f.sort_order, lower(f.name)`, categoryID)
}

// FiltersForMenuItem returns the filters attached to one item.
func FiltersForMenuItem(
	ctx context.Context, q Querier, menuItemID uuid.UUID,
) ([]model.AvailabilityFilter, error) {
	return filtersForOne(ctx, q, `
		SELECT `+availabilityColumns+`
		FROM menu_item_availability a
		JOIN availability_filter f ON f.id = a.filter_id
		WHERE a.menu_item_id = $1
		ORDER BY f.sort_order, lower(f.name)`, menuItemID)
}

func filtersForOne(
	ctx context.Context, q Querier, query string, owner uuid.UUID,
) ([]model.AvailabilityFilter, error) {
	rows, err := q.Query(ctx, query, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	filters := make([]model.AvailabilityFilter, 0, 4)
	for rows.Next() {
		f, err := scanAvailabilityFilter(rows)
		if err != nil {
			return nil, err
		}
		filters = append(filters, f)
	}
	return filters, rows.Err()
}

// itemServedAt answers whether a menu item's filters, and its category's,
// permit a moment.
//
// Inside the caller's transaction, so that the answer and the insert that
// follows it see the same rules.
func itemServedAt(
	ctx context.Context, q Querier, menuItemID uuid.UUID, categoryID *uuid.UUID, at time.Time,
) (bool, error) {
	own, err := FiltersForMenuItem(ctx, q, menuItemID)
	if err != nil {
		return false, err
	}

	var category []model.AvailabilityFilter
	if categoryID != nil {
		category, err = FiltersForCategory(ctx, q, *categoryID)
		if err != nil {
			return false, err
		}
	}

	return model.ItemAvailableAt(category, own, at), nil
}
