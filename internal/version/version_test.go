package version

import (
	"strings"
	"testing"
)

func TestAccessorsReturnTheInjectedValues(t *testing.T) {
	if Version() != version {
		t.Errorf("Version() = %q, want %q", Version(), version)
	}
	if Commit() != commit {
		t.Errorf("Commit() = %q, want %q", Commit(), commit)
	}
	if BuildDate() != buildDate {
		t.Errorf("BuildDate() = %q, want %q", BuildDate(), buildDate)
	}
}

func TestStringNamesTheBinaryAndItsBuild(t *testing.T) {
	s := String()
	for _, want := range []string{"doenerstag", Version(), Commit(), BuildDate()} {
		if !strings.Contains(s, want) {
			t.Errorf("String() = %q, missing %q", s, want)
		}
	}
}

// An unstamped binary must not claim a release version it does not have.
func TestIsDevelopmentTracksThePlaceholder(t *testing.T) {
	original := version
	t.Cleanup(func() { version = original })

	version = "0.0.0-dev"
	if !IsDevelopment() {
		t.Error("IsDevelopment() = false for the placeholder version")
	}

	version = "1.0.0"
	if IsDevelopment() {
		t.Error("IsDevelopment() = true for a stamped version")
	}
}
