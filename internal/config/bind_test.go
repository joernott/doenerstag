package config

import (
	"testing"
	"time"

	"github.com/spf13/viper"

	"github.com/joernott/doenerstag/internal/logging"
)

// 20.17: every setting the file records is bound to a field, and the field's
// type is the one its Kind reads and writes. A setting without a binding would
// be read from nowhere and written as its default, the shape of 17.13.1's lost
// --behind-tls-proxy.
func TestEveryBoundSettingMatchesItsKind(t *testing.T) {
	cfg := &Config{}
	fields := map[any]string{}

	for _, setting := range Settings {
		if !setting.InConfigFile() {
			if setting.Bind != nil {
				t.Errorf("%s is bound but never written to the file", setting.Flag)
			}
			continue
		}
		if setting.Bind == nil {
			t.Errorf("%s is written to the file but bound to no field", setting.Flag)
			continue
		}

		field := setting.Bind(cfg)
		var ok bool
		switch setting.Kind {
		case KindString:
			_, ok = field.(*string)
			if !ok {
				_, ok = field.(*logging.Level)
			}
		case KindInt:
			_, ok = field.(*int)
		case KindBool:
			_, ok = field.(*bool)
		case KindDuration:
			_, ok = field.(*time.Duration)
		case KindByteSize:
			_, ok = field.(*int64)
		}
		if !ok {
			t.Errorf("%s is a kind %d setting bound to a %T", setting.Flag, setting.Kind, field)
		}

		// Two settings bound to one field would each overwrite the other.
		if other, taken := fields[field]; taken {
			t.Errorf("%s and %s are bound to the same field", other, setting.Flag)
		}
		fields[field] = setting.Flag
	}
}

// Every setting survives being read and written back: a value that is not its
// default goes in through the loader's path and comes out of ValuesFrom
// unchanged. Unlike the boolean test beside it, this covers every kind.
func TestEverySettingSurvivesARoundTrip(t *testing.T) {
	samples := map[Kind]string{
		KindString:   "zwiebel",
		KindInt:      "4711",
		KindBool:     "true",
		KindDuration: "3h",
		KindByteSize: "7MiB",
	}

	v := viper.New()
	for _, setting := range Settings {
		if setting.Bind == nil {
			continue
		}
		sample := samples[setting.Kind]
		if setting.Flag == "log-level" {
			sample = "WARN"
		}
		if declared, ok := setting.Default.(bool); ok && declared {
			sample = "false"
		}
		v.Set(setting.Key, sample)
	}

	cfg := &Config{}
	for _, setting := range Settings {
		if setting.Bind == nil {
			continue
		}
		if err := readSetting(v, setting, setting.Bind(cfg)); err != nil {
			t.Fatalf("reading %s: %v", setting.Flag, err)
		}
	}

	values := ValuesFrom(cfg)
	for _, setting := range Settings {
		if setting.Bind == nil {
			continue
		}
		if got, want := values[setting.Flag], v.GetString(setting.Key); got != want {
			t.Errorf("%s went in as %q and came out as %q", setting.Flag, want, got)
		}
	}
}
