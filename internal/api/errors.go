// Package api serves the REST API, the static assets and the frontend.
//
// Routing, the error envelope and the middleware chain live here. See
// docs/04_api.md for the API contract.
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// Code is an internal, stable error number.
//
// It is what the frontend keys its translations on, so a code's meaning may
// never change once released: a new situation gets a new number rather than
// reusing one whose text no longer fits.
type Code int

// The error numbers from docs/04_api.md. The ranges are:
//
//	1000-1999  request validation
//	2000-2999  authentication
//	3000-3999  authorization
//	4000-4999  resource state
//	5000-5999  rate limiting
//	9000-9999  server and database
const (
	// Request validation.
	CodeMalformedJSON          Code = 1000
	CodeMissingField           Code = 1001
	CodeInvalidField           Code = 1002
	CodeDeadlineAfterFulfil    Code = 1003
	CodeQuantityTooSmall       Code = 1004
	CodeItemWrongRestaurant    Code = 1005
	CodeModificationWrongItem  Code = 1006
	CodeRestaurantNeedsContact Code = 1007
	CodeOpeningHoursZeroLength Code = 1008
	CodeUserNameTaken          Code = 1009
	CodePasswordTooWeak        Code = 1010
	CodeImageTooLarge          Code = 1011
	CodeImageUnsupportedType   Code = 1012
	CodeUnknownCurrency        Code = 1013
	CodeDeadlineInThePast      Code = 1014

	// Authentication.
	CodeNotAuthenticated  Code = 2000
	CodeInvalidLogin      Code = 2001
	CodeSessionExpired    Code = 2002
	CodeSessionSuperseded Code = 2003
	CodeInvalidToken      Code = 2004
	CodeInvalidCSRF       Code = 2005
	CodeAlreadyLoggedIn   Code = 2006

	// Authorization.
	CodeAdminRequired       Code = 3000
	CodeNotOrderCreator     Code = 3001
	CodeNotItemOwner        Code = 3002
	CodePlaceholderReadOnly Code = 3003
	CodeNotParticipant      Code = 3004
	CodeNotOwner            Code = 3005

	// Resource state.
	CodeNotFound         Code = 4000
	CodeOrderClosed      Code = 4001
	CodeRestaurantLocked Code = 4002
	CodeRestaurantInUse  Code = 4003
	CodeItemUnavailable  Code = 4004
	CodeNameExistsHere   Code = 4005
	CodeMethodNotAllowed Code = 4006

	// Rate limiting.
	CodeTooManyLogins Code = 5000

	// Server.
	CodeInternal            Code = 9000
	CodeDatabaseUnavailable Code = 9001
)

// definition pairs a code with the HTTP status it is reported as and the
// English message that accompanies it.
type definition struct {
	status  int
	message string
}

// registry is the single source for every documented error. A code missing from
// here has no HTTP status, which the tests treat as a defect rather than a
// default.
var registry = map[Code]definition{
	CodeMalformedJSON:          {http.StatusBadRequest, "malformed JSON body"},
	CodeMissingField:           {http.StatusBadRequest, "required field missing"},
	CodeInvalidField:           {http.StatusBadRequest, "field value out of range or wrongly formatted"},
	CodeDeadlineAfterFulfil:    {http.StatusBadRequest, "deadline must be before the fulfilment time"},
	CodeQuantityTooSmall:       {http.StatusBadRequest, "quantity must be at least 1"},
	CodeItemWrongRestaurant:    {http.StatusBadRequest, "menu item does not belong to the order's restaurant"},
	CodeModificationWrongItem:  {http.StatusBadRequest, "modification does not belong to the selected menu item"},
	CodeRestaurantNeedsContact: {http.StatusBadRequest, "restaurant needs at least one contact entry"},
	CodeOpeningHoursZeroLength: {http.StatusBadRequest, "opening hours entry has equal start and end time"},
	CodeUserNameTaken:          {http.StatusBadRequest, "user name already taken"},
	CodePasswordTooWeak:        {http.StatusBadRequest, "password does not meet the minimum requirements"},
	CodeImageTooLarge:          {http.StatusRequestEntityTooLarge, "uploaded image exceeds the configured maximum size"},
	CodeImageUnsupportedType:   {http.StatusUnsupportedMediaType, "unsupported image media type"},
	CodeUnknownCurrency:        {http.StatusBadRequest, "unknown currency code"},
	CodeDeadlineInThePast:      {http.StatusBadRequest, "the deadline is already in the past"},

	CodeNotAuthenticated:  {http.StatusUnauthorized, "not authenticated"},
	CodeInvalidLogin:      {http.StatusUnauthorized, "invalid user name or password"},
	CodeSessionExpired:    {http.StatusUnauthorized, "session expired"},
	CodeSessionSuperseded: {http.StatusUnauthorized, "session superseded by a newer login"},
	CodeInvalidToken:      {http.StatusUnauthorized, "invalid or revoked API token"},
	CodeInvalidCSRF:       {http.StatusForbidden, "missing or invalid CSRF token"},
	CodeAlreadyLoggedIn:   {http.StatusForbidden, "already logged in; log out before registering another account"},

	CodeAdminRequired:       {http.StatusForbidden, "administrator privileges required"},
	CodeNotOrderCreator:     {http.StatusForbidden, "only the order creator may change this order"},
	CodeNotItemOwner:        {http.StatusForbidden, "only the owner may change this order item"},
	CodePlaceholderReadOnly: {http.StatusForbidden, "the deleted-user placeholder cannot be modified"},
	CodeNotParticipant:      {http.StatusForbidden, "only participants of this order may see its summary"},
	CodeNotOwner:            {http.StatusForbidden, "only the owner of this resource may act on it"},

	CodeNotFound:         {http.StatusNotFound, "resource not found"},
	CodeOrderClosed:      {http.StatusConflict, "order deadline has passed; the order is read-only"},
	CodeRestaurantLocked: {http.StatusConflict, "the restaurant cannot be changed once the order has items"},
	CodeRestaurantInUse:  {http.StatusConflict, "the restaurant is still referenced by an order"},
	CodeItemUnavailable:  {http.StatusConflict, "menu item is marked unavailable"},
	CodeNameExistsHere:   {http.StatusConflict, "name already exists within this restaurant"},
	CodeMethodNotAllowed: {http.StatusMethodNotAllowed, "the HTTP method is not allowed on this path"},

	CodeTooManyLogins: {http.StatusTooManyRequests, "too many login attempts"},

	CodeInternal:            {http.StatusInternalServerError, "unexpected server error"},
	CodeDatabaseUnavailable: {http.StatusServiceUnavailable, "database unavailable"},
}

