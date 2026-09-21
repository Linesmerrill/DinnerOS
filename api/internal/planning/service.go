package planning

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// Limits.
const (
	// MaxEntriesPerWeek bounds the embedded entries array.
	MaxEntriesPerWeek = 50
	// MaxRangeWeeks is the longest range GET /plans accepts.
	MaxRangeWeeks = 26
	// MaxNoteLength is the longest entry note, in characters.
	MaxNoteLength = 500
)

// RecipeReader loads a household's recipes with ingredient categories filled
// in. *recipes.Service implements it.
type RecipeReader interface {
	// Get returns recipes.ErrNotFound for recipes outside the household.
	Get(ctx context.Context, householdID, id string) (recipes.Recipe, error)
	// GetMany skips IDs that are missing or belong to another household.
	GetMany(ctx context.Context, householdID string, ids []string) ([]recipes.Recipe, error)
}

// PantrySource provides a household's pantry for grocery lists.
// *pantry.Service implements it.
type PantrySource interface {
	GroceryPantry(ctx context.Context, householdID string) (grocery.PantryStock, error)
}

// SpecialtySource resolves the specialty ingredients (meal-kit blends,
// sauces, and concentrates) among a grocery list's lines, with the
// household's choices. *substitutes.Service implements it.
type SpecialtySource interface {
	GrocerySpecialties(ctx context.Context, householdID string, lines []grocery.Line) (grocery.Specialties, error)
}

// SkipSource provides the ingredients a household chose not to buy in week,
// already narrowed to the skips that apply to it. *skips.Service implements it.
type SkipSource interface {
	GrocerySkips(ctx context.Context, householdID, week string) (grocery.SkipSet, error)
}

// WeekStartSource says which day a household's week starts on ("sun".."sat").
// *households.Service implements it.
type WeekStartSource interface {
	WeekStartsOn(ctx context.Context, householdID string) (string, error)
}

// Service implements the weekly planner. Like recipes.Service it takes a
// household ID: HTTP routes authorize first with households.RequirePermission.
type Service struct {
	store   Store
	recipes RecipeReader
	// pantry is optional; without it grocery lists use an empty pantry.
	pantry PantrySource
	// specialties is optional; without it specialty lines stay as they are.
	specialties SpecialtySource
	// skips is optional; without it the household buys every ingredient.
	skips SkipSource
	// extras is optional; without it the list has only recipe lines.
	extras ExtrasSource
	// customizations is optional; without it customized entries contribute
	// their recipe as written (customization.go).
	customizations CustomizationSource
	// events is optional; without it entry changes record nothing.
	events events.Recorder
	// weekStart is optional; without it weeks start on Monday.
	weekStart WeekStartSource
	// frozen, households, and thawNotifier are set by WithFreezer; without
	// them thaw reminders are off (thaw.go).
	frozen       FrozenSource
	households   HouseholdSource
	thawNotifier ThawNotifier
	logger       *slog.Logger
	now          func() time.Time
}

// NewService returns a Service.
func NewService(store Store, recipeReader RecipeReader) *Service {
	return &Service{store: store, recipes: recipeReader, now: time.Now}
}

// WithPantry makes GroceryList apply the household pantry from source, and
// returns s.
func (s *Service) WithPantry(source PantrySource) *Service {
	s.pantry = source
	return s
}

// WithSpecialties makes GroceryList apply the household's specialty
// ingredient choices from source (grocery.ApplySpecialties), and returns s.
func (s *Service) WithSpecialties(source SpecialtySource) *Service {
	s.specialties = source
	return s
}

// WithSkips makes GroceryList hold back the ingredients the household chose
// not to buy, and returns s.
func (s *Service) WithSkips(source SkipSource) *Service {
	s.skips = source
	return s
}

// WithEvents makes AddEntry and DeleteEntry record recipe.planned and
// recipe.unplanned through recorder, and returns s. Recording is best effort
// (events.RecordOrLog): a failure is logged to logger (slog.Default when nil)
// and never fails the change.
func (s *Service) WithEvents(recorder events.Recorder, logger *slog.Logger) *Service {
	s.events, s.logger = recorder, logger
	return s
}

// WithWeekStart makes plans carry the household's first day of the week
// (Plan.FirstDay), which decides the dates their days fall on, and returns s.
func (s *Service) WithWeekStart(source WeekStartSource) *Service {
	s.weekStart = source
	return s
}

