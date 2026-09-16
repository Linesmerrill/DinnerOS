package pantry

import (
	"context"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
)

// PurgeHousehold deletes every document this package stores for the
// household. Account deletion calls it when the household's last member
// deletes their account. It is idempotent.
func (s *MongoStore) PurgeHousehold(ctx context.Context, householdID string) error {
	return mongodb.DeleteByID(ctx, "householdId", householdID, s.items, s.purchases, s.cookUsage, s.settings)
}

// PurgeUser removes the user from the cook usage they recorded in households
// they shared. The usage stays with the household's pantry.
func (s *MongoStore) PurgeUser(ctx context.Context, userID string) error {
	return mongodb.UnsetID(ctx, s.cookUsage, "userId", userID)
}
