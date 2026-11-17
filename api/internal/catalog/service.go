package catalog

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
	"github.com/Linesmerrill/DinnerOS/api/internal/recommendations"
)

// Library is the household's own recipes, as the catalog needs them:
// to mark what it already has, and to copy an entry into it.
// *recipes.Service implements it.
type Library interface {
	InLibrary(ctx context.Context, householdID string, keys []string) (map[string]string, error)
	AddFromCatalog(ctx context.Context, householdID string, content recipes.Recipe) (recipes.Recipe, bool, error)
}

// Taste is the household's Autopilot profile, which discovery ranks with.
// *recommendations.Service implements it. It is optional: without it, and
// when it fails, discovery falls back to the deterministic alphabetical
// order a household with no history would get anyway.
type Taste interface {
	Profile(ctx context.Context, householdID string) (recommendations.Profile, error)
	TimeBands(ctx context.Context, householdID string) (autopilot.TimeBands, error)
}

// ServiceOptions configures the catalog service.
type ServiceOptions struct {
	Store   Store
	Library Library
	Taste   Taste
	// Now defaults to time.Now.
	Now func() time.Time
}

// Service implements publishing, discovery, search, and adding to a library.
type Service struct {
	store       Store
	library     Library
	taste       Taste
	reports     ReportStore
	saves       SaveStore
	ratings     RatingSignals
	collections CollectionStore
	now         func() time.Time
}

// NewService returns a Service.
func NewService(opts ServiceOptions) *Service {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &Service{store: opts.Store, library: opts.Library, taste: opts.Taste, now: now}
}

// SetLibrary wires the household library after construction. The catalog and
// the recipe service each need the other — one to publish, one to mark and
// copy — so one of the two links is made second.
func (s *Service) SetLibrary(l Library) { s.library = l }

// SetTaste wires the Autopilot profile discovery ranks with.
func (s *Service) SetTaste(t Taste) { s.taste = t }

// PublishRecipes implements recipes.CatalogPublisher.
//
// It is the only way into the catalog, and it enforces the rule by itself
// rather than trusting its caller: a recipe whose source is not public and
// whose household has not shared it is dropped, silently, because a publisher
// handing over a mixed batch (an import, a backfill) is the normal case.
// Only content is copied — recipes.Recipe's household fields are stripped by
// Content before anything is written.
func (s *Service) PublishRecipes(ctx context.Context, rs []recipes.Recipe) (int, error) {
	entries := make([]Recipe, 0, len(rs))
	for _, r := range rs {
		if !recipes.Publishable(r) {
			continue
		}
		// The origin is taken before Content strips the household.
		entries = append(entries, Recipe{CatalogKey: recipes.CatalogKey(r), Origin: recipes.CatalogOrigin(r), Content: Content(r)})
	}
	if len(entries) == 0 {
		return 0, nil
	}
	n, err := s.store.Upsert(ctx, entries, s.now().UTC())
	if err != nil {
		return 0, err
	}
	return n, nil
}

// PublishOne publishes a single recipe, reporting ErrNotPublishable rather
// than dropping it. Operator tools use it so a mistake is visible.
func (s *Service) PublishOne(ctx context.Context, r recipes.Recipe) error {
	if !recipes.Publishable(r) {
		return fmt.Errorf("%w: %q is from %q and is not shared", ErrNotPublishable, r.Name, r.Source)
	}
	_, err := s.PublishRecipes(ctx, []recipes.Recipe{r})
	return err
}

// Discover returns catalog recipes the household does not already have,
// ordered by how well they suit it (ranking.go).
//
// The exclusion is the feature: this is the "try something else" surface, so
// a household with three recipes and a household with three hundred both get
// a page of things they have never cooked.
func (s *Service) Discover(ctx context.Context, householdID string, q Query) (Page, error) {
	f, err := q.validate()
	if err != nil {
		return Page{}, err
	}
	entries, err := s.store.Scan(ctx, ScanLimit)
	if err != nil {
		return Page{}, fmt.Errorf("scan catalog: %w", err)
	}
	entries = withoutHidden(shown(entries), s.hidden(ctx, householdID))
	entries, err = s.withoutLibrary(ctx, householdID, entries)
	if err != nil {
		return Page{}, err
	}
	entries = narrow(entries, f)

	profile, bands := s.tasteOf(ctx, householdID)
	ranked := rank(entries, profile, bands)
	return page(ranked, f), nil
}

