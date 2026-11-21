package api_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/joernott/doenerstag/internal/api"
	"github.com/joernott/doenerstag/internal/sse"
)

type voterResp struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type voteChoiceResp struct {
	ID             string       `json:"id"`
	RestaurantID   string       `json:"restaurant_id"`
	RestaurantName string       `json:"restaurant_name"`
	Count          int          `json:"count"`
	AddedByID      string       `json:"added_by_id"`
	Voters         *[]voterResp `json:"voters"`
}

type voteResp struct {
	ID             string           `json:"id"`
	Status         string           `json:"status"`
	DecisionAt     string           `json:"decision_at"`
	DeadlineAt     string           `json:"deadline_at"`
	FulfilmentAt   string           `json:"fulfilment_at"`
	OpenChoices    bool             `json:"open_choices"`
	PublicBallots  bool             `json:"public_ballots"`
	Outcome        *string          `json:"outcome"`
	OrderID        *string          `json:"order_id"`
	Total          int              `json:"total"`
	LeaderChoiceID *string          `json:"leader_choice_id"`
	CreatorName    string           `json:"creator_name"`
	MyChoiceID     *string          `json:"my_choice_id"`
	Choices        []voteChoiceResp `json:"choices"`
}

// voteFixture is an order fixture with four restaurants and a vote over the
// first three, created by the fixture's cook.
type voteFixture struct {
	*orderFixture
	restaurants []string
	vote        voteResp
}

func newVoteFixture(t *testing.T, options map[string]any) *voteFixture {
	t.Helper()
	o := newOrderFixture(t)
	v := &voteFixture{orderFixture: o, restaurants: []string{o.restaurant}}
	for _, name := range []string{"Pizzeria Roma", "Thai Garden", "Burger Eck"} {
		v.restaurants = append(v.restaurants, o.createRestaurant(name, o.cookies).ID)
	}
	v.vote = v.createVote(o.cookies, v.restaurants[:3], options)
	return v
}

func (v *voteFixture) voteBody(restaurants []string, options map[string]any) map[string]any {
	body := map[string]any{
		"restaurant_ids": restaurants,
		"decision_at":    v.now.Add(time.Hour).UTC().Format(time.RFC3339),
		"deadline_at":    v.now.Add(2 * time.Hour).UTC().Format(time.RFC3339),
		"fulfilment_at":  v.now.Add(3 * time.Hour).UTC().Format(time.RFC3339),
	}
	for k, value := range options {
		body[k] = value
	}
	return body
}

func (v *voteFixture) createVote(cookies []*http.Cookie, restaurants []string, options map[string]any) voteResp {
	v.t.Helper()
	rec := v.post("/votes", v.voteBody(restaurants, options), cookies...)
	if rec.Code != http.StatusCreated {
		v.t.Fatalf("creating a vote: %d %s", rec.Code, rec.Body.String())
	}
	var body voteResp
	decode(v.t, rec, &body)
	return body
}

func (v *voteFixture) readVote(cookies ...*http.Cookie) voteResp {
	v.t.Helper()
	rec := v.get("/votes/"+v.vote.ID, cookies...)
	if rec.Code != http.StatusOK {
		v.t.Fatalf("reading the vote: %d %s", rec.Code, rec.Body.String())
	}
	var body voteResp
	decode(v.t, rec, &body)
	return body
}

// ballot casts a ballot for the n-th choice (0-based).
func (v *voteFixture) ballot(cookies []*http.Cookie, n int) voteResp {
	v.t.Helper()
	rec := v.put("/votes/"+v.vote.ID+"/ballot", map[string]any{"choice_id": v.vote.Choices[n].ID}, cookies...)
	if rec.Code != http.StatusOK {
		v.t.Fatalf("casting a ballot: %d %s", rec.Code, rec.Body.String())
	}
	var body voteResp
	decode(v.t, rec, &body)
	return body
}

