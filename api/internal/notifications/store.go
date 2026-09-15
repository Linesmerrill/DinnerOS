package notifications

import "context"

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
