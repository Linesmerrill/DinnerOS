package notifications

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
)

// PurgeHousehold deletes every document this package stores for the
// household. Account deletion calls it when the household's last member
// deletes their account. It is idempotent.
func (s *MongoStore) PurgeHousehold(ctx context.Context, householdID string) error {
	return mongodb.DeleteByID(ctx, "householdId", householdID, s.notifications)
}

// PurgeUser removes the user from every notification's readBy. The
// notifications stay with the households they belong to.
func (s *MongoStore) PurgeUser(ctx context.Context, userID string) error {
	uid, err := mongodb.ParseID(userID)
	if err != nil {
		return nil
	}
	_, err = s.notifications.UpdateMany(ctx,
		bson.D{{Key: "readBy", Value: uid}},
		bson.D{{Key: "$pull", Value: bson.D{{Key: "readBy", Value: uid}}}})
	if err != nil {
		return fmt.Errorf("notifications: pull readBy: %w", err)
	}
	return nil
}
