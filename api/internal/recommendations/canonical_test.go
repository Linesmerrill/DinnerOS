package recommendations

import (
	"context"
	"reflect"
	"slices"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot"
	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot/baseline"
	"github.com/Linesmerrill/DinnerOS/api/internal/cuisine"
	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// Cuisine labels here are made up to exercise the shapes imported catalogs
// use (singular and plural regions, regions next to countries); none come from
// a real catalog.

func TestCanonicalCuisine(t *testing.T) {
	for in, want := range map[string]string{
		"North America":      "american",
		" north   American ": "american",
		"American":           "american",
		"USA":                "american",
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
	// The region shows nowhere but still matches (decision 732).
	regions := []string{"southern european", "european"}
	if !slices.Equal(a.Cuisines, []string{"italian"}) || !slices.Equal(a.CuisineRegions, regions) ||
		!slices.Equal(a.Tags, []string{"family friendly"}) {
		t.Errorf("attributes = cuisines %v, regions %v, tags %v", a.Cuisines, a.CuisineRegions, a.Tags)
	}
	if it := a.item(r); !slices.Equal(it.Cuisines, []string{"italian"}) || !slices.Equal(it.CuisineRegions, regions) {
		t.Errorf("item cuisines = %v, regions %v", it.Cuisines, it.CuisineRegions)
	}
	if got := newAttributesResponse(a).CuisineRegions; !slices.Equal(got, regions) {
		t.Errorf("response regions = %v", got)
	}

	// A recipe stored with only a region, or an unknown value, shows none
	// and matches what it stored.
	for _, tc := range []struct {
		stored        []string
		name          string
		shown, region []string
	}{
		{[]string{"East Asia"}, "Dinner", nil, []string{"east asian", "asian"}},
		{[]string{"North America"}, "Dinner", []string{"american"}, nil},
		{[]string{"Southern Europe"}, "Pesto Penne", []string{"italian"}, []string{"southern european", "european"}},
		{[]string{"Klingon"}, "Dinner", nil, []string{"klingon"}},
	} {
		r := recipeWith(tc.name, "Rice")
		r.Cuisines = tc.stored
		a := attributes(r, nil, defaultBands)
		if !slices.Equal(a.Cuisines, tc.shown) || !slices.Equal(a.CuisineRegions, tc.region) {
			t.Errorf("%q %v: cuisines %v, regions %v; want %v, %v", tc.name, tc.stored, a.Cuisines, a.CuisineRegions, tc.shown, tc.region)
		}
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
		"likes.cuisines":    {got.Taste.Likes.Cuisines, {"american", "thai"}},
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
	if err != nil || !slices.Equal(written.Taste.Likes.Cuisines, []string{"american", "east asian"}) {
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
	if err != nil || !slices.Equal(p.Taste.Likes.Cuisines, []string{"american", "thai"}) {
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
	// Only standard cuisines are listed (decision 732): both spellings of
	// North America count as American, and regions and unknown values
	// aren't offered.
	for value, want := range map[string]Option{
		"american": {Label: "American", RecipeCount: 5},
		"italian":  {Label: "Italian", RecipeCount: 3},
		"thai":     {Label: "Thai", RecipeCount: 0},
	} {
		if got := byValue[value]; got.Label != want.Label || got.RecipeCount != want.RecipeCount {
			t.Errorf("%s = %+v, want %+v", value, got, want)
		}
	}
	for _, gone := range []string{
		"north american", "north america", "asian", "east asian", "east asia", "european", "southern european",
		"southern europe", "klingon fusion",
	} {
		if o, ok := byValue[gone]; ok {
			t.Errorf("%q = %+v; only standard cuisines are listed", gone, o)
		}
	}
	if v.Cuisines[0].Value != "american" {
		t.Errorf("most used first: %+v", v.Cuisines[:2])
	}
}

// The cuisine picker always offers the standard list, whatever the catalog
// holds, even when many other cuisines are counted; a recipe with no cuisine
// counts for the one its title names (decisions 721 and 722).
func TestVocabularyAlwaysHasTheStandardCuisines(t *testing.T) {
	empty := buildVocabulary(nil)
	values := func(v Vocabulary) []string {
		var out []string
		for _, o := range v.Cuisines {
			out = append(out, o.Value)
		}
		return out
	}
	for _, want := range cuisine.Standard() {
		if !slices.Contains(values(empty), want) {
			t.Errorf("empty catalog: missing %q", want)
		}
	}
	if o := empty.Cuisines[0]; o.Value != "american" || o.Label != "American" {
		t.Errorf("standard list isn't alphabetical: first %+v", o)
	}

	var catalog []recipes.Recipe
	for i := range 40 {
		r := recipeWith("Dinner", "Rice")
		r.Cuisines = []string{"Made Up " + string(rune('A'+i%26)) + string(rune('a'+i/26))}
		catalog = append(catalog, r)
	}
	catalog = append(catalog, recipeWith("Carnitas Tacos", "Pork"))
	full := buildVocabulary(catalog)
	if len(full.Cuisines) > MaxVocabularyOptions {
		t.Errorf("%d options, over the cap", len(full.Cuisines))
	}
	for _, want := range cuisine.Standard() {
		if !slices.Contains(values(full), want) {
			t.Errorf("crowded catalog: missing %q", want)
		}
	}
	if full.Cuisines[0].Value != "mexican" && !slices.ContainsFunc(full.Cuisines, func(o Option) bool { return o.Value == "mexican" && o.RecipeCount == 1 }) {
		t.Errorf("the tacos didn't count as Mexican: %+v", full.Cuisines[:3])
	}
}
