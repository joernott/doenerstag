package config

import (
	"fmt"
	"strconv"
	"time"

	"github.com/spf13/viper"

	"github.com/joernott/doenerstag/internal/logging"
)

// readSetting fills the field a setting is bound to from the resolved values.
//
// The field's type decides how the value is read, and it has to agree with the
// setting's Kind -- TestEveryBoundSettingMatchesItsKind holds every setting to
// that. A duration and a byte size are both read as text and parsed, because
// "7d" and "5MiB" are what a person writes.
func readSetting(v *viper.Viper, setting Setting, field any) error {
	switch p := field.(type) {
	case *string:
		*p = v.GetString(setting.Key)
	case *int:
		*p = v.GetInt(setting.Key)
	case *bool:
		*p = v.GetBool(setting.Key)
	case *time.Duration:
		d, err := ParseDuration(v.GetString(setting.Key))
		if err != nil {
			return err
		}
		*p = d
	case *int64:
		n, err := ParseByteSize(v.GetString(setting.Key))
		if err != nil {
			return err
		}
		*p = n
	case *logging.Level:
		level, err := logging.ParseLevel(v.GetString(setting.Key))
		if err != nil {
			// A level that cannot be read still leaves a working logger.
			*p = logging.DefaultLevel
			return err
		}
		*p = level
	default:
		return fmt.Errorf("bound to a %T, which no setting kind reads", field)
	}
	return nil
}

// formatSetting renders a bound field the way the configuration file writes it.
//
// The inverse of readSetting, case for case: durations and byte sizes go back
// out in the form a person would write -- 604800000000000 nanoseconds is "7d".
func formatSetting(field any) string {
	switch p := field.(type) {
	case *string:
		return *p
	case *int:
		return strconv.Itoa(*p)
	case *bool:
		return strconv.FormatBool(*p)
	case *time.Duration:
		return FormatDuration(*p)
	case *int64:
		return FormatByteSize(*p)
	case *logging.Level:
		return p.String()
	default:
		return ""
	}
}