// Status returns the HTTP status a code is reported as.
func (c Code) Status() int {
	if def, ok := registry[c]; ok {
		return def.status
	}
	// An unregistered code is a programming error, reported as one rather than
	// leaking a 200 for a failure.
	return http.StatusInternalServerError
}

// Message returns the English text for a code.
//
// It is developer-facing. The frontend translates the code and falls back to
// this only when it has no translation, so it is never the primary way a user
// learns what went wrong.
func (c Code) Message() string {
	if def, ok := registry[c]; ok {
		return def.message
	}
	return "unexpected server error"
}

// Registered reports whether a code is in the registry.
func (c Code) Registered() bool {
	_, ok := registry[c]
	return ok
}

// Codes lists every registered code, for the tests that check the registry
// against docs/04_api.md.
func Codes() []Code {
	out := make([]Code, 0, len(registry))
	for code := range registry {
		out = append(out, code)
	}
	return out
}

// Error is the failure this API reports. It carries everything the envelope
// needs plus an optional wrapped cause for the log.
type Error struct {
	Code Code

	// Field names the offending request field, for validation errors.
	Field string

	// Detail replaces the registry's message when a specific one helps. It is
	// still developer-facing English and must never contain SQL, a stack trace,
	// a file path or an internal host name.
	Detail string

	// Cause is logged and never sent to the client.
	Cause error
}

func (e *Error) Error() string {
	if e.Detail != "" {
		return e.Detail
	}
	return e.Code.Message()
}

func (e *Error) Unwrap() error { return e.Cause }

// Status is the HTTP status this error is reported as.
func (e *Error) Status() int { return e.Code.Status() }

// Errorf builds an Error with a specific message.
func Errorf(code Code, format string, args ...any) *Error {
	return &Error{Code: code, Detail: fmt.Sprintf(format, args...)}
}

// Newf is Errorf with a wrapped cause, for a failure worth logging in full but
// not worth describing to the client.
func Newf(code Code, cause error, format string, args ...any) *Error {
	return &Error{Code: code, Detail: fmt.Sprintf(format, args...), Cause: cause}
}

// FieldError builds a validation error naming the offending field.
func FieldError(code Code, field, format string, args ...any) *Error {
	return &Error{Code: code, Field: field, Detail: fmt.Sprintf(format, args...)}
}

// envelope is the wire format from docs/04_api.md.
type envelope struct {
	Error envelopeBody `json:"error"`
}

type envelopeBody struct {
	Code      Code   `json:"code"`
	Message   string `json:"message"`
	Field     string `json:"field,omitempty"`
	RequestID string `json:"request_id"`
}

// WriteError sends an error response.
//
// The request ID ties the response a user can see to the log line that has the
// detail, which is what makes "it went wrong just now" traceable without the
// response ever carrying SQL text or a stack trace.
func WriteError(w http.ResponseWriter, r *http.Request, err *Error) {
	body := envelope{Error: envelopeBody{
		Code:      err.Code,
		Message:   err.Error(),
		Field:     err.Field,
		RequestID: RequestIDFrom(r.Context()),
	}}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(err.Status())

	// The status is already written, so a failure here can only be logged, not
	// reported. The middleware sees it through the response writer's state.
	_ = json.NewEncoder(w).Encode(body)
}

// WriteJSON sends a successful JSON response.
func WriteJSON(w http.ResponseWriter, status int, body any) error {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if body == nil {
		return nil
	}
	return json.NewEncoder(w).Encode(body)
}
