package catalog

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
	"github.com/Linesmerrill/DinnerOS/api/internal/recommendations"
)

// fakeSaves keeps saves in memory.
type fakeSaves struct {
	mu    sync.Mutex
	saves []Save
	err   error
}

func (f *fakeSaves) Record(_ context.Context, s Save) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.saves = append(f.saves, s)
	return nil
}

func (f *fakeSaves) Counts(_ context.Context, since time.Time) (map[string]int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	seen := map[string]bool{}
	out := map[string]int{}
	for _, s := range f.saves {
		if s.At.Before(since) || seen[s.Group+"/"+s.HouseholdID] {
			continue
		}
		seen[s.Group+"/"+s.HouseholdID] = true
		out[s.Group]++
	}
	return out, nil
}

type fakeRatings map[string]RatingStat

func (f fakeRatings) CatalogRatings(context.Context) (map[string]RatingStat, error) { return f, nil }

// memCollections is an in-memory CollectionStore.
type memCollections struct{ items []Collection }

func (m *memCollections) List(context.Context) ([]Collection, error) {
	return slices.Clone(m.items), nil
}

func (m *memCollections) Put(_ context.Context, c Collection) error {
	if i := slices.IndexFunc(m.items, func(o Collection) bool { return o.ID == c.ID }); i >= 0 {
		m.items[i] = c
		return nil
	}
	m.items = append(m.items, c)
	return nil
}

func (m *memCollections) Delete(_ context.Context, id string) error {
	i := slices.IndexFunc(m.items, func(o Collection) bool { return o.ID == id })
	if i < 0 {
		return ErrNotFound
	}
	m.items = slices.Delete(m.items, i, i+1)
	return nil
}

func sectionIDs(h Home) []string {
	out := make([]string, 0, len(h.Sections))
	for _, s := range h.Sections {
		out = append(out, s.ID)
	}
	return out
}

func section(t *testing.T, h Home, id string) Section {
	t.Helper()
	for _, s := range h.Sections {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("no %q section in %v", id, sectionIDs(h))
	return Section{}
}

func itemNames(items []Result) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Content.Name)
	}
	return out
}

// seedHome publishes a small catalog: a quick dinner, a big chili, a long
// spring dinner, and an add-on put in the store before add-ons were kept out.
func seedHome(t *testing.T, h *harness) (quick, chili, spring string) {
	t.Helper()
	quick = publishWith(t, h, "hf-quick", "Quick Tacos", func(r *recipes.Recipe) {
		r.TotalMinutes, r.Cuisines = 20, []string{"Mexican"}
	})
	chili = publishWith(t, h, "hf-chili", "Big Beef Chili", func(r *recipes.Recipe) {
		r.TotalMinutes, r.Servings = 90, []int{6, 8}
	})
	spring = publishWith(t, h, "hf-spring", "Lemon Asparagus Pasta", func(r *recipes.Recipe) {
		r.TotalMinutes, r.Cuisines = 45, []string{"italian"}
	})
	addon := Recipe{CatalogKey: "import:hf-addon", Content: recipes.Recipe{
		Source: recipes.SourceImport, SourceRecipeID: "hf-addon", Name: "Quick Garlic Bread", IsAddon: true,
		TotalMinutes: 10, Servings: []int{6}, Cuisines: []string{"Italian"},
	}}
	if _, err := h.store.Upsert(t.Context(), []Recipe{addon}, testNow); err != nil {
		t.Fatal(err)
	}
	return quick, chili, spring
}

