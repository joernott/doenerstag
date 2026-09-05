package logging

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestIsSensitiveCoversTheDenyList(t *testing.T) {
	sensitive := []string{
		"password",
		"Password",
		"PASSWORD",
		"password_hash",
		"database_password",
		"DOENER_DATABASE_ROOT_PASSWORD",
		"jwt_secret",
		"Secret",
		"token",
		"api_token",
		"X-CSRF-Token",
		"Authorization",
		"Cookie",
		"Set-Cookie",
		"csrf",
		"doener_csrf",
		"credentials",
		"private_key",
		"apiKey",
	}
	for _, name := range sensitive {
		if !IsSensitive(name) {
			t.Errorf("IsSensitive(%q) = false, want true", name)
		}
	}

	harmless := []string{
		"user", "name", "email", "restaurant_id", "quantity",
		"price_cents", "deadline_at", "request_id", "path", "status",
	}
	for _, name := range harmless {
		if IsSensitive(name) {
			t.Errorf("IsSensitive(%q) = true, want false", name)
		}
	}
}

func TestRedactMapLeavesInputUnchanged(t *testing.T) {
	in := map[string]any{"user": "anna", "password": "hunter2"}
	out := RedactMap(in)

	if out["password"] != Redacted {
		t.Errorf("password = %v, want %s", out["password"], Redacted)
	}
	if out["user"] != "anna" {
		t.Errorf("user = %v, want anna", out["user"])
	}
	if in["password"] != "hunter2" {
		t.Error("RedactMap modified its input")
	}
}

type credentials struct {
	User       string
	Password   string
	Token      string `log:"api_token"`
	Internal   string `log:"-"`
	Note       string `log:"note"`
	unexported string //nolint:unused // present to prove it is skipped
}

func TestRedactStructHidesAndRenamesFields(t *testing.T) {
	got := RedactStruct(credentials{
		User:       "anna",
		Password:   "hunter2",
		Token:      "abc123",
		Internal:   "must not appear",
		Note:       "visible",
		unexported: "must not appear",
	})

	if got["User"] != "anna" {
		t.Errorf("User = %v, want anna", got["User"])
	}
	if got["Password"] != Redacted {
		t.Errorf("Password = %v, want %s", got["Password"], Redacted)
	}
	// Renamed by the tag to api_token, which is itself sensitive.
	if got["api_token"] != Redacted {
		t.Errorf("api_token = %v, want %s", got["api_token"], Redacted)
	}
	if _, present := got["Token"]; present {
		t.Error("Token should have been renamed to api_token")
	}
	if _, present := got["Internal"]; present {
		t.Error(`field tagged log:"-" leaked into the output`)
	}
	if got["note"] != "visible" {
		t.Errorf("note = %v, want visible", got["note"])
	}
	if _, present := got["unexported"]; present {
		t.Error("unexported field leaked into the output")
	}
}

func TestRedactStructHandlesPointersAndNil(t *testing.T) {
	if got := RedactStruct(nil); got != nil {
		t.Errorf("RedactStruct(nil) = %v, want nil", got)
	}

	var nilPtr *credentials
	if got := RedactStruct(nilPtr); got != nil {
		t.Errorf("RedactStruct(nil pointer) = %v, want nil", got)
	}

	got := RedactStruct(&credentials{User: "bo", Password: "x"})
	if got["User"] != "bo" || got["Password"] != Redacted {
		t.Errorf("pointer struct not handled: %v", got)
	}
}

func TestRedactStructOnNonStruct(t *testing.T) {
	got := RedactStruct(42)
	if got["value"] != 42 {
		t.Errorf("RedactStruct(42) = %v, want map with value 42", got)
	}
}

// The point of the deny-list is that a secret never reaches the output stream.
// This asserts on the bytes actually written rather than on the helper.
func TestSecretsNeverReachTheOutput(t *testing.T) {
	var buf bytes.Buffer
	logger, err := New(Options{Level: LevelDebug, Output: &buf})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	FuncCall(logger.Zerolog(), "auth.Login", map[string]any{
		"user":        "anna",
		"password":    "hunter2",
		"jwt_secret":  "s3cr3t",
		"csrf_token":  "deadbeef",
		"remote_addr": "10.0.0.5",
	})

	output := buf.String()
	for _, secret := range []string{"hunter2", "s3cr3t", "deadbeef"} {
		if strings.Contains(output, secret) {
			t.Errorf("secret %q appeared in the log output: %s", secret, output)
		}
	}
	for _, expected := range []string{"anna", "10.0.0.5", "auth.Login"} {
		if !strings.Contains(output, expected) {
			t.Errorf("expected %q in the log output: %s", expected, output)
		}
	}
	if strings.Count(output, Redacted) != 3 {
		t.Errorf("expected three redactions, got: %s", output)
	}
}

func TestFuncCallIsSilentBelowDebug(t *testing.T) {
	var buf bytes.Buffer
	logger, err := New(Options{Level: LevelInfo, Output: &buf})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	FuncCall(logger.Zerolog(), "auth.Login", map[string]any{"user": "anna"})

	if buf.Len() != 0 {
		t.Errorf("FuncCall emitted at INFO level: %s", buf.String())
	}
}

func TestFieldNamesReturnsSortedNamesWithoutValues(t *testing.T) {
	names := FieldNames(map[string]any{
		"password": "hunter2",
		"user":     "anna",
		"email":    "a@example.invalid",
	})

	want := []string{"email", "password", "user"}
	if len(names) != len(want) {
		t.Fatalf("FieldNames returned %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("FieldNames returned %v, want %v", names, want)
		}
	}

	if FieldNames(nil) != nil {
		t.Error("FieldNames(nil) should be nil")
	}
}

func TestAddFieldsProducesValidJSON(t *testing.T) {
	var buf bytes.Buffer
	logger, err := New(Options{Level: LevelInfo, Output: &buf})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	AddFields(logger.Zerolog().Info(), map[string]any{
		"user":     "anna",
		"password": "hunter2",
		"count":    3,
	}).Msg("test")

	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v (%s)", err, buf.String())
	}
	if decoded["password"] != Redacted {
		t.Errorf("password = %v, want %s", decoded["password"], Redacted)
	}
	if decoded["count"] != float64(3) {
		t.Errorf("count = %v, want 3", decoded["count"])
	}
}
