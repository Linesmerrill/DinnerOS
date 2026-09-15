package ratings

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/users"
)

// RecipeChecker reports which recipe IDs belong to a household. The recipes
// module implements it.
type RecipeChecker interface {
	ExistingRecipeIDs(ctx context.Context, householdID string, ids []string) ([]string, error)
}

// UserDirectory looks up raters' display names.
type UserDirectory interface {
	GetUser(ctx context.Context, id string) (users.User, error)
}

// ServiceOptions configures a Service.
type ServiceOptions struct {
	Store   Store
	Recipes RecipeChecker
	Users   UserDirectory
	// Events records recipe.rated and recipe.unrated. Optional.
	Events events.Recorder
	Logger *slog.Logger
	// Now is the clock. Default time.Now.
	Now func() time.Time
}

// Service implements rating use cases.
type Service struct {
	store   Store
	recipes RecipeChecker
	users   UserDirectory
	events  events.Recorder
	logger  *slog.Logger
	now     func() time.Time
}

// NewService returns a Service.
func NewService(opts ServiceOptions) *Service {
	s := &Service{store: opts.Store, recipes: opts.Recipes, users: opts.Users, events: opts.Events, logger: opts.Logger, now: opts.Now}
	if s.logger == nil {
		s.logger = slog.New(slog.DiscardHandler)
	}
	if s.now == nil {
		s.now = time.Now
	}
	return s
}

// Rate creates or replaces the actor's rating of a recipe. It returns a
// *ValidationError for invalid input, ErrNotFound when the recipe isn't the
// household's, and households.ErrForbidden when the actor may not view the
// household. A failure to record the recipe.rated event is logged and does
// not fail the rating.
func (s *Service) Rate(ctx context.Context, actor households.Membership, recipeID string, in RateInput) (Rating, error) {
	if err := authorize(actor); err != nil {
		return Rating{}, err
	}
	v, err := in.validate()
	if err != nil {
		return Rating{}, err
	}
	if err := s.requireRecipe(ctx, actor.HouseholdID, recipeID); err != nil {
		return Rating{}, err
	}
	now := s.now().UTC()
	saved, previous, err := s.store.Upsert(ctx, Rating{
		HouseholdID: actor.HouseholdID, RecipeID: recipeID, UserID: actor.UserID,
		Score: v.score, Comment: v.comment, Tags: v.tags, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return Rating{}, fmt.Errorf("save rating: %w", err)
	}

	payload := events.RecipeRated{Score: saved.Score}
	for _, tag := range saved.Tags {
		payload.Tags = append(payload.Tags, string(tag))
	}
	if previous != nil {
		payload.PreviousScore = previous.Score
	}
	events.RecordOrLog(ctx, s.events, s.logger, events.Event{
		HouseholdID: actor.HouseholdID, UserID: actor.UserID, Type: events.TypeRecipeRated,
		RecipeID: recipeID, OccurredAt: now, Payload: payload,
	})
	return saved, nil
}

// Remove deletes the actor's rating of a recipe. Removing a rating that
// doesn't exist succeeds. It returns ErrNotFound when the recipe isn't the
// household's.
func (s *Service) Remove(ctx context.Context, actor households.Membership, recipeID string) error {
	if err := authorize(actor); err != nil {
		return err
	}
	if err := s.requireRecipe(ctx, actor.HouseholdID, recipeID); err != nil {
		return err
	}
	deleted, err := s.store.Delete(ctx, actor.HouseholdID, recipeID, actor.UserID)
	switch {
	case errors.Is(err, ErrNotFound):
		return nil
	case err != nil:
		return fmt.Errorf("delete rating: %w", err)
	}
	events.RecordOrLog(ctx, s.events, s.logger, events.Event{
		HouseholdID: actor.HouseholdID, UserID: actor.UserID, Type: events.TypeRecipeUnrated,
		RecipeID: recipeID, OccurredAt: s.now().UTC(), Payload: events.RecipeUnrated{PreviousScore: deleted.Score},
	})
	return nil
}

// List returns every member's rating of a recipe, most recently updated
// first, with the household summary (Mine is the actor's rating). It returns
// ErrNotFound when the recipe isn't the household's.
func (s *Service) List(ctx context.Context, actor households.Membership, recipeID string) ([]MemberRating, Summary, error) {
	if err := authorize(actor); err != nil {
		return nil, Summary{}, err
	}
	if err := s.requireRecipe(ctx, actor.HouseholdID, recipeID); err != nil {
		return nil, Summary{}, err
	}
	list, err := s.store.ListForRecipe(ctx, actor.HouseholdID, recipeID)
	if err != nil {
		return nil, Summary{}, fmt.Errorf("list ratings: %w", err)
	}
	names := map[string]string{}
	out := make([]MemberRating, 0, len(list))
	var summary Summary
	for i, r := range list {
		name, ok := names[r.UserID]
		if !ok {
			user, err := s.users.GetUser(ctx, r.UserID)
			switch {
			case err == nil:
				name = user.DisplayName
			case !errors.Is(err, users.ErrNotFound):
				return nil, Summary{}, fmt.Errorf("get rater: %w", err)
			}
			names[r.UserID] = name
		}
		out = append(out, MemberRating{Rating: r, DisplayName: name})
		summary.Count++
		summary.Sum += r.Score
		if r.UserID == actor.UserID {
			summary.Mine = &list[i]
		}
	}
	return out, summary, nil
}

// Summaries returns rating aggregates and userID's own rating for each of
// recipeIDs; recipes without ratings map to the zero Summary. Callers must
// already have authorized userID to view the household.
func (s *Service) Summaries(ctx context.Context, householdID, userID string, recipeIDs []string) (map[string]Summary, error) {
	out := make(map[string]Summary, len(recipeIDs))
	if len(recipeIDs) == 0 {
		return out, nil
	}
	found, err := s.store.Summaries(ctx, householdID, userID, recipeIDs)
	if err != nil {
		return nil, fmt.Errorf("rating summaries: %w", err)
	}
	for _, id := range recipeIDs {
		out[id] = found[id]
	}
	return out, nil
}

func authorize(actor households.Membership) error {
	if !actor.Role.Can(households.PermHouseholdView) {
		return households.ErrForbidden
	}
	return nil
}

func (s *Service) requireRecipe(ctx context.Context, householdID, recipeID string) error {
	ids, err := s.recipes.ExistingRecipeIDs(ctx, householdID, []string{recipeID})
	if err != nil {
		return fmt.Errorf("check recipe: %w", err)
	}
	if len(ids) == 0 {
		return ErrNotFound
	}
	return nil
}
