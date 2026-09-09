// Package install provisions a doenerstag database and writes a configuration
// file. It is what the install and update verbs are made of.
package install

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"golang.org/x/term"
)

// ErrNonInteractive reports a question that had to be asked while running with
// --non-interactive and no value to fall back on.
//
// It names the flag and the environment variable that would have supplied the
// answer, because the operator running unattended cannot see the question.
var ErrNonInteractive = errors.New("a value is required but cannot be asked for")

// MissingValueError is the concrete form of ErrNonInteractive.
type MissingValueError struct {
	// Question is the prompt that would have been shown.
	Question string
	// Flag and Env name the two ways to supply the value without being asked.
	Flag string
	Env  string
}

func (e *MissingValueError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %s", ErrNonInteractive, e.Question)
	switch {
	case e.Flag != "" && e.Env != "":
		fmt.Fprintf(&b, "\nSupply it with --%s or %s.", e.Flag, e.Env)
	case e.Env != "":
		fmt.Fprintf(&b, "\nSupply it with %s.", e.Env)
	case e.Flag != "":
		fmt.Fprintf(&b, "\nSupply it with --%s.", e.Flag)
	}
	return b.String()
}

// Is makes errors.Is(err, ErrNonInteractive) match, so a caller can react to
// "unattended and unanswerable" without knowing this type.
func (e *MissingValueError) Is(target error) bool { return target == ErrNonInteractive }

// Prompter asks the operator questions, or refuses to when running unattended.
type Prompter struct {
	in  *bufio.Reader
	out io.Writer

	// NonInteractive makes every question fall back to its default, and fail
	// when there is none.
	NonInteractive bool

	// readSecret reads a line without echoing it. Tests replace it; the
	// application leaves it nil, which means the terminal.
	readSecret func(prompt string) (string, error)
}

// NewPrompter builds a Prompter reading from in and writing to out.
func NewPrompter(in io.Reader, out io.Writer, nonInteractive bool) *Prompter {
	return &Prompter{
		in:             bufio.NewReader(in),
		out:            out,
		NonInteractive: nonInteractive,
	}
}

// Question describes one thing to ask for.
type Question struct {
	// Prompt is the text shown, without the default or the colon.
	Prompt string

	// Default is offered in brackets and used when the answer is empty. An
	// existing configuration file supplies these, which is what makes re-running
	// install a matter of pressing return.
	Default string

	// Flag and Env name the non-interactive ways to supply the value. They
	// appear only in the error when the question cannot be asked.
	Flag string
	Env  string

	// Validate rejects an answer and explains why. The question is asked again.
	Validate func(string) error

	// Secret suppresses echo and omits the default from the prompt.
	Secret bool

	// AllowEmpty permits an empty answer when there is no default.
	AllowEmpty bool
}

// Ask puts one question and returns the answer.
//
// An empty answer takes the default. A rejected answer is asked again, which is
// why validation belongs here rather than after the whole interview: being told
// on question three that question one was wrong is worse than being asked
// twice.
func (p *Prompter) Ask(q Question) (string, error) {
	if p.NonInteractive {
		return p.unattended(q)
	}

	for {
		answer, err := p.readAnswer(q)
		if err != nil {
			return "", err
		}

		if answer == "" {
			answer = q.Default
		}
		if answer == "" && !q.AllowEmpty {
			p.printf("A value is required.\n")
			continue
		}

		if q.Validate != nil {
			if err := q.Validate(answer); err != nil {
				p.printf("%v\n", err)
				continue
			}
		}
		return answer, nil
	}
}

// unattended answers a question from its default, or reports that it cannot.
func (p *Prompter) unattended(q Question) (string, error) {
	if q.Default == "" && !q.AllowEmpty {
		return "", &MissingValueError{Question: q.Prompt, Flag: q.Flag, Env: q.Env}
	}
	if q.Validate != nil {
		if err := q.Validate(q.Default); err != nil {
			return "", fmt.Errorf("%s: %w", q.Prompt, err)
		}
	}
	return q.Default, nil
}