func (v *voteFixture) decide() {
	v.t.Helper()
	if err := v.votes.DecideVotes(v.ctx()); err != nil {
		v.t.Fatalf("deciding: %v", err)
	}
}

// F12.1: two to five distinct restaurants, the times in order.
func TestAVoteIsValidatedOnCreation(t *testing.T) {
	v := newVoteFixture(t, nil)
	r := v.restaurants
	hour := func(h float64) string {
		return v.now.Add(time.Duration(h * float64(time.Hour))).UTC().Format(time.RFC3339)
	}

	cases := []struct {
		name string
		body map[string]any
		code api.Code
	}{
		{"one restaurant", v.voteBody(r[:1], nil), api.CodeVoteChoiceCount},
		{"six restaurants", v.voteBody(append(append([]string{}, r...), r[0], r[1]), nil), api.CodeVoteChoiceCount},
		{"a restaurant twice", v.voteBody([]string{r[0], r[0]}, nil), api.CodeVoteChoiceCount},
		{"a decision in the past", v.voteBody(r[:2], map[string]any{"decision_at": hour(-1)}), api.CodeDecisionTime},
		{"a decision after the deadline", v.voteBody(r[:2], map[string]any{"decision_at": hour(2.5)}), api.CodeDecisionTime},
		{"a deadline after the fulfilment", v.voteBody(r[:2], map[string]any{"deadline_at": hour(4)}), api.CodeDeadlineAfterFulfil},
		{"an unknown restaurant", v.voteBody([]string{r[0], "00000000-0000-7000-8000-00000000ffff"}, nil), api.CodeInvalidField},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			expectError(t, v.post("/votes", c.body, v.cookies...), http.StatusBadRequest, c.code)
		})
	}
}

// F12.4, F12.7: one ballot each, changeable, and the leader follows.
func TestBallotsAreCountedAndTheLeaderFollowsThem(t *testing.T) {
	v := newVoteFixture(t, nil)
	berta := v.register("berta")
	carl := v.register("carl")

	if got := v.readVote(); got.LeaderChoiceID != nil || got.Total != 0 {
		t.Fatalf("a vote nobody voted in has a leader or a total: %+v", got)
	}

	v.ballot(v.cookies, 0)
	got := v.ballot(berta, 1)
	// Level at one each: the first restaurant reached its count first.
	if got.Total != 2 || got.LeaderChoiceID == nil || *got.LeaderChoiceID != v.vote.Choices[0].ID {
		t.Fatalf("after one ballot each the leader is %v, want the first choice: %+v", got.LeaderChoiceID, got)
	}

	got = v.ballot(carl, 1)
	if *got.LeaderChoiceID != v.vote.Choices[1].ID {
		t.Errorf("two ballots against one, but the leader is %v", *got.LeaderChoiceID)
	}

	// Carl changes his mind: one ballot still, now for the first.
	got = v.ballot(carl, 0)
	if got.Total != 3 || got.Choices[0].Count != 2 || got.Choices[1].Count != 1 {
		t.Errorf("after a change of mind the counts are %+v", got.Choices)
	}
	if got.MyChoiceID == nil || *got.MyChoiceID != v.vote.Choices[0].ID {
		t.Errorf("carl's own choice is %v", got.MyChoiceID)
	}

	rec := v.remove("/votes/"+v.vote.ID+"/ballot", carl...)
	if rec.Code != http.StatusOK {
		t.Fatalf("withdrawing: %d %s", rec.Code, rec.Body.String())
	}
	if after := v.readVote(carl...); after.Total != 2 || after.MyChoiceID != nil {
		t.Errorf("after withdrawing: total %d, own choice %v", after.Total, after.MyChoiceID)
	}
}

