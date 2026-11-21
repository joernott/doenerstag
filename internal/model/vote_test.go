package model

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func choice(n, count int, last time.Time) VoteChoice {
	c := VoteChoice{ID: uuid.MustParse("00000000-0000-7000-8000-00000000000" + string(rune('0'+n))), Count: count}
	if count > 0 {
		c.LastCastAt = &last
	}
	return c
}

// F12.7, the rule the decision and the highlight share.
func TestTheLeader(t *testing.T) {
	t0 := time.Date(2026, 11, 21, 11, 0, 0, 0, time.UTC)
	later := t0.Add(time.Minute)

	cases := []struct {
		name    string
		choices []VoteChoice
		want    int // index, -1 for none
	}{
		{"nobody voted", []VoteChoice{choice(1, 0, t0), choice(2, 0, t0)}, -1},
		{"most votes", []VoteChoice{choice(1, 1, t0), choice(2, 2, later)}, 1},
		{"level: the one that got there first", []VoteChoice{choice(1, 2, later), choice(2, 2, t0)}, 1},
		{"level at the same moment: the one added first", []VoteChoice{choice(1, 1, t0), choice(2, 1, t0)}, 0},
		{"a choice without votes never leads", []VoteChoice{choice(1, 0, t0), choice(2, 1, later)}, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := Vote{Choices: c.choices}
			leader := v.Leader()
			switch {
			case c.want < 0 && leader != nil:
				t.Errorf("a leader with nobody ahead: %+v", leader)
			case c.want >= 0 && (leader == nil || leader.ID != c.choices[c.want].ID):
				t.Errorf("leader %+v, want choice %d", leader, c.want)
			}
		})
	}
}

// A decided vote's leader is its winner, whatever the counts say since.
func TestADecidedVotesLeaderIsItsWinner(t *testing.T) {
	t0 := time.Now()
	v := Vote{Decided: true, Choices: []VoteChoice{choice(1, 3, t0), choice(2, 1, t0)}}
	if v.Leader() != nil {
		t.Error("a vote decided without a winner has a leader")
	}
	winner := v.Choices[1].ID
	v.WinnerChoiceID = &winner
	if leader := v.Leader(); leader == nil || leader.ID != winner {
		t.Errorf("the leader of a decided vote is %+v, want its winner", leader)
	}
}

func TestAVoteClosesAtItsDecisionTime(t *testing.T) {
	decision := time.Now()
	v := Vote{DecisionAt: decision}
	if !v.Open(decision.Add(-time.Second)) || v.Open(decision) {
		t.Error("a vote is open until, and not at, its decision time")
	}
	v.Decided = true
	if v.Open(decision.Add(-time.Hour)) {
		t.Error("a decided vote is open")
	}
}
