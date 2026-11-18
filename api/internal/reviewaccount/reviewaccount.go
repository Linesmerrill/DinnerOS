// Package reviewaccount sets up the App Review account (decisions 752 and
// 759): the first time it signs in it gets a household with eight recipes
// DinnerOS wrote for it, each with a credited Unsplash photo, and never the
// starter library or Plus on the house.
//
// Each sign-in also plans the current week from the seed when nothing is
// planned yet, so the Menu and the grocery list are full whenever a reviewer
// looks. Otherwise a later sign-in leaves the household as the reviewer left
// it, except that the recipes of an earlier seed are swapped for this one's;
// if the reviewer deletes the account, the next sign-in makes a new user and a
// freshly seeded household.
package reviewaccount

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/photosearch"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
	"github.com/Linesmerrill/DinnerOS/api/internal/users"
)

//go:embed recipes.json
var recipesJSON []byte

// Seed is the household and recipes the review account starts with.
type Seed struct {
	Household string `json:"household"`
	// TimeZone is App Review's, in Cupertino. The reviewer can change it.
	TimeZone string `json:"timeZone"`
	// Plan is the week's dinners by recipe name, one a day from the week's
	// first day.
	Plan    []string     `json:"plan"`
	Recipes []SeedRecipe `json:"recipes"`
}

// earlierSeed is the first seed's recipes (build 116). A household that still
// has them gets this seed's recipes in their place.
var earlierSeed = []string{
	"Lemon Garlic Chicken with Green Beans", "Black Bean Tacos with Lime Slaw",
	"Spaghetti with Tomato and Basil", "Coconut Chickpea Curry", "Beef and Broccoli Stir-Fry",
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
	Photo        SeedPhoto        `json:"photo"`
}

// SeedPhoto is a free Unsplash photo of the dish, hotlinked as the Unsplash
// API asks, with its photographer (docs/review-account.md).
type SeedPhoto struct {
	ImageURL     string `json:"imageUrl"`
	Photographer string `json:"photographer"`
	ProfileURL   string `json:"profileUrl"`
	Page         string `json:"page"`
}

// Credit is the photographer, with Unsplash's referral tags on their page.
func (p SeedPhoto) Credit() photosearch.Credit {
	return photosearch.Credit{Name: p.Photographer, ProfileURL: p.ProfileURL + "?utm_medium=referral&utm_source=dinneros"}
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
	if _, err := time.LoadLocation(s.TimeZone); err != nil {
		return Seed{}, fmt.Errorf("reviewaccount: time zone: %w", err)
	}
	for _, name := range s.Plan {
		if _, ok := s.recipe(name); !ok {
			return Seed{}, fmt.Errorf("reviewaccount: the plan names %q, which isn't a seed recipe", name)
		}
	}
	if len(s.Plan) > 7 {
		return Seed{}, fmt.Errorf("reviewaccount: the plan has %d dinners for a 7-day week", len(s.Plan))
	}
	return s, nil
}

func (s Seed) recipe(name string) (SeedRecipe, bool) {
	for _, r := range s.Recipes {
		if r.Name == name {
			return r, true
		}
	}
	return SeedRecipe{}, false
}

// Draft is the recipe as a member's typed recipe.
func (r SeedRecipe) Draft() recipes.Draft {
	d := recipes.Draft{
		Name: r.Name, Headline: r.Headline, Servings: r.Servings, PrepMinutes: r.PrepMinutes, TotalMinutes: r.TotalMinutes,
		Cuisines: r.Cuisines, CuisinesChosen: true, Steps: r.Steps, ImageURL: r.Photo.ImageURL,
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
	KeepPhotoCredit(ctx context.Context, imageURL string, c photosearch.Credit) error
	List(ctx context.Context, householdID string, q recipes.ListQuery) (recipes.ListPage, error)
	Delete(ctx context.Context, householdID, userID, recipeID string) (recipes.DeleteResult, error)
}

// Planner is what seeding needs from the planning module.
type Planner interface {
	Get(ctx context.Context, householdID, week string) (planning.Plan, error)
	AddEntries(ctx context.Context, householdID, userID, week string, in []planning.NewEntry) (planning.Plan, []planning.Entry, error)
}

// Seeder implements auth.ReviewSeeder.
type Seeder struct {
	households Households
	recipes    Recipes
	planner    Planner
	seed       Seed
	now        func() time.Time
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
	return &Seeder{households: h, recipes: r, seed: seed, now: time.Now, logger: logger}, nil
}

// WithPlanner turns on planning the current week at each sign-in and returns s.
// The planning module is made after sign-in, so it comes in separately.
func (s *Seeder) WithPlanner(p Planner) *Seeder {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.planner = p
	return s
}

// ReviewAccountSignedIn gives the review account its household the first
// time: when it belongs to no household. A later sign-in swaps an earlier
// seed's recipes for this one's. Every sign-in plans the current week when
// nothing is planned. If a recipe can't be saved while seeding, the new
// household is removed again so the next sign-in starts over.
func (s *Seeder) ReviewAccountSignedIn(ctx context.Context, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	have, err := s.households.ListForUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("reviewaccount: list households: %w", err)
	}
	if len(have) == 0 {
		h, err := s.create(ctx, userID)
		if err != nil {
			return err
		}
		return s.planWeek(ctx, h, userID)
	}
	for _, uh := range have {
		// Only the household the account made: one it was invited to is left alone.
		if uh.Household.CreatedBy != userID {
			continue
		}
		if err := s.update(ctx, uh.Household.ID, userID); err != nil {
			return err
		}
		return s.planWeek(ctx, uh.Household, userID)
	}
	return nil
}

