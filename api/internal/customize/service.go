package customize

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
	"github.com/Linesmerrill/DinnerOS/api/internal/recommendations"
)

// Errors returned by the service. Planning errors (planning.ErrNotFound,
// planning.ErrFinalized, planning.ErrInvalidWeek) pass through.
var (
	// ErrInvalid wraps a problem with a request; its message is safe to show.
	ErrInvalid = errors.New("customize: invalid customization")
	// ErrRecipeNotFound means the recipe isn't in the household.
	ErrRecipeNotFound = errors.New("customize: recipe not found")
)

// MaxSelections bounds a customization request.
const MaxSelections = events.MaxCustomizationChanges

// PlanService is the planner. *planning.Service implements it.
type PlanService interface {
	Get(ctx context.Context, householdID, week string) (planning.Plan, error)
	SetEntryCustomizations(ctx context.Context, householdID, week, entryID string, list []planning.Customization) (planning.Plan, error)
	FindEntry(ctx context.Context, householdID, entryID string, near time.Time) (planning.Plan, planning.Entry, bool, error)
}

// RecipeReader loads household recipes. *recipes.Service implements it.
type RecipeReader interface {
	Get(ctx context.Context, householdID, id string) (recipes.Recipe, error)
}

// Catalog reads the global ingredient catalog. *recipes.Service implements
// it.
type Catalog interface {
	IngredientsByID(ctx context.Context, ids []string) ([]recipes.Ingredient, error)
	IngredientsByKey(ctx context.Context, keys []string) ([]recipes.Ingredient, error)
}

// ProfileSource reads the household's Autopilot profile for its restrictions.
// *recommendations.Service implements it.
type ProfileSource interface {
	Profile(ctx context.Context, householdID string) (recommendations.Profile, error)
}

// ServiceOptions configures a Service.
type ServiceOptions struct {
	// Table defaults to DefaultTable.
	Table   *Table
	Plans   PlanService
	Recipes RecipeReader
	// Catalog is optional; without it swaps have no catalog ID or image.
	Catalog Catalog
	// Profiles is optional; without it no choices are filtered.
	Profiles ProfileSource
	// Events is optional; without it customizations record nothing.
	Events events.Recorder
	Logger *slog.Logger
	Now    func() time.Time
}

// Service computes, validates, stores, and applies meal customizations.
type Service struct {
	table    *Table
	plans    PlanService
	recipes  RecipeReader
	catalog  Catalog
	profiles ProfileSource
	events   events.Recorder
	logger   *slog.Logger
	now      func() time.Time
}

// NewService returns a Service.
func NewService(o ServiceOptions) *Service {
	s := &Service{
		table: o.Table, plans: o.Plans, recipes: o.Recipes, catalog: o.Catalog, profiles: o.Profiles,
		events: o.Events, logger: o.Logger, now: o.Now,
	}
	if s.table == nil {
		s.table = DefaultTable()
	}
	if s.logger == nil {
		s.logger = slog.New(slog.DiscardHandler)
	}
	if s.now == nil {
		s.now = time.Now
	}
	return s
}

// Query selects the serving size of a recipe's customization options.
type Query struct {
	// Servings is one of the recipe's sizes; 0 means the entry's servings, or
	// the recipe's smallest size without an entry.
	Servings int
	// EntryID and Week name a plan entry of the recipe; both or neither.
	EntryID string
	Week    string
}

// Options are a recipe's customization groups at one serving size.
type Options struct {
	RecipeID string
	Servings int
	Groups   []Group
}

// Group is one protein line and its choices.
type Group struct {
	Line     Line
	ImageURL string
	// Selected is the entry's choice ("original" when not customized), or ""
	// when no entry was given.
	Selected string
	Choices  []ChoiceOption
}

// ChoiceOption is a choice with what it cooks.
type ChoiceOption struct {
	Choice
	IngredientName string
	Quantity       ingredients.Quantity
	Unit           string
	ImageURL       string
}

// Selection is a requested choice for one line.
type Selection struct {
	IngredientKey string
	ChoiceID      string
}

