package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/joernott/doenerstag/internal/db"
	"github.com/joernott/doenerstag/internal/model"
	"github.com/joernott/doenerstag/internal/sse"
)

// Votes (F12, ADR-0013): a vote chooses the restaurant for an order that does
// not exist yet, and the server turns it into one at the decision time.

// The number of restaurants a vote offers (F12.3).
const (
	MinVoteChoices = 2
	MaxVoteChoices = 5
)

// VoteHandlers serves votes.
type VoteHandlers struct {
	Pool *pgxpool.Pool

	// Events carries live updates, sharing the orders' registry: a vote's id
	// and an order's are both UUIDv7 and never the same. Nil disables
	// streaming.
	Events *sse.Registry

	// Logger records what must not fail a request. Nil discards.
	Logger *zerolog.Logger

	// Now is the clock; a test moves it past a decision time.
	Now func() time.Time
}

func (h *VoteHandlers) now() time.Time { return nowFrom(h.Now) }

// Register adds the vote routes.
func (h *VoteHandlers) Register(r *Router) {
	r.HandleFunc(http.MethodGet, "/votes", h.list)
	r.HandleFunc(http.MethodPost, "/votes", h.create)
	r.HandleFunc(http.MethodGet, "/votes/:id", h.get)
	r.HandleFunc(http.MethodPatch, "/votes/:id", h.patch)
	r.HandleFunc(http.MethodDelete, "/votes/:id", h.remove)

	r.HandleFunc(http.MethodPost, "/votes/:id/choices", h.addChoice)
	r.HandleFunc(http.MethodDelete, "/votes/:id/choices/:cid", h.removeChoice)

	r.HandleFunc(http.MethodPut, "/votes/:id/ballot", h.castBallot)
	r.HandleFunc(http.MethodDelete, "/votes/:id/ballot", h.withdrawBallot)

	r.HandleFunc(http.MethodGet, "/votes/:id/events", h.events)
}

// voteHeaderBody is what every caller sees about the vote itself. Like
// orderHeaderBody it has no field that could name a person: the type, not a
// rule remembered at each call site, keeps the anonymous shape anonymous.
type voteHeaderBody struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	DecisionAt   string `json:"decision_at"`
	DeadlineAt   string `json:"deadline_at"`
	FulfilmentAt string `json:"fulfilment_at"`
	Status       string `json:"status"`

	OpenChoices   bool `json:"open_choices"`
	PublicBallots bool `json:"public_ballots"`

	Outcome *string `json:"outcome"`
	OrderID *string `json:"order_id"`

	Total          int     `json:"total"`
	LeaderChoiceID *string `json:"leader_choice_id"`

	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// voteChoicePublic is a restaurant on offer and its count.
type voteChoicePublic struct {
	ID             string  `json:"id"`
	RestaurantID   string  `json:"restaurant_id"`
	RestaurantName string  `json:"restaurant_name"`
	RestaurantLogo *string `json:"restaurant_logo_image_id"`
	Count          int     `json:"count"`
}

// votePublicBody is the anonymous shape (F12.6).
type votePublicBody struct {
	voteHeaderBody
	Choices []voteChoicePublic `json:"choices"`
}

// voterBody is one person who chose a restaurant, shown only with public
// ballots.
type voterBody struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// voteChoiceBody is a choice as a signed-in caller sees it.
//
// Voters is a pointer so that a secret vote's response has no voters key at
// all (F12.5): an empty list would still say "there is a list, and somebody
// decided you see none of it".
type voteChoiceBody struct {
	voteChoicePublic
	AddedByID   string       `json:"added_by_id"`
	AddedByName string       `json:"added_by_name"`
	Voters      *[]voterBody `json:"voters,omitempty"`
}

// voteSharedBody is the signed-in shape without the caller's own ballot: what
// the live stream sends, since a stream knows whether its subscriber is
// signed in but not who they are.
type voteSharedBody struct {
	voteHeaderBody
	CreatorID   string           `json:"creator_id"`
	CreatorName string           `json:"creator_name"`
	Choices     []voteChoiceBody `json:"choices"`
}

// voteDetailBody is the signed-in shape with the caller's own ballot.
type voteDetailBody struct {
	voteSharedBody
	MyChoiceID *string `json:"my_choice_id"`
}

