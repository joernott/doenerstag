package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The registry is what the frontend keys its translations on, so it has to
// agree with docs/04_api.md exactly. Parsing the specification rather than
// restating it here means the two cannot drift apart silently.
func TestRegistryMatchesTheSpecification(t *testing.T) {
	documented := documentedCodes(t)

	if len(documented) == 0 {
		t.Fatal("no error codes were found in docs/04_api.md")
	}

	for code, status := range documented {
		if !code.Registered() {
			t.Errorf("code %d is documented but not registered", code)
			continue
		}
		if got := code.Status(); got != status {
			t.Errorf("code %d is registered as HTTP %d but documented as %d",
				code, got, status)
		}
	}

	for _, code := range Codes() {
		if _, ok := documented[code]; !ok {
			t.Errorf("code %d is registered but not documented in docs/04_api.md", code)
		}
	}
}

// documentedCodes reads the error table out of the specification.
func documentedCodes(t *testing.T) map[Code]int {
	t.Helper()

	body, err := os.ReadFile("../../docs/04_api.md")
	if err != nil {
		t.Fatalf("reading the API specification: %v", err)
	}

	// | 1003 | 400  | Deadline is not before the fulfilment time. |
	row := regexp.MustCompile(`^\|\s*(\d{4})\s*\|\s*(\d{3})\s*\|`)

	out := map[Code]int{}
	for _, line := range strings.Split(string(body), "\n") {
		match := row.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		code, _ := strconv.Atoi(match[1])
		status, _ := strconv.Atoi(match[2])
		out[Code(code)] = status
	}
	return out
}

// A code's range says what kind of failure it is, and the documented ranges map
// onto HTTP status families. A 2xxx code answering 500 would be a defect.
func TestCodeRangesMatchTheirStatusFamilies(t *testing.T) {
	for _, code := range Codes() {
		status := code.Status()

		switch {
		case code >= 1000 && code < 2000:
			if status < 400 || status >= 500 {
				t.Errorf("validation code %d answers %d, want a 4xx", code, status)
			}
		case code >= 2000 && code < 3000:
			// Authentication, except CSRF which is documented as a 403.
			if status != http.StatusUnauthorized && status != http.StatusForbidden {
				t.Errorf("authentication code %d answers %d, want 401 or 403", code, status)
			}
		case code >= 3000 && code < 4000:
			if status != http.StatusForbidden {
				t.Errorf("authorization code %d answers %d, want 403", code, status)
			}
		case code >= 4000 && code < 5000:
			if status != http.StatusNotFound && status != http.StatusMethodNotAllowed &&
				status != http.StatusConflict && status != http.StatusGone {
				t.Errorf("resource code %d answers %d, want 404, 405, 409 or 410", code, status)
			}
		case code >= 5000 && code < 6000:
			if status != http.StatusTooManyRequests {
				t.Errorf("rate limit code %d answers %d, want 429", code, status)
			}
		case code >= 9000:
			if status < 500 {
				t.Errorf("server code %d answers %d, want a 5xx", code, status)
			}
		default:
			t.Errorf("code %d falls outside every documented range", code)
		}
	}
}

// An unregistered code must not produce a success. A missing entry is a
// programming error and should look like one.
func TestUnregisteredCodeIsAnInternalError(t *testing.T) {
	unknown := Code(7777)

	if unknown.Registered() {
		t.Fatal("the test picked a code that is actually registered")
	}
	if got := unknown.Status(); got != http.StatusInternalServerError {
		t.Errorf("an unregistered code answers %d, want 500", got)
	}
}

func TestErrorEnvelopeShape(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", http.NoBody)

	WriteError(rec, req, FieldError(CodeDeadlineAfterFulfil, "deadline_at",
		"deadline must be before the fulfilment time"))

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status %d, want 400", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content type is %q", ct)
	}

	var decoded map[string]map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}

	body, ok := decoded["error"]
	if !ok {
		t.Fatalf("the response has no error object: %s", rec.Body)
	}
	if body["code"] != float64(CodeDeadlineAfterFulfil) {
		t.Errorf("code is %v", body["code"])
	}
	if body["field"] != "deadline_at" {
		t.Errorf("field is %v, want deadline_at", body["field"])
	}
	if body["message"] == "" {
		t.Error("the envelope carries no message")
	}
	if _, present := body["request_id"]; !present {
		t.Error("the envelope carries no request_id")
	}
}

// field is omitted rather than sent empty, so a client can tell "no particular
// field" from "a field named empty string".
func TestFieldIsOmittedWhenAbsent(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteError(rec, httptest.NewRequest(http.MethodGet, "/x", http.NoBody),
		&Error{Code: CodeNotFound})

	if strings.Contains(rec.Body.String(), `"field"`) {
		t.Errorf("an empty field was sent: %s", rec.Body)
	}
}

// The cause is for the log. Sending it would be how SQL text and file paths
// reach a client.
func TestTheWrappedCauseIsNeverSent(t *testing.T) {
	cause := errors.New(`pq: relation "app_user" does not exist at /srv/doenerstag/db.go:42`)

	rec := httptest.NewRecorder()
	WriteError(rec, httptest.NewRequest(http.MethodGet, "/x", http.NoBody),
		Newf(CodeDatabaseUnavailable, cause, "the database is unavailable"))

	if strings.Contains(rec.Body.String(), "app_user") ||
		strings.Contains(rec.Body.String(), "db.go") {
		t.Errorf("the wrapped cause leaked into the response: %s", rec.Body)
	}

	// It is still reachable for the log.
	err := Newf(CodeDatabaseUnavailable, cause, "x")
	if !errors.Is(err, cause) {
		t.Error("the cause is not retrievable with errors.Is")
	}
}

func TestEveryRegisteredCodeHasAMessage(t *testing.T) {
	for _, code := range Codes() {
		if strings.TrimSpace(code.Message()) == "" {
			t.Errorf("code %d has no message", code)
		}
	}
}
