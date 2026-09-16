package users

import (
	"context"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
)

// PurgeUser deletes the user's sign-in identities and then the user. Account
// deletion calls it last, so a failure earlier leaves an account the user can
// still sign in to and delete again. It is idempotent.
func (s *MongoStore) PurgeUser(ctx context.Context, userID string) error {
	if err := mongodb.DeleteByID(ctx, "userId", userID, s.identities); err != nil {
		return err
	}
	return mongodb.DeleteByID(ctx, "_id", userID, s.users)
}