// FirstDay returns the day the household's week starts on: Monday without a
// source or when the stored value is unknown.
func (s *Service) FirstDay(ctx context.Context, householdID string) (Day, error) {
	if s.weekStart == nil {
		return LegacyWeekStart, nil
	}
	v, err := s.weekStart.WeekStartsOn(ctx, householdID)
	if err != nil {
		return "", fmt.Errorf("planning: load week start: %w", err)
	}
	if d, err := ParseDay(v); err == nil {
		return d, nil
	}
	return LegacyWeekStart, nil
}

// stamp sets p.FirstDay from the household, passing err through. Reads use
// it; writes use written, so a failed lookup can't fail a change that was
// already saved.
func (s *Service) stamp(ctx context.Context, householdID string, p Plan, err error) (Plan, error) {
	if err != nil {
		return p, err
	}
	first, ferr := s.FirstDay(ctx, householdID)
	if ferr != nil {
		return Plan{}, ferr
	}
	p.FirstDay = first
	return p, nil
}

// written loads the household's first day, then runs write and sets the
// returned plan's FirstDay.
func (s *Service) written(ctx context.Context, householdID string, write func() (Plan, error)) (Plan, error) {
	first, err := s.FirstDay(ctx, householdID)
	if err != nil {
		return Plan{}, err
	}
	p, err := write()
	if err != nil {
		return p, err
	}
	p.FirstDay = first
	return p, nil
}

// Get returns the household's plan for week. A week nobody has planned is an
// empty draft, not ErrNotFound.
func (s *Service) Get(ctx context.Context, householdID, week string) (Plan, error) {
	w, err := s.parse(householdID, week)
	if err != nil {
		return Plan{}, err
	}
	return s.getOrEmpty(ctx, householdID, w)
}

func (s *Service) getOrEmpty(ctx context.Context, householdID string, w Week) (Plan, error) {
	p, err := s.store.GetPlan(ctx, householdID, w)
	if errors.Is(err, ErrNotFound) {
		p, err = Plan{HouseholdID: householdID, Week: w, Status: StatusDraft}, nil
	}
	return s.stamp(ctx, householdID, p, err)
}

// List returns one summary per week from..to inclusive, including weeks
// nobody has planned (as empty drafts). The range covers at most
// MaxRangeWeeks weeks.
func (s *Service) List(ctx context.Context, householdID, from, to string) ([]Summary, error) {
	if householdID == "" {
		return nil, errHouseholdRequired
	}
	if from == "" || to == "" {
		return nil, fmt.Errorf("%w: from and to are required", ErrInvalidRange)
	}
	fw, err := ParseWeek(from)
	if err != nil {
		return nil, fmt.Errorf("from: %w", err)
	}
	tw, err := ParseWeek(to)
	if err != nil {
		return nil, fmt.Errorf("to: %w", err)
	}
	n := fw.WeeksUntil(tw) + 1
	switch {
	case n < 1:
		return nil, fmt.Errorf("%w: to must not be before from", ErrInvalidRange)
	case n > MaxRangeWeeks:
		return nil, fmt.Errorf("%w: a range covers at most %d weeks", ErrInvalidRange, MaxRangeWeeks)
	}
	stored, err := s.store.ListSummaries(ctx, householdID, fw, tw)
	if err != nil {
		return nil, err
	}
	byWeek := make(map[Week]Summary, len(stored))
	for _, sum := range stored {
		byWeek[sum.Week] = sum
	}
	out := make([]Summary, 0, n)
	for i := range n {
		w := fw.AddWeeks(i)
		sum, ok := byWeek[w]
		if !ok {
			sum = Summary{Week: w, Status: StatusDraft}
		}
		out = append(out, sum)
	}
	return out, nil
}

// NewEntry is a recipe to add to a week.
type NewEntry struct {
	RecipeID string
	// Day is "" for unscheduled.
	Day      string
	Servings int
	Note     string
	// Origin defaults to OriginManual. Only server code (accepting an
	// Autopilot proposal) sets OriginAutopilot and ProposalID; HTTP handlers
	// never take them from a request.
	Origin     Origin
	ProposalID string
}

// AddEntry adds a recipe to the week, creating the plan if needed. The recipe
// must belong to the household and offer the requested serving size.
func (s *Service) AddEntry(ctx context.Context, householdID, userID, week string, in NewEntry) (Plan, Entry, error) {
	p, added, err := s.AddEntries(ctx, householdID, userID, week, []NewEntry{in})
	if err != nil {
		return Plan{}, Entry{}, err
	}
	return p, added[0], nil
}