func TestHomeBuildsRowsAndLeavesEmptyOnesOut(t *testing.T) {
	h := newHarness(t, nil)
	seedHome(t, h)
	home, err := h.service.Home(t.Context(), hhAda)
	if err != nil {
		t.Fatal(err)
	}
	// No ratings, no saves: Top Rated and Trending have nothing. Chef’s Picks
	// shows all three dinners, so the filter rows would only repeat it and
	// are left out too.
	if got, want := sectionIDs(home), []string{"chefs-picks"}; !slices.Equal(got, want) {
		t.Fatalf("sections = %v; want %v", got, want)
	}
	// Each row's own list (See All) still holds what it qualifies for.
	for id, want := range map[string][]string{
		"quick": {"Quick Tacos"}, "crowd": {"Big Beef Chili"}, "seasonal": {"Lemon Asparagus Pasta"},
	} {
		c, page, err := h.service.Section(t.Context(), hhAda, id, Query{})
		if err != nil || !slices.Equal(itemNames(page.Items), want) {
			t.Errorf("%s = %v, %v; want %v", id, itemNames(page.Items), err, want)
		}
		// testNow is in March: spring.
		if id == "seasonal" && c.Subtitle != "Just right for spring" {
			t.Errorf("seasonal subtitle = %q", c.Subtitle)
		}
	}
	if picks := section(t, home, "chefs-picks"); len(picks.Items) != 3 || picks.Title != "Chef’s Picks This Week" {
		t.Errorf("chef's picks = %q %v", picks.Title, itemNames(picks.Items))
	}
}

func TestAddOnsNeverShowAnywhere(t *testing.T) {
	h := newHarness(t, nil)
	seedHome(t, h)
	// A household's add-on is never published, even shared.
	addon := libraryRecipe(recipes.SourceManual, "", "Side Salad Add-on", "lettuce")
	addon.IsAddon, addon.SharedToCatalog = true, true
	if n, err := h.service.PublishRecipes(t.Context(), []recipes.Recipe{addon}); err != nil || n != 0 {
		t.Fatalf("PublishRecipes(add-on) = %d, %v; want 0, nil", n, err)
	}
	if err := h.service.PublishOne(t.Context(), addon); !errors.Is(err, ErrNotPublishable) {
		t.Errorf("PublishOne(add-on) = %v; want ErrNotPublishable", err)
	}

	check := func(where string, names []string) {
		t.Helper()
		for _, n := range names {
			if strings.Contains(n, "Garlic Bread") || strings.Contains(n, "Add-on") {
				t.Errorf("%s shows an add-on: %v", where, names)
			}
		}
	}
	home, err := h.service.Home(t.Context(), hhAda)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range home.Sections {
		check("row "+s.ID, itemNames(s.Items))
	}
	for _, q := range []Query{{}, {Text: "garlic"}, {MaxMinutes: 30}, {Cuisine: "italian"}} {
		d, err := h.service.Discover(t.Context(), hhAda, q)
		if err != nil {
			t.Fatal(err)
		}
		check("discover", resultNames(d))
		s, err := h.service.Search(t.Context(), hhAda, q)
		if err != nil {
			t.Fatal(err)
		}
		check("search", resultNames(s))
	}
	// Its stored entry is not found by ID either, and can't be added.
	var addonID string
	for _, e := range h.store.entries {
		if e.Content.IsAddon {
			addonID = e.ID
		}
	}
	if _, err := h.service.Get(t.Context(), hhAda, addonID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get(add-on) = %v; want ErrNotFound", err)
	}
	if _, _, err := h.service.Add(t.Context(), hhAda, addonID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Add(add-on) = %v; want ErrNotFound", err)
	}
	// Its cuisine is not offered as a filter on its own.
	filters, err := h.service.FilterGroups(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range filters[0].Options {
		if o.Value == "" {
			t.Errorf("empty cuisine option")
		}
	}
}

func TestBrowseFilters(t *testing.T) {
	h := newHarness(t, nil)
	seedHome(t, h)
	for _, tc := range []struct {
		name string
		q    Query
		want []string
	}{
		{"30 min or less", Query{MaxMinutes: 30}, []string{"Quick Tacos"}},
		{"1 hour or less", Query{MaxMinutes: 60}, []string{"Lemon Asparagus Pasta", "Quick Tacos"}},
		{"6+", Query{MinServings: 6}, []string{"Big Beef Chili"}},
		{"2+", Query{MinServings: 2}, []string{"Big Beef Chili", "Lemon Asparagus Pasta", "Quick Tacos"}},
		// Stored as "Mexican" and "italian": the choice is read canonically.
		{"cuisine, any spelling", Query{Cuisine: "mexican"}, []string{"Quick Tacos"}},
		{"cuisine, label", Query{Cuisine: "Italian"}, []string{"Lemon Asparagus Pasta"}},
		{"unknown cuisine", Query{Cuisine: "martian"}, []string{}},
		{"text and time", Query{Text: "pasta", MaxMinutes: 30}, []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := h.service.Search(t.Context(), hhAda, tc.q)
			if err != nil {
				t.Fatal(err)
			}
			if got := resultNames(s); !slices.Equal(got, tc.want) {
				t.Errorf("search = %v; want %v", got, tc.want)
			}
			d, err := h.service.Discover(t.Context(), hhAda, tc.q)
			if err != nil {
				t.Fatal(err)
			}
			got := resultNames(d)
			slices.Sort(got)
			if !slices.Equal(got, tc.want) {
				t.Errorf("discover = %v; want %v", got, tc.want)
			}
		})
	}
	for _, bad := range []Query{{MaxMinutes: -1}, {MinServings: 51}, {MaxMinutes: 24*60 + 1}} {
		if _, err := h.service.Search(t.Context(), hhAda, bad); !errors.Is(err, ErrInvalidQuery) {
			t.Errorf("Search(%+v) = %v; want ErrInvalidQuery", bad, err)
		}
	}
}

