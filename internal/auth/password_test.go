package auth

import (
	"errors"
	"strings"
	"testing"
)

const goodPassword = "Correct-Horse9"

func TestHashAndVerifyRoundTrip(t *testing.T) {
	encoded, err := Hash(goodPassword)
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}

	if err := Verify(goodPassword, encoded); err != nil {
		t.Errorf("the password did not verify against its own hash: %v", err)
	}
	if err := Verify(goodPassword+"x", encoded); !errors.Is(err, ErrMismatch) {
		t.Errorf("a wrong password produced %v, want ErrMismatch", err)
	}
}

// The salt is what stops two people with the same password having the same
// stored hash.
func TestHashesAreSalted(t *testing.T) {
	first, err := Hash(goodPassword)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Hash(goodPassword)
	if err != nil {
		t.Fatal(err)
	}

	if first == second {
		t.Error("hashing the same password twice produced the same value, so it is unsalted")
	}
	for _, encoded := range []string{first, second} {
		if err := Verify(goodPassword, encoded); err != nil {
			t.Errorf("a salted hash did not verify: %v", err)
		}
	}
}

func TestHashIsPHCFormatWithTheCurrentParameters(t *testing.T) {
	encoded, err := Hash(goodPassword)
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"$argon2id$", "v=19", "m=65536", "t=3", "p=2"} {
		if !strings.Contains(encoded, want) {
			t.Errorf("hash %q does not contain %q", encoded, want)
		}
	}

	// The parameters travel with the hash, which is what allows them to be
	// raised later without invalidating anything.
	if fields := strings.Split(encoded, "$"); len(fields) != 6 {
		t.Errorf("hash %q has %d PHC fields, want 6", encoded, len(fields))
	}
}

// The stored hash must never contain the password.
func TestHashDoesNotContainThePassword(t *testing.T) {
	const password = "Zebra-Crossing42"

	encoded, err := Hash(password)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(encoded, password) {
		t.Errorf("the hash contains the plaintext: %s", encoded)
	}
}

// A password typed on a machine that produces decomposed characters must
// verify against a hash made from the precomposed form, and the other way
// round. Without normalisation the same keystrokes would fail on a different
// operating system.
func TestNormalisationMakesEquivalentPasswordsInterchangeable(t *testing.T) {
	const precomposed = "Pässwort-42" // ä as U+00E4
	const decomposed = "Pässwort-42" // a + U+0308

	if precomposed == decomposed {
		t.Fatal("the test inputs are byte-identical, so it proves nothing")
	}

	encoded, err := Hash(precomposed)
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(decomposed, encoded); err != nil {
		t.Errorf("the decomposed form did not verify: %v", err)
	}

	encoded, err = Hash(decomposed)
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(precomposed, encoded); err != nil {
		t.Errorf("the precomposed form did not verify: %v", err)
	}
}

func TestVerifyRejectsMalformedHashes(t *testing.T) {
	cases := map[string]string{
		"empty":              "",
		"unusable":           UnusablePasswordHash,
		"not PHC":            "hunter2",
		"wrong algorithm":    "$argon2i$v=19$m=65536,t=3,p=2$c2FsdA$a2V5",
		"missing fields":     "$argon2id$v=19$m=65536,t=3,p=2",
		"bad version":        "$argon2id$v=16$m=65536,t=3,p=2$c2FsdA$a2V5",
		"bad base64 salt":    "$argon2id$v=19$m=65536,t=3,p=2$!!!!$a2V5",
		"bad base64 key":     "$argon2id$v=19$m=65536,t=3,p=2$c2FsdA$!!!!",
		"empty salt and key": "$argon2id$v=19$m=65536,t=3,p=2$$",
	}

	for name, encoded := range cases {
		err := Verify(goodPassword, encoded)
		if err == nil {
			t.Errorf("%s: a malformed hash was accepted", name)
			continue
		}
		if errors.Is(err, ErrMismatch) {
			t.Errorf("%s: reported as a wrong password rather than a broken hash", name)
		}
		if !errors.Is(err, ErrMalformedHash) {
			t.Errorf("%s: error is %v, want ErrMalformedHash", name, err)
		}
	}
}

// The placeholder account must be impossible to log into, whatever is tried.
func TestUnusableHashNeverVerifies(t *testing.T) {
	if !IsUnusable(UnusablePasswordHash) {
		t.Error("the unusable hash is not recognised as unusable")
	}
	for _, attempt := range []string{"", "*", goodPassword, "deleted"} {
		if err := Verify(attempt, UnusablePasswordHash); err == nil {
			t.Errorf("the unusable hash accepted %q", attempt)
		}
	}

	real, err := Hash(goodPassword)
	if err != nil {
		t.Fatal(err)
	}
	if IsUnusable(real) {
		t.Error("a real hash was reported as unusable")
	}
}

func TestNeedsRehash(t *testing.T) {
	current, err := Hash(goodPassword)
	if err != nil {
		t.Fatal(err)
	}
	if NeedsRehash(current) {
		t.Error("a freshly created hash wants rehashing")
	}

	// A hash made with weaker parameters must be replaced on the next
	// successful login; that is how the work factor is raised over time.
	weak, err := hashWith(goodPassword, Params{
		Memory: argonMemory / 4, Iterations: 1, Parallelism: 1, KeyLength: argonKeyLength,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !NeedsRehash(weak) {
		t.Error("a weaker hash does not want rehashing")
	}
	// It must still verify, or raising the parameters would lock everyone out.
	if err := Verify(goodPassword, weak); err != nil {
		t.Errorf("a weaker hash no longer verifies: %v", err)
	}

	if !NeedsRehash("not a hash at all") {
		t.Error("an unreadable hash does not want replacing")
	}
}

// Hash applies the complexity rules; HashWithoutValidation deliberately does
// not, and exists only for the placeholder and for tests.
func TestHashEnforcesComplexity(t *testing.T) {
	if _, err := Hash("short"); err == nil {
		t.Error("Hash accepted a password that fails the rules")
	}
	if _, err := HashWithoutValidation("short"); err != nil {
		t.Errorf("HashWithoutValidation rejected a weak password: %v", err)
	}
}

func TestCurrentParamsMatchTheSpecification(t *testing.T) {
	p := CurrentParams()
	if p.Memory != 64*1024 {
		t.Errorf("memory is %d KiB, want 65536", p.Memory)
	}
	if p.Iterations != 3 {
		t.Errorf("iterations is %d, want 3", p.Iterations)
	}
	if p.Parallelism != 2 {
		t.Errorf("parallelism is %d, want 2", p.Parallelism)
	}
	if p.KeyLength != 32 {
		t.Errorf("key length is %d, want 32", p.KeyLength)
	}
	if argonSaltLength != 16 {
		t.Errorf("salt length is %d, want 16", argonSaltLength)
	}
}
