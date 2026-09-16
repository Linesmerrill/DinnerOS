package customize

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
	"github.com/Linesmerrill/DinnerOS/api/internal/recommendations"
)

const (
	hhAda    = "66e5a1f2c3b4a5d6e7f80a01"
	hhBob    = "66e5a1f2c3b4a5d6e7f80b01"
	userAda  = "66e5a1f2c3b4a5d6e7f83001"
	userView = "66e5a1f2c3b4a5d6e7f83002"
	testWeek = "2026-W38"
	entryID  = "66e5a1f2c3b4a5d6e7f8c001"
	// onionRecipe has no protein lines.
	onionRecipe = "66e5a1f2c3b4a5d6e7f8b002"
)

var testNow = time.Date(2026, 9, 15, 18, 30, 0, 0, time.UTC)

// --- fakes --------------------------------------------------------------------

type fakePlans struct {
	mu    sync.Mutex
	plans map[string]planning.Plan // week → plan
	err   error
}

func newFakePlans(entries ...planning.Entry) *fakePlans {
	w, _ := planning.ParseWeek(testWeek)
	return &fakePlans{plans: map[string]planning.Plan{
		testWeek: {HouseholdID: hhAda, Week: w, Status: planning.StatusDraft, Entries: entries},
	}}
}

func (f *fakePlans) Get(_ context.Context, householdID, week string) (planning.Plan, error) {
	w, err := planning.ParseWeek(week)
	if err != nil {
		return planning.Plan{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return planning.Plan{}, f.err
	}
	p, ok := f.plans[week]
	if !ok || p.HouseholdID != householdID {
		return planning.Plan{HouseholdID: householdID, Week: w, Status: planning.StatusDraft}, nil
	}
	return p, nil
}

func (f *fakePlans) SetEntryCustomizations(_ context.Context, householdID, week, id string, list []planning.Customization) (planning.Plan, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.plans[week]
	if !ok || p.HouseholdID != householdID {
		return planning.Plan{}, planning.ErrNotFound
	}
	i := slices.IndexFunc(p.Entries, func(e planning.Entry) bool { return e.ID == id })
	if i < 0 {
		return planning.Plan{}, planning.ErrNotFound
	}
	if p.Status == planning.StatusFinalized {
		return planning.Plan{}, planning.ErrFinalized
	}
	p.Entries = slices.Clone(p.Entries)
	p.Entries[i].Customizations = slices.Clone(list)
	if len(list) == 0 {
		p.Entries[i].Customizations = nil
	}
	f.plans[week] = p
	return p, nil
}

// entrySearchWeeks mirrors planning's entry lookup window, so a test can tell
// a plan that is still reachable from one that is too old.
const entrySearchWeeks = 8

func (f *fakePlans) FindEntry(_ context.Context, householdID, id string, near time.Time) (planning.Plan, planning.Entry, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return planning.Plan{}, planning.Entry{}, false, f.err
	}
	w := planning.WeekOf(near)
	first, last := w.AddWeeks(-entrySearchWeeks).Monday(), w.AddWeeks(1).Monday()
	for _, p := range f.plans {
		if p.HouseholdID != householdID {
			continue
		}
		if monday := p.Week.Monday(); monday.Before(first) || monday.After(last) {
			continue
		}
		for _, e := range p.Entries {
			if e.ID == id {
				return p, e, true, nil
			}
		}
	}
	return planning.Plan{}, planning.Entry{}, false, nil
}

func (f *fakePlans) entry(t *testing.T, id string) planning.Entry {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, e := range f.plans[testWeek].Entries {
		if e.ID == id {
			return e
		}
	}
	t.Fatalf("no entry %s", id)
	return planning.Entry{}
}

type fakeRecipes map[string]recipes.Recipe

func (f fakeRecipes) Get(_ context.Context, householdID, id string) (recipes.Recipe, error) {
	r, ok := f[id]
	if !ok || r.HouseholdID != householdID {
		return recipes.Recipe{}, recipes.ErrNotFound
	}
	return r, nil
}