// Options returns the recipe's customization groups. Only protein lines of
// the curated table with a weight get a group. Choices the household's
// restrictions rule out are dropped, except the original and an entry's
// current choice.
func (s *Service) Options(ctx context.Context, householdID, recipeID string, q Query) (Options, error) {
	r, err := s.recipe(ctx, householdID, recipeID)
	if err != nil {
		return Options{}, err
	}
	var entry *planning.Entry
	if q.EntryID != "" || q.Week != "" {
		if q.EntryID == "" || q.Week == "" {
			return Options{}, fmt.Errorf("%w: entryId and week must be given together", ErrInvalid)
		}
		p, err := s.plans.Get(ctx, householdID, q.Week)
		if err != nil {
			return Options{}, err
		}
		e, ok := entryOf(p, q.EntryID)
		if !ok || e.RecipeID != r.ID {
			return Options{}, planning.ErrNotFound
		}
		entry = &e
	}
	servings := 0
	switch {
	case q.Servings != 0:
		if !slices.Contains(r.Servings, q.Servings) {
			return Options{}, fmt.Errorf("%w: servings must be one of %v", ErrInvalid, r.Servings)
		}
		servings = q.Servings
	case entry != nil:
		servings = entry.Servings
	case len(r.Servings) > 0:
		servings = slices.Min(r.Servings)
	}
	return s.options(ctx, householdID, r, servings, entry)
}

func (s *Service) options(ctx context.Context, householdID string, r recipes.Recipe, servings int, entry *planning.Entry) (Options, error) {
	out := Options{RecipeID: r.ID, Servings: servings, Groups: []Group{}}
	lines := ProteinLines(s.table, r, servings)
	if len(lines) == 0 {
		return out, nil
	}
	restrictions, err := s.restrictions(ctx, householdID)
	if err != nil {
		return Options{}, err
	}
	current := selectedOf(entry)
	swapped := map[string]Protein{}
	var ids []string
	choices := make([][]Choice, len(lines))
	for i, l := range lines {
		choices[i] = restrictions.Filter(s.table.Choices(l.Protein, l.Name), l, current[l.Key])
		for _, c := range choices[i] {
			if c.IsSwap() {
				swapped[c.Protein.ID] = c.Protein
			}
		}
		if l.IngredientID != "" {
			ids = append(ids, l.IngredientID)
		}
	}
	targets, err := s.targets(ctx, swapped)
	if err != nil {
		return Options{}, err
	}
	images, err := s.images(ctx, ids)
	if err != nil {
		return Options{}, err
	}
	for i, l := range lines {
		g := Group{Line: l, ImageURL: images[l.IngredientID]}
		if entry != nil {
			g.Selected = current[l.Key]
			if g.Selected == "" {
				g.Selected = ChoiceOriginal
			}
		}
		for _, c := range choices[i] {
			opt := ChoiceOption{Choice: c, IngredientName: l.Name, Quantity: l.Quantity.MulRat(c.Factor), Unit: l.Unit, ImageURL: g.ImageURL}
			if c.IsSwap() {
				t := targets[c.Protein.ID]
				opt.IngredientName, opt.ImageURL = t.Name, t.ImageURL
			}
			g.Choices = append(g.Choices, opt)
		}
		out.Groups = append(out.Groups, g)
	}
	return out, nil
}

// SetCustomizations replaces a plan entry's customizations with selections;
// an empty list resets the meal to the recipe as written. Each selection must
// name a group of the entry's options and one of its choices (ErrInvalid).
// Choosing "original" stores nothing for the line. It records
// meal.customized with the lines whose choice changed.
func (s *Service) SetCustomizations(ctx context.Context, householdID, userID, week, entryID string, selections []Selection) (planning.Plan, error) {
	if len(selections) > MaxSelections {
		return planning.Plan{}, fmt.Errorf("%w: at most %d selections", ErrInvalid, MaxSelections)
	}
	p, err := s.plans.Get(ctx, householdID, week)
	if err != nil {
		return planning.Plan{}, err
	}
	e, ok := entryOf(p, entryID)
	if !ok {
		return planning.Plan{}, planning.ErrNotFound
	}
	if p.Status == planning.StatusFinalized {
		return planning.Plan{}, planning.ErrFinalized
	}
	r, err := s.recipe(ctx, householdID, e.RecipeID)
	if err != nil {
		return planning.Plan{}, err
	}
	opts, err := s.options(ctx, householdID, r, e.Servings, &e)
	if err != nil {
		return planning.Plan{}, err
	}
	chosen := map[string]string{}
	for i, sel := range selections {
		gi := slices.IndexFunc(opts.Groups, func(g Group) bool { return g.Line.Key == sel.IngredientKey })
		switch {
		case gi < 0:
			return planning.Plan{}, fmt.Errorf("%w: selections[%d]: ingredientKey %q is not a customizable line of this meal", ErrInvalid, i, sel.IngredientKey)
		case chosen[sel.IngredientKey] != "":
			return planning.Plan{}, fmt.Errorf("%w: selections[%d]: ingredientKey %q is listed more than once", ErrInvalid, i, sel.IngredientKey)
		case !slices.ContainsFunc(opts.Groups[gi].Choices, func(c ChoiceOption) bool { return c.ID == sel.ChoiceID }):
			return planning.Plan{}, fmt.Errorf("%w: selections[%d]: choiceId %q is not a choice for %s", ErrInvalid, i, sel.ChoiceID, opts.Groups[gi].Line.Name)
		}
		chosen[sel.IngredientKey] = sel.ChoiceID
	}
	var list []planning.Customization
	for _, g := range opts.Groups {
		id := chosen[g.Line.Key]
		if id == "" || id == ChoiceOriginal {
			continue
		}
		i := slices.IndexFunc(g.Choices, func(c ChoiceOption) bool { return c.ID == id })
		list = append(list, planning.Customization{IngredientKey: g.Line.Key, ChoiceID: id, Label: g.Choices[i].Label})
	}
	saved, err := s.plans.SetEntryCustomizations(ctx, householdID, week, entryID, list)
	if err != nil {
		return planning.Plan{}, err
	}
	if changes := diff(e.Customizations, list); len(changes) > 0 {
		events.RecordOrLog(ctx, s.events, s.logger, events.Event{
			HouseholdID: householdID, UserID: userID, Type: events.TypeMealCustomized, RecipeID: e.RecipeID,
			Week: saved.Week.String(), OccurredAt: s.now().UTC(),
			Payload: events.MealCustomized{EntryID: e.ID, Changes: changes},
		})
	}
	return saved, nil
}

