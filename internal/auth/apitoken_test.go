package auth_test

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/joernott/doenerstag/internal/auth"
)

func TestGeneratedTokenHasTheDocumentedShape(t *testing.T) {
	token, err := auth.GenerateAPIToken()
	if err != nil {
		t.Fatalf("generating: %v", err)
	}

	raw, err := base64.RawURLEncoding.DecodeString(token.Value)
	if err != nil {
		t.Fatalf("the value is not base64url: %v", err)
	}
	if len(raw) != auth.APITokenBytes {
		t.Errorf("the value carries %d bytes, want %d", len(raw), auth.APITokenBytes)
	}
	if !strings.HasPrefix(token.Value, token.Prefix) {
		t.Errorf("prefix %q is not a prefix of the value", token.Prefix)
	}
	if len(token.Prefix) != auth.APITokenPrefixLength {
		t.Errorf("prefix is %d characters, want %d",
			len(token.Prefix), auth.APITokenPrefixLength)
	}
	if token.Hash != auth.HashAPIToken(token.Value) {
		t.Error("the stored hash is not the hash of the value")
	}
	// The stored prefix must not be enough to reconstruct anything: assert it
	// is a small fraction of the whole rather than, say, half.
	if len(token.Prefix)*3 > len(token.Value) {
		t.Errorf("the prefix is %d of %d characters, which is too much of the token",
			len(token.Prefix), len(token.Value))
	}
}

func TestTwoTokensDiffer(t *testing.T) {
	seen := make(map[string]bool, 64)
	for range 64 {
		token, err := auth.GenerateAPIToken()
		if err != nil {
			t.Fatal(err)
		}
		if seen[token.Value] {
			t.Fatal("two generated tokens were identical")
		}
		seen[token.Value] = true
	}
}

// The hash must be the only thing that could be stored: it must not be
// reversible by construction, and the same value must always hash the same, or
// lookup by hash could not work.
func TestHashIsStableAndNotTheValue(t *testing.T) {
	const value = "PJvNQrz3EXAMPLEtokenvalue0000000000000000000"
	first := auth.HashAPIToken(value)
	if first != auth.HashAPIToken(value) {
		t.Error("hashing the same value twice gave different answers")
	}
	if strings.Contains(first, value) || first == value {
		t.Error("the hash contains the token")
	}
	if auth.HashAPIToken(value) == auth.HashAPIToken(value+"x") {
		t.Error("two different values hashed the same")
	}
}

func TestParseBearerAcceptsAGeneratedToken(t *testing.T) {
	token, err := auth.GenerateAPIToken()
	if err != nil {
		t.Fatal(err)
	}

	// The scheme is case-insensitive per RFC 7235, so all of these are the
	// same request.
	for _, header := range []string{
		"Bearer " + token.Value,
		"bearer " + token.Value,
		"BEARER " + token.Value,
		"Bearer  " + token.Value + " ",
	} {
		got, err := auth.ParseBearer(header)
		if err != nil {
			t.Errorf("%q: %v", header, err)
			continue
		}
		if got != token.Value {
			t.Errorf("%q gave %q, want the token", header, got)
		}
	}
}

func TestParseBearerRejectsAnythingElse(t *testing.T) {
	token, err := auth.GenerateAPIToken()
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]string{
		"empty":            "",
		"scheme only":      "Bearer",
		"scheme and space": "Bearer ",
		"another scheme":   "Basic " + token.Value,
		"no scheme":        token.Value,
		"too short":        "Bearer abc",
		"not base64url":    "Bearer " + strings.Repeat("!", len(token.Value)),
		"right length, base64 with padding": "Bearer " +
			strings.Repeat("A", len(token.Value)-1) + "=",
	}

	for name, header := range cases {
		if _, err := auth.ParseBearer(header); !errors.Is(err, auth.ErrMalformedToken) {
			t.Errorf("%s: got %v, want ErrMalformedToken", name, err)
		}
	}
}
