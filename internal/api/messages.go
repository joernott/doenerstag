package api

// The chat on an order (21.5, 21.6): what the people in it say to each other
// about it. Its own file because it is its own feature, with the summary's
// permission rule rather than the order's.

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/joernott/doenerstag/internal/db"
	"github.com/joernott/doenerstag/internal/model"
	"github.com/joernott/doenerstag/internal/sse"
)

// MaxMessageLength matches the CHECK constraint in migration 000017.
const MaxMessageLength = 1000

type messageBody struct {
	ID        string `json:"id"`
	UserID    string `json:"user_id"`
	UserName  string `json:"user_name"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
}

func publicMessage(m model.OrderMessage) messageBody {
	return messageBody{
		ID:        m.ID.String(),
		UserID:    m.UserID.String(),
		UserName:  m.UserName,
		Body:      m.Body,
		CreatedAt: m.CreatedAt.UTC().Format(time.RFC3339),
	}
}

// listMessages serves an order's whole chat to its participants.
//
// The people the summary is for, and for the same reason: what is said in an
// order is between the people in it (docs/05). The creator and both
// jobholders count, whether or not they ordered anything, since 19.2. Open
// after the deadline too -- "the food is here" is said after it.
func (h *OrderHandlers) listMessages(w http.ResponseWriter, r *http.Request) {
	order, lookupErr := h.lookupOrder(r)
	if lookupErr != nil {
		WriteError(w, r, lookupErr)
		return
	}
	if _, authErr := RequireParticipant(r.Context(), r, h.Pool, order.ID); authErr != nil {
		WriteError(w, r, authErr)
		return
	}

	messages, err := db.ListMessages(r.Context(), h.Pool, order.ID)
	if err != nil {
		WriteError(w, r, &Error{Code: CodeDatabaseUnavailable, Cause: err})
		return
	}

	bodies := make([]messageBody, 0, len(messages))
	for i := range messages {
		bodies = append(bodies, publicMessage(messages[i]))
	}
	_ = WriteJSON(w, http.StatusOK, map[string]any{"messages": bodies})
}

type postMessageRequest struct {
	Body string `json:"body"`
}

// postMessage adds a line to the chat, for the same people who may read it.
func (h *OrderHandlers) postMessage(w http.ResponseWriter, r *http.Request) {
	order, lookupErr := h.lookupOrder(r)
	if lookupErr != nil {
		WriteError(w, r, lookupErr)
		return
	}
	principal, authErr := RequireParticipant(r.Context(), r, h.Pool, order.ID)
	if authErr != nil {
		WriteError(w, r, authErr)
		return
	}

	var body postMessageRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	text := strings.TrimSpace(body.Body)
	if text == "" {
		WriteError(w, r, &Error{Code: CodeMissingField, Field: "body"})
		return
	}
	if longerThan(text, MaxMessageLength) {
		WriteError(w, r, tooLong("body", "message", MaxMessageLength))
		return
	}

	created, err := db.CreateMessage(r.Context(), h.Pool, order.ID, principal.User.ID, text)
	if err != nil {
		WriteError(w, r, dbError(err))
		return
	}

	h.publishMessageCount(order.ID)
	_ = WriteJSON(w, http.StatusCreated, publicMessage(created))
}

// publishMessageCount tells the pages watching an order that its chat has
// grown, and to how many lines.
//
// The count and not the message. A stream knows only whether its subscriber
// is signed in, not whether they are in the order, and a message is for the
// people in it. So every signed-in page hears the number, which is what the
// tab's title shows, and a participant's page fetches the lines from the
// route that checks.
func (h *OrderHandlers) publishMessageCount(orderID uuid.UUID) {
	if h.Events == nil {
		return
	}

	count, err := db.CountMessages(context.Background(), h.Pool, orderID)
	if err != nil {
		return
	}
	data, err := json.Marshal(map[string]int{"message_count": count})
	if err != nil {
		return
	}
	h.Events.Publish(orderID, sse.Event{
		Name: sse.EventOrderMessage, Audience: sse.AuthenticatedOnly, Data: data,
	})
}
