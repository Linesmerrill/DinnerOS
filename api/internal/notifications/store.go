package notifications

import (
	"context"
	"time"
)

// Store persists notifications.
//
// Every method is scoped by householdID. Returned lists are newest first
// (descending ID). Malformed IDs match nothing.
type Store interface {
	// Insert stores n, assigning its ID. A notification with the same
	// (householdId, dedupeKey) is ErrDuplicate (wrapped).
	Insert(ctx context.Context, n Notification) (Notification, error)
	// FindByDedupeKey returns the household's notification with key, or
	// ErrNotFound.
	FindByDedupeKey(ctx context.Context, householdID, key string) (Notification, error)
	// List returns the household's notifications matching f, newest first,
	// at most f.Limit of them.
	List(ctx context.Context, householdID string, f ListFilter) ([]Notification, error)
	// CountUnread counts the household's notifications userID hasn't read.
	CountUnread(ctx context.Context, householdID, userID string) (int, error)
	// MarkRead records that userID read the notifications with ids, or every
	// household notification when ids is nil. Unknown IDs are ignored.
	MarkRead(ctx context.Context, householdID, userID string, ids []string) error
}

// Outbox is the push side of the notifications collection: the sweep
// (cmd/sendreminders) takes pending notifications from it and records the
// outcome. MongoStore implements it.
type Outbox interface {
	// PendingPush returns notifications whose push is pending, oldest first,
	// at most limit of them.
	PendingPush(ctx context.Context, limit int) ([]Notification, error)
	// ClaimPush moves one notification from pending to sending, counting the
	// attempt. It reports false when the notification is no longer pending
	// (another sweep claimed it), which is what makes a push go out at most
	// once.
	ClaimPush(ctx context.Context, id string, at time.Time) (bool, error)
	// FinishPush records the outcome of a claimed push: PushSent (sentAt is
	// at), PushSkipped, or PushFailed.
	FinishPush(ctx context.Context, id string, status PushStatus, at time.Time) error
}
