package config

import (
	"fmt"
	"strconv"
	"strings"
)

// Byte size units. Both the binary (IEC) and decimal (SI) forms are accepted,
// and both mean bytes: this is a file size, never a bit rate.
const (
	Byte     int64 = 1
	Kibibyte       = 1024 * Byte
	Mebibyte       = 1024 * Kibibyte
	Gibibyte       = 1024 * Mebibyte

	Kilobyte = 1000 * Byte
	Megabyte = 1000 * Kilobyte
	Gigabyte = 1000 * Megabyte
)

var byteUnits = []struct {
	suffix string
	factor int64
}{
	// Longest suffixes first, so that "MiB" is not matched as "M" plus "iB".
	{"KIB", Kibibyte},
	{"MIB", Mebibyte},
	{"GIB", Gibibyte},
	{"KB", Kilobyte},
	{"MB", Megabyte},
	{"GB", Gigabyte},
	{"K", Kilobyte},
	{"M", Megabyte},
	{"G", Gigabyte},
	{"B", Byte},
}

// ParseByteSize parses a byte size such as "5MiB", "512KB", "2G" or a bare
// number of bytes.
//
// docs/09_configuration.md documents --max-image-size as "5MiB" and says the
// KiB and MiB suffixes are accepted. The standard library has no parser for
// this, so this is it.
//
// Matching is case-insensitive and every unit means bytes. "MB" is a megabyte,
// never a megabit: the setting is a file size and no other reading would be
// useful. A bare number is bytes.
func ParseByteSize(s string) (int64, error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return 0, fmt.Errorf("byte size is empty")
	}

	upper := strings.ToUpper(trimmed)

	factor := Byte
	numeric := upper
	for _, unit := range byteUnits {
		if strings.HasSuffix(upper, unit.suffix) {
			factor = unit.factor
			numeric = strings.TrimSpace(strings.TrimSuffix(upper, unit.suffix))
			break
		}
	}

	if numeric == "" {
		return 0, fmt.Errorf("invalid byte size %q: no number before the unit", s)
	}

	value, err := strconv.ParseFloat(numeric, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid byte size %q: expected a value such as 5MiB, 512KB or 1048576", s)
	}
	if value < 0 {
		return 0, fmt.Errorf("invalid byte size %q: must not be negative", s)
	}

	// A fractional size is allowed on input ("1.5MiB") and truncated to whole
	// bytes, which is the only thing a size can be.
	return int64(value * float64(factor)), nil
}

// FormatByteSize renders a size using the largest binary unit that divides it
// exactly, so 5242880 reads back as "5MiB". Sizes that are not exact multiples
// fall back to a plain byte count, because an approximate size in a generated
// configuration file would be a bug rather than a convenience.
func FormatByteSize(n int64) string {
	if n < 0 {
		return strconv.FormatInt(n, 10)
	}

	units := []struct {
		suffix string
		factor int64
	}{
		{"GiB", Gibibyte},
		{"MiB", Mebibyte},
		{"KiB", Kibibyte},
	}

	for _, unit := range units {
		if n >= unit.factor && n%unit.factor == 0 {
			return strconv.FormatInt(n/unit.factor, 10) + unit.suffix
		}
	}
	return strconv.FormatInt(n, 10)
}
