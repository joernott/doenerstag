package api

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// The field validators every handler shares.
//
// They lived wherever each was first needed -- orders.go, restaurants.go,
// classification.go -- with the "not a valid identifier" error written out
// five times and three separate RFC 3339 parsers, one of which neither trimmed
// its input nor normalised to UTC (20.19). A handler's own rule, a restaurant
// name being unique say, stays in the handler; what is here is what a value of
// a given kind has to be anywhere.

// invalidID is the answer for a value that should be a UUID and is not.
func invalidID(field string) *Error {
	return &Error{Code: CodeInvalidField, Field: field, Detail: "this is not a valid identifier"}
}

// longerThan reports whether a value has more than limit characters, counted
// as a person counts them: "Döner" is five, not six.
func longerThan(value string, limit int) bool {
	return utf8.RuneCountInString(value) > limit
}

// tooLong is the answer for a value over its length limit. The limit in the
// message is the limit in force, not a number typed beside it.
func tooLong(field, what string, limit int) *Error {
	return &Error{
		Code:   CodeInvalidField,
		Field:  field,
		Detail: fmt.Sprintf("the %s may be at most %d characters", what, limit),
	}
}

// requiredUUID reads an identifier that must be present.
func requiredUUID(value, field string) (uuid.UUID, *Error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return uuid.Nil, &Error{Code: CodeMissingField, Field: field}
	}
	id, err := uuid.Parse(trimmed)
	if err != nil {
		return uuid.Nil, invalidID(field)
	}
	return id, nil
}

// parseOptionalUUID reads an identifier that may be absent or empty.
func parseOptionalUUID(value *string, field string) (*uuid.UUID, *Error) {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil, nil
	}
	id, err := uuid.Parse(strings.TrimSpace(*value))
	if err != nil {
		return nil, invalidID(field)
	}
	return &id, nil
}

// parseIDList reads a list of identifiers, all of which must parse.
func parseIDList(values []string, field string) ([]uuid.UUID, *Error) {
	ids := make([]uuid.UUID, 0, len(values))
	for _, raw := range values {
		id, err := uuid.Parse(strings.TrimSpace(raw))
		if err != nil {
			return nil, invalidID(field)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// idString renders an optional identifier for a response body.
func idString(id *uuid.UUID) *string {
	if id == nil {
		return nil
	}
	s := id.String()
	return &s
}

// requiredTime reads an RFC 3339 timestamp that must be present, in UTC.
//
// RFC 3339 rather than a looser format, because an order's times are the one
// thing everybody has to agree on and an ambiguous "12:30" would be read
// differently by two clients in two time zones.
func requiredTime(value, field string) (time.Time, *Error) {
	parsed, err := optionalTime(value, field)
	if err != nil {
		return time.Time{}, err
	}
	if parsed == nil {
		return time.Time{}, &Error{Code: CodeMissingField, Field: field}
	}
	return *parsed, nil
}

// optionalTime reads an RFC 3339 timestamp that may be absent, in UTC.
func optionalTime(value, field string) (*time.Time, *Error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, trimmed)
	if err != nil {
		return nil, &Error{
			Code:   CodeInvalidField,
			Field:  field,
			Detail: "the time must be an RFC 3339 timestamp",
		}
	}
	utc := parsed.UTC()
	return &utc, nil
}

// validateFreeText checks a free-text value against its length limit.
func validateFreeText(value, field string, limit int) *Error {
	if longerThan(strings.TrimSpace(value), limit) {
		return &Error{
			Code:   CodeInvalidField,
			Field:  field,
			Detail: "this value is too long",
		}
	}
	return nil
}

// validateMoney checks an optional amount of money is not negative.
func validateMoney(cents *int64, field string) *Error {
	if cents != nil && *cents < 0 {
		return &Error{
			Code:   CodeInvalidField,
			Field:  field,
			Detail: "the amount may not be negative",
		}
	}
	return nil
}
