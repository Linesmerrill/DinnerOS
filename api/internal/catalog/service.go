package catalog

import (
	"context"
	"errors"
	"fmt"
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
	store   Store
	library Library
	taste   Taste
	now     func() time.Time
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
		entries = append(entries, Recipe{CatalogKey: recipes.CatalogKey(r), Content: Content(r)})
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
	if s.library == nil {
		return recipes.Recipe{}, false, errors.New("catalog: no library is wired")
	}
	return s.library.AddFromCatalog(ctx, householdID, Content(e.Content))
}

// --- helpers ------------------------------------------------------------------

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

// withoutLibrary drops the entries the household already has.
func (s *Service) withoutLibrary(ctx context.Context, householdID string, entries []Recipe) ([]Recipe, error) {
	if s.library == nil || len(entries) == 0 {
		return entries, nil
	}
	keys := make([]string, 0, len(entries))
	for _, e := range entries {
		keys = append(keys, e.CatalogKey)
	}
	have, err := s.library.InLibrary(ctx, householdID, keys)
	if err != nil {
		return nil, fmt.Errorf("mark library: %w", err)
	}
	out := make([]Recipe, 0, len(entries))
	for _, e := range entries {
		if _, ok := have[e.CatalogKey]; !ok {
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
	keys := make([]string, 0, len(items))
	for _, it := range items {
		keys = append(keys, it.CatalogKey)
	}
	have, err := s.library.InLibrary(ctx, householdID, keys)
	if err != nil {
		return fmt.Errorf("mark library: %w", err)
	}
	for i := range items {
		items[i].LibraryRecipeID = have[items[i].CatalogKey]
	}
	return nil
}

// narrow applies the exact filters in memory, for the scanned discovery set.
func narrow(entries []Recipe, f Filter) []Recipe {
	if f.Cuisine == "" && f.Tag == "" && f.Ingredient == "" && f.Text == "" {
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
	if key := fold(f.Cuisine); key != "" && !containsFold(e.Content.Cuisines, key) {
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
