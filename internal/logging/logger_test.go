package logging

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEveryLineCarriesTheStandardFields(t *testing.T) {
	var buf bytes.Buffer
	logger, err := New(Options{Level: LevelInfo, Output: &buf})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	logger.Component("api").Info().Msg("request completed")

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("output is not valid JSON: %v (%s)", err, buf.String())
	}

	for _, field := range []string{FieldTime, FieldLevel, FieldMessage, FieldComponent} {
		if _, present := line[field]; !present {
			t.Errorf("missing standard field %q in %s", field, buf.String())
		}
	}
	if line[FieldLevel] != "info" {
		t.Errorf("level = %v, want info", line[FieldLevel])
	}
	if line[FieldComponent] != "api" {
		t.Errorf("component = %v, want api", line[FieldComponent])
	}
}

func TestTimestampsAreUTCWithMilliseconds(t *testing.T) {
	var buf bytes.Buffer
	logger, err := New(Options{Level: LevelInfo, Output: &buf})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	logger.Zerolog().Info().Msg("x")

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	raw, ok := line[FieldTime].(string)
	if !ok {
		t.Fatalf("time field is not a string: %v", line[FieldTime])
	}
	if !strings.HasSuffix(raw, "Z") {
		t.Errorf("timestamp %q is not UTC", raw)
	}

	parsed, err := time.Parse(timeFormat, raw)
	if err != nil {
		t.Fatalf("timestamp %q does not match the documented format: %v", raw, err)
	}
	// Three fractional digits: the format keeps milliseconds, no more, no less.
	dot := strings.LastIndex(raw, ".")
	if dot < 0 || len(raw)-dot-2 != 3 {
		t.Errorf("timestamp %q does not carry exactly three fractional digits", raw)
	}
	if time.Since(parsed) > time.Minute {
		t.Errorf("timestamp %q is not close to now", raw)
	}
}

// Each level must also emit everything more severe, and nothing less severe.
func TestLevelFiltering(t *testing.T) {
	cases := []struct {
		configured Level
		wantLevels []string
	}{
		{LevelFatal, []string{}},
		{LevelError, []string{"error"}},
		{LevelWarn, []string{"error", "warn"}},
		{LevelInfo, []string{"error", "warn", "info"}},
		{LevelDebug, []string{"error", "warn", "info", "debug"}},
	}

	for _, tc := range cases {
		var buf bytes.Buffer
		logger, err := New(Options{Level: tc.configured, Output: &buf})
		if err != nil {
			t.Fatalf("New: %v", err)
		}

		zl := logger.Zerolog()
		// FATAL is not exercised here: zerolog's Fatal calls os.Exit.
		zl.Error().Msg("e")
		zl.Warn().Msg("w")
		zl.Info().Msg("i")
		zl.Debug().Msg("d")

		emitted := emittedLevels(t, buf.String())
		if len(emitted) != len(tc.wantLevels) {
			t.Errorf("level %v emitted %v, want %v", tc.configured, emitted, tc.wantLevels)
			continue
		}
		for i := range tc.wantLevels {
			if emitted[i] != tc.wantLevels[i] {
				t.Errorf("level %v emitted %v, want %v", tc.configured, emitted, tc.wantLevels)
				break
			}
		}
	}
}

func emittedLevels(t *testing.T, output string) []string {
	t.Helper()
	var levels []string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		var decoded map[string]any
		if err := json.Unmarshal([]byte(line), &decoded); err != nil {
			t.Fatalf("line is not valid JSON: %v (%s)", err, line)
		}
		levels = append(levels, decoded[FieldLevel].(string))
	}
	return levels
}

func TestInvalidLevelFallsBackToDefault(t *testing.T) {
	var buf bytes.Buffer
	logger, err := New(Options{Level: Level(99), Output: &buf})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	logger.Zerolog().Info().Msg("i")
	logger.Zerolog().Debug().Msg("d")

	if got := emittedLevels(t, buf.String()); len(got) != 1 || got[0] != "info" {
		t.Errorf("an out-of-range level did not fall back to INFO: %v", got)
	}
}

func TestLogsToFileWhenConfigured(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doenerstag.log")

	logger, err := New(Options{Level: LevelInfo, File: path})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = logger.Close() })

	logger.Zerolog().Info().Msg("to file")

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the log file: %v", err)
	}
	if !strings.Contains(string(content), "to file") {
		t.Errorf("message not found in the log file: %s", content)
	}
}

func TestNewFailsOnAnUnwritableLogFile(t *testing.T) {
	// A path whose parent directory does not exist cannot be opened on any
	// platform, which makes this portable.
	path := filepath.Join(t.TempDir(), "no-such-directory", "doenerstag.log")

	if _, err := New(Options{Level: LevelInfo, File: path}); err == nil {
		t.Error("New accepted an unwritable log file path")
	}
}

// Reopen is what makes SIGHUP-driven logrotate work: the file is renamed out
// from under the process, and the next write must land in a newly created file
// at the original path rather than in the renamed one.
func TestReopenWritesToANewFileAfterRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doenerstag.log")
	rotated := filepath.Join(dir, "doenerstag.log.1")

	logger, err := New(Options{Level: LevelInfo, File: path})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = logger.Close() })

	logger.Zerolog().Info().Msg("before rotation")

	if err := os.Rename(path, rotated); err != nil {
		t.Skipf("cannot rename an open file on this platform: %v", err)
	}
	if err := logger.Reopen(); err != nil {
		t.Fatalf("Reopen: %v", err)
	}

	logger.Zerolog().Info().Msg("after rotation")

	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the reopened log file: %v", err)
	}
	if !strings.Contains(string(current), "after rotation") {
		t.Errorf("post-rotation message missing from the new file: %s", current)
	}
	if strings.Contains(string(current), "before rotation") {
		t.Errorf("new file contains pre-rotation content: %s", current)
	}

	old, err := os.ReadFile(rotated)
	if err != nil {
		t.Fatalf("reading the rotated log file: %v", err)
	}
	if !strings.Contains(string(old), "before rotation") {
		t.Errorf("pre-rotation message lost: %s", old)
	}
}

func TestReopenAndCloseAreNoOpsForStdout(t *testing.T) {
	logger, err := New(Options{Level: LevelInfo})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := logger.Reopen(); err != nil {
		t.Errorf("Reopen on a stdout logger: %v", err)
	}
	if err := logger.Close(); err != nil {
		t.Errorf("Close on a stdout logger: %v", err)
	}
}

func TestWriteAfterCloseDoesNotPanic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doenerstag.log")
	logger, err := New(Options{Level: LevelInfo, File: path})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := logger.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// zerolog swallows the write error; the requirement is that shutdown
	// ordering cannot crash the process.
	logger.Zerolog().Info().Msg("after close")
}