type fakeCatalog struct {
	items []recipes.Ingredient
	err   error
	calls int
}

func newFakeCatalog() *fakeCatalog {
	return &fakeCatalog{items: []recipes.Ingredient{
		{ID: ingPork, Key: "ground pork", Name: "Ground Pork", Category: "meat-seafood", ImageURL: "https://img.example.com/pork.png"},
		{ID: ingBeef, Key: "ground beef", Name: "Ground Beef", Category: "meat-seafood", ImageURL: "https://img.example.com/beef.png"},
		{ID: "66e5a1f2c3b4a5d6e7f8a010", Key: "chopped chicken breast", Name: "Chopped Chicken Breast", Category: "meat-seafood"},
	}}
}

func (f *fakeCatalog) IngredientsByID(_ context.Context, ids []string) ([]recipes.Ingredient, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	var out []recipes.Ingredient
	for _, ing := range f.items {
		if slices.Contains(ids, ing.ID) {
			out = append(out, ing)
		}
	}
	return out, nil
}

func (f *fakeCatalog) IngredientsByKey(_ context.Context, keys []string) ([]recipes.Ingredient, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	var out []recipes.Ingredient
	for _, ing := range f.items {
		if slices.Contains(keys, ing.Key) {
			out = append(out, ing)
		}
	}
	return out, nil
}

type fakeProfiles struct {
	restrictions recommendations.Restrictions
	err          error
}

func (f fakeProfiles) Profile(_ context.Context, householdID string) (recommendations.Profile, error) {
	if f.err != nil {
		return recommendations.Profile{}, f.err
	}
	return recommendations.Profile{HouseholdID: householdID, Restrictions: f.restrictions}, nil
}

type fakeRecorder struct {
	mu     sync.Mutex
	stored []events.Event
	err    error
}

func (f *fakeRecorder) Record(_ context.Context, e events.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.stored = append(f.stored, e)
	return nil
}

func (f *fakeRecorder) all() []events.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.stored)
}

// --- fixture ------------------------------------------------------------------

type fixture struct {
	svc      *Service
	plans    *fakePlans
	catalog  *fakeCatalog
	profiles *fakeProfiles
	events   *fakeRecorder
	ctx      context.Context
}

func householdTacos() recipes.Recipe {
	r := tacosRecipe()
	r.HouseholdID = hhAda
	return r
}

func newFixture(t *testing.T, entries ...planning.Entry) *fixture {
	t.Helper()
	if len(entries) == 0 {
		entries = []planning.Entry{{ID: entryID, RecipeID: householdTacos().ID, RecipeName: "One-Pan Pork Tacos", Servings: 4}}
	}
	plans := newFakePlans(entries...)
	catalog, profiles, recorder := newFakeCatalog(), &fakeProfiles{}, &fakeRecorder{}
	svc := NewService(ServiceOptions{
		Plans:   plans,
		Recipes: fakeRecipes{householdTacos().ID: householdTacos(), onionRecipe: {ID: onionRecipe, HouseholdID: hhAda, Name: "Onion Soup", Servings: []int{2}, Ingredients: []recipes.RecipeIngredient{{IngredientID: ingOnion, Name: "Yellow Onion", Amounts: amounts("count", "1", "2")}}}},
		Catalog: catalog, Profiles: profiles, Events: recorder, Now: func() time.Time { return testNow },
	})
	return &fixture{svc: svc, plans: plans, catalog: catalog, profiles: profiles, events: recorder, ctx: context.Background()}
}

func groupIDs(g Group) []string {
	out := make([]string, 0, len(g.Choices))
	for _, c := range g.Choices {
		out = append(out, c.ID)
	}
	return out
}

// --- tests --------------------------------------------------------------------