// CustomizeGrocery implements planning.CustomizationSource with ApplyGrocery.
// A stored choice that no longer applies (the recipe changed, or the table no
// longer offers it) leaves its line as written.
func (s *Service) CustomizeGrocery(ctx context.Context, _ string, entries []planning.Entry, selections []grocery.RecipeSelection) ([]grocery.RecipeSelection, error) {
	out := slices.Clone(selections)
	picks := make([][]Pick, len(selections))
	swapped := map[string]Protein{}
	for i, sel := range selections {
		if i >= len(entries) {
			break
		}
		for _, c := range entries[i].Customizations {
			li := slices.IndexFunc(sel.Lines, func(l grocery.Line) bool { return l.IngredientKey == c.IngredientKey })
			if li < 0 {
				continue
			}
			choice, ok := s.resolve(sel.Lines[li].Name, c.ChoiceID)
			if !ok {
				s.logger.DebugContext(ctx, "meal customization no longer applies", "entryId", entries[i].ID, "ingredientKey", c.IngredientKey, "choiceId", c.ChoiceID)
				continue
			}
			picks[i] = append(picks[i], Pick{IngredientKey: c.IngredientKey, Choice: choice})
			if choice.IsSwap() {
				swapped[choice.Protein.ID] = choice.Protein
			}
		}
	}
	targets, err := s.targets(ctx, swapped)
	if err != nil {
		return nil, err
	}
	for i := range out {
		if len(picks[i]) == 0 {
			continue
		}
		for j := range picks[i] {
			picks[i][j].Target = targets[picks[i][j].Choice.Protein.ID]
		}
		out[i] = ApplyGrocery(out[i], picks[i])
	}
	return out, nil
}

// AdjustCookedRecipe implements pantry.CookAdjuster with ApplyRecipe: a
// cooked plan entry deducts its customized ingredients and amounts. It finds
// the entry near when the meal was cooked (planning.Service.FindEntry) and
// returns r unchanged when it isn't found, is for another recipe, or isn't
// customized.
func (s *Service) AdjustCookedRecipe(ctx context.Context, householdID, entryID string, occurredAt time.Time, r recipes.Recipe) (recipes.Recipe, error) {
	_, e, found, err := s.plans.FindEntry(ctx, householdID, entryID, occurredAt)
	if err != nil {
		return recipes.Recipe{}, fmt.Errorf("find plan entry: %w", err)
	}
	if !found || e.RecipeID != r.ID || len(e.Customizations) == 0 {
		return r, nil
	}
	var picks []Pick
	swapped := map[string]Protein{}
	for _, c := range e.Customizations {
		i := slices.IndexFunc(r.Ingredients, func(ing recipes.RecipeIngredient) bool { return LineKey(ing) == c.IngredientKey })
		if i < 0 {
			continue
		}
		choice, ok := s.resolve(r.Ingredients[i].Name, c.ChoiceID)
		if !ok {
			continue
		}
		picks = append(picks, Pick{IngredientKey: c.IngredientKey, Choice: choice})
		if choice.IsSwap() {
			swapped[choice.Protein.ID] = choice.Protein
		}
	}
	targets, err := s.targets(ctx, swapped)
	if err != nil {
		return recipes.Recipe{}, err
	}
	for j := range picks {
		picks[j].Target = targets[picks[j].Choice.Protein.ID]
	}
	return ApplyRecipe(r, picks), nil
}

