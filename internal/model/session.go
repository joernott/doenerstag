package model

import (
	"time"

	"github.com/google/uuid"
)

// Session is the server-side half of a browser session.
//
// The JWT in the cookie is a signed reference to this row. The row is what
// makes an idle timeout and the one-session-per-user rule expressible at all;
// see docs/adr/0004-jwt-with-server-side-sessions.md.
type Session struct {
	ID              uuid.UUID
	UserID          uuid.UUID
	IssuedAt        time.Time
	LastSeenAt      time.Time
	AbsoluteExpires time.Time
	RemoteAddr      string
	UserAgent       string
}

// Expired reports whether the session has passed its absolute lifetime.
func (s Session) Expired(now time.Time) bool {
	return !now.Before(s.AbsoluteExpires)
}

// Idle reports whether the session has gone untouched for longer than the idle
// timeout.
func (s Session) Idle(now time.Time, timeout time.Duration) bool {
	return now.Sub(s.LastSeenAt) > timeout
}

// APIToken is a long-lived credential for scripted access.
//
// The token value itself is never stored: only its SHA-256 hash, and a prefix
// long enough to tell two of a user's tokens apart in a list.
type APIToken struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	Name       string
	Prefix     string
	ExpiresAt  *time.Time
	LastUsedAt *time.Time
	CreatedAt  time.Time
}

// Expired reports whether the token has passed its expiry date. A token with no
// expiry never expires and is revoked by deleting it.
func (t APIToken) Expired(now time.Time) bool {
	return t.ExpiresAt != nil && !now.Before(*t.ExpiresAt)
}