func TestOptionsServings(t *testing.T) {
	f := newFixture(t)
	recipeID := householdTacos().ID

	// Without a serving size, the recipe's smallest.
	o, err := f.svc.Options(f.ctx, hhAda, recipeID, Query{})
	if err != nil {
		t.Fatal(err)
	}
	if o.Servings != 2 || len(o.Groups) != 2 || o.RecipeID != recipeID {
		t.Fatalf("Options() = %+v", o)
	}
	pork := o.Groups[0]
	if pork.Line.Key != ingPork || pork.Line.Quantity.String() != "10" || pork.Selected != "" {
		t.Errorf("pork group = %+v", pork)
	}
	if pork.Choices[0].Quantity.String() != "10" || pork.Choices[1].Quantity.String() != "20" {
		t.Errorf("amounts = %+v", pork.Choices[:2])
	}
	// The catalog fills in images and the swap's ingredient.
	if pork.ImageURL != "https://img.example.com/pork.png" {
		t.Errorf("group image = %q", pork.ImageURL)
	}
	beef := pork.Choices[slices.IndexFunc(pork.Choices, func(c ChoiceOption) bool { return c.ID == "swap:ground-beef" })]
	if beef.IngredientName != "Ground Beef" || beef.ImageURL != "https://img.example.com/beef.png" || beef.Quantity.String() != "10" {
		t.Errorf("beef choice = %+v", beef)
	}
	// A swap without a catalog ingredient still offers the protein's name.
	turkey := pork.Choices[slices.IndexFunc(pork.Choices, func(c ChoiceOption) bool { return c.ID == "swap:ground-turkey:double" })]
	if turkey.IngredientName != "Ground Turkey" || turkey.ImageURL != "" || turkey.Quantity.String() != "20" {
		t.Errorf("turkey choice = %+v", turkey)
	}

	// An explicit size scales the amounts; the cutlets line has no 4-serving
	// amount, so it has no group.
	o, err = f.svc.Options(f.ctx, hhAda, recipeID, Query{Servings: 4})
	if err != nil || o.Servings != 4 || len(o.Groups) != 1 || o.Groups[0].Choices[1].Quantity.String() != "40" {
		t.Fatalf("Options(servings=4) = %+v, %v", o, err)
	}
	if _, err := f.svc.Options(f.ctx, hhAda, recipeID, Query{Servings: 3}); !errors.Is(err, ErrInvalid) {
		t.Errorf("Options(servings=3) error = %v", err)
	}

	// A recipe without protein lines has no groups.
	o, err = f.svc.Options(f.ctx, hhAda, onionRecipe, Query{})
	if err != nil || len(o.Groups) != 0 || o.Groups == nil {
		t.Errorf("Options(onion) = %+v, %v", o, err)
	}
	if _, err := f.svc.Options(f.ctx, hhAda, "66e5a1f2c3b4a5d6e7f89999", Query{}); !errors.Is(err, ErrRecipeNotFound) {
		t.Errorf("unknown recipe error = %v", err)
	}
	if _, err := f.svc.Options(f.ctx, hhBob, recipeID, Query{}); !errors.Is(err, ErrRecipeNotFound) {
		t.Errorf("other household error = %v", err)
	}
}