// F12.7: re-casting the same choice keeps the ballot's place in a tie.
func TestChoosingTheSameRestaurantAgainKeepsItsPlace(t *testing.T) {
	v := newVoteFixture(t, nil)
	berta := v.register("berta")

	v.ballot(v.cookies, 0)
	v.advance(time.Minute)
	v.ballot(berta, 1)
	v.advance(time.Minute)
	got := v.ballot(v.cookies, 0)

	if *got.LeaderChoiceID != v.vote.Choices[0].ID {
		t.Errorf("clicking the same restaurant again lost it the lead to %v", *got.LeaderChoiceID)
	}
}

// F12.5: secret ballots name nobody, to anybody.
func TestSecretBallotsNameNobody(t *testing.T) {
	v := newVoteFixture(t, nil)
	berta := v.register("berta")
	v.ballot(berta, 1)

	for _, who := range []struct {
		name    string
		cookies []*http.Cookie
	}{
		{"anonymous", nil},
		{"another user", v.register("carl")},
		{"the creator", v.cookies},
		{"the administrator", v.admin},
		{"the voter herself", berta},
	} {
		rec := v.get("/votes/"+v.vote.ID, who.cookies...)
		if strings.Contains(rec.Body.String(), "voters") {
			t.Errorf("%s is shown a voters field on a secret vote: %s", who.name, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "berta") {
			t.Errorf("%s learns that berta voted: %s", who.name, rec.Body.String())
		}
	}

	// She still learns her own choice.
	if mine := v.readVote(berta...).MyChoiceID; mine == nil || *mine != v.vote.Choices[1].ID {
		t.Errorf("berta's own choice is %v", mine)
	}
}

// F12.5, F12.6: public ballots name the voters to the signed-in, not to the
// anonymous.
func TestWhoSeesTheVoters(t *testing.T) {
	v := newVoteFixture(t, map[string]any{"public_ballots": true})
	berta := v.register("berta")
	v.ballot(berta, 1)

	for _, who := range []struct {
		name    string
		cookies []*http.Cookie
		sees    bool
	}{
		{"anonymous", nil, false},
		{"another user", v.register("carl"), true},
		{"the creator", v.cookies, true},
		{"the administrator", v.admin, true},
	} {
		got := v.readVote(who.cookies...)
		voters := got.Choices[1].Voters
		named := voters != nil && len(*voters) == 1 && (*voters)[0].Name == "berta"
		if named != who.sees {
			t.Errorf("%s sees the voters: %v, want %v (%+v)", who.name, named, who.sees, voters)
		}
	}

	// And in a secret vote, nobody does: the other half of the two matrix
	// rows, asserted where TestSecretBallotsNameNobody leaves off.
	secret := newVoteFixture(t, nil)
	secret.ballot(secret.cookies, 0)
	if got := secret.readVote(secret.admin...); got.Choices[0].Voters != nil {
		t.Errorf("the administrator sees the voters of a secret vote: %+v", got.Choices[0].Voters)
	}
}

// F12.6: the anonymous shape names nobody, scanned as raw text.
func TestAnAnonymousVoteReadLeaksNothing(t *testing.T) {
	v := newVoteFixture(t, map[string]any{"public_ballots": true})
	berta := v.register("berta")
	v.ballot(berta, 0)

	for _, path := range []string{"/votes/" + v.vote.ID, "/votes"} {
		rec := v.get(path)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d", path, rec.Code)
		}
		body := opaqueValues.ReplaceAllString(rec.Body.String(), "")
		for what, needle := range map[string]string{
			"a voter's name":       "berta",
			"the creator's name":   "cook",
			"the creator":          "creator",
			"the voters":           "voters",
			"who added a choice":   "added_by",
			"anybody's own ballot": "my_choice",
		} {
			if strings.Contains(body, needle) {
				t.Errorf("the anonymous %s leaks %s (%q):\n%s", path, what, needle, body)
			}
		}
	}
	if got := v.readVote(); got.Total != 1 || got.Choices[0].Count != 1 {
		t.Errorf("the anonymous counts are wrong: %+v", got)
	}
}

