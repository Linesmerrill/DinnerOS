package companions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// RecipeReader reads the household's recipes. *recipes.Service implements
// it.
type RecipeReader interface {
	Catalog(ctx context.Context, householdID string) ([]recipes.Recipe, error)
	Get(ctx context.Context, householdID, id string) (recipes.Recipe, error)
}

// Hint is what a household's pairing rules say about an add-on recipe, for
// migration: the cuisines and meal categories rules pair it with.
type Hint struct {
	RecipeID       string
	Cuisines       []string
	MealCategories []string
}

// HintSource reads migration hints. *recommendations.Service implements it.
type HintSource interface {
	CompanionHints(ctx context.Context, householdID string) ([]Hint, error)
}

// Options configures a Service.
type Options struct {
	Store   Store
	Recipes RecipeReader
	// Hints is optional; without it migrated companions pair with nothing
	// until a member edits them.
	Hints HintSource
	// CanonicalCuisine and CuisineLabel are Autopilot's cuisine vocabulary
	// (recommendations.CanonicalCuisine, recommendations.CuisineLabel).
	// MealCategory reports a known meal category. All optional.
	CanonicalCuisine func(string) string
	CuisineLabel     func(string) string
	MealCategory     func(string) bool
	Logger           *slog.Logger
	Now              func() time.Time
}

// Service manages companions. Like the other household services it takes
// IDs that HTTP routes have already authorized.
type Service struct {
	opts   Options
	logger *slog.Logger
	now    func() time.Time
}

// NewService returns a Service.
func NewService(opts Options) *Service {
	s := &Service{opts: opts, logger: opts.Logger, now: opts.Now}
	if s.logger == nil {
		s.logger = slog.New(slog.DiscardHandler)
	}
	if s.now == nil {
		s.now = time.Now
	}
	return s
}

// WithHints sets the migration hint source, and returns s. The hint source
// (Autopilot) is built after this service, so it is set here.
func (s *Service) WithHints(h HintSource) *Service {
	s.opts.Hints = h
	return s
}

// CuisineLabel returns a cuisine's display label, or "".
func (s *Service) CuisineLabel(value string) string {
	if s.opts.CuisineLabel == nil {
		return ""
	}
	return s.opts.CuisineLabel(value)
}

var hexID = regexp.MustCompile(`^[0-9a-f]{24}$`)

// List returns the household's companions, oldest first, making companions
// for add-on recipes that don't have one yet. Recipe names are read fresh
// from the household's recipes.
func (s *Service) List(ctx context.Context, householdID string) ([]Companion, error) {
	catalog, err := s.opts.Recipes.Catalog(ctx, householdID)
	if err != nil {
		return nil, fmt.Errorf("load recipes: %w", err)
	}
	return s.ForEvaluation(ctx, householdID, catalog)
}

// ForEvaluation is List with the catalog the caller already loaded.
func (s *Service) ForEvaluation(ctx context.Context, householdID string, catalog []recipes.Recipe) ([]Companion, error) {
	if err := s.migrate(ctx, householdID, catalog); err != nil {
		// Migration is a convenience: companions made by hand still work.
		s.logger.WarnContext(ctx, "make companions from add-ons", "householdId", householdID, "error", err)
	}
	list, err := s.opts.Store.List(ctx, householdID, false)
	if err != nil {
		return nil, err
	}
	names := make(map[string]string, len(catalog))
	for _, r := range catalog {
		names[r.ID] = r.Name
	}
	for i := range list {
		freshNames(&list[i], names)
	}
	return list, nil
}

// Active returns the visible companions without migrating or loading
// recipes, for the grocery list's quick check.
func (s *Service) Active(ctx context.Context, householdID string) ([]Companion, error) {
	return s.opts.Store.List(ctx, householdID, false)
}

func freshNames(c *Companion, names map[string]string) {
	if c.LinkedRecipe != nil {
		if n, ok := names[c.LinkedRecipe.ID]; ok {
			c.LinkedRecipe.Name = n
		}
	}
	for j := range c.Recipes {
		if n, ok := names[c.Recipes[j].ID]; ok {
			c.Recipes[j].Name = n
		}
	}
}

