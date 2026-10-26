package api_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/joernott/doenerstag/internal/api"
	"github.com/joernott/doenerstag/internal/model"
)

type messageResp struct {
	ID        string `json:"id"`
	UserID    string `json:"user_id"`
	UserName  string `json:"user_name"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
}

// say posts one line to the fixture order's chat.
func (o *orderFixture) say(cookies []*http.Cookie, body string) messageResp {
	o.t.Helper()

	rec := o.post("/orders/"+o.order.ID+"/messages", map[string]any{"body": body}, cookies...)
	if rec.Code != http.StatusCreated {
		o.t.Fatalf("posting %q: %d %s", body, rec.Code, rec.Body.String())
	}
	var created messageResp
	decode(o.t, rec, &created)
	return created
}

// chat reads the fixture order's chat.
func (o *orderFixture) chat(cookies []*http.Cookie) []messageResp {
	o.t.Helper()

	rec := o.get("/orders/"+o.order.ID+"/messages", cookies...)
	if rec.Code != http.StatusOK {
		o.t.Fatalf("reading the chat: %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Messages []messageResp `json:"messages"`
	}
	decode(o.t, rec, &body)
	return body.Messages
}

// 21.6: the people in an order talk, and read it back oldest first, each line
// with its author's name.
func TestTheChatIsReadBackInOrder(t *testing.T) {
	o := newOrderFixture(t)
	ayse := o.register("ayse")
	o.addOrderItem(ayse, map[string]any{})

	o.say(o.cookies, "Wer holt?")
	o.say(ayse, "  Ich, um zwölf.  ")

	got := o.chat(ayse)
	if len(got) != 2 {
		t.Fatalf("the chat has %d lines", len(got))
	}
	if got[0].Body != "Wer holt?" || got[0].UserID != o.userID("cook") || got[0].UserName != "cook" {
		t.Errorf("the first line is %+v", got[0])
	}
	if got[1].Body != "Ich, um zwölf." {
		t.Errorf("the second line is %q; surrounding space is trimmed", got[1].Body)
	}
	if _, err := time.Parse(time.RFC3339, got[1].CreatedAt); err != nil {
		t.Errorf("the time %q is not RFC 3339", got[1].CreatedAt)
	}

	if count := o.readOrder(o.cookies...).MessageCount; count != 2 {
		t.Errorf("the order says its chat has %d lines", count)
	}
}

// The chat is for the participants, and they are more than the people who
// ordered: whoever fetches the food or collects the money is one too, as for
// the summary (19.2), with or without an item.
func TestAJobholderWithoutAnItemMayChat(t *testing.T) {
	o := newOrderFixture(t)
	runner := o.register("runner")
	if rec := o.post("/orders/"+o.order.ID+"/pickup-person", nil, runner...); rec.Code != http.StatusOK {
		t.Fatalf("volunteering: %d %s", rec.Code, rec.Body.String())
	}

	o.say(runner, "Bin unterwegs")
	if got := o.chat(o.cookies); len(got) != 1 {
		t.Errorf("the chat has %d lines", len(got))
	}

	stranger := o.register("stranger")
	rec := o.post("/orders/"+o.order.ID+"/messages", map[string]any{"body": "Hallo?"}, stranger...)
	expectErrorCode(t, rec, api.CodeNotParticipant)
}

// An empty line says nothing, and a long one is refused rather than cut.
func TestAMessageMustSaySomethingAndNotTooMuch(t *testing.T) {
	o := newOrderFixture(t)
	path := "/orders/" + o.order.ID + "/messages"

	expectErrorCode(t, o.post(path, map[string]any{"body": "   "}, o.cookies...), api.CodeMissingField)
	expectErrorCode(t, o.post(path,
		map[string]any{"body": strings.Repeat("ä", api.MaxMessageLength+1)}, o.cookies...),
		api.CodeInvalidField)
	o.say(o.cookies, strings.Repeat("ä", api.MaxMessageLength))
}

// The chat goes with the order, and an author's deletion leaves their lines
// in place under the deleted-user placeholder (21.5).
func TestTheChatOutlivesItsAuthorButNotItsOrder(t *testing.T) {
	o := newOrderFixture(t)
	ayse := o.register("ayse")
	o.addOrderItem(ayse, map[string]any{})
	o.say(ayse, "Bitte ohne Zwiebeln")

	if rec := o.remove("/users/"+o.userID("ayse"), o.admin...); rec.Code != http.StatusNoContent {
		t.Fatalf("deleting ayse: %d %s", rec.Code, rec.Body.String())
	}
	got := o.chat(o.cookies)
	if len(got) != 1 || got[0].UserID != model.DeletedUserID.String() {
		t.Fatalf("after deleting the author the chat is %+v", got)
	}

	if rec := o.remove("/orders/"+o.order.ID, o.cookies...); rec.Code != http.StatusNoContent {
		t.Fatalf("deleting the order: %d %s", rec.Code, rec.Body.String())
	}
	var left int
	if err := o.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM order_message`).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Errorf("%d messages outlived their order", left)
	}
}

// A new line reaches the pages watching the order as a count, never as text:
// a stream cannot tell a participant from anybody else signed in, and the
// anonymous page hears nothing at all.
func TestANewMessageIsAnnouncedAsACount(t *testing.T) {
	o := newOrderFixture(t)
	signedIn := o.openStream(o.order.ID, o.cookies)
	anonymous := o.openStream(o.order.ID, nil)
	waitForSubscribers(t, o, 2)

	o.say(o.cookies, "Geheimnis")

	_, data := signedIn.waitFor("order.message")
	if data != `{"message_count":1}` {
		t.Errorf("the event carried %s", data)
	}
	anonymous.expectNothing(300 * time.Millisecond)
}