// F12.2: a vote somebody voted in secretly cannot be made public.
func TestSecretBallotsStaySecret(t *testing.T) {
	v := newVoteFixture(t, nil)
	v.ballot(v.register("berta"), 0)

	rec := v.patch("/votes/"+v.vote.ID, map[string]any{"public_ballots": true}, v.cookies...)
	expectError(t, rec, http.StatusBadRequest, api.CodeInvalidField)

	// Public to secret is always fine.
	open := newVoteFixture(t, map[string]any{"public_ballots": true})
	open.ballot(open.cookies, 0)
	rec = open.patch("/votes/"+open.vote.ID, map[string]any{"public_ballots": false}, open.cookies...)
	if rec.Code != http.StatusOK {
		t.Errorf("making a public vote secret: %d %s", rec.Code, rec.Body.String())
	}
}

// F12.3: adding and removing restaurants.
func TestRestaurantsAreAddedAndRemovedWithinTheLimits(t *testing.T) {
	v := newVoteFixture(t, map[string]any{"open_choices": true})
	berta := v.register("berta")
	path := "/votes/" + v.vote.ID + "/choices"

	expectError(t, v.post(path, map[string]any{"restaurant_id": v.restaurants[0]}, berta...),
		http.StatusConflict, api.CodeChoiceExists)

	rec := v.post(path, map[string]any{"restaurant_id": v.restaurants[3]}, berta...)
	if rec.Code != http.StatusCreated {
		t.Fatalf("berta adding a fourth restaurant: %d %s", rec.Code, rec.Body.String())
	}
	var added voteResp
	decode(t, rec, &added)
	bertas := added.Choices[3].ID

	fifth := v.createRestaurant("Sushi Bar", v.cookies).ID
	if rec := v.post(path, map[string]any{"restaurant_id": fifth}, v.cookies...); rec.Code != http.StatusCreated {
		t.Fatalf("adding a fifth: %d %s", rec.Code, rec.Body.String())
	}
	sixth := v.createRestaurant("Falafel Haus", v.cookies).ID
	expectError(t, v.post(path, map[string]any{"restaurant_id": sixth}, v.cookies...),
		http.StatusConflict, api.CodeChoiceLimit)

	// Somebody else may not remove berta's; she may.
	carl := v.register("carl")
	expectError(t, v.remove(path+"/"+bertas, carl...), http.StatusForbidden, api.CodeNotChoiceOwner)

	// Not once it has a ballot.
	v.vote = added
	v.ballot(carl, 3)
	expectError(t, v.remove(path+"/"+bertas, berta...), http.StatusConflict, api.CodeChoiceChosen)
	if rec := v.remove("/votes/"+v.vote.ID+"/ballot", carl...); rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	if rec := v.remove(path+"/"+bertas, berta...); rec.Code != http.StatusOK {
		t.Errorf("berta removing her unchosen restaurant: %d %s", rec.Code, rec.Body.String())
	}
}

func TestAVoteKeepsAtLeastTwoRestaurants(t *testing.T) {
	o := newVoteFixture(t, nil)
	two := o.createVote(o.cookies, o.restaurants[:2], nil)
	rec := o.remove("/votes/"+two.ID+"/choices/"+two.Choices[1].ID, o.cookies...)
	expectError(t, rec, http.StatusConflict, api.CodeChoiceLimit)
}

