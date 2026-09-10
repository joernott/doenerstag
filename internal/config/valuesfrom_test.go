package config

import (
	"reflect"
	"testing"
)

// ValuesFrom maps every setting, and nothing quietly falls back to its default.
//
// The map inside ValuesFrom is written out by hand, one line per setting, and
// the loop after it fills in anything missing with the declared default. That
// fallback is there for a real case -- a verb that never populated a section
// still writes a documented file -- and it also means a setting left out of the
// map is invisible: the file is written, it is well formed, and the value the
// operator asked for is silently replaced by the default.
//
// It happened. `behind-tls-proxy` was added to the registry, the struct and the
// loader, and left out of the map, so `DOENER_BEHIND_TLS_PROXY=true` produced a
// configuration file saying `behind_tls_proxy: false` -- which for that
// particular setting means session cookies stop being marked Secure.
//
// Only the boolean settings are checked, because a boolean has an unambiguous
// non-default value to set: flipping every bool in the struct to the opposite
// of its declared default and asking for it back catches an unmapped one
// without needing a plausible value per type.
func TestEveryBooleanSettingIsMapped(t *testing.T) {
	cfg := &Config{}

	// Flip every boolean in the struct, whatever its default, by walking the
	// sections rather than naming fields: a field added later is covered
	// without an edit here.
	flipBools(reflect.ValueOf(cfg).Elem())

	values := ValuesFrom(cfg)

	for _, setting := range Settings {
		if !setting.InConfigFile() || setting.Kind != KindBool {
			continue
		}
		want := "true"
		if declared, ok := setting.Default.(bool); ok && declared {
			// A setting that defaults to true is now false in the struct.
			want = "false"
		}
		if got := values[setting.Flag]; got != want {
			t.Errorf("%s is %q, want %q: ValuesFrom does not read it from the "+
				"configuration and the file will carry the default instead",
				setting.Flag, got, want)
		}
	}
}

func flipBools(v reflect.Value) {
	for i := range v.NumField() {
		field := v.Field(i)
		switch field.Kind() {
		case reflect.Struct:
			flipBools(field)
		case reflect.Bool:
			if field.CanSet() {
				field.SetBool(!field.Bool())
			}
		default:
		}
	}
}