func TestFilterGroupsOfferThePresentCuisines(t *testing.T) {
	h := newHarness(t, nil)
	seedHome(t, h)
	publishWith(t, h, "hf-thai", "Green Curry", func(r *recipes.Recipe) { r.Cuisines = []string{"thai", "Thai "} })
	groups, err := h.service.FilterGroups(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, g := range groups {
		ids = append(ids, g.ID)
	}
	if !slices.Equal(ids, []string{"cuisine", "time", "servings"}) {
		t.Fatalf("groups = %v", ids)
	}
	var labels []string
	for _, o := range groups[0].Options {
		labels = append(labels, o.Label+"="+o.Value)
	}
	if want := []string{"Italian=italian", "Mexican=mexican", "Thai=thai"}; !slices.Equal(labels, want) {
		t.Errorf("cuisines = %v; want %v", labels, want)
	}
	if groups[1].Param != "maxMinutes" || groups[1].Options[0].Label != "30 min or less" || groups[1].Options[2].Label != "1 hour or less" {
		t.Errorf("time = %+v", groups[1])
	}
	if groups[2].Param != "minServings" || groups[2].Options[2].Label != "6+" {
		t.Errorf("servings = %+v", groups[2])
	}
}

// Stored cuisines read onto the standard list everywhere Shared Recipes
// shows them: chips, cards, and the cuisine filter (decision 732).
func TestCuisinesReadOntoTheStandardList(t *testing.T) {
	h := newHarness(t, nil)
	seedHome(t, h)
	publishWith(t, h, "hf-burger", "Smash Burgers", func(r *recipes.Recipe) { r.Cuisines = []string{"North America"} })
	publishWith(t, h, "hf-ribs", "Sticky Ribs", func(r *recipes.Recipe) { r.Cuisines = []string{"North American"} })
	publishWith(t, h, "hf-roast", "Roast Chicken", func(r *recipes.Recipe) { r.Cuisines = []string{"Southern Europe"} })
	publishWith(t, h, "hf-bowl", "Noodle Bowl", func(r *recipes.Recipe) { r.Cuisines = []string{"East Asia", "Japanese"} })

	groups, err := h.service.FilterGroups(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var labels []string
	for _, o := range groups[0].Options {
		labels = append(labels, o.Label+"="+o.Value)
	}
	if want := []string{"American=american", "Italian=italian", "Japanese=japanese", "Mexican=mexican"}; !slices.Equal(labels, want) {
		t.Errorf("cuisine chips = %v; want %v", labels, want)
	}

	for _, q := range []Query{{Cuisine: "american"}, {Cuisine: "North America"}} {
		s, err := h.service.Search(t.Context(), hhAda, q)
		if err != nil {
			t.Fatal(err)
		}
		d, err := h.service.Discover(t.Context(), hhAda, q)
		if err != nil {
			t.Fatal(err)
		}
		for where, page := range map[string]Page{"search": s, "discover": d} {
			got := resultNames(page)
			slices.Sort(got)
			if want := []string{"Smash Burgers", "Sticky Ribs"}; !slices.Equal(got, want) {
				t.Errorf("%s %q = %v; want %v", where, q.Cuisine, got, want)
			}
		}
	}

	d, err := h.service.Discover(t.Context(), hhAda, Query{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	cards := map[string][]string{}
	for _, it := range d.Items {
		cards[it.Content.Name] = summaryResponse(it).Cuisines
	}
	for name, want := range map[string][]string{
		"Smash Burgers": {"American"}, "Sticky Ribs": {"American"}, "Roast Chicken": {},
		"Noodle Bowl": {"Japanese"}, "Quick Tacos": {"Mexican"}, "Lemon Asparagus Pasta": {"Italian"},
	} {
		if got, ok := cards[name]; !ok || !slices.Equal(got, want) {
			t.Errorf("%s card cuisines = %q; want %q", name, got, want)
		}
	}
}

func TestHomeAppliesRestrictionsAndMarksTheLibrary(t *testing.T) {
	profile := recommendations.Profile{Version: 1, Restrictions: recommendations.Restrictions{ExcludedCuisines: []string{"mexican"}}}
	h := newHarness(t, func(o *ServiceOptions) { o.Taste = fakeTaste{profile: profile} })
	seedHome(t, h)
	h.library.have[hhAda] = map[string]string{"import:hf-chili": "own-chili"}
	home, err := h.service.Home(t.Context(), hhAda)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(sectionIDs(home), "quick") {
		t.Errorf("the only quick recipe is Mexican, which the household excluded: %v", sectionIDs(home))
	}
	picks := section(t, home, "chefs-picks").Items
	if got := itemNames(picks); !slices.Equal(got, []string{"Lemon Asparagus Pasta", "Big Beef Chili"}) {
		t.Fatalf("picks = %v; want the new one first, the one it has last", got)
	}
	if picks[1].LibraryRecipeID != "own-chili" || picks[0].LibraryRecipeID != "" {
		t.Errorf("library marks = %q, %q", picks[0].LibraryRecipeID, picks[1].LibraryRecipeID)
	}
}

func TestAddRecordsOneSaveAndTrendingCountsTheWeek(t *testing.T) {
	saves := &fakeSaves{}
	h := newHarness(t, nil)
	h.service.WithSaves(saves)
	quick, chili, _ := seedHome(t, h)

	for range 2 {
		if _, _, err := h.service.Add(t.Context(), hhAda, quick); err != nil {
			t.Fatal(err)
		}
	}
	if len(saves.saves) != 1 || saves.saves[0].CatalogID != quick || saves.saves[0].HouseholdID != hhAda || saves.saves[0].Group == "" {
		t.Fatalf("saves = %+v; want one save of the quick recipe", saves.saves)
	}
	if _, _, err := h.service.Add(t.Context(), hhBob, quick); err != nil {
		t.Fatal(err)
	}
	// An old save of the chili is outside the week.
	old := Save{CatalogID: chili, Group: groupKey(h.store.entries[1]), HouseholdID: hhBob, At: testNow.Add(-8 * 24 * time.Hour)}
	saves.saves = append(saves.saves, old)

	// Read by a third household, which has neither recipe.
	home, err := h.service.Home(t.Context(), "66e5a1f2c3b4a5d6e7f80a03")
	if err != nil {
		t.Fatal(err)
	}
	if got := itemNames(section(t, home, "trending").Items); !slices.Equal(got, []string{"Quick Tacos"}) {
		t.Errorf("trending = %v; want only this week's save", got)
	}
	// Saves count toward Top Rated too, with no ratings at all. Trending
	// claimed the tacos, so Top Rated shows the chili and nothing repeats;
	// its own list still has both.
	if got := itemNames(section(t, home, "top-rated").Items); !slices.Equal(got, []string{"Big Beef Chili"}) {
		t.Errorf("top rated = %v", got)
	}
	if _, all, err := h.service.Section(t.Context(), "66e5a1f2c3b4a5d6e7f80a03", "top-rated", Query{}); err != nil ||
		!slices.Equal(itemNames(all.Items), []string{"Quick Tacos", "Big Beef Chili"}) {
		t.Errorf("top rated See All = %v, %v", itemNames(all.Items), err)
	}

	// A failed save write never fails the add.
	saves.err = errors.New("down")
	if _, created, err := h.service.Add(t.Context(), hhBob, chili); err != nil || !created {
		t.Errorf("Add with saves down = %v, %v", created, err)
	}
}

// Chef’s Picks and Top Rated never show the same recipe (build 121 review):
// the rated recipes stay in Top Rated and Chef’s Picks shows the others.
func TestTopRatedAndChefsPicksDontRepeat(t *testing.T) {
	h := newHarness(t, nil)
	seedHome(t, h)
	h.service.WithRatings(fakeRatings{
		"import:hf-quick": {Households: 3, Sum: 15},
		"import:hf-chili": {Households: 3, Sum: 14},
	})
	home, err := h.service.Home(t.Context(), hhAda)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	for _, sec := range home.Sections {
		for _, n := range itemNames(sec.Items) {
			if first, ok := seen[n]; ok {
				t.Errorf("%s shows in %s and %s", n, first, sec.ID)
			}
			seen[n] = sec.ID
		}
	}
	if seen["Quick Tacos"] != "top-rated" || seen["Lemon Asparagus Pasta"] != "chefs-picks" {
		t.Errorf("rows = %v", seen)
	}
}

func TestTopRatedUsesHouseholdRatingsDamped(t *testing.T) {
	h := newHarness(t, nil)
	seedHome(t, h)
	h.service.WithRatings(fakeRatings{
		// One household's 5 against three households' 4.7 average.
		"import:hf-quick":  {Households: 1, Sum: 5},
		"import:hf-spring": {Households: 3, Sum: 14.1},
	})
	home, err := h.service.Home(t.Context(), hhAda)
	if err != nil {
		t.Fatal(err)
	}
	if got := itemNames(section(t, home, "top-rated").Items); !slices.Equal(got, []string{"Lemon Asparagus Pasta", "Quick Tacos"}) {
		t.Errorf("top rated = %v", got)
	}
}

func TestStoredCollectionsOverrideAndAdd(t *testing.T) {
	cols := &memCollections{}
	h := newHarness(t, nil)
	h.service.WithCollections(cols)
	quick, chili, spring := seedHome(t, h)

	// Editorial picks in order; an unknown ID is skipped.
	if _, err := h.service.PutCollection(t.Context(), Collection{
		ID: "chefs-picks", Title: "Chef’s Picks This Week", Type: TypeEditorial, Position: 10,
		RecipeIDs: []string{chili, "66e5a1f2c3b4a5d6e7f8dead", quick},
	}); err != nil {
		t.Fatal(err)
	}
	// A sponsored row, and one that ended.
	if _, err := h.service.PutCollection(t.Context(), Collection{
		ID: "pasta-week", Title: "Pasta Week", Type: TypeSponsored, Position: 5, RecipeIDs: []string{spring},
		Sponsor: &Sponsor{Name: "Acme Pasta", URL: "https://example.com"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.service.PutCollection(t.Context(), Collection{
		ID: "last-week", Title: "Last Week", Type: TypeAlgorithmic, Rule: RuleBest, Position: 1,
		EndsAt: testNow.Add(-time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	home, err := h.service.Home(t.Context(), hhAda)
	if err != nil {
		t.Fatal(err)
	}
	if got := sectionIDs(home); got[0] != "pasta-week" || got[1] != "chefs-picks" || slices.Contains(got, "last-week") {
		t.Fatalf("sections = %v", got)
	}
	if s := section(t, home, "pasta-week"); s.SponsorLabel() != "Sponsored by Acme Pasta" {
		t.Errorf("sponsor label = %q", s.SponsorLabel())
	}
	if got := itemNames(section(t, home, "chefs-picks").Items); !slices.Equal(got, []string{"Big Beef Chili", "Quick Tacos"}) {
		t.Errorf("picks = %v", got)
	}
	// See All pages through the whole row.
	c, p, err := h.service.Section(t.Context(), hhAda, "chefs-picks", Query{Limit: 1})
	if err != nil || c.ID != "chefs-picks" || len(p.Items) != 1 || p.NextCursor == "" || p.Total != 2 {
		t.Errorf("Section = %v %+v %v", c.ID, p, err)
	}
	if _, _, err := h.service.Section(t.Context(), hhAda, "last-week", Query{}); !errors.Is(err, ErrNotFound) {
		t.Errorf("an ended row = %v; want ErrNotFound", err)
	}
	// Deleting the override brings the built-in back.
	if err := h.service.DeleteCollection(t.Context(), "chefs-picks"); err != nil {
		t.Fatal(err)
	}
	all, _ := h.service.Collections(t.Context())
	if i := slices.IndexFunc(all, func(c Collection) bool { return c.ID == "chefs-picks" }); i < 0 || !all[i].BuiltIn {
		t.Errorf("chef's picks after delete = %+v", all)
	}
}

func TestCollectionValidate(t *testing.T) {
	ok := "66e5a1f2c3b4a5d6e7f80b01"
	for _, tc := range []struct {
		name string
		c    Collection
		want string
	}{
		{"good editorial", Collection{ID: "fall-soups", Title: "Fall Soups", Type: TypeEditorial, RecipeIDs: []string{ok, ok}}, ""},
		{"good computed", Collection{ID: "quick-2", Title: "Fast", Type: TypeAlgorithmic, Rule: RuleQuick}, ""},
		{"bad slug", Collection{ID: "Fall Soups", Title: "x", Type: TypeEditorial, Rule: RuleBest}, "lowercase words"},
		{"no title", Collection{ID: "a", Type: TypeEditorial, Rule: RuleBest}, "title"},
		{"bad type", Collection{ID: "a", Title: "A", Type: "promo", Rule: RuleBest}, "type"},
		{"bad rule", Collection{ID: "a", Title: "A", Type: TypeAlgorithmic, Rule: "random"}, "rule"},
		{"computed without rule", Collection{ID: "a", Title: "A", Type: TypeAlgorithmic}, "needs a rule"},
		{"editorial with nothing", Collection{ID: "a", Title: "A", Type: TypeEditorial}, "Pick some recipes"},
		{"bad recipe id", Collection{ID: "a", Title: "A", Type: TypeEditorial, RecipeIDs: []string{"x"}}, "recipe IDs"},
		{"sponsored without sponsor", Collection{ID: "a", Title: "A", Type: TypeSponsored, Rule: RuleBest}, "sponsor’s name"},
		{"sponsored http link", Collection{ID: "a", Title: "A", Type: TypeSponsored, Rule: RuleBest, Sponsor: &Sponsor{Name: "X", URL: "http://x"}}, "https"},
		{"ends before it starts", Collection{ID: "a", Title: "A", Type: TypeEditorial, Rule: RuleBest, StartsAt: testNow, EndsAt: testNow}, "end date"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := tc.c
			err := c.Validate()
			if tc.want == "" {
				if err != nil {
					t.Fatalf("Validate = %v", err)
				}
				if len(c.RecipeIDs) > 1 {
					t.Errorf("duplicate IDs kept: %v", c.RecipeIDs)
				}
				return
			}
			if !errors.Is(err, ErrInvalidCollection) || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Validate = %v; want one mentioning %q", err, tc.want)
			}
		})
	}
	// Sponsor fields belong only to a sponsored row.
	c := Collection{ID: "a", Title: "A", Type: TypeEditorial, Rule: RuleBest, Sponsor: &Sponsor{Name: "X"}}
	if err := c.Validate(); err != nil || c.Sponsor != nil || c.SponsorLabel() != "" {
		t.Errorf("editorial kept a sponsor: %+v %v", c.Sponsor, err)
	}
}

func TestSponsoredRowWithNothingToShowIsLeftOut(t *testing.T) {
	cols := &memCollections{}
	h := newHarness(t, nil)
	h.service.WithCollections(cols)
	seedHome(t, h)
	// Every pick is gone, so the paid row has nothing; it must not show as an empty shelf.
	if _, err := h.service.PutCollection(t.Context(), Collection{
		ID: "pasta-week", Title: "Pasta Week", Type: TypeSponsored, Position: 5,
		RecipeIDs: []string{"66e5a1f2c3b4a5d6e7f8dead"}, Sponsor: &Sponsor{Name: "Acme Pasta"},
	}); err != nil {
		t.Fatal(err)
	}
	home, err := h.service.Home(t.Context(), hhAda)
	if err != nil {
		t.Fatal(err)
	}
	if got := sectionIDs(home); slices.Contains(got, "pasta-week") {
		t.Errorf("sections = %v; an empty sponsored row showed", got)
	}
	for _, s := range home.Sections {
		if len(s.Items) == 0 {
			t.Errorf("%s has no recipes", s.ID)
		}
	}
}

func TestDefaultCollectionsShipNoSponsoredRow(t *testing.T) {
	want := []string{"chefs-picks", "top-rated", "trending", "seasonal", "quick", "crowd"}
	var got []string
	for _, c := range DefaultCollections() {
		got = append(got, c.ID)
		if c.Type == TypeSponsored || c.Sponsor != nil {
			t.Errorf("%s is sponsored", c.ID)
		}
		if err := c.Validate(); err != nil {
			t.Errorf("%s: %v", c.ID, err)
		}
		if strings.ContainsAny(c.Title+c.Subtitle, "\u2014\u2013") {
			t.Errorf("%s has a dash in its words", c.ID)
		}
	}
	if !slices.Equal(got, want) {
		t.Errorf("defaults = %v; want %v", got, want)
	}
}

func TestSeasonOfAndSeasonWords(t *testing.T) {
	for month, want := range map[time.Month]string{
		time.January: "winter", time.April: "spring", time.July: "summer", time.October: "fall", time.December: "winter",
	} {
		if got := seasonOf(time.Date(2026, month, 10, 0, 0, 0, 0, time.UTC)); got != want {
			t.Errorf("seasonOf(%s) = %s; want %s", month, got, want)
		}
	}
	for _, tc := range []struct {
		name, season string
		want         bool
	}{
		{"Butternut Squash Soup", "fall", true},
		{"Shepherd's Pie", "winter", true},
		{"Grilled Peach Salad", "summer", true},
		{"Peanut Noodles", "spring", false}, // "pea" is not "peas"
		{"Lemon Asparagus Pasta", "winter", false},
	} {
		r := Result{Recipe: Recipe{Content: recipes.Recipe{Name: tc.name}}}
		if got := inSeason(r, tc.season); got != tc.want {
			t.Errorf("inSeason(%q, %s) = %v; want %v", tc.name, tc.season, got, tc.want)
		}
	}
}
