package events

import (
	"context"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
)

// PurgeHousehold deletes every document this package stores for the
// household. Account deletion calls it when the household's last member
// deletes their account. It is idempotent.
func (s *MongoStore) PurgeHousehold(ctx context.Context, householdID string) error {
	return mongodb.DeleteByID(ctx, "householdId", householdID, s.events)
}

// PurgeUser removes the user from the events they recorded in households they
// shared. The events stay: they are the household's history, not the user's.
func (s *MongoStore) PurgeUser(ctx context.Context, userID string) error {
	return mongodb.UnsetID(ctx, s.events, "userId", userID)
}