func (h *VoteHandlers) header(v model.Vote) voteHeaderBody {
	body := voteHeaderBody{
		ID:            v.ID.String(),
		Title:         v.Title(),
		DecisionAt:    v.DecisionAt.UTC().Format(time.RFC3339),
		DeadlineAt:    v.DeadlineAt.UTC().Format(time.RFC3339),
		FulfilmentAt:  v.FulfilmentAt.UTC().Format(time.RFC3339),
		Status:        v.Status(),
		OpenChoices:   v.OpenChoices,
		PublicBallots: v.PublicBallots,
		Outcome:       v.Outcome,
		OrderID:       idString(v.OrderID),
		Total:         v.Total(),
		CreatedAt:     v.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:     v.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if leader := v.Leader(); leader != nil {
		id := leader.ID.String()
		body.LeaderChoiceID = &id
	}
	return body
}

func publicChoice(c *model.VoteChoice) voteChoicePublic {
	return voteChoicePublic{
		ID:             c.ID.String(),
		RestaurantID:   c.RestaurantID.String(),
		RestaurantName: c.RestaurantName,
		RestaurantLogo: idString(c.RestaurantLogo),
		Count:          c.Count,
	}
}

func (h *VoteHandlers) publicBody(v model.Vote) votePublicBody {
	body := votePublicBody{voteHeaderBody: h.header(v), Choices: make([]voteChoicePublic, 0, len(v.Choices))}
	for i := range v.Choices {
		body.Choices = append(body.Choices, publicChoice(&v.Choices[i]))
	}
	return body
}

func (h *VoteHandlers) sharedBody(v model.Vote) voteSharedBody {
	body := voteSharedBody{
		voteHeaderBody: h.header(v),
		CreatorID:      v.CreatorID.String(),
		CreatorName:    v.CreatorName,
		Choices:        make([]voteChoiceBody, 0, len(v.Choices)),
	}
	for i := range v.Choices {
		c := &v.Choices[i]
		choice := voteChoiceBody{
			voteChoicePublic: publicChoice(c),
			AddedByID:        c.AddedByID.String(),
			AddedByName:      c.AddedByName,
		}
		if v.PublicBallots {
			voters := make([]voterBody, 0, len(c.Voters))
			for _, voter := range c.Voters {
				voters = append(voters, voterBody{ID: voter.ID.String(), Name: voter.Name})
			}
			choice.Voters = &voters
		}
		body.Choices = append(body.Choices, choice)
	}
	return body
}

// writeVote serves a vote in the shape the caller is entitled to.
func (h *VoteHandlers) writeVote(w http.ResponseWriter, r *http.Request, v model.Vote, status int) {
	principal := PrincipalFrom(r.Context())
	if principal == nil {
		_ = WriteJSON(w, status, h.publicBody(v))
		return
	}
	_ = WriteJSON(w, status, voteDetailBody{
		voteSharedBody: h.sharedBody(v),
		MyChoiceID:     idString(v.ChoiceOf(principal.User.ID)),
	})
}

// list serves the undecided votes.
func (h *VoteHandlers) list(w http.ResponseWriter, r *http.Request) {
	votes, err := db.ListOpenVotes(r.Context(), h.Pool)
	if err != nil {
		WriteError(w, r, dbError(err))
		return
	}
	principal := PrincipalFrom(r.Context())
	if principal == nil {
		bodies := make([]votePublicBody, 0, len(votes))
		for i := range votes {
			bodies = append(bodies, h.publicBody(votes[i]))
		}
		_ = WriteJSON(w, http.StatusOK, map[string]any{"votes": bodies})
		return
	}
	bodies := make([]voteDetailBody, 0, len(votes))
	for i := range votes {
		bodies = append(bodies, voteDetailBody{
			voteSharedBody: h.sharedBody(votes[i]),
			MyChoiceID:     idString(votes[i].ChoiceOf(principal.User.ID)),
		})
	}
	_ = WriteJSON(w, http.StatusOK, map[string]any{"votes": bodies})
}

func (h *VoteHandlers) get(w http.ResponseWriter, r *http.Request) {
	vote, err := h.lookupVote(r)
	if err != nil {
		WriteError(w, r, err)
		return
	}
	h.writeVote(w, r, vote, http.StatusOK)
}

func (h *VoteHandlers) lookupVote(r *http.Request) (model.Vote, *Error) {
	return loadParam(r, h.Pool, "id", db.VoteByID)
}

type createVoteRequest struct {
	RestaurantIDs []string `json:"restaurant_ids"`
	DecisionAt    string   `json:"decision_at"`
	DeadlineAt    string   `json:"deadline_at"`
	FulfilmentAt  string   `json:"fulfilment_at"`
	OpenChoices   bool     `json:"open_choices"`
	PublicBallots bool     `json:"public_ballots"`
}

func (h *VoteHandlers) create(w http.ResponseWriter, r *http.Request) {
	principal, authErr := RequireAuthenticated(r)
	if authErr != nil {
		WriteError(w, r, authErr)
		return
	}

	var body createVoteRequest
	if !decodeJSON(w, r, &body) {
		return
	}

	in, err := h.validateNewVote(r.Context(), body, principal.User.ID)
	if err != nil {
		WriteError(w, r, err)
		return
	}

	vote, dbErr := db.CreateVote(r.Context(), h.Pool, in)
	switch {
	case errors.Is(dbErr, db.ErrUnknownReference):
		WriteError(w, r, &Error{Code: CodeInvalidField, Field: "restaurant_ids", Detail: "no restaurant has this id"})
		return
	case errors.Is(dbErr, db.ErrChoiceLimit), errors.Is(dbErr, db.ErrChoiceExists):
		WriteError(w, r, &Error{Code: CodeVoteChoiceCount, Field: "restaurant_ids"})
		return
	case dbErr != nil:
		WriteError(w, r, dbError(dbErr))
		return
	}
	h.writeVote(w, r, vote, http.StatusCreated)
}

func (h *VoteHandlers) validateNewVote(ctx context.Context, body createVoteRequest, creator uuid.UUID) (db.NewVote, *Error) {
	ids, err := parseIDList(body.RestaurantIDs, "restaurant_ids")
	if err != nil {
		return db.NewVote{}, err
	}
	seen := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		if seen[id] {
			return db.NewVote{}, &Error{Code: CodeVoteChoiceCount, Field: "restaurant_ids"}
		}
		seen[id] = true
	}
	if len(ids) < MinVoteChoices || len(ids) > MaxVoteChoices {
		return db.NewVote{}, &Error{Code: CodeVoteChoiceCount, Field: "restaurant_ids"}
	}
	for _, id := range ids {
		if err := h.requireRestaurant(ctx, id, "restaurant_ids"); err != nil {
			return db.NewVote{}, err
		}
	}

	decisionAt, err := requiredTime(body.DecisionAt, "decision_at")
	if err != nil {
		return db.NewVote{}, err
	}
	deadlineAt, err := requiredTime(body.DeadlineAt, "deadline_at")
	if err != nil {
		return db.NewVote{}, err
	}
	fulfilmentAt, err := requiredTime(body.FulfilmentAt, "fulfilment_at")
	if err != nil {
		return db.NewVote{}, err
	}
	if err := h.checkVoteTimes(decisionAt, deadlineAt, fulfilmentAt); err != nil {
		return db.NewVote{}, err
	}

	return db.NewVote{
		CreatorID:     creator,
		RestaurantIDs: ids,
		DecisionAt:    decisionAt,
		DeadlineAt:    deadlineAt,
		FulfilmentAt:  fulfilmentAt,
		OpenChoices:   body.OpenChoices,
		PublicBallots: body.PublicBallots,
	}, nil
}

