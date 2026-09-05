// Package logging configures structured JSON logging for doenerstag.
//
// The application uses five log levels, numbered so that a lower number is more
// severe, as specified in docs/08_technologies.md. Each level also emits
// everything from the lower-numbered levels.
package logging

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/rs/zerolog"
)

// Level is a doenerstag log level. The numbering runs from the most severe
// (FATAL, 1) to the least severe (DEBUG, 5), which is the reverse of zerolog's
// own ordering. Zerolog translates the two.
type Level uint8

// The five log levels.
const (
	LevelFatal Level = 1
	LevelError Level = 2
	LevelWarn  Level = 3
	LevelInfo  Level = 4
	LevelDebug Level = 5
)

// DefaultLevel is used when nothing is configured.
const DefaultLevel = LevelInfo

var levelNames = map[Level]string{
	LevelFatal: "FATAL",
	LevelError: "ERROR",
	LevelWarn:  "WARN",
	LevelInfo:  "INFO",
	LevelDebug: "DEBUG",
}

// String returns the upper-case name of the level, as accepted by --log-level.
func (l Level) String() string {
	if name, ok := levelNames[l]; ok {
		return name
	}
	return "UNKNOWN(" + strconv.Itoa(int(l)) + ")"
}

// Valid reports whether l is one of the five defined levels.
func (l Level) Valid() bool {
	_, ok := levelNames[l]
	return ok
}

// Zerolog maps a doenerstag level onto the zerolog level that emits it and
// everything more severe.
func (l Level) Zerolog() zerolog.Level {
	switch l {
	case LevelFatal:
		return zerolog.FatalLevel
	case LevelError:
		return zerolog.ErrorLevel
	case LevelWarn:
		return zerolog.WarnLevel
	case LevelInfo:
		return zerolog.InfoLevel
	case LevelDebug:
		return zerolog.DebugLevel
	default:
		return DefaultLevel.Zerolog()
	}
}

// LevelNames lists the accepted level names from most to least severe. It is
// used in flag help and in error messages.
func LevelNames() []string {
	return []string{"FATAL", "ERROR", "WARN", "INFO", "DEBUG"}
}

// ParseLevel converts a level name to a Level. Input is accepted in any casing
// and with surrounding whitespace, as required by docs/09_configuration.md.
//
// The numeric forms "1" to "5" are also accepted, because the numbers are part
// of the documented level definition and an operator may reasonably try them.
func ParseLevel(s string) (Level, error) {
	normalised := strings.ToUpper(strings.TrimSpace(s))
	if normalised == "" {
		return 0, fmt.Errorf("log level is empty, expected one of %s",
			strings.Join(LevelNames(), ", "))
	}

	for level, name := range levelNames {
		if name == normalised {
			return level, nil
		}
	}

	// "WARNING" is a common enough synonym to accept rather than reject.
	if normalised == "WARNING" {
		return LevelWarn, nil
	}

	if n, err := strconv.Atoi(normalised); err == nil {
		if level := Level(n); level.Valid() { //nolint:gosec // range checked by Valid
			return level, nil
		}
	}

	return 0, fmt.Errorf("unknown log level %q, expected one of %s",
		s, strings.Join(LevelNames(), ", "))
}

// MustParseLevel is ParseLevel for values known to be valid at compile time.
func MustParseLevel(s string) Level {
	level, err := ParseLevel(s)
	if err != nil {
		panic(err)
	}
	return level
}
