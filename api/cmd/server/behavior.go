package main

import (
	"log/slog"
	"slices"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/auth"
	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/ratelimit"
	"github.com/Linesmerrill/DinnerOS/api/internal/ratings"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
	"github.com/Linesmerrill/DinnerOS/api/internal/users"
)

// Event ingestion allows each user a burst of 30 batches (up to 3000 events),
// refilling one batch every 10 seconds. An app flushing its queue on launch
// and on backgrounding stays far below this.
const (
	eventIngestRateBurst = 30
	eventIngestRateEvery = 10 * time.Second
)

// behaviorIndexes returns the indexes of the ratings and events modules.
func behaviorIndexes() []mongodb.IndexSet {
	return slices.Concat(ratings.Indexes(), events.Indexes())
}

// behavior holds the ratings and events modules. The event service is the
// events.Recorder other modules record through; the rating service feeds
// householdRating and myRating into recipe responses.
type behavior struct {
	events        *events.Service
	ratings       *ratings.Service
	eventHandler  *events.Handler
	ratingHandler *ratings.Handler
}

func newBehavior(db *mongodb.Client, recipeService *recipes.Service, userService *users.Service, householdService *households.Service, tokens *auth.TokenService, logger *slog.Logger) behavior {
	eventService := events.NewService(events.ServiceOptions{
		Store:   events.NewMongoStore(db.Database()),
		Recipes: recipeService,
		Logger:  logger,
	})
	ratingService := ratings.NewService(ratings.ServiceOptions{
		Store:   ratings.NewMongoStore(db.Database()),
		Recipes: recipeService,
		Users:   userService,
		Events:  eventService,
		Logger:  logger,
	})
	return behavior{
		events:  eventService,
		ratings: ratingService,
		eventHandler: events.NewHandler(events.HandlerOptions{
			Service:    eventService,
			Authorizer: householdService,
			Tokens:     tokens,
			Logger:     logger,
			RateLimit:  ratelimit.New(ratelimit.Options{Burst: eventIngestRateBurst, Every: eventIngestRateEvery}).MiddlewareBy(events.RateLimitKey),
		}),
		ratingHandler: ratings.NewHandler(ratings.HandlerOptions{
			Service:    ratingService,
			Authorizer: householdService,
			Tokens:     tokens,
			Logger:     logger,
		}),
	}
}