// checkVoteTimes is F12.1: now < decision < deadline < fulfilment.
func (h *VoteHandlers) checkVoteTimes(decisionAt, deadlineAt, fulfilmentAt time.Time) *Error {
	if !decisionAt.After(h.now()) || !decisionAt.Before(deadlineAt) {
		return &Error{Code: CodeDecisionTime, Field: "decision_at"}
	}
	return checkDeadline(deadlineAt, fulfilmentAt)
}

// requireRestaurant answers 1002 for a restaurant that does not exist or has
// been deleted.
func (h *VoteHandlers) requireRestaurant(ctx context.Context, id uuid.UUID, field string) *Error {
	exists, err := db.RestaurantExists(ctx, h.Pool, id)
	if err != nil {
		return dbError(err)
	}
	if !exists {
		return &Error{Code: CodeInvalidField, Field: field, Detail: "no restaurant has this id"}
	}
	return nil
}

// requireOpen answers 4009 once the decision time has passed, decided or not.
func (h *VoteHandlers) requireOpen(v model.Vote) *Error {
	if !v.Open(h.now()) {
		return &Error{Code: CodeVoteDecided}
	}
	return nil
}

// requireVoteCreator allows the vote's creator or the administrator.
func requireVoteCreator(r *http.Request, v model.Vote) (*Principal, *Error) {
	principal, err := RequireAuthenticated(r)
	if err != nil {
		return nil, err
	}
	if !principal.Is(v.CreatorID) && !principal.IsAdmin() {
		return nil, &Error{Code: CodeNotVoteCreator}
	}
	return principal, nil
}

