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
	"fmt"
	"slices"
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
	// MaxMealKitWeeklyCents and MaxMealKitMeals bound the meal kit baseline:
	// $10,000 a week, and three meals a day.
	MaxMealKitWeeklyCents = 1_000_000
	MaxMealKitMeals       = 21
)

// MealKit is a household's meal kit spend: WeeklyCents (US cents) for Meals
// meals a week, such as $130 for 5 meals. Shopping compares a week's grocery
// cost per meal with WeeklyCents ÷ Meals (docs/grocery-engine.md#weekly-cost).
type MealKit struct {
	WeeklyCents int64
	Meals       int
}

// PerMealCents is the baseline cost of one meal, rounded to the nearest cent.
func (m MealKit) PerMealCents() int64 {
	if m.Meals <= 0 {
		return 0
	}
	return (m.WeeklyCents + int64(m.Meals)/2) / int64(m.Meals)
}

func validateMealKit(m *MealKit) error {
	if m == nil {
		return nil
	}
	if m.WeeklyCents < 1 || m.WeeklyCents > MaxMealKitWeeklyCents {
		return invalid(fmt.Sprintf("mealKit.weeklyCents must be between 1 and %d", MaxMealKitWeeklyCents))
	}
	if m.Meals < 1 || m.Meals > MaxMealKitMeals {
		return invalid(fmt.Sprintf("mealKit.meals must be between 1 and %d", MaxMealKitMeals))
	}
	return nil
}

// Household is a group of people who plan and shop together. It is the data
// isolation boundary for every household-owned document.
type Household struct {
	ID              string
	Name            string
	DefaultServings int
	// TimeZone is an IANA name (e.g. "America/Denver"); plan dates are
	// interpreted in it.
	TimeZone string
	// OrderDay is the weekday the household means to place its grocery order
	// ("mon".."sun"), or "" when they haven't chosen one. It drives the
	// shopping order reminder (docs/shopping-providers.md#order-reminders).
	OrderDay string
	// WeekStartsOn is the first day of the household's week ("sun".."sat"). It
	// decides which dates each week covers (planning.Week.DateOn). "" is a
	// household created before the setting existed, whose weeks start on
	// Monday; read it through FirstDay.
	WeekStartsOn string
	// ThawReminderHour is the local hour (0–23) the household wants its
	// thaw reminders, or nil for DefaultThawReminderHour. Moving a frozen
	// item to the fridge is a morning errand, so the default is early and
	// the setting exists because not every household's morning is the same
	// (docs/pantry-usage.md#thaw-reminders).
	ThawReminderHour *int
	// MealKit is what the household spent on meal kits, the baseline the
	// weekly grocery cost is compared with, or nil when not set.
	MealKit   *MealKit
	CreatedBy string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// DefaultThawReminderHour is when thaw reminders go out for a household
// that hasn't chosen an hour.
const DefaultThawReminderHour = 6

// ThawHour returns the local hour the household wants thaw reminders.
func (h Household) ThawHour() int {
	if h.ThawReminderHour == nil {
		return DefaultThawReminderHour
	}
	return *h.ThawReminderHour
}

// FirstDay returns the day the household's week starts on, "mon" for a
// household that never chose one.
func (h Household) FirstDay() string {
	if h.WeekStartsOn == "" {
		return LegacyWeekStart
	}
	return h.WeekStartsOn
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
	// WeekStartsOn nil uses DefaultWeekStart.
	WeekStartsOn *string
}

// UpdateInput is a partial update; nil fields are left unchanged. A non-nil
// OrderDay holding "" clears the household's order day.
type UpdateInput struct {
	Name            *string
	TimeZone        *string
	DefaultServings *int
	OrderDay        *string
	WeekStartsOn    *string
	// SetThawReminderHour changes the thaw reminder hour to
	// ThawReminderHour; nil returns the household to the default.
	SetThawReminderHour bool
	ThawReminderHour    *int
	// SetMealKit changes the meal kit baseline to MealKit; nil clears it.
	SetMealKit bool
	MealKit    *MealKit
}

// HouseholdPatch holds validated field changes for Store.UpdateHousehold.
type HouseholdPatch struct {
	Name            *string
	TimeZone        *string
	DefaultServings *int
	OrderDay        *string
	WeekStartsOn    *string
	// SetThawReminderHour sets ThawReminderHour, or removes it when
	// ThawReminderHour is nil.
	SetThawReminderHour bool
	ThawReminderHour    *int
	// SetMealKit sets MealKit, or removes it when MealKit is nil.
	SetMealKit bool
	MealKit    *MealKit
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

// OrderDays are the weekday codes an order day may take, Monday first. They
// match planning.Day, without depending on that package.
var OrderDays = []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}

// validateThawReminderHour accepts a local hour of the day.
func validateThawReminderHour(hour *int) error {
	if hour != nil && (*hour < 0 || *hour > 23) {
		return invalid("thawReminderHour must be between 0 and 23, or null")
	}
	return nil
}

// normalizeOrderDay accepts a weekday code, or "" meaning no order day.
func normalizeOrderDay(day string) (string, error) {
	day = strings.TrimSpace(day)
	if day == "" {
		return "", nil
	}
	if !slices.Contains(OrderDays, day) {
		return "", invalid("orderDay must be one of mon, tue, wed, thu, fri, sat, sun, or empty to clear it")
	}
	return day, nil
}

// DefaultWeekStart is the first day of the week for new households, and
// LegacyWeekStart the first day of households created before they could
// choose (ISO weeks). They match planning.DefaultWeekStart and
// planning.LegacyWeekStart.
const (
	DefaultWeekStart = "sun"
	LegacyWeekStart  = "mon"
)

// normalizeWeekStart accepts a weekday code.
func normalizeWeekStart(day string) (string, error) {
	day = strings.TrimSpace(day)
	if !slices.Contains(OrderDays, day) {
		return "", invalid("weekStartsOn must be one of sun, mon, tue, wed, thu, fri, sat")
	}
	return day, nil
}

func validateServings(n int) error {
	if n < MinServings || n > MaxServings {
		return invalid("defaultServings must be between 1 and 12")
	}
	return nil
}
