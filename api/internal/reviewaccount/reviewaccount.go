// Package reviewaccount sets up the App Review account (decision 752): the
// first time it signs in it gets a household with five recipes DinnerOS
// wrote for it, and never the starter library or Plus on the house.
//
// Seeding happens once per household. A later sign-in leaves the household as
// the reviewer left it; if the reviewer deletes the account, the next sign-in
// makes a new user and a freshly seeded household.
package reviewaccount

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
	"github.com/Linesmerrill/DinnerOS/api/internal/users"
)

//go:embed recipes.json
var recipesJSON []byte

// Seed is the household and recipes the review account starts with.
type Seed struct {
	Household string `json:"household"`
	// TimeZone is App Review's, in Cupertino. The reviewer can change it.
	TimeZone string       `json:"timeZone"`
	Recipes  []SeedRecipe `json:"recipes"`
}

// SeedRecipe is one recipe, in the shape Write Your Own saves.
type SeedRecipe struct {
	Name         string           `json:"name"`
	Headline     string           `json:"headline"`
	Servings     int              `json:"servings"`
	PrepMinutes  int              `json:"prepMinutes"`
	TotalMinutes int              `json:"totalMinutes"`
	Cuisines     []string         `json:"cuisines"`
	Ingredients  []SeedIngredient `json:"ingredients"`
	Steps        []string         `json:"steps"`
}

// SeedIngredient is one ingredient line.
type SeedIngredient struct {
	Name         string `json:"name"`
	Quantity     string `json:"quantity"`
	Unit         string `json:"unit"`
	Note         string `json:"note"`
	PantryStaple bool   `json:"pantryStaple"`
}

// LoadSeed returns the seed shipped in the binary.
func LoadSeed() (Seed, error) {
	var s Seed
	if err := json.Unmarshal(recipesJSON, &s); err != nil {
		return Seed{}, fmt.Errorf("reviewaccount: read recipes.json: %w", err)
	}
	if s.Household == "" || s.TimeZone == "" || len(s.Recipes) == 0 {
		return Seed{}, fmt.Errorf("reviewaccount: recipes.json has no household name, time zone or recipes")
	}
	return s, nil
}

// Draft is the recipe as a member's typed recipe.
func (r SeedRecipe) Draft() recipes.Draft {
	d := recipes.Draft{
		Name: r.Name, Headline: r.Headline, Servings: r.Servings, PrepMinutes: r.PrepMinutes, TotalMinutes: r.TotalMinutes,
		Cuisines: r.Cuisines, CuisinesChosen: true, Steps: r.Steps,
	}
	for _, in := range r.Ingredients {
		d.Ingredients = append(d.Ingredients, recipes.DraftIngredient{
			Name: in.Name, Quantity: in.Quantity, Unit: in.Unit, Note: in.Note, PantryStaple: in.PantryStaple,
		})
	}
	return d
}

// Households is what seeding needs from the households module.
type Households interface {
	ListForUser(ctx context.Context, userID string) ([]households.UserHousehold, error)
	Create(ctx context.Context, userID string, in households.CreateInput) (households.Household, households.Membership, error)
	DeleteEmptiedHousehold(ctx context.Context, householdID, userID string) error
}

// Recipes is what seeding needs from the recipes module.
type Recipes interface {
	CreateFromDraft(ctx context.Context, householdID string, d recipes.Draft, fromURL bool) (recipes.Recipe, error)
}

// Seeder implements auth.ReviewSeeder.
type Seeder struct {
	households Households
	recipes    Recipes
	seed       Seed
	logger     *slog.Logger
	// mu keeps two sign-ins at once from making two households. The API runs
	// on one dyno; with more, the second household would only be a spare.
	mu sync.Mutex
}

// NewSeeder returns a Seeder with the shipped seed.
func NewSeeder(h Households, r Recipes, logger *slog.Logger) (*Seeder, error) {
	seed, err := LoadSeed()
	if err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Seeder{households: h, recipes: r, seed: seed, logger: logger}, nil
}

// ReviewAccountSignedIn gives the review account its household the first
// time: when it belongs to no household. Otherwise it does nothing. If a
// recipe can't be saved, the new household is removed again so the next
// sign-in starts over.
func (s *Seeder) ReviewAccountSignedIn(ctx context.Context, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	have, err := s.households.ListForUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("reviewaccount: list households: %w", err)
	}
	if len(have) > 0 {
		return nil
	}
	h, _, err := s.households.Create(ctx, userID, households.CreateInput{Name: s.seed.Household, TimeZone: s.seed.TimeZone})
	if err != nil {
		return fmt.Errorf("reviewaccount: create household: %w", err)
	}
	for _, r := range s.seed.Recipes {
		if _, err := s.recipes.CreateFromDraft(ctx, h.ID, r.Draft(), false); err != nil {
			if delErr := s.households.DeleteEmptiedHousehold(context.WithoutCancel(ctx), h.ID, userID); delErr != nil {
				s.logger.ErrorContext(ctx, "remove half-seeded review household failed", "householdId", h.ID, "error", delErr)
			}
			return fmt.Errorf("reviewaccount: save %q: %w", r.Name, err)
		}
	}
	s.logger.InfoContext(ctx, "review household seeded", "householdId", h.ID, "userId", userID, "recipes", len(s.seed.Recipes))
	return nil
}

// IdentityLister is what SkipStarter needs from the users module.
type IdentityLister interface {
	ListIdentities(ctx context.Context, userID string) ([]users.AuthIdentity, error)
}

// HouseholdReader is what SkipStarter needs from the households module.
type HouseholdReader interface {
	GetHousehold(ctx context.Context, id string) (households.Household, error)
}

// SkipStarter reports whether a new household was made by the review
// account, so it gets no starter library: the one the seeder makes, and any
// the reviewer makes in the app. A lookup that fails copies as usual.
func SkipStarter(h HouseholdReader, ids IdentityLister) func(ctx context.Context, householdID string) bool {
	return func(ctx context.Context, householdID string) bool {
		hh, err := h.GetHousehold(ctx, householdID)
		if err != nil || hh.CreatedBy == "" {
			return false
		}
		list, err := ids.ListIdentities(ctx, hh.CreatedBy)
		if err != nil {
			return false
		}
		for _, id := range list {
			if id.Provider == users.ProviderReview {
				return true
			}
		}
		return false
	}
}