// Search returns catalog recipes matching the query, with the household's own
// marked rather than hidden: when someone searches for "tikka masala" they
// want to be told they already have it.
func (s *Service) Search(ctx context.Context, householdID string, q Query) (Page, error) {
	f, err := q.validate()
	if err != nil {
		return Page{}, err
	}
	// The store drops hidden groups before paging, so the count and the
	// cursor are right.
	f.HiddenGroups = hiddenList(s.hidden(ctx, householdID))
	if f.Cuisine != "" {
		spellings, err := s.cuisineSpellings(ctx, f.Cuisine)
		if err != nil {
			return Page{}, fmt.Errorf("search catalog: %w", err)
		}
		if len(spellings) == 0 {
			// No stored recipe has that cuisine.
			return Page{Items: []Result{}}, nil
		}
		f.CuisineAny = spellings
	}
	entries, total, err := s.store.Search(ctx, f)
	if err != nil {
		return Page{}, fmt.Errorf("search catalog: %w", err)
	}
	items := make([]Result, 0, len(entries))
	for _, e := range entries {
		items = append(items, Result{Recipe: e})
	}
	if err := s.mark(ctx, householdID, items); err != nil {
		return Page{}, err
	}
	next := ""
	if f.Offset+len(items) < total && f.Offset+f.Limit <= MaxOffset {
		next = encodeCursor(f.Offset + len(items))
	}
	return Page{Items: items, NextCursor: next, Total: total}, nil
}

// Get returns one catalog entry, marked against the household's library.
func (s *Service) Get(ctx context.Context, householdID, id string) (Result, error) {
	e, err := s.store.Get(ctx, id)
	if err != nil {
		return Result{}, err
	}
	if !servable(e) || isHidden(e, s.hidden(ctx, householdID)) {
		return Result{}, ErrNotFound
	}
	out := []Result{{Recipe: e}}
	if err := s.mark(ctx, householdID, out); err != nil {
		return Result{}, err
	}
	return out[0], nil
}

// Add copies a catalog entry into the household's library and returns the
// stored recipe and whether this call created it.
func (s *Service) Add(ctx context.Context, householdID, id string) (recipes.Recipe, bool, error) {
	e, err := s.store.Get(ctx, id)
	if err != nil {
		return recipes.Recipe{}, false, err
	}
	if !servable(e) || isHidden(e, s.hidden(ctx, householdID)) {
		return recipes.Recipe{}, false, ErrNotFound
	}
	if s.library == nil {
		return recipes.Recipe{}, false, errors.New("catalog: no library is wired")
	}
	stored, created, err := s.library.AddFromCatalog(ctx, householdID, Content(e.Content))
	if err != nil || !created {
		return stored, created, err
	}
	// Trending and Top Rated count saves (decision 711). Only a new copy
	// counts, so tapping Save twice is one save; a failed write costs a
	// count, never the save itself.
	if s.saves != nil {
		_ = s.saves.Record(ctx, Save{CatalogID: e.ID, Group: groupKey(e), HouseholdID: householdID, At: s.now().UTC()})
	}
	return stored, created, nil
}

// --- helpers ------------------------------------------------------------------

// shown drops entries that are never served: ones from withheld sources (a
// meal kit's recipes, decision 672) and add-ons (decision 713). Entries
// published before those rules stay stored but are never served.
func shown(entries []Recipe) []Recipe {
	out := entries[:0:0]
	for _, e := range entries {
		if servable(e) {
			out = append(out, e)
		}
	}
	return out
}

// servable reports whether an entry may be shown or added at all.
func servable(e Recipe) bool {
	return !recipes.CatalogWithheld(e.Content.Source) && !e.Content.IsAddon
}

func (s *Service) tasteOf(ctx context.Context, householdID string) (recommendations.Profile, autopilot.TimeBands) {
	// The zero value is the default bands (autopilot.TimeBands.Of).
	var bands autopilot.TimeBands
	if s.taste == nil {
		return recommendations.Profile{}, bands
	}
	if b, err := s.taste.TimeBands(ctx, householdID); err == nil {
		bands = b
	}
	profile, err := s.taste.Profile(ctx, householdID)
	if err != nil {
		// A household that has never opened Autopilot has no profile, and a
		// profile read that fails must not empty the browse surface. Either
		// way the fallback is the cold-start order.
		return recommendations.Profile{}, bands
	}
	return profile, bands
}

// withoutLibrary drops the entries the household already has, under any of
// their group's keys: a household with one copy of a recipe has the recipe.
func (s *Service) withoutLibrary(ctx context.Context, householdID string, entries []Recipe) ([]Recipe, error) {
	if s.library == nil || len(entries) == 0 {
		return entries, nil
	}
	var keys []string
	for _, e := range entries {
		keys = append(keys, e.keys()...)
	}
	have, err := s.library.InLibrary(ctx, householdID, keys)
	if err != nil {
		return nil, fmt.Errorf("mark library: %w", err)
	}
	out := make([]Recipe, 0, len(entries))
	for _, e := range entries {
		if libraryID(e, have) == "" {
			out = append(out, e)
		}
	}
	return out, nil
}

