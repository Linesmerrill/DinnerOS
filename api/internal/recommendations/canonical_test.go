package recommendations

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"
	"unicode"

	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot"
	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot/baseline"
	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// Cuisine labels here are made up to exercise the shapes imported catalogs
// use (singular and plural regions, regions next to countries); none come from
// a real catalog.

func TestCanonicalCuisine(t *testing.T) {
	for in, want := range map[string]string{
		"North America":      "north american",
		" north   American ": "north american",
		"American":           "north american",
		"East Asia":          "east asian",
		"East Asian":         "east asian",
		"Asian":              "asian",
		"South-East Asian":   "southeast asian",
		"Southern Europe":    "southern european",
		"Italian":            "italian",
		"Middle East":        "middle eastern",
		"Latin":              "latin american",
		"Southwest":          "southwestern",
		"Tex Mex":            "tex-mex",
		"Pacific Islands":    "pacific islander",
		"Central Africa":     "central african",
		"Klingon":            "klingon",
		"  ":                 "",
	} {
		if got := canonicalCuisine(in); got != want {
			t.Errorf("canonicalCuisine(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{"FamilyFriendly": "family friendly", " One  Pot": "one pot", "Spicy": "spicy"} {
		if got := canonicalTag(in); got != want {
			t.Errorf("canonicalTag(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCuisineTable(t *testing.T) {
	seen := map[string]string{}
	for _, e := range cuisineTable {
		for _, key := range append([]string{e.value}, e.aliases...) {
			if normalizeValue(key) != key {
				t.Errorf("%q isn't normalized", key)
			}
			if other, dup := seen[key]; dup {
				t.Errorf("%q maps to both %q and %q", key, other, e.value)
			}
			seen[key] = e.value
			if got := canonicalCuisine(key); got != e.value {
				t.Errorf("canonicalCuisine(%q) = %q, want %q", key, got, e.value)
			}
		}
		if strings.ToLower(e.label) != e.value {
			t.Errorf("label %q doesn't spell %q", e.label, e.value)
		}
		for _, word := range strings.FieldsFunc(e.label, func(r rune) bool { return r == ' ' || r == '-' }) {
			if !unicode.IsUpper([]rune(word)[0]) {
				t.Errorf("label %q isn't title case", e.label)
			}
		}
		if e.parent != "" && cuisineLabel(e.parent) == "" {
			t.Errorf("%q has unknown parent %q", e.value, e.parent)
		}
		if n := len(cuisineAncestors(e.value)); n > 3 {
			t.Errorf("%q has %d ancestors; the hierarchy is cyclic or too deep", e.value, n)
		}
	}
}

func TestCuisineHierarchy(t *testing.T) {
	if got := cuisineAncestors("italian"); !slices.Equal(got, []string{"southern european", "european"}) {
		t.Errorf("italian ancestors = %v", got)
	}
	if got := cuisineAncestors("klingon"); got != nil {
		t.Errorf("unknown cuisine ancestors = %v", got)
	}
	if got := cuisineRegions([]string{"italian", "southern european", "japanese"}); !slices.Equal(got, []string{"european", "east asian", "asian"}) {
		t.Errorf("regions = %v", got)
	}

	r := recipeWith("Dinner", "Rice")
	r.Cuisines = []string{"Southern Europe", "Italian", "southern european"}
	r.Tags = []string{"FamilyFriendly", "Family Friendly"}
	a := attributes(r, nil, defaultBands)
	if !slices.Equal(a.Cuisines, []string{"southern european", "italian"}) || !slices.Equal(a.CuisineRegions, []string{"european"}) ||
		!slices.Equal(a.Tags, []string{"family friendly"}) {
		t.Errorf("attributes = cuisines %v, regions %v, tags %v", a.Cuisines, a.CuisineRegions, a.Tags)
	}
	if it := a.item(r); !slices.Equal(it.Cuisines, []string{"southern european", "italian"}) || !slices.Equal(it.CuisineRegions, []string{"european"}) {
		t.Errorf("item cuisines = %v, regions %v", it.Cuisines, it.CuisineRegions)
	}
	if got := newAttributesResponse(a).CuisineRegions; !slices.Equal(got, []string{"european"}) {
		t.Errorf("response regions = %v", got)
	}
}

// TestRegionPreferences checks that a region in a like, an exclusion, or a
// weekday rule covers the region's cuisines end to end.
func TestRegionPreferences(t *testing.T) {
	recipe := func(id string, cuisines ...string) recipes.Recipe {
		r := recipeWith("Dinner "+id, "Rice")
		r.ID, r.Cuisines = id, cuisines
		return r
	}
	catalog := []recipes.Recipe{
		recipe("pasta", "Italian"), recipe("ramen", "East Asia"), recipe("bowl", "Asian"), recipe("stew", "Klingon"),
	}
	profile := DefaultProfile(hhA)
	profile.Taste.Likes.Cuisines = []string{"Southern Europe"}
	profile.Restrictions.ExcludedCuisines = []string{"Asia"}
	profile.WeekdayRules = []WeekdayRule{{Day: "tue", Label: "Euro Tuesday", Cuisines: []string{"European"}}}
	profile, err := normalizeProfile(profile)
	if err != nil {
		t.Fatal(err)
	}
	in := autopilot.Input{HouseholdID: hhA, Week: "2026-W38", Preferences: profile.preferences(2)}
	for _, r := range catalog {
		in.Catalog = append(in.Catalog, attributes(r, nil, profile.bands()).item(r))
	}
	res, err := baseline.New(baseline.Options{}).RankMeals(context.Background(), autopilot.RankRequest{Input: in, Day: autopilot.Tuesday, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, rec := range res.Items {
		ids = append(ids, rec.ItemID)
	}
	if !slices.Equal(ids, []string{"pasta", "stew"}) {
		t.Fatalf("ranked %v; want Italian first and every Asian recipe excluded", ids)
	}
	if !slices.ContainsFunc(res.Items[0].Reasons, func(r autopilot.Reason) bool { return r.Text == "Euro Tuesday · European" }) {
		t.Errorf("pasta reasons = %+v; want the European rule", res.Items[0].Reasons)
	}
}

func TestProfilesReadAndWriteCanonicalValues(t *testing.T) {
	stored := DefaultProfile(hhA)
	stored.Taste.Likes = Choices{Cuisines: []string{"thai", "north america"}, Tags: []string{"familyfriendly"}}
	stored.Taste.Dislikes.Cuisines = []string{"east asia"}
	stored.Restrictions.ExcludedCuisines = []string{"middle east"}
	stored.Restrictions.ExcludedTags = []string{"onepot"}
	stored.WeekdayRules = []WeekdayRule{{Day: "tue", Label: "Taco Tuesday", Cuisines: []string{"southwest", "latin america"}, Frequency: "every_week"}}
	got := canonicalProfile(stored)
	for name, pair := range map[string][2][]string{
		"likes.cuisines":    {got.Taste.Likes.Cuisines, {"north american", "thai"}},
		"likes.tags":        {got.Taste.Likes.Tags, {"family friendly"}},
		"dislikes.cuisines": {got.Taste.Dislikes.Cuisines, {"east asian"}},
		"excludedCuisines":  {got.Restrictions.ExcludedCuisines, {"middle eastern"}},
		"excludedTags":      {got.Restrictions.ExcludedTags, {"one pot"}},
		"rule cuisines":     {got.WeekdayRules[0].Cuisines, {"latin american", "southwestern"}},
	} {
		if !slices.Equal(pair[0], pair[1]) {
			t.Errorf("%s = %v, want %v", name, pair[0], pair[1])
		}
	}
	if stored.Taste.Likes.Cuisines[1] != "north america" {
		t.Error("canonicalProfile changed the stored profile's lists")
	}

	// Merged spellings can conflict; the restrictive choice wins.
	conflict := DefaultProfile(hhA)
	conflict.Taste.Likes.Cuisines = []string{"North America", "thai"}
	conflict.Restrictions.ExcludedCuisines = []string{"north american"}
	if got := canonicalProfile(conflict).Taste.Likes.Cuisines; !slices.Equal(got, []string{"thai"}) {
		t.Errorf("likes = %v; an excluded cuisine can't stay liked", got)
	}

	written := DefaultProfile(hhA)
	written.Taste.Likes.Cuisines = []string{"North America", "north american", "East Asia"}
	written, err := normalizeProfile(written)
	if err != nil || !slices.Equal(written.Taste.Likes.Cuisines, []string{"east asian", "north american"}) {
		t.Fatalf("written likes = %v, %v", written.Taste.Likes.Cuisines, err)
	}
	if again := canonicalProfile(written); !reflect.DeepEqual(again, written) {
		t.Errorf("reading a written profile changed it:\n%+v\n%+v", again, written)
	}
	if _, err := normalizeProfile(conflict); err == nil {
		t.Error("liking and excluding spellings of the same cuisine should be invalid")
	}

	// A profile saved with older spellings reads canonically, and saving the
	// same choices again isn't a change.
	env := newTestEnv(t)
	ctx := context.Background()
	stored.Version = 1
	env.store.profiles[hhA] = stored
	p, err := env.svc.Profile(ctx, hhA)
	if err != nil || !slices.Equal(p.Taste.Likes.Cuisines, []string{"north american", "thai"}) {
		t.Fatalf("read profile likes = %v, %v", p.Taste.Likes.Cuisines, err)
	}
	same := Taste{Likes: Choices{Cuisines: []string{"North America", "Thai"}, Tags: []string{"Family Friendly"}}, Dislikes: Choices{Cuisines: []string{"East Asian"}}}
	p, err = env.svc.UpdateProfile(ctx, hhA, userAda, ProfileUpdate{Taste: &same}, false)
	if err != nil || p.Version != 1 || len(env.events.ofType(events.TypeAutopilotPreferencesUpdated)) != 0 {
		t.Errorf("saving the same taste = version %d, %v, %d events; want no change", p.Version, err, len(env.events.ofType(events.TypeAutopilotPreferencesUpdated)))
	}
}

func TestVocabularyMergesCuisineSpellings(t *testing.T) {
	var catalog []recipes.Recipe
	add := func(n int, cuisines ...string) {
		for range n {
			r := recipeWith("Dinner", "Rice")
			r.Cuisines = cuisines
			catalog = append(catalog, r)
		}
	}
	add(3, "North America")
	add(2, "North American")
	add(2, "East Asia")
	add(1, "East Asian")
	add(2, "Asian")
	add(1, "Southern Europe")
	add(2, "Italian")
	add(1, "Italian", "Southern European")
	add(1, "Klingon Fusion")
	v := buildVocabulary(catalog)

	byValue := map[string]Option{}
	for _, o := range v.Cuisines {
		if _, dup := byValue[o.Value]; dup {
			t.Errorf("%q is listed twice", o.Value)
		}
		byValue[o.Value] = o
	}
	for value, want := range map[string]Option{
		"north american":    {Label: "North American", RecipeCount: 5},
		"asian":             {Label: "Asian", RecipeCount: 5},
		"east asian":        {Label: "East Asian", RecipeCount: 3},
		"european":          {Label: "European", RecipeCount: 4},
		"southern european": {Label: "Southern European", RecipeCount: 4},
		"italian":           {Label: "Italian", RecipeCount: 3},
		"klingon fusion":    {Label: "Klingon Fusion", RecipeCount: 1},
		"thai":              {Label: "Thai", RecipeCount: 0},
	} {
		if got := byValue[value]; got.Label != want.Label || got.RecipeCount != want.RecipeCount {
			t.Errorf("%s = %+v, want %+v", value, got, want)
		}
	}
	for _, gone := range []string{"north america", "east asia", "southern europe", "american"} {
		if o, ok := byValue[gone]; ok {
			t.Errorf("%q = %+v; it should merge into its canonical value", gone, o)
		}
	}
	if v.Cuisines[0].Value != "asian" && v.Cuisines[0].Value != "north american" {
		t.Errorf("most used first: %+v", v.Cuisines[:2])
	}
}