func TestOptionsForEntry(t *testing.T) {
	f := newFixture(t)
	recipeID := householdTacos().ID

	// The entry's servings win, and the selection is reported.
	o, err := f.svc.Options(f.ctx, hhAda, recipeID, Query{EntryID: entryID, Week: testWeek})
	if err != nil {
		t.Fatal(err)
	}
	if o.Servings != 4 || len(o.Groups) != 1 || o.Groups[0].Selected != ChoiceOriginal {
		t.Fatalf("Options(entry) = %+v", o)
	}
	// An explicit size still wins over the entry's.
	if o, err := f.svc.Options(f.ctx, hhAda, recipeID, Query{EntryID: entryID, Week: testWeek, Servings: 2}); err != nil || o.Servings != 2 {
		t.Errorf("Options(entry, servings=2) = %+v, %v", o, err)
	}
	// A customized entry reports its choice.
	if _, err := f.svc.SetCustomizations(f.ctx, hhAda, userAda, testWeek, entryID, []Selection{{IngredientKey: ingPork, ChoiceID: "swap:ground-beef"}}); err != nil {
		t.Fatal(err)
	}
	if o, err := f.svc.Options(f.ctx, hhAda, recipeID, Query{EntryID: entryID, Week: testWeek}); err != nil || o.Groups[0].Selected != "swap:ground-beef" {
		t.Errorf("selected = %+v, %v", o.Groups[0].Selected, err)
	}

	for _, tc := range []struct {
		name string
		q    Query
		want error
	}{
		{"entry without week", Query{EntryID: entryID}, ErrInvalid},
		{"week without entry", Query{Week: testWeek}, ErrInvalid},
		{"bad week", Query{EntryID: entryID, Week: "nonsense"}, planning.ErrInvalidWeek},
		{"unknown entry", Query{EntryID: "66e5a1f2c3b4a5d6e7f89999", Week: testWeek}, planning.ErrNotFound},
	} {
		if _, err := f.svc.Options(f.ctx, hhAda, recipeID, tc.q); !errors.Is(err, tc.want) {
			t.Errorf("%s: error = %v, want %v", tc.name, err, tc.want)
		}
	}
	// An entry for another recipe isn't this recipe's entry.
	if _, err := f.svc.Options(f.ctx, hhAda, onionRecipe, Query{EntryID: entryID, Week: testWeek}); !errors.Is(err, planning.ErrNotFound) {
		t.Errorf("entry of another recipe error = %v", err)
	}
}

func TestOptionsRestrictions(t *testing.T) {
	f := newFixture(t)
	f.profiles.restrictions = recommendations.Restrictions{
		ExcludedProteins:    []string{"beef"},
		Allergens:           []string{"soy"},
		ExcludedIngredients: []string{"turkey"},
	}
	o, err := f.svc.Options(f.ctx, hhAda, householdTacos().ID, Query{})
	if err != nil {
		t.Fatal(err)
	}
	got := groupIDs(o.Groups[0])
	for _, unwanted := range []string{"swap:ground-beef", "swap:ground-beef:double", "swap:ground-turkey", "swap:ground-turkey:double"} {
		if slices.Contains(got, unwanted) {
			t.Errorf("%s survived the restrictions: %v", unwanted, got)
		}
	}
	if !slices.Contains(got, "original") || !slices.Contains(got, "swap:chopped-chicken-breast") {
		t.Errorf("choices = %v", got)
	}
	// Tofu is out for the cutlets line (soy allergen).
	if cutlets := groupIDs(o.Groups[1]); slices.Contains(cutlets, "swap:tofu") {
		t.Errorf("tofu survived a soy allergy: %v", cutlets)
	}

	// A vegetarian household keeps the original and tofu only.
	f.profiles.restrictions = recommendations.Restrictions{Diets: []string{"vegetarian"}}
	o, err = f.svc.Options(f.ctx, hhAda, householdTacos().ID, Query{})
	if err != nil {
		t.Fatal(err)
	}
	// Every swap for ground pork is meat, so only the original remains.
	if got := groupIDs(o.Groups[0]); !slices.Equal(got, []string{"original"}) {
		t.Errorf("vegetarian pork choices = %v", got)
	}
	// The chicken line keeps tofu.
	if got := groupIDs(o.Groups[1]); !slices.Equal(got, []string{"original", "swap:tofu", "swap:tofu:double"}) {
		t.Errorf("vegetarian cutlet choices = %v", got)
	}

	// A profile failure fails the request: restrictions are hard constraints.
	f.profiles.err = errors.New("profile store down")
	if _, err := f.svc.Options(f.ctx, hhAda, householdTacos().ID, Query{}); err == nil {
		t.Error("Options() ignored a profile failure")
	}
}