// migrate makes one companion per add-on recipe that has none, hidden or
// not: named for the recipe, linked to it, Just Suggest It, and paired with
// what the household's pairing rules pair it with. The recipe is not
// changed. Running it again does nothing.
func (s *Service) migrate(ctx context.Context, householdID string, catalog []recipes.Recipe) error {
	var addons []recipes.Recipe
	for _, r := range catalog {
		if r.IsAddon && hexID.MatchString(r.ID) {
			addons = append(addons, r)
		}
	}
	if len(addons) == 0 {
		return nil
	}
	existing, err := s.opts.Store.List(ctx, householdID, true)
	if err != nil {
		return err
	}
	linked := map[string]bool{}
	for _, c := range existing {
		if c.LinkedRecipe != nil {
			linked[c.LinkedRecipe.ID] = true
		}
	}
	addons = slices.DeleteFunc(addons, func(r recipes.Recipe) bool { return linked[r.ID] })
	if len(addons) == 0 {
		return nil
	}
	hints := map[string]Hint{}
	if s.opts.Hints != nil {
		list, err := s.opts.Hints.CompanionHints(ctx, householdID)
		if err != nil {
			s.logger.WarnContext(ctx, "load companion hints", "householdId", householdID, "error", err)
		}
		for _, h := range list {
			hints[h.RecipeID] = h
		}
	}
	now := s.now().UTC()
	var errs []error
	for _, r := range addons {
		h := hints[r.ID]
		c := Companion{
			HouseholdID: householdID, Name: truncate(r.Name, MaxNameLength), LinkedRecipe: &RecipeRef{ID: r.ID, Name: r.Name},
			Cuisines: h.Cuisines, MealCategories: h.MealCategories, Frequency: FrequencyNone, Mode: ModeSuggest,
			CreatedAt: now, UpdatedAt: now,
		}
		if c.Name == "" {
			c.Name = "Add-on"
		}
		created, err := s.opts.Store.EnsureLinked(ctx, c)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if created {
			s.logger.InfoContext(ctx, "companion made from an add-on", "householdId", householdID, "recipeId", r.ID)
		}
	}
	return errors.Join(errs...)
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// Get returns one companion.
func (s *Service) Get(ctx context.Context, householdID, id string) (Companion, error) {
	return s.opts.Store.Get(ctx, householdID, id)
}

// Create stores a new companion.
func (s *Service) Create(ctx context.Context, householdID, userID string, in Input) (Companion, error) {
	c, err := s.build(ctx, householdID, in)
	if err != nil {
		return Companion{}, err
	}
	existing, err := s.opts.Store.List(ctx, householdID, false)
	if err != nil {
		return Companion{}, err
	}
	made := 0
	for _, e := range existing {
		if !e.Migrated() {
			made++
		}
	}
	if made >= MaxPerHousehold {
		return Companion{}, invalid("You have %d companions. Delete one to add another.", MaxPerHousehold)
	}
	now := s.now().UTC()
	c.HouseholdID, c.CreatedBy, c.CreatedAt, c.UpdatedAt = householdID, userID, now, now
	return s.opts.Store.Insert(ctx, c)
}

// Update replaces what a member edits on a companion. Its link to an add-on
// recipe and when it was last on a list stay.
func (s *Service) Update(ctx context.Context, householdID, id string, in Input) (Companion, error) {
	current, err := s.opts.Store.Get(ctx, householdID, id)
	if err != nil {
		return Companion{}, err
	}
	c, err := s.build(ctx, householdID, in)
	if err != nil {
		return Companion{}, err
	}
	c.ID, c.HouseholdID, c.UpdatedAt = current.ID, householdID, s.now().UTC()
	return s.opts.Store.Update(ctx, c)
}

// Delete removes a companion. One made from an add-on recipe is hidden
// instead, so it isn't made again; the recipe itself is never touched.
func (s *Service) Delete(ctx context.Context, householdID, id string) error {
	c, err := s.opts.Store.Get(ctx, householdID, id)
	if err != nil {
		return err
	}
	if c.Migrated() {
		return s.opts.Store.Hide(ctx, householdID, id, s.now())
	}
	return s.opts.Store.Delete(ctx, householdID, id)
}

// MarkListed records that a companion went on week's grocery list (or its
// recipe into week's plan).
func (s *Service) MarkListed(ctx context.Context, householdID, id, week string, at time.Time) error {
	return s.opts.Store.MarkListed(ctx, householdID, id, week, at)
}

// build validates in and resolves its recipes.
func (s *Service) build(ctx context.Context, householdID string, in Input) (Companion, error) {
	n, err := in.normalize(s.opts.CanonicalCuisine, s.opts.MealCategory)
	if err != nil {
		return Companion{}, err
	}
	c := Companion{
		Name: n.Name, StoreItem: n.StoreItem, Quantity: n.Quantity, Unit: n.Unit, ProductURL: n.ProductURL,
		Cuisines: n.Cuisines, MealCategories: n.MealCategories, Frequency: n.Frequency, Mode: n.Mode,
	}
	for _, id := range n.RecipeIDs {
		if !hexID.MatchString(id) {
			return Companion{}, invalid("One of those recipes isn't in your recipes anymore.")
		}
		r, err := s.opts.Recipes.Get(ctx, householdID, id)
		switch {
		case errors.Is(err, recipes.ErrNotFound):
			return Companion{}, invalid("One of those recipes isn't in your recipes anymore.")
		case err != nil:
			return Companion{}, fmt.Errorf("load recipe: %w", err)
		}
		c.Recipes = append(c.Recipes, RecipeRef{ID: r.ID, Name: r.Name})
	}
	return c, nil
}