func (s *Seeder) create(ctx context.Context, userID string) (households.Household, error) {
	h, _, err := s.households.Create(ctx, userID, households.CreateInput{Name: s.seed.Household, TimeZone: s.seed.TimeZone})
	if err != nil {
		return households.Household{}, fmt.Errorf("reviewaccount: create household: %w", err)
	}
	for _, r := range s.seed.Recipes {
		if err := s.add(ctx, h.ID, r); err != nil {
			if delErr := s.households.DeleteEmptiedHousehold(context.WithoutCancel(ctx), h.ID, userID); delErr != nil {
				s.logger.ErrorContext(ctx, "remove half-seeded review household failed", "householdId", h.ID, "error", delErr)
			}
			return households.Household{}, err
		}
	}
	s.logger.InfoContext(ctx, "review household seeded", "householdId", h.ID, "userId", userID, "recipes", len(s.seed.Recipes))
	return h, nil
}

func (s *Seeder) add(ctx context.Context, householdID string, r SeedRecipe) error {
	if r.Photo.ImageURL != "" {
		if err := s.recipes.KeepPhotoCredit(ctx, r.Photo.ImageURL, r.Photo.Credit()); err != nil {
			return fmt.Errorf("reviewaccount: credit %q's photo: %w", r.Name, err)
		}
	}
	if _, err := s.recipes.CreateFromDraft(ctx, householdID, r.Draft(), false); err != nil {
		return fmt.Errorf("reviewaccount: save %q: %w", r.Name, err)
	}
	return nil
}

// update swaps an earlier seed's recipes for this seed's. Recipes the
// reviewer added stay, and so does a household with none of the earlier ones.
func (s *Seeder) update(ctx context.Context, householdID, userID string) error {
	list, err := s.allRecipes(ctx, householdID)
	if err != nil {
		return err
	}
	byName := map[string]string{}
	for _, r := range list {
		byName[r.Name] = r.ID
	}
	var old []string
	for _, name := range earlierSeed {
		if id, ok := byName[name]; ok {
			old = append(old, id)
		}
	}
	if len(old) == 0 {
		return nil
	}
	for _, r := range s.seed.Recipes {
		if _, ok := byName[r.Name]; ok {
			continue
		}
		if err := s.add(ctx, householdID, r); err != nil {
			return err
		}
	}
	for _, id := range old {
		if _, err := s.recipes.Delete(ctx, householdID, userID, id); err != nil {
			return fmt.Errorf("reviewaccount: remove an earlier recipe: %w", err)
		}
	}
	s.logger.InfoContext(ctx, "review household updated", "householdId", householdID, "removed", len(old))
	return nil
}

func (s *Seeder) allRecipes(ctx context.Context, householdID string) ([]recipes.RecipeSummary, error) {
	var out []recipes.RecipeSummary
	q := recipes.ListQuery{Limit: recipes.MaxListLimit}
	for range 20 {
		page, err := s.recipes.List(ctx, householdID, q)
		if err != nil {
			return nil, fmt.Errorf("reviewaccount: list recipes: %w", err)
		}
		out = append(out, page.Items...)
		if page.NextCursor == "" {
			break
		}
		q.Cursor = page.NextCursor
	}
	return out, nil
}

// planWeek plans the seed's dinners for the household's current week when
// nothing is planned for it yet, one a day from the week's first day.
func (s *Seeder) planWeek(ctx context.Context, h households.Household, userID string) error {
	if s.planner == nil || len(s.seed.Plan) == 0 {
		return nil
	}
	loc, err := time.LoadLocation(h.TimeZone)
	if err != nil {
		loc, _ = time.LoadLocation(s.seed.TimeZone)
	}
	first := planning.Day(h.FirstDay())
	week := planning.WeekOfOn(s.now().In(loc), first).String()
	plan, err := s.planner.Get(ctx, h.ID, week)
	if err != nil {
		return fmt.Errorf("reviewaccount: read the week: %w", err)
	}
	if len(plan.Entries) > 0 {
		return nil
	}
	list, err := s.allRecipes(ctx, h.ID)
	if err != nil {
		return err
	}
	ids := map[string]string{}
	for _, r := range list {
		ids[r.Name] = r.ID
	}
	days := weekDays(first)
	var entries []planning.NewEntry
	for i, name := range s.seed.Plan {
		r, _ := s.seed.recipe(name)
		id, ok := ids[name]
		if !ok {
			continue // the reviewer deleted it
		}
		entries = append(entries, planning.NewEntry{RecipeID: id, Day: string(days[i]), Servings: r.Servings})
	}
	if len(entries) == 0 {
		return nil
	}
	if _, _, err := s.planner.AddEntries(ctx, h.ID, userID, week, entries); err != nil {
		return fmt.Errorf("reviewaccount: plan the week: %w", err)
	}
	s.logger.InfoContext(ctx, "review week planned", "householdId", h.ID, "week", week, "dinners", len(entries))
	return nil
}

// weekDays lists the seven days from first.
func weekDays(first planning.Day) []planning.Day {
	all := []planning.Day{
		planning.Monday, planning.Tuesday, planning.Wednesday, planning.Thursday, planning.Friday,
		planning.Saturday, planning.Sunday,
	}
	start := 0
	for i, d := range all {
		if strings.EqualFold(string(d), string(first)) {
			start = i
		}
	}
	return append(all[start:], all[:start]...)
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
