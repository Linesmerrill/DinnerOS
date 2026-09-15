// Package households owns Household and HouseholdMembership, including the
// role/permission model used for server-side authorization.
//
// A household always has at least one admin. The guard is an adminCount kept
// on the household document and changed only with conditional updates, so two
// concurrent demotions or removals can never both take away the last admin,
// even without multi-document transactions.
package households

import (
	"errors"
	"strings"
	"time"
	_ "time/tzdata" // time-zone validation must not depend on the host's zoneinfo (the Alpine image has none)
	"unicode/utf8"
)

// Errors returned by the store and service.
var (
	// ErrNotFound means the household or membership does not exist, or the
	// caller is not a member (callers must not learn which).
	ErrNotFound = errors.New("households: not found")
	// ErrDuplicate means a unique constraint was violated.
	ErrDuplicate = errors.New("households: duplicate")
	// ErrForbidden means the caller is a member but lacks the permission.
	ErrForbidden = errors.New("households: forbidden")
	// ErrLastAdmin means the change would leave the household without an admin.
	ErrLastAdmin = errors.New("households: household must keep at least one admin")
	// ErrConflict means a concurrent change won; the caller may retry.
	ErrConflict = errors.New("households: concurrent change")
)

// ValidationError describes invalid input. Its message is safe to return to
// API clients.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

func invalid(msg string) error { return &ValidationError{Message: msg} }

// Limits and defaults for household fields.
const (
	DefaultServings   = 2
	MinServings       = 1
	MaxServings       = 12
	MaxNameLength     = 100
	maxTimeZoneLength = 64
)

// Household is a group of people who plan and shop together. It is the data
// isolation boundary for every household-owned document.
type Household struct {
	ID              string
	Name            string
	DefaultServings int
	// TimeZone is an IANA name (e.g. "America/Denver"); plan dates are
	// interpreted in it.
	TimeZone  string
	CreatedBy string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Membership places a user in a household with a role. A user may belong to
// several households.
type Membership struct {
	ID          string
	HouseholdID string
	UserID      string
	Role        Role
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Member is a membership with the user's display name, for member lists.
type Member struct {
	UserID      string
	DisplayName string
	Role        Role
	JoinedAt    time.Time
}

// UserHousehold pairs a household with the caller's membership in it.
type UserHousehold struct {
	Household  Household
	Membership Membership
}

// CreateInput is the input for Service.Create. A nil DefaultServings uses
// DefaultServings.
type CreateInput struct {
	Name            string
	TimeZone        string
	DefaultServings *int
}

// UpdateInput is a partial update; nil fields are left unchanged.
type UpdateInput struct {
	Name            *string
	TimeZone        *string
	DefaultServings *int
}

// HouseholdPatch holds validated field changes for Store.UpdateHousehold.
type HouseholdPatch struct {
	Name            *string
	TimeZone        *string
	DefaultServings *int
}

func normalizeName(name string) (string, error) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return "", invalid("name is required")
	case utf8.RuneCountInString(name) > MaxNameLength:
		return "", invalid("name must be at most 100 characters")
	}
	return name, nil
}

// normalizeTimeZone accepts IANA zone names only. "Local" is rejected because
// it means the server's zone, which is meaningless to a household.
func normalizeTimeZone(tz string) (string, error) {
	tz = strings.TrimSpace(tz)
	if tz == "" {
		return "", invalid("timeZone is required")
	}
	if len(tz) > maxTimeZoneLength || tz == "Local" {
		return "", invalid("timeZone must be an IANA time zone name such as America/Denver")
	}
	if _, err := time.LoadLocation(tz); err != nil {
		return "", invalid("timeZone must be an IANA time zone name such as America/Denver")
	}
	return tz, nil
}

func validateServings(n int) error {
	if n < MinServings || n > MaxServings {
		return invalid("defaultServings must be between 1 and 12")
	}
	return nil
}