// F12.8: after the decision time a vote is read-only, even before the pass
// has decided it; the administrator may still delete it.
func TestAVoteClosesAtItsDecisionTime(t *testing.T) {
	v := newVoteFixture(t, map[string]any{"open_choices": true})
	v.advance(61 * time.Minute)
	id := "/votes/" + v.vote.ID

	for _, rec := range []struct {
		name string
		rec  func() int
	}{
		{"a ballot", func() int {
			return v.put(id+"/ballot", map[string]any{"choice_id": v.vote.Choices[0].ID}, v.cookies...).Code
		}},
	} {
		if code := rec.rec(); code != http.StatusConflict {
			t.Errorf("%s after the decision time answered %d", rec.name, code)
		}
	}
	expectError(t, v.patch(id, map[string]any{"open_choices": false}, v.cookies...), http.StatusConflict, api.CodeVoteDecided)
	expectError(t, v.post(id+"/choices", map[string]any{"restaurant_id": v.restaurants[3]}, v.cookies...),
		http.StatusConflict, api.CodeVoteDecided)
	expectError(t, v.remove(id, v.cookies...), http.StatusConflict, api.CodeVoteDecided)

	if rec := v.remove(id, v.admin...); rec.Code != http.StatusNoContent {
		t.Errorf("the administrator deleting a closed vote: %d %s", rec.Code, rec.Body.String())
	}
}

// F12.9: the winner becomes an order with the vote's creator and times, as
// a pickup, once.
func TestTheDecisionMakesAnOrderForTheWinner(t *testing.T) {
	v := newVoteFixture(t, nil)
	berta := v.register("berta")
	v.ballot(berta, 2)
	v.ballot(v.cookies, 1)
	v.ballot(v.register("carl"), 2)

	v.advance(61 * time.Minute)
	v.decide()
	v.decide() // a second pass changes nothing

	got := v.readVote(berta...)
	if got.Status != "decided" || got.Outcome == nil || *got.Outcome != "ordered" || got.OrderID == nil {
		t.Fatalf("the vote was not decided into an order: %+v", got)
	}
	if *got.LeaderChoiceID != v.vote.Choices[2].ID {
		t.Errorf("the winner is %v, want the third choice", *got.LeaderChoiceID)
	}

	rec := v.get("/orders/"+*got.OrderID, berta...)
	var order struct {
		orderResponse
		VoteID *string `json:"vote_id"`
	}
	decode(t, rec, &order)
	if order.RestaurantID != v.restaurants[2] || order.Fulfilment != "pickup" ||
		order.CreatorName != "cook" || order.VoteID == nil || *order.VoteID != v.vote.ID {
		t.Errorf("the order is not what the vote said: %+v", order)
	}
	if order.DeadlineAt != v.vote.DeadlineAt || order.FulfilmentAt != v.vote.FulfilmentAt {
		t.Errorf("the order's times are %s / %s, the vote's %s / %s",
			order.DeadlineAt, order.FulfilmentAt, v.vote.DeadlineAt, v.vote.FulfilmentAt)
	}

	var list struct {
		Orders []orderResponse `json:"orders"`
	}
	decode(t, v.get("/orders"), &list)
	made := 0
	for _, o := range list.Orders {
		if o.RestaurantID == v.restaurants[2] {
			made++
		}
	}
	if made != 1 {
		t.Errorf("%d orders were made for the winner, want 1", made)
	}

	// The decided vote leaves the list of open ones.
	var votes struct {
		Votes []voteResp `json:"votes"`
	}
	decode(t, v.get("/votes"), &votes)
	if len(votes.Votes) != 0 {
		t.Errorf("a decided vote is still listed: %+v", votes.Votes)
	}
}

// F12.10: no ballots, no order; too late, no order.
func TestADecisionWithoutAnOrder(t *testing.T) {
	t.Run("nobody voted", func(t *testing.T) {
		v := newVoteFixture(t, nil)
		v.advance(61 * time.Minute)
		v.decide()
		got := v.readVote()
		if got.Outcome == nil || *got.Outcome != "no_ballots" || got.OrderID != nil {
			t.Errorf("got %+v", got)
		}
	})
	t.Run("the server was down past the deadline", func(t *testing.T) {
		v := newVoteFixture(t, nil)
		v.ballot(v.cookies, 0)
		v.advance(150 * time.Minute)
		v.decide()
		got := v.readVote()
		if got.Outcome == nil || *got.Outcome != "too_late" || got.OrderID != nil {
			t.Errorf("got %+v", got)
		}
	})
}

