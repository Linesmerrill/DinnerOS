// Package notifications keeps a household's in-app notifications, such as
// "Butter is running low". Every notification is addressed to the whole
// household, and each member reads it separately.
//
// The collection is also the outbox for push delivery. A notification is
// created with a pending push status, and the push sweep (cmd/sendreminders,
// internal/push) claims it, sends it to members' devices, and records the
// outcome (docs/pantry-usage.md#push-delivery).
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
	// TypeShoppingOrderDue: the household's order day has arrived and nobody
	// has marked this week's groceries ordered.
	TypeShoppingOrderDue Type = "shopping.order_due"
	// TypePantryThaw: a frozen pantry item is needed for one of today's
	// meals and should move to the fridge
	// (docs/pantry-usage.md#thaw-reminders).
	TypePantryThaw Type = "pantry.thaw"
	// TypePlusReferral: an invite counted and a free month of Plus is on
	// (decision 698). It shows as a banner until each member has seen it.
	TypePlusReferral Type = "plus.referral"
)

// SubjectKind is the kind of record a notification points at.
type SubjectKind string

// Subject kinds.
const (
	SubjectPantryItem SubjectKind = "pantry_item"
	// SubjectShoppingWeek is an ISO week ("2026-W38"): the week to open.
	SubjectShoppingWeek SubjectKind = "shopping_week"
	// SubjectPlus is the household's Plus screen; ID is empty.
	SubjectPlus SubjectKind = "plus"
)

// Subject is the record a notification is about, so an app can open it.
type Subject struct {
	Kind SubjectKind
	ID   string
}

// PushStatus is where a notification is in push delivery.
type PushStatus string

// Push statuses. Producers write pending; the push sweep writes the rest.
const (
	PushPending PushStatus = "pending"
	// PushSending: a sweep claimed it. A sweep that dies mid-send leaves it
	// here for good, so a push is never sent twice.
	PushSending PushStatus = "sending"
	// PushSent: at least one device accepted it.
	PushSent PushStatus = "sent"
	// PushFailed: every device send failed.
	PushFailed PushStatus = "failed"
	// PushSkipped: nothing was sent — no member who hadn't read it had a
	// device, or it was too old to be news.
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
	ReadBy []string
	// DismissedBy lists the members who swiped it away. It's gone for them
	// and stays for the rest of the household.
	DismissedBy []string
	// Banner shows the notification at the top of the app, the next time a
	// member opens it, until that member has read it (seen the push or the
	// banner).
	Banner    bool
	Push      Push
	CreatedAt time.Time
	// GroupIDs, set only on a read, are the IDs of every notification this
	// one stands for when repeats are shown as one (group.go), newest first.
	GroupIDs []string
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
	// Banner: see Notification.Banner.
	Banner bool
}

// Limits.
const (
	DefaultListLimit = 50
	MaxListLimit     = 100
	MaxMarkRead      = 100
	// MaxBanners is how many banners a read returns.
	MaxBanners     = 5
	maxTitleLength = 200
	maxBodyLength  = 1000
	maxDedupeKey   = 200
)

// ListFilter selects a household's notifications, newest first.
type ListFilter struct {
	// UnreadBy, when set, keeps notifications that user hasn't read.
	UnreadBy string
	// Viewer, when set, drops notifications that user dismissed.
	Viewer string
	// Before, when set, keeps notifications older than the one with this ID
	// (the previous page's last ID).
	Before string
	// BannerOnly keeps notifications shown as banners.
	BannerOnly bool
	Limit      int
}
