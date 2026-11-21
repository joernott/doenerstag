package model

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// The outcomes of a decided vote (F12.9, F12.10).
const (
	VoteOrdered   = "ordered"
	VoteNoBallots = "no_ballots"
	VoteTooLate   = "too_late"
)

// Vote chooses the restaurant for an order that does not exist yet (F12).
type Vote struct {
	ID        uuid.UUID
	CreatorID uuid.UUID
	// CreatorName is the creator's display label, joined in for the header.
	CreatorName string

	DecisionAt   time.Time
	DeadlineAt   time.Time
	FulfilmentAt time.Time

	OpenChoices   bool
	PublicBallots bool

	Decided        bool
	Outcome        *string
	WinnerChoiceID *uuid.UUID
	OrderID        *uuid.UUID

	// Choices in the order they were added: their ids are UUIDv7.
	Choices []VoteChoice

	CreatedAt time.Time
	UpdatedAt time.Time
}

// VoteChoice is one restaurant a vote offers, with its ballots.
type VoteChoice struct {
	ID             uuid.UUID
	RestaurantID   uuid.UUID
	RestaurantName string
	RestaurantLogo *uuid.UUID

	AddedByID   uuid.UUID
	AddedByName string

	// Count is the number of ballots: live while the vote is open, the stored
	// final count once it is decided (F12.12).
	Count int

	// LastCastAt is when the newest current ballot for this choice was cast or
	// changed; nil without one. The tie rule reads it (F12.7).
	LastCastAt *time.Time

	// Voters are the people whose ballot names this choice, oldest first.
	// Always loaded; whether anybody is shown them is the API's decision
	// (F12.5), not the database's.
	Voters []Voter
}

// Voter is a person and the name to show for them.
type Voter struct {
	ID   uuid.UUID
	Name string
}

// Open reports whether ballots, restaurants and edits are still accepted.
//
// The decision time closes a vote, not the pass that decides it: between the
// two the vote is closed but not yet decided, which lasts at most one pass.
func (v Vote) Open(now time.Time) bool {
	return !v.Decided && now.Before(v.DecisionAt)
}

// Status is the derived state, for the API.
func (v Vote) Status() string {
	if v.Decided {
		return "decided"
	}
	return "open"
}

// Total is the number of ballots.
func (v Vote) Total() int {
	total := 0
	for i := range v.Choices {
		total += v.Choices[i].Count
	}
	return total
}

// Leader is the restaurant ahead (F12.7), or nil while nobody has voted.
//
// Most votes wins. Between choices level on votes, the one that reached its
// count first is ahead: the one whose newest current ballot is the oldest. A
// choice still level after that -- two ballots in the same microsecond --
// goes to the one added first, so the answer is never left to map order.
//
// A decided vote's leader is its winner, if it has one, whatever the counts
// say now.
func (v Vote) Leader() *VoteChoice {
	if v.Decided {
		if v.WinnerChoiceID == nil {
			return nil
		}
		for i := range v.Choices {
			if v.Choices[i].ID == *v.WinnerChoiceID {
				return &v.Choices[i]
			}
		}
		return nil
	}

	var leader *VoteChoice
	for i := range v.Choices {
		c := &v.Choices[i]
		if c.Count == 0 {
			continue
		}
		if leader == nil || ahead(c, leader) {
			leader = c
		}
	}
	return leader
}

// ahead reports whether a is ahead of b. Choices are walked in the order they
// were added, so on a complete tie the earlier one, b, stays ahead.
func ahead(a, b *VoteChoice) bool {
	if a.Count != b.Count {
		return a.Count > b.Count
	}
	if a.LastCastAt == nil || b.LastCastAt == nil {
		return false
	}
	return a.LastCastAt.Before(*b.LastCastAt)
}

// Title is the computed display title, as an order's is (F5.6). The browser
// builds its own in the viewer's time zone.
func (v Vote) Title() string {
	return fmt.Sprintf("Vote — %s", v.FulfilmentAt.Format("2006-01-02 15:04"))
}

// ChoiceByID finds one of the vote's choices.
func (v Vote) ChoiceByID(id uuid.UUID) (VoteChoice, bool) {
	for i := range v.Choices {
		if v.Choices[i].ID == id {
			return v.Choices[i], true
		}
	}
	return VoteChoice{}, false
}

// ChoiceOf returns the choice the given user's ballot names, if they have one.
func (v Vote) ChoiceOf(userID uuid.UUID) *uuid.UUID {
	for i := range v.Choices {
		for _, voter := range v.Choices[i].Voters {
			if voter.ID == userID {
				id := v.Choices[i].ID
				return &id
			}
		}
	}
	return nil
}
