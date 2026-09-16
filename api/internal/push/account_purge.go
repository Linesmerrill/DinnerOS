package push

import (
	"context"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
)

// PurgeUser deletes every device token registered to the user. Account
// deletion calls it. It is idempotent.
func (s *MongoStore) PurgeUser(ctx context.Context, userID string) error {
	return mongodb.DeleteByID(ctx, "userId", userID, s.tokens)
}