func (s *Service) resolve(name, choiceID string) (Choice, bool) {
	p, ok := s.table.Match(name)
	if !ok {
		return Choice{}, false
	}
	return s.table.Resolve(p, name, choiceID)
}

func (s *Service) recipe(ctx context.Context, householdID, id string) (recipes.Recipe, error) {
	r, err := s.recipes.Get(ctx, householdID, id)
	if errors.Is(err, recipes.ErrNotFound) {
		return recipes.Recipe{}, ErrRecipeNotFound
	}
	if err != nil {
		return recipes.Recipe{}, fmt.Errorf("customize: load recipe: %w", err)
	}
	return r, nil
}

func (s *Service) restrictions(ctx context.Context, householdID string) (Restrictions, error) {
	if s.profiles == nil {
		return Restrictions{}, nil
	}
	p, err := s.profiles.Profile(ctx, householdID)
	if err != nil {
		return Restrictions{}, fmt.Errorf("customize: load autopilot restrictions: %w", err)
	}
	r := p.Restrictions
	return Restrictions{Diets: r.Diets, Allergens: r.Allergens, ExcludedIngredients: r.ExcludedIngredients, ExcludedProteins: r.ExcludedProteins}, nil
}

// targets resolves swapped-in proteins to catalog ingredients by name.
func (s *Service) targets(ctx context.Context, proteins map[string]Protein) (map[string]Target, error) {
	out := make(map[string]Target, len(proteins))
	keys := make([]string, 0, len(proteins))
	for id, p := range proteins {
		category, _ := ingredients.Categorize(p.Name)
		out[id] = Target{Name: p.Name, Category: category}
		keys = append(keys, ingredients.NormalizeName(p.Name))
	}
	if s.catalog == nil || len(keys) == 0 {
		return out, nil
	}
	sort.Strings(keys)
	found, err := s.catalog.IngredientsByKey(ctx, keys)
	if err != nil {
		return nil, fmt.Errorf("customize: look up catalog ingredients: %w", err)
	}
	byKey := make(map[string]recipes.Ingredient, len(found))
	for _, ing := range found {
		byKey[ing.Key] = ing
	}
	for id, p := range proteins {
		ing, ok := byKey[ingredients.NormalizeName(p.Name)]
		if !ok {
			continue
		}
		t := out[id]
		t.IngredientID, t.Name, t.ImageURL = ing.ID, ing.Name, ing.ImageURL
		if ing.Category != "" {
			t.Category = ing.Category
		}
		out[id] = t
	}
	return out, nil
}

// images returns catalog image URLs by ingredient ID.
func (s *Service) images(ctx context.Context, ids []string) (map[string]string, error) {
	out := map[string]string{}
	if s.catalog == nil || len(ids) == 0 {
		return out, nil
	}
	found, err := s.catalog.IngredientsByID(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("customize: look up catalog ingredients: %w", err)
	}
	for _, ing := range found {
		out[ing.ID] = ing.ImageURL
	}
	return out, nil
}

func entryOf(p planning.Plan, id string) (planning.Entry, bool) {
	for _, e := range p.Entries {
		if e.ID == id {
			return e, true
		}
	}
	return planning.Entry{}, false
}

// selectedOf maps an entry's customized line keys to their choices.
func selectedOf(e *planning.Entry) map[string]string {
	out := map[string]string{}
	if e != nil {
		for _, c := range e.Customizations {
			out[c.IngredientKey] = c.ChoiceID
		}
	}
	return out
}

// diff lists the lines whose choice changed, by key; a line without a
// customization is "original".
func diff(before, after []planning.Customization) []events.CustomizationChange {
	from, to := map[string]string{}, map[string]string{}
	var keys []string
	for _, c := range before {
		from[c.IngredientKey] = c.ChoiceID
		keys = append(keys, c.IngredientKey)
	}
	for _, c := range after {
		to[c.IngredientKey] = c.ChoiceID
		keys = append(keys, c.IngredientKey)
	}
	slices.Sort(keys)
	keys = slices.Compact(keys)
	var out []events.CustomizationChange
	for _, k := range keys {
		f, t := cmpOr(from[k], ChoiceOriginal), cmpOr(to[k], ChoiceOriginal)
		if f != t {
			out = append(out, events.CustomizationChange{IngredientKey: k, From: f, To: t})
		}
	}
	return out
}

func cmpOr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
