package install

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// prompterFor builds a Prompter fed by a canned script of answers.
func prompterFor(answers string) (*Prompter, *bytes.Buffer) {
	out := &bytes.Buffer{}
	return NewPrompter(strings.NewReader(answers), out, false), out
}

func TestAskUsesTheAnswerGiven(t *testing.T) {
	p, _ := prompterFor("db.example.invalid\n")

	got, err := p.Ask(Question{Prompt: "Database host", Default: "localhost"})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if got != "db.example.invalid" {
		t.Errorf("got %q, want the typed answer", got)
	}
}

// Pressing return takes the default. That is what makes re-running install
// against an existing configuration a matter of holding down the return key.
func TestEmptyAnswerTakesTheDefault(t *testing.T) {
	p, out := prompterFor("\n")

	got, err := p.Ask(Question{Prompt: "Database host", Default: "localhost"})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if got != "localhost" {
		t.Errorf("got %q, want the default", got)
	}
	if !strings.Contains(out.String(), "[localhost]") {
		t.Errorf("the default was not offered in the prompt: %q", out)
	}
}

func TestWhitespaceIsTrimmed(t *testing.T) {
	p, _ := prompterFor("  spaced  \n")

	got, err := p.Ask(Question{Prompt: "Value", Default: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "spaced" {
		t.Errorf("got %q, want the trimmed answer", got)
	}
}

// A required question with no default must not be escapable by pressing
// return, or the installer would carry on with an empty database name.
func TestEmptyAnswerWithNoDefaultAsksAgain(t *testing.T) {
	p, out := prompterFor("\n\nfinally\n")

	got, err := p.Ask(Question{Prompt: "Required"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "finally" {
		t.Errorf("got %q, want the eventual answer", got)
	}
	if strings.Count(out.String(), "A value is required.") != 2 {
		t.Errorf("the operator was not told why the answer was refused: %q", out)
	}
}

func TestAllowEmptyAcceptsNothing(t *testing.T) {
	p, _ := prompterFor("\n")

	got, err := p.Ask(Question{Prompt: "Optional", AllowEmpty: true})
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("got %q, want an empty answer to be accepted", got)
	}
}

// Validation belongs at the question, not after the interview: being told on
// question twelve that question one was wrong is worse than being asked twice.
func TestInvalidAnswerIsAskedAgainImmediately(t *testing.T) {
	p, out := prompterFor("nonsense\nprefer\n")

	got, err := p.Ask(Question{
		Prompt: "SSL mode",
		Validate: func(s string) error {
			if s != "prefer" {
				return fmt.Errorf("%q is not a valid mode", s)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "prefer" {
		t.Errorf("got %q, want the corrected answer", got)
	}
	if !strings.Contains(out.String(), `"nonsense" is not a valid mode`) {
		t.Errorf("the validation message was not shown: %q", out)
	}
}

func TestAskIntRejectsNonNumbers(t *testing.T) {
	p, out := prompterFor("many\n5432\n")

	got, err := p.AskInt(Question{Prompt: "Port", Default: "5432"})
	if err != nil {
		t.Fatal(err)
	}
	if got != 5432 {
		t.Errorf("got %d, want 5432", got)
	}
	if !strings.Contains(out.String(), "not a whole number") {
		t.Errorf("the operator was not told why %q was refused: %q", "many", out)
	}
}

func TestAskBoolAcceptsTheUsualWords(t *testing.T) {
	cases := map[string]bool{
		"y\n": true, "yes\n": true, "Y\n": true, "true\n": true, "1\n": true,
		"n\n": false, "no\n": false, "NO\n": false, "false\n": false, "0\n": false,
	}
	for answer, want := range cases {
		p, _ := prompterFor(answer)
		got, err := p.AskBool("Serve HTTPS", true)
		if err != nil {
			t.Errorf("%q: %v", answer, err)
			continue
		}
		if got != want {
			t.Errorf("%q was read as %v, want %v", answer, got, want)
		}
	}

	p, out := prompterFor("maybe\nyes\n")
	got, err := p.AskBool("Serve HTTPS", true)
	if err != nil || !got {
		t.Errorf("got %v, %v", got, err)
	}
	if !strings.Contains(out.String(), "answer yes or no") {
		t.Errorf("an unparseable answer was not explained: %q", out)
	}
}

func TestAskBoolTakesItsDefault(t *testing.T) {
	for _, def := range []bool{true, false} {
		p, out := prompterFor("\n")
		got, err := p.AskBool("Question", def)
		if err != nil {
			t.Fatal(err)
		}
		if got != def {
			t.Errorf("default %v was read as %v", def, got)
		}
		if !strings.Contains(out.String(), "["+boolWord(def)+"]") {
			t.Errorf("the default was not offered: %q", out)
		}
	}
}

// A secret must not be echoed, and must not have its previous value offered as
// a visible default.
func TestSecretIsNeitherEchoedNorOffered(t *testing.T) {
	out := &bytes.Buffer{}
	p := NewPrompter(strings.NewReader(""), out, false)
	p.readSecret = func(prompt string) (string, error) {
		fmt.Fprint(out, prompt)
		return "hunter2", nil
	}

	got, err := p.Ask(Question{Prompt: "Password", Default: "old-secret", Secret: true})
	if err != nil {
		t.Fatal(err)
	}
	if got != "hunter2" {
		t.Errorf("got %q", got)
	}
	if strings.Contains(out.String(), "hunter2") {
		t.Errorf("the secret was echoed: %q", out)
	}
	if strings.Contains(out.String(), "old-secret") {
		t.Errorf("a previous secret was offered as a visible default: %q", out)
	}
}

// A mistyped password nobody can see is a password nobody can use, and now is
// the only moment it can be caught.
func TestAskSecretTwiceRequiresAMatch(t *testing.T) {
	out := &bytes.Buffer{}
	p := NewPrompter(strings.NewReader(""), out, false)

	entries := []string{"first", "mismatch", "Correct-Horse9", "Correct-Horse9"}
	p.readSecret = func(string) (string, error) {
		next := entries[0]
		entries = entries[1:]
		return next, nil
	}

	got, err := p.AskSecretTwice(Question{Prompt: "New password"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "Correct-Horse9" {
		t.Errorf("got %q", got)
	}
	if !strings.Contains(out.String(), "do not match") {
		t.Errorf("the mismatch was not reported: %q", out)
	}
}

func TestAskSecretTwiceValidatesBeforeConfirming(t *testing.T) {
	out := &bytes.Buffer{}
	p := NewPrompter(strings.NewReader(""), out, false)

	entries := []string{"weak", "Correct-Horse9", "Correct-Horse9"}
	p.readSecret = func(string) (string, error) {
		next := entries[0]
		entries = entries[1:]
		return next, nil
	}

	got, err := p.AskSecretTwice(Question{
		Prompt: "New password",
		Validate: func(s string) error {
			if len(s) < 10 {
				return errors.New("too short")
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "Correct-Horse9" {
		t.Errorf("got %q", got)
	}
	// Confirming a password that was going to be rejected anyway wastes the
	// operator's time, so validation comes first.
	if !strings.Contains(out.String(), "too short") {
		t.Errorf("the weak password was not rejected before confirmation: %q", out)
	}
}

// ---------------------------------------------------------------------------
// Non-interactive mode
// ---------------------------------------------------------------------------

func TestNonInteractiveUsesDefaults(t *testing.T) {
	p := NewPrompter(strings.NewReader(""), &bytes.Buffer{}, true)

	got, err := p.Ask(Question{Prompt: "Database host", Default: "localhost"})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if got != "localhost" {
		t.Errorf("got %q, want the default", got)
	}
}

// The whole point of --non-interactive is that it fails rather than hangs on a
// question nobody is there to answer.
func TestNonInteractiveFailsWithoutADefault(t *testing.T) {
	p := NewPrompter(strings.NewReader(""), &bytes.Buffer{}, true)

	_, err := p.Ask(Question{
		Prompt: "Database root password",
		Flag:   "database-root-user",
		Env:    "DOENER_DATABASE_ROOT_PASSWORD",
	})
	if err == nil {
		t.Fatal("a question with no answer was allowed to pass")
	}
	if !errors.Is(err, ErrNonInteractive) {
		t.Errorf("error is %v, want ErrNonInteractive", err)
	}

	// The operator cannot see the question, so the error has to say how to
	// supply the value instead.
	message := err.Error()
	for _, want := range []string{
		"Database root password",
		"--database-root-user",
		"DOENER_DATABASE_ROOT_PASSWORD",
	} {
		if !strings.Contains(message, want) {
			t.Errorf("the error does not mention %q:\n%s", want, message)
		}
	}
}

func TestNonInteractiveValidatesItsDefaults(t *testing.T) {
	p := NewPrompter(strings.NewReader(""), &bytes.Buffer{}, true)

	_, err := p.Ask(Question{
		Prompt:   "SSL mode",
		Default:  "nonsense",
		Validate: func(string) error { return errors.New("not a valid mode") },
	})
	if err == nil {
		t.Error("an invalid default was accepted unattended")
	}
}

func TestNonInteractiveAsksNothing(t *testing.T) {
	out := &bytes.Buffer{}
	p := NewPrompter(strings.NewReader(""), out, true)

	if _, err := p.Ask(Question{Prompt: "Database host", Default: "localhost"}); err != nil {
		t.Fatal(err)
	}
	p.Say("this should not appear")

	if out.Len() != 0 {
		t.Errorf("something was written to a terminal nobody is watching: %q", out)
	}
}

func TestNonInteractiveSecretTwiceUsesTheDefault(t *testing.T) {
	p := NewPrompter(strings.NewReader(""), &bytes.Buffer{}, true)

	got, err := p.AskSecretTwice(Question{Prompt: "Password", Default: "from-env"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "from-env" {
		t.Errorf("got %q, want the value supplied out of band", got)
	}
}

// Input ending mid-interview must say what to do instead of reporting a bare
// EOF, which is what happens when install is run from a script by mistake.
func TestInputEndingIsExplained(t *testing.T) {
	p, _ := prompterFor("")

	_, err := p.Ask(Question{Prompt: "Database host"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "--non-interactive") {
		t.Errorf("the error does not suggest the unattended mode: %v", err)
	}
}

// A secret prompt has to show that a value is already available.
//
// Reported against the docker compose install: every value there comes from the
// environment, and a prompt reading "Password for postgres:" with no bracket
// looks exactly like a variable that was ignored. It was not -- pressing return
// accepted it -- but nothing on screen said so, and the operator drew the only
// conclusion available.
func TestASecretPromptSaysWhenAValueIsAlreadySet(t *testing.T) {
	var asked []string
	p, _ := prompterFor("\n")
	p.readSecret = func(prompt string) (string, error) {
		asked = append(asked, prompt)
		return "", nil
	}

	answer, err := p.Ask(Question{
		Prompt:  "Password for postgres",
		Secret:  true,
		Default: "from-the-environment",
		Env:     "DOENER_DATABASE_ROOT_PASSWORD",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(asked) != 1 {
		t.Fatalf("asked %d times, want 1", len(asked))
	}
	// It names the variable the operator set, because that is what they will
	// look for when they wonder whether it was read.
	if !strings.Contains(asked[0], "DOENER_DATABASE_ROOT_PASSWORD") {
		t.Errorf("the prompt does not name the variable: %q", asked[0])
	}
	if !strings.Contains(asked[0], "press return") {
		t.Errorf("the prompt does not say return accepts it: %q", asked[0])
	}
	// And the secret itself is never printed.
	if strings.Contains(asked[0], "from-the-environment") {
		t.Errorf("the prompt echoed the secret: %q", asked[0])
	}

	// Pressing return still accepts it, which is the behaviour the message now
	// describes rather than changes.
	if answer != "from-the-environment" {
		t.Errorf("return gave %q, want the default", answer)
	}
}

// With nothing set, the prompt stays as it was: no brackets, no promise.
func TestASecretPromptWithoutAValueIsUnchanged(t *testing.T) {
	var asked []string
	p, _ := prompterFor("typed\n")
	p.readSecret = func(prompt string) (string, error) {
		asked = append(asked, prompt)
		return "typed", nil
	}

	if _, err := p.Ask(Question{Prompt: "Password for postgres", Secret: true}); err != nil {
		t.Fatal(err)
	}
	if asked[0] != "Password for postgres: " {
		t.Errorf("prompt is %q, want the plain one", asked[0])
	}
}
