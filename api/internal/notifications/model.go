// Package notifications keeps a household's in-app notifications, such as
// "Butter is running low". Every notification is addressed to the whole
// household, and each member reads it separately.
//
// The collection is also the outbox for push delivery. A notification is
// created with a pending push status. A future APNs worker will send pending
// notifications to members' devices and record the outcome. Nothing sends
// pushes yet (docs/pantry-usage.md#notifications).
package notifications

import (
	"errors"
	"fmt"
	"time"
)

// Errors returned by stores and the service.
var (
	ErrNotFound = errors.New("notifications: not found")
	// ErrDuplicate means a notification with the same dedupe key exists.
	ErrDuplicate = errors.New("notifications: duplicate")

	errHouseholdRequired = errors.New("notifications: household id is required")
)

// ValidationError describes invalid input. Its message is safe to return to
// API clients.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

func invalid(format string, args ...any) error {
	return &ValidationError{Message: fmt.Sprintf(format, args...)}
}

// Type names what a notification is about. Types are stable: apps may choose
// icons and navigation by type.
type Type string

// Notification types.
const (
	// TypePantryLow: the usage estimate marked a pantry item low.
	TypePantryLow Type = "pantry.low"
)

// SubjectKind is the kind of record a notification points at.
type SubjectKind string

// Subject kinds.
const (
	SubjectPantryItem SubjectKind = "pantry_item"
)

// Subject is the record a notification is about, so an app can open it.
type Subject struct {
	Kind SubjectKind
	ID   string
}

// PushStatus is where a notification is in push delivery.
type PushStatus string

// Push statuses. Only pending is written today; the others are reserved for
// the APNs worker.
const (
	PushPending PushStatus = "pending"
	PushSent    PushStatus = "sent"
	PushFailed  PushStatus = "failed"
	// PushSkipped: nobody in the household had a device to send to.
	PushSkipped PushStatus = "skipped"
)

// Push is the delivery state of a notification's push.
type Push struct {
	Status        PushStatus
	Attempts      int
	LastAttemptAt time.Time
	SentAt        time.Time
}

// Notification is one household notification.
type Notification struct {
	ID          string
	HouseholdID string
	Type        Type
	// Title and Body are ready to display (and to send as a push alert).
	Title   string
	Body    string
	Subject Subject
	// DedupeKey is unique per household. Creating a notification whose key
	// exists returns the existing one, so a producer can retry safely and
	// never alerts twice about the same thing.
	DedupeKey string
	// ReadBy lists the members who have read it.
	ReadBy    []string
	Push      Push
	CreatedAt time.Time
}

// ReadByUser reports whether userID has read n.
func (n Notification) ReadByUser(userID string) bool {
	for _, id := range n.ReadBy {
		if id == userID {
			return true
		}
	}
	return false
}

// New is the input for Service.Create.
type New struct {
	HouseholdID string
	Type        Type
	Title       string
	Body        string
	Subject     Subject
	DedupeKey   string
}

// Limits.
const (
	DefaultListLimit = 50
	MaxListLimit     = 100
	MaxMarkRead      = 100
	maxTitleLength   = 200
	maxBodyLength    = 1000
	maxDedupeKey     = 200
)

// ListFilter selects a household's notifications, newest first.
type ListFilter struct {
	// UnreadBy, when set, keeps notifications that user hasn't read.
	UnreadBy string
	// Before, when set, keeps notifications older than the one with this ID
	// (the previous page's last ID).
	Before string
	Limit  int
}