// F12.12, F12.14: a decided vote keeps its counts when a voter goes.
func TestADecidedVoteKeepsItsCounts(t *testing.T) {
	v := newVoteFixture(t, nil)
	berta := v.register("berta")
	v.ballot(berta, 0)
	v.advance(61 * time.Minute)
	v.decide()

	if rec := v.remove("/users/"+v.userID("berta"), berta...); rec.Code != http.StatusNoContent {
		t.Fatalf("deleting berta: %d %s", rec.Code, rec.Body.String())
	}
	if got := v.readVote(); got.Total != 1 || got.Choices[0].Count != 1 {
		t.Errorf("after the voter left the decided vote shows %+v", got)
	}
}

// A restaurant an open vote offers cannot be deleted (4003).
func TestARestaurantInAVoteCannotBeDeleted(t *testing.T) {
	v := newVoteFixture(t, nil)
	expectError(t, v.remove("/restaurants/"+v.restaurants[1], v.admin...),
		http.StatusConflict, api.CodeRestaurantInUse)
}

// F12.13: the stream tells anonymous subscribers the counts and nobody's
// name, and signed-in ones the voters only when ballots are public.
func TestTheVoteStreamTellsEachTierItsShape(t *testing.T) {
	v := newVoteFixture(t, map[string]any{"public_ballots": true})
	id := mustParseUUID(t, v.vote.ID)
	anonymous := v.registry.Subscribe(id, false)
	signedIn := v.registry.Subscribe(id, true)
	defer v.registry.Unsubscribe(id, anonymous)
	defer v.registry.Unsubscribe(id, signedIn)

	v.ballot(v.register("berta"), 0)

	next := func(sub *sse.Subscriber) sse.Event {
		select {
		case e := <-sub.Events():
			return e
		case <-time.After(5 * time.Second):
			t.Fatal("no event arrived")
			return sse.Event{}
		}
	}
	anon := next(anonymous)
	if anon.Name != sse.EventVoteUpdated || strings.Contains(string(anon.Data), "berta") {
		t.Errorf("the anonymous event: %s %s", anon.Name, anon.Data)
	}
	named := next(signedIn)
	if !strings.Contains(string(named.Data), "berta") || strings.Contains(string(named.Data), "my_choice") {
		t.Errorf("the signed-in event: %s", named.Data)
	}

	v.advance(61 * time.Minute)
	v.decide()
	var decided struct {
		Outcome string `json:"outcome"`
	}
	e := next(anonymous)
	if e.Name != sse.EventVoteDecided {
		t.Fatalf("got %s, want vote.decided", e.Name)
	}
	if err := json.Unmarshal(e.Data, &decided); err != nil || decided.Outcome != "ordered" {
		t.Errorf("vote.decided carried %s", e.Data)
	}
}

// F12.13 over the wire: an anonymous reader of the vote stream sees the
// counts move and nobody's name, a signed-in one the voters of a public vote.
func TestTheVoteStreamOverHTTP(t *testing.T) {
	v := newVoteFixture(t, map[string]any{"public_ballots": true})
	anonymous := v.openStreamAt("/votes/"+v.vote.ID+"/events", nil)
	signedIn := v.openStreamAt("/votes/"+v.vote.ID+"/events", v.cookies)

	v.ballot(v.register("berta"), 1)

	if _, data := anonymous.waitFor(sse.EventVoteUpdated); strings.Contains(data, "berta") ||
		!strings.Contains(data, `"total":1`) {
		t.Errorf("the anonymous stream said %s", data)
	}
	if _, data := signedIn.waitFor(sse.EventVoteUpdated); !strings.Contains(data, "berta") {
		t.Errorf("the signed-in stream of a public vote does not name the voter: %s", data)
	}

	rec := v.remove("/votes/"+v.vote.ID, v.cookies...)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("deleting: %d", rec.Code)
	}
	anonymous.waitFor(sse.EventVoteDeleted)
}
