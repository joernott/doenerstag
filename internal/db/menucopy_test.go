package db

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// CopyName steps past every name already taken, case-insensitively as the
// unique indexes compare, and marks the one it hands out as taken.
func TestCopyNameFindsAFreeName(t *testing.T) {
	taken := map[string]bool{"döner (kopie)": true}

	if got := CopyName("Döner", "Kopie", taken); got != "Döner (Kopie 2)" {
		t.Errorf("got %q", got)
	}
	if got := CopyName("Döner", "Kopie", taken); got != "Döner (Kopie 3)" {
		t.Errorf("a second call got %q", got)
	}
}

// A name at the length limit is shortened, not the suffix, so the copy fits
// the column and still says what it is.
func TestCopyNameFitsTheLengthLimit(t *testing.T) {
	long := strings.Repeat("ä", maxCopyNameLength)

	got := CopyName(long, "copy", map[string]bool{})
	if utf8.RuneCountInString(got) != maxCopyNameLength {
		t.Errorf("the name is %d characters", utf8.RuneCountInString(got))
	}
	if !strings.HasSuffix(got, " (copy)") {
		t.Errorf("the name %q lost its suffix", got)
	}
}