func (p *Prompter) readAnswer(q Question) (string, error) {
	if q.Secret {
		// A secret cannot be shown in brackets the way an ordinary default is,
		// but the fact that there *is* one has to be visible: without this the
		// prompt looks like a demand for a value the operator has already
		// supplied through the environment, and the natural conclusion is that
		// the variable was ignored. It was not -- pressing return accepts it.
		// Reported against the docker compose install, where every value comes
		// from the environment and every secret prompt looked like a refusal.
		if q.Default != "" {
			return p.secret(fmt.Sprintf("%s [%s; press return to accept]: ", q.Prompt, q.suppliedBy()))
		}
		return p.secret(q.Prompt + ": ")
	}

	if q.Default != "" {
		p.printf("%s [%s]: ", q.Prompt, q.Default)
	} else {
		p.printf("%s: ", q.Prompt)
	}

	line, err := p.in.ReadString('\n')
	if err != nil && (err != io.EOF || line == "") {
		if errors.Is(err, io.EOF) {
			return "", fmt.Errorf("input ended while asking %q; "+
				"use --non-interactive with flags or environment variables instead", q.Prompt)
		}
		return "", fmt.Errorf("reading the answer to %q: %w", q.Prompt, err)
	}
	return strings.TrimSpace(line), nil
}

func (p *Prompter) secret(prompt string) (string, error) {
	if p.readSecret != nil {
		return p.readSecret(prompt)
	}

	p.printf("%s", prompt)

	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		// Piped input cannot have its echo suppressed. Reading the line anyway
		// is better than failing, but the operator should know the value was
		// visible.
		line, err := p.in.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		p.printf("(input is not a terminal; the value above was echoed)\n")
		return strings.TrimSpace(line), nil
	}

	raw, err := term.ReadPassword(fd)
	p.printf("\n")
	if err != nil {
		return "", fmt.Errorf("reading a password: %w", err)
	}
	return strings.TrimSpace(string(raw)), nil
}

// AskSecretTwice asks for a new secret and makes the operator repeat it.
//
// A mistyped password that nobody sees is a password nobody can use, and the
// only moment it can be caught is now.
func (p *Prompter) AskSecretTwice(q Question) (string, error) {
	if p.NonInteractive {
		return p.unattended(q)
	}

	for {
		first, err := p.Ask(Question{
			Prompt: q.Prompt, Secret: true, Flag: q.Flag, Env: q.Env,
			Validate: q.Validate, AllowEmpty: q.AllowEmpty,
		})
		if err != nil {
			return "", err
		}

		second, err := p.Ask(Question{
			Prompt: q.Prompt + " (again)", Secret: true, AllowEmpty: true,
		})
		if err != nil {
			return "", err
		}

		if first == second {
			return first, nil
		}
		p.printf("The two entries do not match. Try again.\n")
	}
}

// AskInt asks for a whole number.
func (p *Prompter) AskInt(q Question) (int, error) {
	numeric := func(s string) error {
		if _, err := strconv.Atoi(s); err != nil {
			return fmt.Errorf("%q is not a whole number", s)
		}
		if q.Validate != nil {
			return q.Validate(s)
		}
		return nil
	}

	answer, err := p.Ask(Question{
		Prompt: q.Prompt, Default: q.Default, Flag: q.Flag, Env: q.Env,
		Validate: numeric,
	})
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(answer)
}

// AskBool asks a yes or no question.
func (p *Prompter) AskBool(prompt string, def bool) (bool, error) {
	answer, err := p.Ask(Question{
		Prompt:  prompt,
		Default: boolWord(def),
		Validate: func(s string) error {
			if _, ok := parseBoolWord(s); !ok {
				return fmt.Errorf("answer yes or no, not %q", s)
			}
			return nil
		},
	})
	if err != nil {
		return false, err
	}
	value, _ := parseBoolWord(answer)
	return value, nil
}

// Say writes a line of explanation to the operator. It is silent in
// non-interactive mode, where there is nobody reading.
func (p *Prompter) Say(format string, args ...any) {
	if p.NonInteractive {
		return
	}
	p.printf(format+"\n", args...)
}

func (p *Prompter) printf(format string, args ...any) {
	if p.out == nil {
		return
	}
	// Nothing useful can be done if the terminal has gone away mid-interview,
	// and failing the install over it would be worse than carrying on.
	_, _ = fmt.Fprintf(p.out, format, args...)
}

func boolWord(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// parseBoolWord reads the usual words for yes and no, reporting whether the
// answer was understood at all.
func parseBoolWord(s string) (value, ok bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "y", "yes", "true", "1":
		return true, true
	case "n", "no", "false", "0":
		return false, true
	default:
		return false, false
	}
}

// suppliedBy names where a value already came from, for a prompt that cannot
// show the value itself.
//
// The environment variable if the question has one, because that is what the
// operator set and what they will look for; otherwise a plain statement that
// something is there, which is the case for a value read out of an existing
// configuration file.
func (q Question) suppliedBy() string {
	if q.Env != "" {
		return "set from " + q.Env
	}
	return "a value is already set"
}
