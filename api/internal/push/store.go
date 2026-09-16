package push

import "context"

// Store persists device tokens.
type Store interface {
	// Upsert stores t keyed by its token, replacing the user and environment
	// of an existing registration and keeping its CreatedAt.
	Upsert(ctx context.Context, t DeviceToken) (DeviceToken, error)
	// Delete removes the token if it belongs to userID. Deleting a token
	// that isn't registered to that user is not an error.
	Delete(ctx context.Context, userID, token string) error
	// DeleteToken removes the token whoever it belongs to, for tokens APNs
	// reports as no longer valid.
	DeleteToken(ctx context.Context, token string) error
	// ListByUsers returns every token registered to any of userIDs.
	ListByUsers(ctx context.Context, userIDs []string) ([]DeviceToken, error)
}
