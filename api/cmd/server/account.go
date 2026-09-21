package main

import (
	"log/slog"

	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/Linesmerrill/DinnerOS/api/internal/account"
	"github.com/Linesmerrill/DinnerOS/api/internal/auth"
	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/invitations"
	"github.com/Linesmerrill/DinnerOS/api/internal/mealkit"
	"github.com/Linesmerrill/DinnerOS/api/internal/notifications"
	"github.com/Linesmerrill/DinnerOS/api/internal/pantry"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/push"
	"github.com/Linesmerrill/DinnerOS/api/internal/ratings"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
	"github.com/Linesmerrill/DinnerOS/api/internal/recommendations"
	"github.com/Linesmerrill/DinnerOS/api/internal/shopping"
	"github.com/Linesmerrill/DinnerOS/api/internal/skips"
	"github.com/Linesmerrill/DinnerOS/api/internal/substitutes"
	"github.com/Linesmerrill/DinnerOS/api/internal/users"
)

// newAccountService wires account deletion to every module that stores
// household- or user-owned documents. A module that adds a collection keyed by
// householdId or userId must be added here; TestIntegrationAccountDeletion
// fails for any indexed collection left behind.
func newAccountService(db *mongo.Database, householdService *households.Service, logger *slog.Logger) *account.Service {
	pantryStore := pantry.NewMongoStore(db)
	eventStore := events.NewMongoStore(db)
	ratingStore := ratings.NewMongoStore(db)
	notificationStore := notifications.NewMongoStore(db)
	mealKitStore := mealkit.NewMongoStore(db)
	return account.NewService(account.Options{
		Households: householdService,
		HouseholdData: []account.HouseholdPurger{
			invitations.NewMongoStore(db),
			recipes.NewMongoStore(db),
			planning.NewMongoStore(db),
			pantryStore,
			notificationStore,
			substitutes.NewMongoStore(db),
			shopping.NewMongoStore(db),
			skips.NewMongoStore(db),
			ratingStore,
			eventStore,
			recommendations.NewMongoStore(db),
			mealKitStore,
		},
		UserData: []account.UserPurger{
			ratingStore,
			eventStore,
			pantryStore,
			notificationStore,
			push.NewMongoStore(db),
			mealKitStore, // the member's encrypted meal-kit tokens go with them

			auth.NewMongoSessionStore(db),
			users.NewMongoStore(db), // last: deletes the user record
		},
		Logger: logger,
	})
}