// AddEntries adds several recipes to the week in one atomic change: either all
// of them are added or none are. Each must pass AddEntry's checks. It records
// recipe.planned for each added entry.
func (s *Service) AddEntries(ctx context.Context, householdID, userID, week string, in []NewEntry) (Plan, []Entry, error) {
	w, err := s.parse(householdID, week)
	if err != nil {
		return Plan{}, nil, err
	}
	if userID == "" {
		return Plan{}, nil, errors.New("planning: user id is required")
	}
	switch {
	case len(in) == 0:
		return Plan{}, nil, fmt.Errorf("%w: at least one entry is required", ErrInvalidEntry)
	case len(in) > MaxEntriesPerWeek:
		return Plan{}, nil, ErrPlanFull
	}
	var recipeIDs []string
	for _, ne := range in {
		if strings.TrimSpace(ne.RecipeID) == "" {
			return Plan{}, nil, fmt.Errorf("%w: recipeId is required", ErrInvalidEntry)
		}
		if !slices.Contains(recipeIDs, ne.RecipeID) {
			recipeIDs = append(recipeIDs, ne.RecipeID)
		}
	}
	live, err := s.recipesByID(ctx, householdID, recipeIDs)
	if err != nil {
		return Plan{}, nil, err
	}
	now := s.now().UTC()
	entries := make([]Entry, 0, len(in))
	for _, ne := range in {
		e := Entry{Servings: ne.Servings, AddedBy: userID, AddedAt: now, ProposalID: ne.ProposalID}
		if ne.Day != "" {
			if e.Day, err = ParseDay(ne.Day); err != nil {
				return Plan{}, nil, err
			}
		}
		if e.Note, err = cleanNote(ne.Note); err != nil {
			return Plan{}, nil, err
		}
		if e.Origin, err = parseOrigin(ne.Origin); err != nil {
			return Plan{}, nil, err
		}
		recipe, ok := live[ne.RecipeID]
		if !ok {
			return Plan{}, nil, ErrRecipeNotFound
		}
		if err := checkServings(recipe, ne.Servings); err != nil {
			return Plan{}, nil, err
		}
		e.RecipeID, e.RecipeName, e.RecipeImageURL = recipe.ID, recipe.Name, recipe.ImageURL
		e.RecipeIsAddon = recipe.IsAddon
		entries = append(entries, e)
	}

	var ids []string
	p, err := s.written(ctx, householdID, func() (Plan, error) {
		p, added, err := s.store.AddEntries(ctx, householdID, w, entries, MaxEntriesPerWeek, now)
		ids = added
		return p, err
	})
	if err != nil {
		return Plan{}, nil, err
	}
	added := make([]Entry, 0, len(ids))
	for _, id := range ids {
		e, ok := p.entry(id)
		if !ok {
			return Plan{}, nil, fmt.Errorf("planning: added entry %s missing from plan", id)
		}
		added = append(added, e)
		events.RecordOrLog(ctx, s.events, s.logger, events.Event{
			HouseholdID: householdID, UserID: userID, Type: events.TypeRecipePlanned, RecipeID: e.RecipeID,
			Week: w.String(), OccurredAt: now,
			Payload: events.RecipePlanned{
				EntryID: e.ID, Day: string(e.Day), Date: p.DateOf(e.Day), Servings: e.Servings,
				Origin: string(e.Origin), ProposalID: e.ProposalID,
			},
		})
	}
	return p, added, nil
}

// recipesByID loads the household's recipes by ID. A single missing recipe is
// reported as ErrRecipeNotFound by the caller.
func (s *Service) recipesByID(ctx context.Context, householdID string, ids []string) (map[string]recipes.Recipe, error) {
	if len(ids) == 1 {
		r, err := s.recipe(ctx, householdID, ids[0])
		if err != nil {
			return nil, err
		}
		return map[string]recipes.Recipe{r.ID: r}, nil
	}
	list, err := s.recipes.GetMany(ctx, householdID, ids)
	if err != nil {
		return nil, fmt.Errorf("planning: load recipes: %w", err)
	}
	out := make(map[string]recipes.Recipe, len(list))
	for _, r := range list {
		out[r.ID] = r
	}
	return out, nil
}

// EarliestPlannedWeek returns the earliest week with at least one planned
// entry, for modules that show how far back history goes (the menu's week
// strip). ok is false when nothing was ever planned.
func (s *Service) EarliestPlannedWeek(ctx context.Context, householdID string) (week Week, ok bool, err error) {
	if householdID == "" {
		return Week{}, false, errHouseholdRequired
	}
	return s.store.EarliestWeek(ctx, householdID)
}