func TestSetCustomizations(t *testing.T) {
	f := newFixture(t)

	p, err := f.svc.SetCustomizations(f.ctx, hhAda, userAda, testWeek, entryID, []Selection{{IngredientKey: ingPork, ChoiceID: "double"}})
	if err != nil {
		t.Fatalf("SetCustomizations() error = %v", err)
	}
	stored := p.Entries[0].Customizations
	if len(stored) != 1 || stored[0].IngredientKey != ingPork || stored[0].ChoiceID != "double" || stored[0].Label != "2x Ground Pork" {
		t.Fatalf("stored = %+v", stored)
	}
	ev := f.events.all()
	if len(ev) != 1 || ev[0].Type != events.TypeMealCustomized || ev[0].RecipeID != householdTacos().ID || ev[0].UserID != userAda || ev[0].Week != testWeek {
		t.Fatalf("event = %+v", ev)
	}
	payload, _ := ev[0].Payload.(events.MealCustomized)
	if payload.EntryID != entryID || !slices.Equal(payload.Changes, []events.CustomizationChange{{IngredientKey: ingPork, From: "original", To: "double"}}) {
		t.Errorf("payload = %+v", payload)
	}

	// Changing the choice records the previous one.
	if _, err := f.svc.SetCustomizations(f.ctx, hhAda, userAda, testWeek, entryID, []Selection{{IngredientKey: ingPork, ChoiceID: "swap:ground-beef:double"}}); err != nil {
		t.Fatal(err)
	}
	payload, _ = f.events.all()[1].Payload.(events.MealCustomized)
	if !slices.Equal(payload.Changes, []events.CustomizationChange{{IngredientKey: ingPork, From: "double", To: "swap:ground-beef:double"}}) {
		t.Errorf("second payload = %+v", payload)
	}

	// Re-sending the same choice changes nothing and records nothing.
	if _, err := f.svc.SetCustomizations(f.ctx, hhAda, userAda, testWeek, entryID, []Selection{{IngredientKey: ingPork, ChoiceID: "swap:ground-beef:double"}}); err != nil {
		t.Fatal(err)
	}
	if n := len(f.events.all()); n != 2 {
		t.Errorf("events after a no-op = %d", n)
	}

	// An empty list resets the meal; "original" stores nothing either.
	p, err = f.svc.SetCustomizations(f.ctx, hhAda, userAda, testWeek, entryID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Entries[0].Customizations) != 0 {
		t.Errorf("after the reset = %+v", p.Entries[0].Customizations)
	}
	payload, _ = f.events.all()[2].Payload.(events.MealCustomized)
	if !slices.Equal(payload.Changes, []events.CustomizationChange{{IngredientKey: ingPork, From: "swap:ground-beef:double", To: "original"}}) {
		t.Errorf("reset payload = %+v", payload)
	}
	if _, err := f.svc.SetCustomizations(f.ctx, hhAda, userAda, testWeek, entryID, []Selection{{IngredientKey: ingPork, ChoiceID: ChoiceOriginal}}); err != nil {
		t.Fatal(err)
	}
	if got := f.plans.entry(t, entryID).Customizations; len(got) != 0 {
		t.Errorf("original stored = %+v", got)
	}
}

