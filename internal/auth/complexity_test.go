package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestLengthLimits(t *testing.T) {
	// Nine characters meeting every class is still too short: length is not
	// traded against complexity.
	if err := ValidateComplexity("Ab3!efghi"); err == nil {
		t.Error("a 9-character password was accepted")
	}
	if err := ValidateComplexity("Ab3!efghij"); err != nil {
		t.Errorf("a 10-character password was rejected: %v", err)
	}

	long := "Aa1!" + strings.Repeat("x", MaxPasswordLength-4)
	if err := ValidateComplexity(long); err != nil {
		t.Errorf("a %d-character password was rejected: %v", MaxPasswordLength, err)
	}
	if err := ValidateComplexity(long + "x"); err == nil {
		t.Errorf("a %d-character password was accepted", MaxPasswordLength+1)
	}
}

// Length is counted in code points, so a passphrase in a non-Latin script is
// not penalised for the bytes it happens to weigh.
func TestLengthIsCountedInCodePointsNotBytes(t *testing.T) {
	// Ten Cyrillic-and-friends code points, well over ten bytes. Classes:
	// extended, digit and special.
	password := "Пароль123!"
	if got := len(password); got <= MinPasswordLength {
		t.Fatalf("the test input is %d bytes, which does not prove the point", got)
	}
	if err := ValidateComplexity(password); err != nil {
		t.Errorf("a 10-code-point password was rejected: %v", err)
	}
}

func TestThreeOfFiveClasses(t *testing.T) {
	accepted := map[string]string{
		"upper, lower, digit":    "Passwordis1",
		"lower, digit, special":  "password1!!",
		"upper, lower, special":  "PasswordAb!",
		"upper, lower, extended": "PasswordÄbc",
		"lower, digit, extended": "passwort1ä9",
		"all five":               "Passwort1!ä",
		"passphrase":             "correct horse battery 9!",
	}
	for name, password := range accepted {
		if err := ValidateComplexity(password); err != nil {
			t.Errorf("%s (%q) was rejected: %v", name, password, err)
		}
	}

	rejected := map[string]string{
		"lower only":        "passwordonly",
		"lower and digit":   "password1234",
		"upper and lower":   "PasswordOnly",
		"digits only":       "1234567890",
		"upper and digit":   "PASSWORD1234",
		"spaces do not add": "password words",
	}
	for name, password := range rejected {
		err := ValidateComplexity(password)
		if err == nil {
			t.Errorf("%s (%q) was accepted with fewer than %d classes",
				name, password, RequiredClasses)
		}
	}
}

func TestEveryClassIsRecognised(t *testing.T) {
	cases := map[Class]string{
		ClassUpper:    "A",
		ClassLower:    "a",
		ClassDigit:    "1",
		ClassSpecial:  "!",
		ClassExtended: "ä",
	}
	for class, sample := range cases {
		got := SatisfiedClasses(sample)
		if len(got) != 1 || got[0] != class {
			t.Errorf("%q satisfied %v, want exactly %v", sample, got, class)
		}
	}
}

// Every character the specification lists must actually count as special. A
// silent omission would reject a password the rules say is fine.
func TestEverySpecifiedSpecialCharacterCounts(t *testing.T) {
	for _, r := range SpecialCharacters {
		classes := SatisfiedClasses(string(r))
		if len(classes) != 1 || classes[0] != ClassSpecial {
			t.Errorf("%q satisfied %v, want ClassSpecial", string(r), classes)
		}
	}

	const specified = `<>|-_.:,;#'!"§$%&/()[]{}?@`
	if SpecialCharacters != specified {
		t.Errorf("SpecialCharacters is %q, want %q", SpecialCharacters, specified)
	}
}