type patchVoteRequest struct {
	DecisionAt    *string `json:"decision_at"`
	DeadlineAt    *string `json:"deadline_at"`
	FulfilmentAt  *string `json:"fulfilment_at"`
	OpenChoices   *bool   `json:"open_choices"`
	PublicBallots *bool   `json:"public_ballots"`
}

func (h *VoteHandlers) patch(w http.ResponseWriter, r *http.Request) {
	vote, lookupErr := h.lookupVote(r)
	if lookupErr != nil {
		WriteError(w, r, lookupErr)
		return
	}
	principal, authErr := requireVoteCreator(r, vote)
	if authErr != nil {
		WriteError(w, r, authErr)
		return
	}
	if err := h.requireOpen(vote); err != nil {
		WriteError(w, r, err)
		return
	}

	var body patchVoteRequest
	if !decodeJSON(w, r, &body) {
		return
	}

	update, err := h.validateVotePatch(r.Context(), vote, body)
	if err != nil {
		WriteError(w, r, err)
		return
	}

	updated, dbErr := db.UpdateVote(r.Context(), h.Pool, vote.ID, principal.User.ID, update)
	if errors.Is(dbErr, db.ErrNotFound) {
		// Decided between the check and the write.
		WriteError(w, r, &Error{Code: CodeVoteDecided})
		return
	}
	if dbErr != nil {
		WriteError(w, r, dbError(dbErr))
		return
	}
	h.publishVoteChange(updated)
	h.writeVote(w, r, updated, http.StatusOK)
}

func (h *VoteHandlers) validateVotePatch(ctx context.Context, vote model.Vote, body patchVoteRequest) (db.VoteUpdate, *Error) {
	var update db.VoteUpdate

	// The three times are checked together, each falling back to the stored
	// value, as an order's two are: a caller must not move one past another
	// they did not send.
	decisionAt, deadlineAt, fulfilmentAt := vote.DecisionAt, vote.DeadlineAt, vote.FulfilmentAt
	for _, f := range []struct {
		value  *string
		field  string
		target *time.Time
		out    **time.Time
	}{
		{body.DecisionAt, "decision_at", &decisionAt, &update.DecisionAt},
		{body.DeadlineAt, "deadline_at", &deadlineAt, &update.DeadlineAt},
		{body.FulfilmentAt, "fulfilment_at", &fulfilmentAt, &update.FulfilmentAt},
	} {
		if f.value == nil {
			continue
		}
		parsed, err := requiredTime(*f.value, f.field)
		if err != nil {
			return update, err
		}
		*f.target = parsed
		*f.out = &parsed
	}
	if body.DecisionAt != nil || body.DeadlineAt != nil || body.FulfilmentAt != nil {
		if err := h.checkVoteTimes(decisionAt, deadlineAt, fulfilmentAt); err != nil {
			return update, err
		}
	}

	update.OpenChoices = body.OpenChoices

	// Ballots cast in secret stay secret (F12.2): a vote with ballots cannot
	// be made public, or a switch would publish what people were promised
	// nobody would see.
	if body.PublicBallots != nil && *body.PublicBallots && !vote.PublicBallots {
		n, err := db.CountBallots(ctx, h.Pool, vote.ID)
		if err != nil {
			return update, dbError(err)
		}
		if n > 0 {
			return update, &Error{
				Code:   CodeInvalidField,
				Field:  "public_ballots",
				Detail: "ballots were cast in secret, so the vote stays secret",
			}
		}
	}
	update.PublicBallots = body.PublicBallots
	return update, nil
}