func TestSetCustomizationsValidation(t *testing.T) {
	f := newFixture(t)
	tooMany := make([]Selection, MaxSelections+1)
	for i := range tooMany {
		tooMany[i] = Selection{IngredientKey: ingPork, ChoiceID: "double"}
	}
	for _, tc := range []struct {
		name       string
		selections []Selection
		want       string
	}{
		{"unknown key", []Selection{{IngredientKey: ingOnion, ChoiceID: "double"}}, "not a customizable line"},
		{"unknown choice", []Selection{{IngredientKey: ingPork, ChoiceID: "swap:lobster"}}, "is not a choice"},
		{"choice of another family", []Selection{{IngredientKey: ingPork, ChoiceID: "swap:shrimp"}}, "is not a choice"},
		{"duplicate key", []Selection{{IngredientKey: ingPork, ChoiceID: "double"}, {IngredientKey: ingPork, ChoiceID: "original"}}, "more than once"},
		{"too many", tooMany, "at most"},
	} {
		_, err := f.svc.SetCustomizations(f.ctx, hhAda, userAda, testWeek, entryID, tc.selections)
		if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error = %v, want %q", tc.name, err, tc.want)
		}
	}
	// A choice the restrictions rule out can't be chosen either.
	f.profiles.restrictions = recommendations.Restrictions{ExcludedProteins: []string{"beef"}}
	if _, err := f.svc.SetCustomizations(f.ctx, hhAda, userAda, testWeek, entryID, []Selection{{IngredientKey: ingPork, ChoiceID: "swap:ground-beef"}}); !errors.Is(err, ErrInvalid) {
		t.Errorf("restricted choice error = %v", err)
	}
	if n := len(f.events.all()); n != 0 {
		t.Errorf("invalid requests recorded %d events", n)
	}

	if _, err := f.svc.SetCustomizations(f.ctx, hhAda, userAda, testWeek, "66e5a1f2c3b4a5d6e7f89999", nil); !errors.Is(err, planning.ErrNotFound) {
		t.Errorf("unknown entry error = %v", err)
	}
	if _, err := f.svc.SetCustomizations(f.ctx, hhAda, userAda, "nonsense", entryID, nil); !errors.Is(err, planning.ErrInvalidWeek) {
		t.Errorf("bad week error = %v", err)
	}
	// A finalized plan follows the planner's conflict behavior.
	f.plans.mu.Lock()
	p := f.plans.plans[testWeek]
	p.Status = planning.StatusFinalized
	f.plans.plans[testWeek] = p
	f.plans.mu.Unlock()
	if _, err := f.svc.SetCustomizations(f.ctx, hhAda, userAda, testWeek, entryID, []Selection{{IngredientKey: ingPork, ChoiceID: "double"}}); !errors.Is(err, planning.ErrFinalized) {
		t.Errorf("finalized error = %v", err)
	}
}

func TestCustomizeGrocery(t *testing.T) {
	f := newFixture(t)
	entry := planning.Entry{ID: entryID, RecipeID: householdTacos().ID, Servings: 2, Customizations: []planning.Customization{
		{IngredientKey: ingPork, ChoiceID: "swap:ground-beef", Label: "Ground Beef"},
	}}
	plain := planning.Entry{ID: "e2", RecipeID: householdTacos().ID, Servings: 2}
	sels := []grocery.RecipeSelection{tacosSelection(), tacosSelection()}
	sels[1].RecipeID, sels[1].RecipeName = "r-other", "Other"

	out, err := f.svc.CustomizeGrocery(f.ctx, hhAda, []planning.Entry{entry, plain}, sels)
	if err != nil {
		t.Fatal(err)
	}
	if l := out[0].Lines[0]; l.IngredientKey != ingBeef || l.Name != "Ground Beef" || l.Quantity.String() != "10" || l.Via == nil || l.Via.Kind != grocery.ViaCustomized {
		t.Errorf("customized line = %+v", l)
	}
	if l := out[1].Lines[0]; l.IngredientKey != ingPork || l.Via != nil {
		t.Errorf("uncustomized line = %+v", l)
	}

	// A choice that no longer applies leaves the line alone.
	entry.Customizations = []planning.Customization{{IngredientKey: ingPork, ChoiceID: "swap:lobster"}}
	out, err = f.svc.CustomizeGrocery(f.ctx, hhAda, []planning.Entry{entry}, sels[:1])
	if err != nil || out[0].Lines[0].IngredientKey != ingPork || out[0].Lines[0].Via != nil {
		t.Errorf("stale choice = %+v, %v", out[0].Lines[0], err)
	}

	// A catalog failure fails the list.
	entry.Customizations = []planning.Customization{{IngredientKey: ingPork, ChoiceID: "swap:ground-beef"}}
	f.catalog.err = errors.New("catalog down")
	if _, err := f.svc.CustomizeGrocery(f.ctx, hhAda, []planning.Entry{entry}, sels[:1]); err == nil {
		t.Error("CustomizeGrocery() ignored a catalog failure")
	}
}

