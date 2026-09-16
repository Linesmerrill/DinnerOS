package auth

import (
	"context"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
)

// PurgeUser deletes every session (refresh token) the user has, so no device
// can refresh into the deleted account. Access tokens already issued stay
// valid until they expire (at most 15 minutes); GET /me answers 401 for them.
func (s *MongoSessionStore) PurgeUser(ctx context.Context, userID string) error {
	return mongodb.DeleteByID(ctx, "userId", userID, s.sessions)
}