// remove deletes a vote: the creator until the decision time, the
// administrator always (F12.8).
func (h *VoteHandlers) remove(w http.ResponseWriter, r *http.Request) {
	vote, lookupErr := h.lookupVote(r)
	if lookupErr != nil {
		WriteError(w, r, lookupErr)
		return
	}
	principal, authErr := requireVoteCreator(r, vote)
	if authErr != nil {
		WriteError(w, r, authErr)
		return
	}
	if !principal.IsAdmin() {
		if err := h.requireOpen(vote); err != nil {
			WriteError(w, r, err)
			return
		}
	}

	if err := db.DeleteVote(r.Context(), h.Pool, vote.ID); err != nil {
		WriteError(w, r, dbError(err))
		return
	}
	h.publishVoteGone(vote.ID)
	w.WriteHeader(http.StatusNoContent)
}

type addChoiceRequest struct {
	RestaurantID string `json:"restaurant_id"`
}

// addChoice adds a restaurant (F12.3): the creator always, anybody signed in
// when the creator allows it.
func (h *VoteHandlers) addChoice(w http.ResponseWriter, r *http.Request) {
	vote, lookupErr := h.lookupVote(r)
	if lookupErr != nil {
		WriteError(w, r, lookupErr)
		return
	}
	principal, authErr := RequireAuthenticated(r)
	if authErr != nil {
		WriteError(w, r, authErr)
		return
	}
	if !vote.OpenChoices && !principal.Is(vote.CreatorID) && !principal.IsAdmin() {
		WriteError(w, r, &Error{Code: CodeChoicesClosed})
		return
	}
	if err := h.requireOpen(vote); err != nil {
		WriteError(w, r, err)
		return
	}

	var body addChoiceRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	restaurantID, err := requiredUUID(body.RestaurantID, "restaurant_id")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	for i := range vote.Choices {
		if vote.Choices[i].RestaurantID == restaurantID {
			WriteError(w, r, &Error{Code: CodeChoiceExists})
			return
		}
	}
	if len(vote.Choices) >= MaxVoteChoices {
		WriteError(w, r, &Error{Code: CodeChoiceLimit})
		return
	}
	if err := h.requireRestaurant(r.Context(), restaurantID, "restaurant_id"); err != nil {
		WriteError(w, r, err)
		return
	}

	dbErr := db.AddVoteChoice(r.Context(), h.Pool, vote.ID, restaurantID, principal.User.ID)
	if dbErr != nil {
		WriteError(w, r, choiceError(dbErr, "restaurant_id"))
		return
	}
	h.writeChangedVote(w, r, vote.ID, http.StatusCreated)
}

// removeChoice removes a restaurant nobody has chosen (F12.3).
func (h *VoteHandlers) removeChoice(w http.ResponseWriter, r *http.Request) {
	vote, lookupErr := h.lookupVote(r)
	if lookupErr != nil {
		WriteError(w, r, lookupErr)
		return
	}
	choiceID, parseErr := uuid.Parse(Param(r, "cid"))
	choice, found := vote.ChoiceByID(choiceID)
	if parseErr != nil || !found {
		WriteError(w, r, &Error{Code: CodeNotFound})
		return
	}
	principal, authErr := RequireAuthenticated(r)
	if authErr != nil {
		WriteError(w, r, authErr)
		return
	}
	if !principal.Is(vote.CreatorID) && !principal.Is(choice.AddedByID) && !principal.IsAdmin() {
		WriteError(w, r, &Error{Code: CodeNotChoiceOwner})
		return
	}
	if err := h.requireOpen(vote); err != nil {
		WriteError(w, r, err)
		return
	}
	if choice.Count > 0 {
		WriteError(w, r, &Error{Code: CodeChoiceChosen})
		return
	}
	if len(vote.Choices) <= MinVoteChoices {
		WriteError(w, r, &Error{Code: CodeChoiceLimit})
		return
	}

	if dbErr := db.RemoveVoteChoice(r.Context(), h.Pool, vote.ID, choiceID); dbErr != nil {
		WriteError(w, r, choiceError(dbErr, ""))
		return
	}
	h.writeChangedVote(w, r, vote.ID, http.StatusOK)
}

