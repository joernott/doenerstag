// Package model holds the domain types shared between the database layer and
// the HTTP handlers.
//
// The types here are deliberately plain: no database tags, no JSON tags, no
// methods that reach out to anything. The db package knows how to read them and
// the api package knows how to render them, and neither has to agree with the
// other about anything but these shapes. See docs/03_data_model.md.
package model

import (
	"time"

	"github.com/google/uuid"
)

// DeletedUserID is the placeholder that owns order items whose user has been
// deleted, so an expired order still reads "1x Döner, no onions" without naming
// anyone.
//
// The value is fixed in migration 000002 and referenced from application code,
// which is why it is a constant here rather than a lookup.
var DeletedUserID = uuid.MustParse("00000000-0000-7000-8000-000000000000")

// AdministratorName is the one account that may hold is_admin.
const AdministratorName = "root"

// User is a registered account.
type User struct {
	ID          uuid.UUID
	Name        string
	DisplayName string // empty means "fall back to Name"
	Email       string // empty means not given; never verified, never mailed
	IsAdmin     bool
	LastLoginAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Label is the name to show in the interface.
//
// docs/05_auth_and_permissions.md makes the display name optional and says it
// falls back to the user name. Doing that here rather than at each call site is
// what keeps the fallback from being forgotten in one template out of ten.
func (u User) Label() string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return u.Name
}

// IsPlaceholder reports whether this is the deleted-user placeholder, which
// must never be modified, logged into or deleted.
func (u User) IsPlaceholder() bool {
	return u.ID == DeletedUserID
}
