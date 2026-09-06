package config

import (
	"testing"
	"time"
)

func TestParseDurationAcceptsStandardUnits(t *testing.T) {
	cases := map[string]time.Duration{
		"30s":    30 * time.Second,
		"15m":    15 * time.Minute,
		"6h":     6 * time.Hour,
		"120s":   120 * time.Second,
		"500ms":  500 * time.Millisecond,
		"1h30m":  90 * time.Minute,
		"0s":     0,
		"-5m":    -5 * time.Minute,
		"1.5h":   90 * time.Minute,
		"2h45m":  165 * time.Minute,
		"1h0m0s": time.Hour,
	}
	for input, want := range cases {
		got, err := ParseDuration(input)
		if err != nil {
			t.Errorf("ParseDuration(%q): %v", input, err)
			continue
		}
		if got != want {
			t.Errorf("ParseDuration(%q) = %v, want %v", input, got, want)
		}
	}
}

// The day and week units are the reason this parser exists: the documented
// defaults "7d" and "14d" are rejected outright by time.ParseDuration.
func TestParseDurationAcceptsDaysAndWeeks(t *testing.T) {
	cases := map[string]time.Duration{
		"1d":      24 * time.Hour,
		"7d":      7 * 24 * time.Hour,
		"14d":     14 * 24 * time.Hour,
		"365d":    365 * 24 * time.Hour,
		"1w":      7 * 24 * time.Hour,
		"2w":      14 * 24 * time.Hour,
		"1d12h":   36 * time.Hour,
		"1.5d":    36 * time.Hour,
		"1w1d":    8 * 24 * time.Hour,
		"-7d":     -7 * 24 * time.Hour,
		"1d6h30m": 24*time.Hour + 6*time.Hour + 30*time.Minute,
	}
	for input, want := range cases {
		got, err := ParseDuration(input)
		if err != nil {
			t.Errorf("ParseDuration(%q): %v", input, err)
			continue
		}
		if got != want {
			t.Errorf("ParseDuration(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestParseDurationRejectsNonsense(t *testing.T) {
	for _, input := range []string{"", "   ", "abc", "7", "d", "7dd", "7x", "1.2.3h", "7 days"} {
		if got, err := ParseDuration(input); err == nil {
			t.Errorf("ParseDuration(%q) = %v, want an error", input, got)
		}
	}
}

func TestParseDurationErrorQuotesTheOriginalInput(t *testing.T) {
	_, err := ParseDuration("7 days")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !contains(err.Error(), `"7 days"`) {
		t.Errorf("error %q does not quote the input the operator typed", err)
	}
}

func TestFormatDurationPrefersTheLargestWholeUnit(t *testing.T) {
	cases := map[time.Duration]string{
		7 * 24 * time.Hour:  "1w",
		14 * 24 * time.Hour: "2w",
		24 * time.Hour:      "1d",
		3 * 24 * time.Hour:  "3d",
		6 * time.Hour:       "6h0m0s",
		15 * time.Minute:    "15m0s",
		0:                   "0s",
		-24 * time.Hour:     "-1d",
	}
	for input, want := range cases {
		if got := FormatDuration(input); got != want {
			t.Errorf("FormatDuration(%v) = %q, want %q", input, got, want)
		}
	}
}

func TestDurationRoundTrips(t *testing.T) {
	for _, input := range []string{"30s", "15m", "6h", "1d", "7d", "2w", "120s"} {
		parsed, err := ParseDuration(input)
		if err != nil {
			t.Fatalf("ParseDuration(%q): %v", input, err)
		}
		reparsed, err := ParseDuration(FormatDuration(parsed))
		if err != nil {
			t.Fatalf("ParseDuration(FormatDuration(%q)): %v", input, err)
		}
		if reparsed != parsed {
			t.Errorf("%q did not survive a round trip: %v became %v", input, parsed, reparsed)
		}
	}
}

func TestParseByteSize(t *testing.T) {
	cases := map[string]int64{
		"5MiB":    5 * 1024 * 1024,
		"5mib":    5 * 1024 * 1024,
		"5MIB":    5 * 1024 * 1024,
		"512KiB":  512 * 1024,
		"1GiB":    1024 * 1024 * 1024,
		"1KB":     1000,
		"1MB":     1000 * 1000,
		"1M":      1000 * 1000,
		"1K":      1000,
		"1024":    1024,
		"0":       0,
		"1048576": 1048576,
		"100B":    100,
		"1.5MiB":  1024 * 1024 * 3 / 2,
	}
	for input, want := range cases {
		got, err := ParseByteSize(input)
		if err != nil {
			t.Errorf("ParseByteSize(%q): %v", input, err)
			continue
		}
		if got != want {
			t.Errorf("ParseByteSize(%q) = %d, want %d", input, got, want)
		}
	}
}

// KiB is 1024 bytes and KB is 1000. Conflating the two would silently change
// the upload limit, so it is asserted rather than assumed.
func TestBinaryAndDecimalUnitsDiffer(t *testing.T) {
	binary, err := ParseByteSize("1KiB")
	if err != nil {
		t.Fatal(err)
	}
	decimal, err := ParseByteSize("1KB")
	if err != nil {
		t.Fatal(err)
	}
	if binary != 1024 || decimal != 1000 {
		t.Errorf("1KiB = %d and 1KB = %d, want 1024 and 1000", binary, decimal)
	}
}

func TestParseByteSizeRejectsNonsense(t *testing.T) {
	for _, input := range []string{"", "   ", "MiB", "abc", "-1", "-5MiB", "5 giga", "5MiBB"} {
		if got, err := ParseByteSize(input); err == nil {
			t.Errorf("ParseByteSize(%q) = %d, want an error", input, got)
		}
	}
}

func TestFormatByteSize(t *testing.T) {
	cases := map[int64]string{
		5 * 1024 * 1024:    "5MiB",
		512 * 1024:         "512KiB",
		1024 * 1024 * 1024: "1GiB",
		1024:               "1KiB",
		1000:               "1000",
		0:                  "0",
		1500:               "1500",
	}
	for input, want := range cases {
		if got := FormatByteSize(input); got != want {
			t.Errorf("FormatByteSize(%d) = %q, want %q", input, got, want)
		}
	}
}

func TestByteSizeRoundTrips(t *testing.T) {
	for _, input := range []string{"5MiB", "512KiB", "1GiB", "1024"} {
		parsed, err := ParseByteSize(input)
		if err != nil {
			t.Fatalf("ParseByteSize(%q): %v", input, err)
		}
		reparsed, err := ParseByteSize(FormatByteSize(parsed))
		if err != nil {
			t.Fatalf("ParseByteSize(FormatByteSize(%q)): %v", input, err)
		}
		if reparsed != parsed {
			t.Errorf("%q did not survive a round trip: %d became %d", input, parsed, reparsed)
		}
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// Values arriving from a configuration file or an environment variable can
// carry incidental whitespace. Both parsers trim it. Kept out of the tables
// above because a map key with padding reads as a typo.
func TestParsersTrimSurroundingWhitespace(t *testing.T) {
	duration, err := ParseDuration(" 6h ")
	if err != nil {
		t.Errorf("ParseDuration(\" 6h \"): %v", err)
	} else if duration != 6*time.Hour {
		t.Errorf("ParseDuration(\" 6h \") = %v, want 6h", duration)
	}

	size, err := ParseByteSize(" 5MiB ")
	if err != nil {
		t.Errorf("ParseByteSize(\" 5MiB \"): %v", err)
	} else if size != 5*1024*1024 {
		t.Errorf("ParseByteSize(\" 5MiB \") = %d, want 5MiB", size)
	}
}