// Class 5 is defined by exclusion, so it has to admit alphabets nobody thought
// to enumerate, not just the German examples in the specification.
func TestExtendedClassIsDefinedByExclusion(t *testing.T) {
	extended := []string{
		"ä", "ö", "ü", "Ä", "Ö", "Ü", "ß",
		"á", "à", "â", "é", "è", "ê", "í", "ì", "î", "ó", "ò", "ô", "ú", "ù", "û",
		"ñ", "ç", "ø", "å", "æ",
		"€", "£", "¥", "¢",
		"π", "Ω", "ж", "李", "あ",
		"+", "=", "~", "^", "*", "`",
	}
	for _, sample := range extended {
		classes := SatisfiedClasses(sample)
		if len(classes) != 1 || classes[0] != ClassExtended {
			t.Errorf("%q satisfied %v, want ClassExtended", sample, classes)
		}
	}
}

// A space is legal inside a passphrase but must not earn a class on its own,
// or "hello world there" would pass on a technicality.
func TestSpaceSatisfiesNothing(t *testing.T) {
	if classes := SatisfiedClasses(" "); len(classes) != 0 {
		t.Errorf("a space satisfied %v, want nothing", classes)
	}
	if err := ValidateComplexity("password words"); err == nil {
		t.Error("a two-class passphrase padded with spaces was accepted")
	}
	if err := ValidateComplexity("Correct horse 9"); err != nil {
		t.Errorf("a genuine three-class passphrase was rejected: %v", err)
	}
}

// A decomposed accented character must count as class 5, not as a plain letter
// plus a combining mark, or the same password would score differently
// depending on the keyboard that produced it.
func TestDecomposedCharactersCountAsExtended(t *testing.T) {
	// These two must differ in bytes while meaning the same thing. The guard
	// below catches a formatter silently normalising one into the other.
	const precomposed = "passwörter" // ö as one code point
	const decomposed = "passwörter" // o followed by a combining diaeresis

	if decomposed == precomposed {
		t.Fatal("the test inputs are byte-identical, so it proves nothing")
	}

	for _, sample := range []string{decomposed, precomposed} {
		classes := SatisfiedClasses(sample)
		if !containsClass(classes, ClassExtended) {
			t.Errorf("%q did not satisfy ClassExtended, got %v", sample, classes)
		}
	}
}

func TestSatisfiedClassesIsStablyOrdered(t *testing.T) {
	got := SatisfiedClasses("aA1!ä")
	want := AllClasses()

	if len(got) != len(want) {
		t.Fatalf("got %v, want all five classes", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("classes are out of order: %v, want %v", got, want)
		}
	}
}

// The error has to tell someone what to change, which means naming the classes
// and which ones they already have.
func TestComplexityErrorIsActionable(t *testing.T) {
	err := ValidateComplexity("password1234")
	if err == nil {
		t.Fatal("expected an error")
	}

	var complexity *ComplexityError
	if !errors.As(err, &complexity) {
		t.Fatalf("error is %T, want *ComplexityError", err)
	}
	if len(complexity.Satisfied) != 2 {
		t.Errorf("reported %v as satisfied, want lower and digit", complexity.Satisfied)
	}

	message := err.Error()
	for _, class := range AllClasses() {
		if !strings.Contains(message, class.Description()) {
			t.Errorf("the message does not describe %v:\n%s", class, message)
		}
	}
	// Satisfied classes are ticked, so the message shows progress.
	if strings.Count(message, "[x]") != 2 {
		t.Errorf("the message does not mark the two satisfied classes:\n%s", message)
	}
}

func TestLengthErrorsSayWhichWay(t *testing.T) {
	short := ValidateComplexity("Ab3!e")
	if short == nil || !strings.Contains(short.Error(), "minimum") {
		t.Errorf("a short password did not report a minimum: %v", short)
	}

	long := ValidateComplexity("Aa1!" + strings.Repeat("x", MaxPasswordLength))
	if long == nil || !strings.Contains(long.Error(), "maximum") {
		t.Errorf("a long password did not report a maximum: %v", long)
	}
}

// The error must never quote the password back.
func TestComplexityErrorDoesNotEchoThePassword(t *testing.T) {
	const password = "onlylowercaseletters"

	err := ValidateComplexity(password)
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), password) {
		t.Errorf("the error echoed the password: %s", err)
	}
}