// ListPlans returns the household's stored plans from..to inclusive, in week
// order, for modules that read planning history (the recommender). Weeks
// nobody planned are omitted. The range covers at most MaxRangeWeeks weeks.
func (s *Service) ListPlans(ctx context.Context, householdID, from, to string) ([]Plan, error) {
	if householdID == "" {
		return nil, errHouseholdRequired
	}
	fw, err := ParseWeek(from)
	if err != nil {
		return nil, fmt.Errorf("from: %w", err)
	}
	tw, err := ParseWeek(to)
	if err != nil {
		return nil, fmt.Errorf("to: %w", err)
	}
	switch n := fw.WeeksUntil(tw) + 1; {
	case n < 1:
		return nil, fmt.Errorf("%w: to must not be before from", ErrInvalidRange)
	case n > MaxRangeWeeks:
		return nil, fmt.Errorf("%w: a range covers at most %d weeks", ErrInvalidRange, MaxRangeWeeks)
	}
	plans, err := s.store.ListPlans(ctx, householdID, fw, tw)
	if err != nil || len(plans) == 0 {
		return plans, err
	}
	first, err := s.FirstDay(ctx, householdID)
	if err != nil {
		return nil, err
	}
	for i := range plans {
		plans[i].FirstDay = first
	}
	return plans, nil
}

// UpdateEntry changes an entry's day, servings, or note. New servings must be
// one of the live recipe's serving sizes.
func (s *Service) UpdateEntry(ctx context.Context, householdID, week, entryID string, c EntryChanges) (Plan, error) {
	w, err := s.parse(householdID, week)
	if err != nil {
		return Plan{}, err
	}
	if c.Day == nil && c.Servings == nil && c.Note == nil {
		return Plan{}, fmt.Errorf("%w: provide at least one of day, servings, or note", ErrInvalidEntry)
	}
	if c.Day != nil && *c.Day != "" {
		day, err := ParseDay(string(*c.Day))
		if err != nil {
			return Plan{}, err
		}
		c.Day = &day
	}
	if c.Note != nil {
		note, err := cleanNote(*c.Note)
		if err != nil {
			return Plan{}, err
		}
		c.Note = &note
	}
	if c.Servings != nil {
		// Servings depend on the entry's recipe, which never changes, so
		// reading the entry first is safe under concurrent edits.
		p, err := s.store.GetPlan(ctx, householdID, w)
		if err != nil {
			return Plan{}, err
		}
		e, ok := p.entry(entryID)
		if !ok {
			return Plan{}, ErrNotFound
		}
		if p.Status == StatusFinalized {
			return Plan{}, ErrFinalized
		}
		recipe, err := s.recipe(ctx, householdID, e.RecipeID)
		if err != nil {
			return Plan{}, err
		}
		if err := checkServings(recipe, *c.Servings); err != nil {
			return Plan{}, err
		}
	}
	return s.written(ctx, householdID, func() (Plan, error) {
		return s.store.UpdateEntry(ctx, householdID, w, entryID, c, s.now().UTC())
	})
}

