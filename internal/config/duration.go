package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Hours per day and per week, for the units time.ParseDuration does not have.
const (
	hoursPerDay  = 24
	hoursPerWeek = 24 * 7
)

// ParseDuration parses a duration, extending time.ParseDuration with the day
// and week units.
//
// The documented defaults in docs/09_configuration.md include "7d" for the
// absolute session timeout and "14d" for the order retention period, neither of
// which the standard library accepts: it knows ns, us, ms, s, m and h and
// stops there. Rejecting an operator's "7d" because Go has an opinion about
// calendar arithmetic would be a poor trade.
//
// Days and weeks are treated as fixed spans of 24 and 168 hours. That is the
// right reading for a session lifetime or a retention window, and it sidesteps
// daylight saving entirely. Anything that genuinely needs calendar arithmetic
// must not use this function.
func ParseDuration(s string) (time.Duration, error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return 0, fmt.Errorf("duration is empty")
	}

	normalised, err := expandDayAndWeekUnits(trimmed)
	if err != nil {
		return 0, err
	}

	d, err := time.ParseDuration(normalised)
	if err != nil {
		// Report the input the operator typed, not the rewritten form.
		return 0, fmt.Errorf("invalid duration %q: expected a value such as 30s, 15m, 6h, 7d or 2w", s)
	}
	return d, nil
}

// expandDayAndWeekUnits rewrites every "<number>d" and "<number>w" component
// into the equivalent number of hours, leaving everything else untouched, so
// that time.ParseDuration can take it from there.
//
// time.ParseDuration sums repeated units, so "1d12h" becoming "24h12h" is
// correct and needs no further assembly.
func expandDayAndWeekUnits(s string) (string, error) {
	var out strings.Builder
	out.Grow(len(s) + 8)

	i := 0

	// A leading sign belongs to the whole duration, not to a component.
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		out.WriteByte(s[i])
		i++
	}

	for i < len(s) {
		start := i

		// The numeric part, which may be fractional.
		for i < len(s) && (isASCIIDigit(s[i]) || s[i] == '.') {
			i++
		}
		if i == start {
			// Not a number where one was expected. Hand the original to
			// time.ParseDuration and let it produce the error.
			out.WriteString(s[i:])
			return out.String(), nil
		}
		number := s[start:i]

		// The unit part.
		unitStart := i
		for i < len(s) && !isASCIIDigit(s[i]) && s[i] != '.' {
			i++
		}
		unit := s[unitStart:i]

		switch unit {
		case "d", "w":
			value, err := strconv.ParseFloat(number, 64)
			if err != nil {
				return "", fmt.Errorf("invalid duration %q: %s is not a number", s, number)
			}
			hours := value * hoursPerDay
			if unit == "w" {
				hours = value * hoursPerWeek
			}
			out.WriteString(strconv.FormatFloat(hours, 'f', -1, 64))
			out.WriteString("h")
		default:
			out.WriteString(number)
			out.WriteString(unit)
		}
	}

	return out.String(), nil
}

func isASCIIDigit(b byte) bool { return b >= '0' && b <= '9' }

// FormatDuration renders a duration the way the generated configuration file
// writes it, preferring the largest whole unit so that 168h reads back as "7d".
func FormatDuration(d time.Duration) string {
	if d == 0 {
		return "0s"
	}

	negative := d < 0
	if negative {
		d = -d
	}

	var s string
	switch {
	case d%(hoursPerWeek*time.Hour) == 0:
		s = strconv.FormatInt(int64(d/(hoursPerWeek*time.Hour)), 10) + "w"
	case d%(hoursPerDay*time.Hour) == 0:
		s = strconv.FormatInt(int64(d/(hoursPerDay*time.Hour)), 10) + "d"
	default:
		s = d.String()
	}

	if negative {
		return "-" + s
	}
	return s
}
