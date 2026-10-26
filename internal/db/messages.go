package db

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/joernott/doenerstag/internal/model"
)

// messageColumns joins in the author's display label, as orderColumns does
// for the creator.
const messageColumns = `m.id, m.order_id, m.user_id, coalesce(u.display_name, u.name),
	m.body, m.created_at`

func scanMessage(row pgx.CollectableRow) (model.OrderMessage, error) {
	var m model.OrderMessage
	err := row.Scan(&m.ID, &m.OrderID, &m.UserID, &m.UserName, &m.Body, &m.CreatedAt)
	return m, err
}

// CreateMessage adds one line to an order's chat and returns it as it will
// be listed.
func CreateMessage(
	ctx context.Context, q Querier, orderID, userID uuid.UUID, body string,
) (model.OrderMessage, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return model.OrderMessage{}, fmt.Errorf("generating a message id: %w", err)
	}

	rows, err := q.Query(ctx, `
		WITH created AS (
			INSERT INTO order_message (id, order_id, user_id, body, created_by, updated_by)
			VALUES ($1, $2, $3, $4, $3, $3)
			RETURNING *
		)
		SELECT `+messageColumns+`
		FROM created m JOIN app_user u ON u.id = m.user_id`,
		id, orderID, userID, body)
	if err != nil {
		return model.OrderMessage{}, err
	}
	m, err := pgx.CollectExactlyOneRow(rows, scanMessage)
	if isForeignKeyViolation(err) {
		return model.OrderMessage{}, ErrNotFound
	}
	return m, err
}

// ListMessages returns an order's whole chat, oldest first.
//
// Whole, because ADR-0006 has no pagination and a lunch order's chat is a
// few dozen lines at most.
func ListMessages(ctx context.Context, q Querier, orderID uuid.UUID) ([]model.OrderMessage, error) {
	rows, err := q.Query(ctx, `
		SELECT `+messageColumns+`
		FROM order_message m JOIN app_user u ON u.id = m.user_id
		WHERE m.order_id = $1
		ORDER BY m.created_at, m.id`, orderID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanMessage)
}

// CountMessages is how many lines an order's chat has, which the Chat tab
// shows in its title.
func CountMessages(ctx context.Context, q Querier, orderID uuid.UUID) (int, error) {
	var count int
	err := q.QueryRow(ctx,
		`SELECT count(*) FROM order_message WHERE order_id = $1`, orderID).Scan(&count)
	return count, err
}