// choiceError maps the schema's answer to a racing choice change.
func choiceError(err error, field string) *Error {
	switch {
	case errors.Is(err, db.ErrChoiceLimit):
		return &Error{Code: CodeChoiceLimit}
	case errors.Is(err, db.ErrChoiceExists):
		return &Error{Code: CodeChoiceExists}
	case errors.Is(err, db.ErrChoiceChosen):
		return &Error{Code: CodeChoiceChosen}
	case errors.Is(err, db.ErrNotFound):
		// Decided, or the choice gone, between the check and the write.
		return &Error{Code: CodeVoteDecided}
	case errors.Is(err, db.ErrUnknownReference):
		return &Error{Code: CodeInvalidField, Field: field, Detail: "no restaurant has this id"}
	}
	return dbError(err)
}

type ballotRequest struct {
	ChoiceID string `json:"choice_id"`
}

// castBallot records the caller's choice, replacing an earlier one (F12.4).
//
// Nothing here logs which restaurant was chosen: the request log names the
// route and the caller, and F12.5 promises that no log line says more.
func (h *VoteHandlers) castBallot(w http.ResponseWriter, r *http.Request) {
	vote, lookupErr := h.lookupVote(r)
	if lookupErr != nil {
		WriteError(w, r, lookupErr)
		return
	}
	principal, authErr := RequireAuthenticated(r)
	if authErr != nil {
		WriteError(w, r, authErr)
		return
	}
	if err := RequireNotPlaceholder(principal.User.ID); err != nil {
		WriteError(w, r, err)
		return
	}
	if err := h.requireOpen(vote); err != nil {
		WriteError(w, r, err)
		return
	}

	var body ballotRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	choiceID, err := requiredUUID(body.ChoiceID, "choice_id")
	if err != nil {
		WriteError(w, r, err)
		return
	}
	if _, found := vote.ChoiceByID(choiceID); !found {
		WriteError(w, r, &Error{Code: CodeChoiceWrongVote, Field: "choice_id"})
		return
	}

	dbErr := db.CastBallot(r.Context(), h.Pool, vote.ID, principal.User.ID, choiceID, h.now())
	switch {
	case errors.Is(dbErr, db.ErrChoiceWrongVote):
		// The choice was removed between the check and the write.
		WriteError(w, r, &Error{Code: CodeChoiceWrongVote, Field: "choice_id"})
		return
	case dbErr != nil:
		WriteError(w, r, dbError(dbErr))
		return
	}
	h.writeChangedVote(w, r, vote.ID, http.StatusOK)
}

// withdrawBallot removes the caller's ballot. Withdrawing one the caller does
// not have is not an error: the state they asked for holds.
func (h *VoteHandlers) withdrawBallot(w http.ResponseWriter, r *http.Request) {
	vote, lookupErr := h.lookupVote(r)
	if lookupErr != nil {
		WriteError(w, r, lookupErr)
		return
	}
	principal, authErr := RequireAuthenticated(r)
	if authErr != nil {
		WriteError(w, r, authErr)
		return
	}
	if err := h.requireOpen(vote); err != nil {
		WriteError(w, r, err)
		return
	}
	if err := db.WithdrawBallot(r.Context(), h.Pool, vote.ID, principal.User.ID); err != nil {
		WriteError(w, r, dbError(err))
		return
	}
	h.writeChangedVote(w, r, vote.ID, http.StatusOK)
}

// writeChangedVote reads the vote back after a change, tells the stream and
// answers the caller.
func (h *VoteHandlers) writeChangedVote(w http.ResponseWriter, r *http.Request, id uuid.UUID, status int) {
	vote, err := db.VoteByID(r.Context(), h.Pool, id)
	if err != nil {
		WriteError(w, r, dbError(err))
		return
	}
	h.publishVoteChange(vote)
	h.writeVote(w, r, vote, status)
}