func TestAdjustCookedRecipe(t *testing.T) {
	f := newFixture(t, planning.Entry{
		ID: entryID, RecipeID: householdTacos().ID, Servings: 4,
		Customizations: []planning.Customization{{IngredientKey: ingPork, ChoiceID: "swap:ground-beef:double", Label: "2x Ground Beef"}},
	})
	r := householdTacos()

	got, err := f.svc.AdjustCookedRecipe(f.ctx, hhAda, entryID, testNow, r)
	if err != nil {
		t.Fatal(err)
	}
	line := got.Ingredients[0]
	if line.IngredientID != ingBeef || line.Name != "Ground Beef" || line.Amounts[0].Quantity != "20" || line.Amounts[1].Quantity != "40" {
		t.Errorf("cooked line = %+v", line)
	}
	if r.Ingredients[0].Name != "Ground Pork" {
		t.Error("AdjustCookedRecipe changed the recipe it was given")
	}

	// Another recipe, an unknown entry, or no entry: unchanged.
	other := householdTacos()
	other.ID = "66e5a1f2c3b4a5d6e7f8b099"
	if got, err := f.svc.AdjustCookedRecipe(f.ctx, hhAda, entryID, testNow, other); err != nil || got.Ingredients[0].Name != "Ground Pork" {
		t.Errorf("other recipe = %+v, %v", got.Ingredients[0], err)
	}
	if got, err := f.svc.AdjustCookedRecipe(f.ctx, hhAda, "66e5a1f2c3b4a5d6e7f89999", testNow, r); err != nil || got.Ingredients[0].Name != "Ground Pork" {
		t.Errorf("unknown entry = %+v, %v", got.Ingredients[0], err)
	}

	// A lookup failure fails the deduction.
	f.plans.err = errors.New("plans down")
	if _, err := f.svc.AdjustCookedRecipe(f.ctx, hhAda, entryID, testNow, r); err == nil {
		t.Error("AdjustCookedRecipe() ignored a plan failure")
	}
}

// TestAdjustCookedRecipeTargets covers the identity a cooked line carries to
// the pantry, which is what the deduction matches on: a swap the catalog knows
// keeps the catalog ingredient's ID, a swap it doesn't know travels by name
// alone, and a double portion keeps the line's own ingredient.
func TestAdjustCookedRecipeTargets(t *testing.T) {
	for _, tc := range []struct {
		name, choice              string
		wantID, wantName, wantKey string
		wantSmall, wantLarge      string
	}{
		{"swap the catalog has", "swap:ground-beef", ingBeef, "Ground Beef", ingBeef, "10", "20"},
		{"swap the catalog lacks", "swap:ground-turkey", "", "Ground Turkey", "name:ground turkey", "10", "20"},
		{"double portion", "double", ingPork, "Ground Pork", ingPork, "20", "40"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, planning.Entry{
				ID: entryID, RecipeID: householdTacos().ID, Servings: 2,
				Customizations: []planning.Customization{{IngredientKey: ingPork, ChoiceID: tc.choice}},
			})
			got, err := f.svc.AdjustCookedRecipe(f.ctx, hhAda, entryID, testNow, householdTacos())
			if err != nil {
				t.Fatal(err)
			}
			line := got.Ingredients[0]
			if line.IngredientID != tc.wantID || line.Name != tc.wantName {
				t.Errorf("cooked line = %+v, want id %q named %q", line, tc.wantID, tc.wantName)
			}
			// The pantry matches this line by catalog ID, else by this key.
			if got := LineKey(line); got != tc.wantKey {
				t.Errorf("line key = %q, want %q", got, tc.wantKey)
			}
			if line.Amounts[0].Quantity != tc.wantSmall || line.Amounts[1].Quantity != tc.wantLarge {
				t.Errorf("amounts = %+v, want %s and %s", line.Amounts, tc.wantSmall, tc.wantLarge)
			}
		})
	}
}