// ReplaceEntryRecipe swaps one entry's recipe in place, keeping the entry's
// ID, day, and note, so the week and its grocery list change in one atomic
// update rather than a remove and an add. Servings stay the entry's own when
// the new recipe is authored in that size, and otherwise become the nearest
// size it has. Customizations name the old recipe's ingredient lines, so they
// are dropped.
//
// It records nothing: the caller knows why the meal changed (Autopilot's
// "try something similar" records meal.swapped and recipe.planned) and
// records that itself. A finalized plan fails with ErrFinalized.
func (s *Service) ReplaceEntryRecipe(ctx context.Context, householdID, week, entryID, recipeID string, origin Origin) (Plan, Entry, Entry, error) {
	w, err := s.parse(householdID, week)
	if err != nil {
		return Plan{}, Entry{}, Entry{}, err
	}
	if strings.TrimSpace(recipeID) == "" {
		return Plan{}, Entry{}, Entry{}, fmt.Errorf("%w: recipeId is required", ErrInvalidEntry)
	}
	if origin, err = parseOrigin(origin); err != nil {
		return Plan{}, Entry{}, Entry{}, err
	}
	current, err := s.store.GetPlan(ctx, householdID, w)
	if err != nil {
		return Plan{}, Entry{}, Entry{}, err
	}
	previous, ok := current.entry(entryID)
	if !ok {
		return Plan{}, Entry{}, Entry{}, ErrNotFound
	}
	if current.Status == StatusFinalized {
		return Plan{}, Entry{}, Entry{}, ErrFinalized
	}
	if previous.RecipeID == recipeID {
		return Plan{}, Entry{}, Entry{}, fmt.Errorf("%w: the entry already has that recipe", ErrInvalidEntry)
	}
	recipe, err := s.recipe(ctx, householdID, recipeID)
	if err != nil {
		return Plan{}, Entry{}, Entry{}, err
	}
	servings := nearestServings(recipe, previous.Servings)
	if err := checkServings(recipe, servings); err != nil {
		return Plan{}, Entry{}, Entry{}, err
	}
	changes := EntryChanges{
		Servings: &servings,
		Recipe: &EntryRecipe{
			ID: recipe.ID, Name: recipe.Name, ImageURL: recipe.ImageURL, IsAddon: recipe.IsAddon, Origin: origin,
		},
	}
	p, err := s.written(ctx, householdID, func() (Plan, error) {
		return s.store.UpdateEntry(ctx, householdID, w, entryID, changes, s.now().UTC())
	})
	if err != nil {
		return Plan{}, Entry{}, Entry{}, err
	}
	next, ok := p.entry(entryID)
	if !ok {
		return Plan{}, Entry{}, Entry{}, ErrNotFound
	}
	return p, previous, next, nil
}