// publishVoteChange sends vote.updated: the anonymous shape to anonymous
// subscribers and the shared signed-in shape to the rest. Neither carries a
// caller's own ballot, and with secret ballots neither names a voter.
func (h *VoteHandlers) publishVoteChange(v model.Vote) {
	if h.Events == nil {
		return
	}
	events := make([]sse.Event, 0, 2)
	if data, err := json.Marshal(h.publicBody(v)); err == nil {
		events = append(events, sse.Event{Name: sse.EventVoteUpdated, Audience: sse.AnonymousOnly, Data: data})
	}
	if data, err := json.Marshal(h.sharedBody(v)); err == nil {
		events = append(events, sse.Event{Name: sse.EventVoteUpdated, Audience: sse.AuthenticatedOnly, Data: data})
	}
	h.Events.Publish(v.ID, events...)
}

func (h *VoteHandlers) publishVoteGone(id uuid.UUID) {
	if h.Events == nil {
		return
	}
	data, err := json.Marshal(map[string]string{"id": id.String()})
	if err != nil {
		return
	}
	h.Events.Publish(id, sse.Event{Name: sse.EventVoteDeleted, Audience: sse.Everyone, Data: data})
}

func (h *VoteHandlers) publishVoteDecided(d db.DecidedVote) {
	if h.Events == nil {
		return
	}
	data, err := json.Marshal(map[string]any{
		"id": d.VoteID.String(), "outcome": d.Outcome, "order_id": idString(d.OrderID),
	})
	if err != nil {
		return
	}
	h.Events.Publish(d.VoteID, sse.Event{Name: sse.EventVoteDecided, Audience: sse.Everyone, Data: data})
}

// events serves the per-vote SSE stream, built like the order stream.
func (h *VoteHandlers) events(w http.ResponseWriter, r *http.Request) {
	vote, lookupErr := h.lookupVote(r)
	if lookupErr != nil {
		WriteError(w, r, lookupErr)
		return
	}
	if h.Events == nil {
		WriteError(w, r, &Error{Code: CodeNotFound})
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		WriteError(w, r, &Error{Code: CodeInternal, Detail: "this server cannot stream events"})
		return
	}

	sub := h.Events.Subscribe(vote.ID, PrincipalFrom(r.Context()) != nil)
	defer h.Events.Unsubscribe(vote.ID, sub)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// The stream is meant to stay open, so the write deadline goes, as on the
	// order stream (ADR-0003).
	if err := http.NewResponseController(w).SetWriteDeadline(time.Time{}); err != nil {
		requestLogger(h.Logger, r).Debug().Err(err).Msg("could not clear the stream write deadline")
	}
	if _, err := fmt.Fprint(w, ": open\n\n"); err != nil {
		return
	}
	flusher.Flush()

	ping := time.NewTicker(PingInterval)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-sub.Events():
			if !ok {
				return
			}
			if err := writeEvent(w, event); err != nil {
				return
			}
			flusher.Flush()
		case <-ping.C:
			if _, err := fmt.Fprint(w, "event: ping\ndata: {}\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// DecideVotes decides every vote whose time has come and tells the pages
// watching them. Exported so a test can stand at a decision time and run it.
func (h *VoteHandlers) DecideVotes(ctx context.Context) error {
	decided, err := db.DecideVotes(ctx, h.Pool, h.now())
	for _, d := range decided {
		h.publishVoteDecided(d)
		if vote, readErr := db.VoteByID(ctx, h.Pool, d.VoteID); readErr == nil {
			h.publishVoteChange(vote)
		}
	}
	return err
}

// RunDecisions calls DecideVotes now and every interval until ctx ends, as
// RunDraws does for the jobs: the decision is made whether or not anybody has
// the page open, and the pass at start catches what fell due while the server
// was down (ADR-0013).
func (h *VoteHandlers) RunDecisions(ctx context.Context, interval time.Duration) {
	if h.Pool == nil {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := h.DecideVotes(ctx); err != nil && ctx.Err() == nil {
			requestLogger(h.Logger, nil).Warn().Err(err).
				Msg("deciding votes failed; trying again")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
