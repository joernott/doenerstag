package logging

import (
	"reflect"
	"strings"

	"github.com/rs/zerolog"
)

// Redacted replaces the value of any field whose name is considered sensitive.
const Redacted = "***"

// StructTag is the struct tag key that suppresses a field entirely. A field
// tagged `log:"-"` never appears in log output, whatever its name.
const StructTag = "log"

// sensitiveSubstrings is the deny-list from docs/08_technologies.md. A field or
// parameter name is sensitive when it contains any of these, compared without
// regard to case.
//
// Substring matching rather than exact matching is deliberate: it catches
// database_password, DOENER_JWT_SECRET, Set-Cookie and X-CSRF-Token without
// each having to be enumerated. The cost is the occasional false positive,
// which redacts something harmless. That is the right direction to be wrong in.
var sensitiveSubstrings = []string{
	"password",
	"passwd",
	"secret",
	"token",
	"authorization",
	"cookie",
	"csrf",
	"credential",
	"private_key",
	"privatekey",
	"apikey",
	"api_key",
}

// IsSensitive reports whether a field or parameter of this name must have its
// value redacted before it is logged.
func IsSensitive(name string) bool {
	lowered := strings.ToLower(name)
	for _, needle := range sensitiveSubstrings {
		if strings.Contains(lowered, needle) {
			return true
		}
	}
	return false
}

// RedactValue returns value, or the redaction marker when name is sensitive.
func RedactValue(name string, value any) any {
	if IsSensitive(name) {
		return Redacted
	}
	return value
}

// RedactMap returns a copy of fields with every sensitive value replaced. The
// input map is not modified.
func RedactMap(fields map[string]any) map[string]any {
	if fields == nil {
		return nil
	}
	out := make(map[string]any, len(fields))
	for name, value := range fields {
		out[name] = RedactValue(name, value)
	}
	return out
}

// RedactStruct flattens a struct into a map suitable for logging. Exported
// fields tagged `log:"-"` are omitted entirely; fields whose name or `log` tag
// name is sensitive are redacted. Unexported fields are always omitted, since
// they cannot be read without unsafe access.
//
// A nil pointer yields nil. A non-struct value yields a single "value" entry,
// redacted if it needs to be, so that callers do not have to special-case it.
func RedactStruct(v any) map[string]any {
	if v == nil {
		return nil
	}

	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}

	if rv.Kind() != reflect.Struct {
		return map[string]any{"value": rv.Interface()}
	}

	rt := rv.Type()
	out := make(map[string]any, rt.NumField())

	for i := range rt.NumField() {
		field := rt.Field(i)
		if !field.IsExported() {
			continue
		}

		name := field.Name
		if tag, ok := field.Tag.Lookup(StructTag); ok {
			tagName, _, _ := strings.Cut(tag, ",")
			if tagName == "-" {
				continue
			}
			if tagName != "" {
				name = tagName
			}
		}

		if IsSensitive(name) {
			out[name] = Redacted
			continue
		}
		out[name] = rv.Field(i).Interface()
	}

	return out
}

// AddFields attaches fields to a zerolog event, redacting sensitive values.
// It returns the event so that it can be chained.
func AddFields(event *zerolog.Event, fields map[string]any) *zerolog.Event {
	for name, value := range fields {
		if IsSensitive(name) {
			event = event.Str(name, Redacted)
			continue
		}
		event = event.Interface(name, value)
	}
	return event
}

// FieldNames returns the keys of fields, sorted for stable output and with no
// values at all.
//
// Request bodies are never logged in full (docs/08_technologies.md); only the
// names of the fields they contained. This produces that list.
func FieldNames(fields map[string]any) []string {
	if len(fields) == 0 {
		return nil
	}
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sortStrings(names)
	return names
}

// sortStrings is a small insertion sort, used to keep this file free of a
// dependency on sort for one call site with a handful of elements.
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