// TestAdjustCookedRecipeLaterWeek: a meal cooked in a later week still cooks
// what was customized while its plan is within the entry lookup window.
func TestAdjustCookedRecipeLaterWeek(t *testing.T) {
	f := newFixture(t, planning.Entry{
		ID: entryID, RecipeID: householdTacos().ID, Servings: 2,
		Customizations: []planning.Customization{{IngredientKey: ingPork, ChoiceID: "swap:ground-beef"}},
	})
	got, err := f.svc.AdjustCookedRecipe(f.ctx, hhAda, entryID, testNow.AddDate(0, 0, 21), householdTacos())
	if err != nil {
		t.Fatal(err)
	}
	if line := got.Ingredients[0]; line.IngredientID != ingBeef || line.Name != "Ground Beef" {
		t.Errorf("cooked three weeks later = %+v", line)
	}

	// Past the window the plan is out of reach, and the meal cooks as written.
	got, err = f.svc.AdjustCookedRecipe(f.ctx, hhAda, entryID, testNow.AddDate(0, 0, 7*(entrySearchWeeks+2)), householdTacos())
	if err != nil {
		t.Fatal(err)
	}
	if line := got.Ingredients[0]; line.Name != "Ground Pork" {
		t.Errorf("cooked past the window = %+v", line)
	}
}

// TestServiceWithoutOptionalDependencies: no catalog, profiles, or recorder.
func TestServiceWithoutOptionalDependencies(t *testing.T) {
	plans := newFakePlans(planning.Entry{ID: entryID, RecipeID: householdTacos().ID, Servings: 2})
	svc := NewService(ServiceOptions{Plans: plans, Recipes: fakeRecipes{householdTacos().ID: householdTacos()}})
	ctx := context.Background()
	o, err := svc.Options(ctx, hhAda, householdTacos().ID, Query{})
	if err != nil || len(o.Groups) != 2 || o.Groups[0].ImageURL != "" {
		t.Fatalf("Options() = %+v, %v", o, err)
	}
	beef := o.Groups[0].Choices[slices.IndexFunc(o.Groups[0].Choices, func(c ChoiceOption) bool { return c.ID == "swap:ground-beef" })]
	if beef.IngredientName != "Ground Beef" {
		t.Errorf("beef without a catalog = %+v", beef)
	}
	if _, err := svc.SetCustomizations(ctx, hhAda, userAda, testWeek, entryID, []Selection{{IngredientKey: ingPork, ChoiceID: "double"}}); err != nil {
		t.Errorf("SetCustomizations() without a recorder = %v", err)
	}
	// Without a catalog a swap keys by name.
	entry := plans.entry(t, entryID)
	entry.Customizations = []planning.Customization{{IngredientKey: ingPork, ChoiceID: "swap:ground-beef"}}
	out, err := svc.CustomizeGrocery(ctx, hhAda, []planning.Entry{entry}, []grocery.RecipeSelection{tacosSelection()})
	if err != nil || out[0].Lines[0].IngredientKey != "name:ground beef" {
		t.Errorf("swap without a catalog = %+v, %v", out[0].Lines[0], err)
	}
}

func TestAmountTextRendering(t *testing.T) {
	for _, tc := range []struct {
		quantity, unit, want string
	}{
		{"10", "oz", "10 ounce"},
		{"20", "oz", "20 ounce"},
		{"1/2", "lb", "½ pound"},
		{"3/2", "lb", "1 ½ pound"},
		{"500", "g", "500 gram"},
		{"2", "count", "2"},
	} {
		q, err := ingredients.ParseQuantity(tc.quantity)
		if err != nil {
			t.Fatal(err)
		}
		if got := AmountText(q, tc.unit); got != tc.want {
			t.Errorf("AmountText(%s %s) = %q, want %q", tc.quantity, tc.unit, got, tc.want)
		}
	}
}