// nearestServings is want when the recipe is authored in that size, and
// otherwise the size closest to it (the smaller one on a tie).
func nearestServings(r recipes.Recipe, want int) int {
	best := 0
	for _, size := range r.Servings {
		switch {
		case size == want:
			return want
		case best == 0, abs(size-want) < abs(best-want), abs(size-want) == abs(best-want) && size < best:
			best = size
		}
	}
	return best
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// DeleteEntry removes an entry from the week. userID is the member removing
// it, recorded on the recipe.unplanned event.
func (s *Service) DeleteEntry(ctx context.Context, householdID, userID, week, entryID string) (Plan, error) {
	w, err := s.parse(householdID, week)
	if err != nil {
		return Plan{}, err
	}
	if userID == "" {
		return Plan{}, errors.New("planning: user id is required")
	}
	// The event describes the removed entry, which the delete doesn't return,
	// so read it first, but only when something records events. The read and
	// the delete aren't atomic: a concurrent edit can leave the event's day
	// stale, which is acceptable for behavioral history.
	var removed Entry
	var found bool
	if s.events != nil {
		if current, err := s.store.GetPlan(ctx, householdID, w); err == nil {
			removed, found = current.entry(entryID)
		}
	}
	now := s.now().UTC()
	p, err := s.written(ctx, householdID, func() (Plan, error) {
		return s.store.DeleteEntry(ctx, householdID, w, entryID, now)
	})
	if err != nil {
		return Plan{}, err
	}
	if found {
		events.RecordOrLog(ctx, s.events, s.logger, events.Event{
			HouseholdID: householdID, UserID: userID, Type: events.TypeRecipeUnplanned, RecipeID: removed.RecipeID,
			Week: w.String(), OccurredAt: now,
			Payload: events.RecipeUnplanned{EntryID: removed.ID, Day: string(removed.Day), Date: p.DateOf(removed.Day), Origin: string(removed.Origin)},
		})
	}
	return p, nil
}

// SetStatus finalizes a week or returns it to draft.
func (s *Service) SetStatus(ctx context.Context, householdID, week, status string) (Plan, error) {
	w, err := s.parse(householdID, week)
	if err != nil {
		return Plan{}, err
	}
	st, err := ParseStatus(status)
	if err != nil {
		return Plan{}, err
	}
	return s.written(ctx, householdID, func() (Plan, error) {
		return s.store.SetStatus(ctx, householdID, w, st, s.now().UTC())
	})
}

// GroceryList aggregates the week's entries into a grocery list using each
// live recipe's authored amounts for the entry's serving size.
//
// With a pantry source (WithPantry), the household pantry decides statuses:
// in-stock ingredients are inPantry and ingredients recorded as out are toBuy
// (see pantry.Service.GroceryPantry). Without one, items are toBuy, or
// pantryHint when every source marks them as a staple. PantryApplied says
// which happened. A pantry read failure fails the list.
//
// With a specialty source (WithSpecialties), specialty ingredient lines are
// handled as the household chose before aggregating: replaced by a store
// alternative's ingredients, kept as a house-made batch in the pantry, or
// replaced by the ingredients to make one (Batches reports these). Lines
// without a choice stay, marked with suggested options. SpecialtiesApplied
// says whether this happened; a failure to load the choices fails the list.
//
// With a skip source (WithSkips), the ingredients the household chose not to
// buy are held out of Categories and reported in SkippedItems instead, so the
// list never asks anyone to buy them and never pretends the recipes stopped
// needing them. A failure to load the skips fails the list.
func (s *Service) GroceryList(ctx context.Context, householdID, week string) (GroceryList, error) {
	w, err := s.parse(householdID, week)
	if err != nil {
		return GroceryList{}, err
	}
	p, err := s.getOrEmpty(ctx, householdID, w)
	if err != nil {
		return GroceryList{}, err
	}
	var ids []string
	for _, e := range p.Entries {
		if !slices.Contains(ids, e.RecipeID) {
			ids = append(ids, e.RecipeID)
		}
	}
	var live []recipes.Recipe
	var pantry grocery.Pantry = grocery.PantrySet{}
	if len(ids) > 0 {
		if live, err = s.recipes.GetMany(ctx, householdID, ids); err != nil {
			return GroceryList{}, fmt.Errorf("planning: load recipes: %w", err)
		}
		if s.pantry != nil {
			stock, err := s.pantry.GroceryPantry(ctx, householdID)
			if err != nil {
				return GroceryList{}, fmt.Errorf("planning: load pantry: %w", err)
			}
			pantry = stock
		}
	}
	selections, skipped := grocerySelections(p, live)
	// Customizations change the recipe's own lines, so they run before
	// specialty ingredients handle what's left.
	if selections, err = s.customizeGrocery(ctx, householdID, p, selections, skipped); err != nil {
		return GroceryList{}, err
	}
	var batches []grocery.BatchPlan
	if s.specialties != nil && len(selections) > 0 {
		var lines []grocery.Line
		for _, sel := range selections {
			lines = append(lines, sel.Lines...)
		}
		specs, err := s.specialties.GrocerySpecialties(ctx, householdID, lines)
		if err != nil {
			return GroceryList{}, fmt.Errorf("planning: load specialty ingredients: %w", err)
		}
		if selections, batches, err = grocery.ApplySpecialties(selections, specs); err != nil {
			return GroceryList{}, err
		}
	}
	if selections, err = s.appendExtras(ctx, p, selections); err != nil {
		return GroceryList{}, fmt.Errorf("planning: load extra grocery items: %w", err)
	}
	// Skips resolve last, against the keys the list actually ends up with, so
	// an ingredient a store alternative introduced can be skipped by its own
	// name without disturbing the rest of that alternative.
	var skips grocery.Skips
	if s.skips != nil {
		set, err := s.skips.GrocerySkips(ctx, householdID, w.String())
		if err != nil {
			return GroceryList{}, fmt.Errorf("planning: load skipped ingredients: %w", err)
		}
		skips = set
	}
	g, err := aggregateGroceryList(p, selections, skipped, pantry, skips)
	if err != nil {
		return GroceryList{}, err
	}
	g.Batches = batches
	g.PantryApplied = s.pantry != nil
	g.SpecialtiesApplied = s.specialties != nil
	return g, nil
}

func (s *Service) parse(householdID, week string) (Week, error) {
	if householdID == "" {
		return Week{}, errHouseholdRequired
	}
	return ParseWeek(week)
}

func (s *Service) recipe(ctx context.Context, householdID, id string) (recipes.Recipe, error) {
	r, err := s.recipes.Get(ctx, householdID, id)
	if errors.Is(err, recipes.ErrNotFound) {
		return recipes.Recipe{}, ErrRecipeNotFound
	}
	if err != nil {
		return recipes.Recipe{}, fmt.Errorf("planning: load recipe: %w", err)
	}
	return r, nil
}

func checkServings(r recipes.Recipe, servings int) error {
	if len(r.Servings) == 0 {
		return fmt.Errorf("%w: the recipe has no serving sizes", ErrInvalidEntry)
	}
	if !slices.Contains(r.Servings, servings) {
		return fmt.Errorf("%w: servings must be one of %v", ErrInvalidEntry, r.Servings)
	}
	return nil
}

func cleanNote(note string) (string, error) {
	note = strings.TrimSpace(note)
	if utf8.RuneCountInString(note) > MaxNoteLength {
		return "", fmt.Errorf("%w: note must be at most %d characters", ErrInvalidEntry, MaxNoteLength)
	}
	return note, nil
}
