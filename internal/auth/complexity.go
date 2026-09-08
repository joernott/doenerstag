package auth

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Password length limits, counted in code points rather than bytes so that a
// passphrase in a non-Latin script is not penalised.
const (
	MinPasswordLength = 10
	MaxPasswordLength = 256
)

// RequiredClasses is how many of the character classes a password must satisfy.
const RequiredClasses = 3

// SpecialCharacters is the set for class 4, exactly as specified in
// docs/05_auth_and_permissions.md.
const SpecialCharacters = `<>|-_.:,;#'!"§$%&/()[]{}?@`

// Class is one of the five character classes a password can satisfy.
type Class int

// The five classes.
const (
	ClassUpper Class = iota
	ClassLower
	ClassDigit
	ClassSpecial
	ClassExtended
)

// Description names a class in the way an error message should.
func (c Class) Description() string {
	switch c {
	case ClassUpper:
		return "an upper case letter"
	case ClassLower:
		return "a lower case letter"
	case ClassDigit:
		return "a digit"
	case ClassSpecial:
		return "a special character (" + SpecialCharacters + ")"
	case ClassExtended:
		return "a language-specific character or currency symbol, for example ä, é, ß or €"
	default:
		return "an unknown class"
	}
}

// AllClasses lists the classes in the order the specification gives them.
func AllClasses() []Class {
	return []Class{ClassUpper, ClassLower, ClassDigit, ClassSpecial, ClassExtended}
}

// ComplexityError reports a password that does not meet the rules. It carries
// which classes were satisfied so the frontend can show progress rather than
// only a verdict.
type ComplexityError struct {
	// TooShort and TooLong are set for a length failure.
	TooShort bool
	TooLong  bool
	Length   int

	// Satisfied lists the classes the password does meet.
	Satisfied []Class
}

func (e *ComplexityError) Error() string {
	switch {
	case e.TooShort:
		return fmt.Sprintf("password is %d characters, the minimum is %d",
			e.Length, MinPasswordLength)
	case e.TooLong:
		return fmt.Sprintf("password is %d characters, the maximum is %d",
			e.Length, MaxPasswordLength)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "password meets %d of the required %d character classes. It must contain at least %d of:",
		len(e.Satisfied), RequiredClasses, RequiredClasses)
	for _, class := range AllClasses() {
		mark := " "
		if containsClass(e.Satisfied, class) {
			mark = "x"
		}
		fmt.Fprintf(&b, "\n  [%s] %s", mark, class.Description())
	}
	return b.String()
}

// ValidateComplexity applies the rules from docs/05_auth_and_permissions.md: a
// length between MinPasswordLength and MaxPasswordLength, and at least
// RequiredClasses of the five character classes.
func ValidateComplexity(password string) error {
	normalised := Normalise(password)

	// Code points, not bytes: a 10-character password in Cyrillic is 10
	// characters, whatever it weighs.
	length := utf8.RuneCountInString(normalised)
	switch {
	case length < MinPasswordLength:
		return &ComplexityError{TooShort: true, Length: length}
	case length > MaxPasswordLength:
		return &ComplexityError{TooLong: true, Length: length}
	}

	satisfied := SatisfiedClasses(normalised)
	if len(satisfied) < RequiredClasses {
		return &ComplexityError{Satisfied: satisfied}
	}
	return nil
}

// SatisfiedClasses reports which character classes a password contains.
//
// It normalises first, so a decomposed "ä" counts as class 5 rather than as a
// bare "a" plus a combining mark.
func SatisfiedClasses(password string) []Class {
	normalised := Normalise(password)

	var upper, lower, digit, special, extended bool
	for _, r := range normalised {
		switch {
		case r >= 'A' && r <= 'Z':
			upper = true
		case r >= 'a' && r <= 'z':
			lower = true
		case r >= '0' && r <= '9':
			digit = true
		case strings.ContainsRune(SpecialCharacters, r):
			special = true
		case isExtended(r):
			extended = true
		}
	}

	var out []Class
	for class, ok := range map[Class]bool{
		ClassUpper:    upper,
		ClassLower:    lower,
		ClassDigit:    digit,
		ClassSpecial:  special,
		ClassExtended: extended,
	} {
		if ok {
			out = append(out, class)
		}
	}

	// Stable order, so an error message and a progress indicator agree.
	ordered := make([]Class, 0, len(out))
	for _, class := range AllClasses() {
		if containsClass(out, class) {
			ordered = append(ordered, class)
		}
	}
	return ordered
}

// isExtended reports whether a rune satisfies class 5.
//
// The class is defined by exclusion rather than by a list. Enumerating accented
// characters would leave out somebody's alphabet: the rule is "a printable
// character none of the first four classes covers", which admits every
// alphabet, every currency symbol and every punctuation mark not in
// SpecialCharacters.
//
// A plain space satisfies nothing on its own, so that "hello world" does not
// pass a class it has not earned, but it remains legal inside a passphrase.
func isExtended(r rune) bool {
	if r == ' ' {
		return false
	}
	if !unicode.IsPrint(r) {
		return false
	}
	// Anything the first four classes already cover is not class 5.
	if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
		return false
	}
	return !strings.ContainsRune(SpecialCharacters, r)
}

func containsClass(classes []Class, want Class) bool {
	for _, c := range classes {
		if c == want {
			return true
		}
	}
	return false
}
