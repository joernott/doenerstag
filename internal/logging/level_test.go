package logging

import (
	"testing"

	"github.com/rs/zerolog"
)

func TestParseLevelAcceptsAnyCasingAndWhitespace(t *testing.T) {
	cases := map[string]Level{
		"FATAL":   LevelFatal,
		"fatal":   LevelFatal,
		"FaTaL":   LevelFatal,
		"  info ": LevelInfo,
		"ERROR":   LevelError,
		"warn":    LevelWarn,
		"WARNING": LevelWarn,
		"debug":   LevelDebug,
		"1":       LevelFatal,
		"5":       LevelDebug,
	}

	for input, want := range cases {
		got, err := ParseLevel(input)
		if err != nil {
			t.Errorf("ParseLevel(%q) returned error: %v", input, err)
			continue
		}
		if got != want {
			t.Errorf("ParseLevel(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestParseLevelRejectsUnknown(t *testing.T) {
	for _, input := range []string{"", "  ", "trace", "verbose", "0", "6", "-1", "INFORMATION"} {
		if _, err := ParseLevel(input); err == nil {
			t.Errorf("ParseLevel(%q) accepted an invalid level", input)
		}
	}
}

func TestParseLevelErrorNamesTheValidLevels(t *testing.T) {
	_, err := ParseLevel("verbose")
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, name := range LevelNames() {
		if !contains(err.Error(), name) {
			t.Errorf("error %q does not mention the valid level %s", err, name)
		}
	}
}

func TestLevelStringRoundTrips(t *testing.T) {
	for _, level := range []Level{LevelFatal, LevelError, LevelWarn, LevelInfo, LevelDebug} {
		parsed, err := ParseLevel(level.String())
		if err != nil {
			t.Errorf("ParseLevel(%q): %v", level.String(), err)
			continue
		}
		if parsed != level {
			t.Errorf("round trip of %v produced %v", level, parsed)
		}
	}
}

// The doenerstag numbering runs the opposite way to zerolog's: FATAL is 1 here
// and the most severe, but 4 and the most severe there. This asserts the
// mapping rather than the arithmetic, because the two orderings are easy to
// conflate.
func TestLevelMapsOntoZerolog(t *testing.T) {
	cases := map[Level]zerolog.Level{
		LevelFatal: zerolog.FatalLevel,
		LevelError: zerolog.ErrorLevel,
		LevelWarn:  zerolog.WarnLevel,
		LevelInfo:  zerolog.InfoLevel,
		LevelDebug: zerolog.DebugLevel,
	}
	for level, want := range cases {
		if got := level.Zerolog(); got != want {
			t.Errorf("%v.Zerolog() = %v, want %v", level, got, want)
		}
	}
}

func TestLevelOrderingIsInverseOfSeverity(t *testing.T) {
	// A lower doenerstag number is more severe, so it must map to a higher
	// zerolog level.
	levels := []Level{LevelFatal, LevelError, LevelWarn, LevelInfo, LevelDebug}
	for i := 1; i < len(levels); i++ {
		if levels[i-1].Zerolog() <= levels[i].Zerolog() {
			t.Errorf("%v should be more severe than %v", levels[i-1], levels[i])
		}
	}
}

func TestValidRejectsOutOfRange(t *testing.T) {
	for _, level := range []Level{0, 6, 255} {
		if level.Valid() {
			t.Errorf("Level(%d).Valid() = true, want false", level)
		}
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
