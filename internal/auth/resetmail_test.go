package auth_test

import (
	"strings"
	"testing"

	"github.com/joernott/doenerstag/internal/auth"
	"github.com/joernott/doenerstag/internal/model"
)

// 20.18: one reset mail for the browser and the command line, which kept a copy
// each and had drifted -- the command line's still greeted "Hello ," when an
// account had no display name, and did not say which account the link was for.
func TestTheResetMailNamesTheAccountAndGreetsEveryone(t *testing.T) {
	body := auth.ResetMailBody(model.User{Name: "chef"}, "https://doener.test/reset-password/x")

	if !strings.HasPrefix(body, "Hello chef,") {
		t.Errorf("an account without a display name is greeted %q", strings.SplitN(body, "\n", 2)[0])
	}
	if !strings.Contains(body, `account "chef"`) {
		t.Errorf("the mail does not say which account it is for:\n%s", body)
	}
	if !strings.Contains(body, "https://doener.test/reset-password/x") {
		t.Errorf("the mail does not carry the link:\n%s", body)
	}
}

func TestTheResetLinkIgnoresATrailingSlash(t *testing.T) {
	for _, base := range []string{"https://doener.test", "https://doener.test/"} {
		if got := auth.ResetLink(base, "tok"); got != "https://doener.test/reset-password/tok" {
			t.Errorf("ResetLink(%q) = %q", base, got)
		}
	}
}