// mark fills LibraryRecipeID on results the household already has.
func (s *Service) mark(ctx context.Context, householdID string, items []Result) error {
	if s.library == nil || len(items) == 0 {
		return nil
	}
	var keys []string
	for _, it := range items {
		keys = append(keys, it.keys()...)
	}
	have, err := s.library.InLibrary(ctx, householdID, keys)
	if err != nil {
		return fmt.Errorf("mark library: %w", err)
	}
	for i := range items {
		items[i].LibraryRecipeID = libraryID(items[i].Recipe, have)
	}
	return nil
}

// libraryID is the household's recipe for e's group, preferring e's own key.
func libraryID(e Recipe, have map[string]string) string {
	for _, k := range e.keys() {
		if id := have[k]; id != "" {
			return id
		}
	}
	return ""
}

// narrow applies the exact filters in memory, for the scanned discovery set.
func narrow(entries []Recipe, f Filter) []Recipe {
	if f.Cuisine == "" && f.Tag == "" && f.Ingredient == "" && f.Text == "" && f.MaxMinutes == 0 && f.MinServings == 0 {
		return entries
	}
	out := make([]Recipe, 0, len(entries))
	for _, e := range entries {
		if matches(e, f) {
			out = append(out, e)
		}
	}
	return out
}

// matches applies one entry against the exact filters and the free-text term.
// Discovery ranks a scanned set in memory, so it matches in memory too;
// search leaves the same work to the text index.
func matches(e Recipe, f Filter) bool {
	if e.Content.IsAddon {
		return false
	}
	if f.Cuisine != "" && !hasCuisine(e.Content, f.Cuisine) {
		return false
	}
	if f.MaxMinutes > 0 && !withinMinutes(e.Content, f.MaxMinutes) {
		return false
	}
	if f.MinServings > 0 && !servesAtLeast(e.Content, f.MinServings) {
		return false
	}
	if key := fold(f.Tag); key != "" && !containsFold(e.Content.Tags, key) {
		return false
	}
	if key := ingredients.NormalizeName(f.Ingredient); key != "" {
		found := false
		for _, line := range e.Content.Ingredients {
			found = found || ingredients.NormalizeName(line.Name) == key
		}
		if !found {
			return false
		}
	}
	if text := fold(f.Text); text != "" {
		hay := fold(e.Content.Name + " " + e.Content.Headline)
		for _, line := range e.Content.Ingredients {
			hay += " " + fold(line.Name)
		}
		hay += " " + fold(strings.Join(e.Content.Cuisines, " ")) + " " + fold(strings.Join(e.Content.Tags, " "))
		if !strings.Contains(hay, text) {
			return false
		}
	}
	return true
}

// cuisineSpellings returns the folded stored spellings of cuisine, read
// canonically ("Mexican", "mexican " for "mexican").
func (s *Service) cuisineSpellings(ctx context.Context, cuisine string) ([]string, error) {
	stored, err := s.store.Cuisines(ctx)
	if err != nil {
		return nil, err
	}
	want := recommendations.CanonicalCuisine(cuisine)
	var out []string
	for _, v := range stored {
		if k := fold(v); recommendations.CanonicalCuisine(v) == want && !slices.Contains(out, k) {
			out = append(out, k)
		}
	}
	return out, nil
}

// hasCuisine reports whether the recipe shows cuisine, read canonically: the
// same cuisines its card shows (decision 732), so an American chip finds a
// recipe stored with "North America".
func hasCuisine(c recipes.Recipe, cuisine string) bool {
	return slices.Contains(c.ShownCuisines(), recommendations.CanonicalCuisine(cuisine))
}

// withinMinutes reports whether the recipe's total time is known and at most
// limit. An unknown time is not "quick": it is left out.
func withinMinutes(c recipes.Recipe, limit int) bool {
	m := recipes.CookMinutes(c.PrepMinutes, c.TotalMinutes)
	return m > 0 && m <= limit
}

// servesAtLeast reports whether any of the recipe's serving sizes is at
// least n.
func servesAtLeast(c recipes.Recipe, n int) bool {
	return slices.ContainsFunc(c.Servings, func(s int) bool { return s >= n })
}

// page cuts one page out of ranked results and returns the cursor for the
// next, which is an offset: the order is a pure function of the household and
// the catalog, so the same offset means the same place.
func page(items []Result, f Filter) Page {
	total := len(items)
	if f.Offset >= total {
		return Page{Items: []Result{}, Total: total}
	}
	end := min(f.Offset+f.Limit, total)
	next := ""
	if end < total && end <= MaxOffset {
		next = encodeCursor(end)
	}
	return Page{Items: items[f.Offset:end], NextCursor: next, Total: total}
}
