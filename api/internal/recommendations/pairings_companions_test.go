package recommendations

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/companions"
	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

const (
	cChips  = "66e5a1f2c3b4a5d6e7f8c001"
	cTowels = "66e5a1f2c3b4a5d6e7f8c002"
	cBread  = "66e5a1f2c3b4a5d6e7f8c003"
)

// fakeCompanions is a CompanionSource over a fixed list.
type fakeCompanions struct {
	mu     sync.Mutex
	list   []companions.Companion
	listed map[string]string
}

func (f *fakeCompanions) ForEvaluation(_ context.Context, _ string, _ []recipes.Recipe) ([]companions.Companion, error) {
	return f.Active(context.Background(), "")
}

func (f *fakeCompanions) Active(context.Context, string) ([]companions.Companion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := slices.Clone(f.list)
	for i := range out {
		if w, ok := f.listed[out[i].ID]; ok {
			out[i].LastListedWeek = w
		}
	}
	return out, nil
}

func (f *fakeCompanions) MarkListed(_ context.Context, _, id, week string, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listed == nil {
		f.listed = map[string]string{}
	}
	f.listed[id] = week
	return nil
}

func (f *fakeCompanions) week(id string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.listed[id]
}

func newCompanionEnv(t *testing.T, list ...companions.Companion) (*pairingEnv, *fakeCompanions) {
	t.Helper()
	env := newPairingEnv(t)
	fc := &fakeCompanions{list: list}
	env.svc.WithCompanions(fc)
	return env, fc
}

func chipsCompanion(mode string) companions.Companion {
	return companions.Companion{
		ID: cChips, HouseholdID: hhA, Name: "Chips and salsa", StoreItem: "Tortilla chips", Quantity: "1", Unit: "package",
		Cuisines: []string{"mexican"}, Frequency: companions.FrequencyNone, Mode: mode,
	}
}

func companionPairing(t *testing.T, ps []Pairing, key string) Pairing {
	t.Helper()
	i := indexByKey(ps, key)
	if i < 0 {
		t.Fatalf("no %s in %v", key, pairingKeys(ps))
	}
	return ps[i]
}

func TestCompanionSuggestedWithMealsOfItsCuisine(t *testing.T) {
	ctx := context.Background()
	env, fc := newCompanionEnv(t, chipsCompanion(companions.ModeSuggest))
	tacos := env.planMeal(t, rTacos, "tue", 2)
	pasta := env.planMeal(t, rRigatoni, "wed", 2)

	v, err := env.svc.WeekPairings(ctx, hhA, userAda, testWeek, "")
	if err != nil {
		t.Fatal(err)
	}
	p := companionPairing(t, mealFor(t, v, tacos.ID).Pairings, "companion:"+cChips)
	if p.Source != PairingSourceCompanion || p.Kind != PairingKindGroceryItem || p.GroceryItem.Name != "Tortilla chips" ||
		p.Frequency != PairingSuggest || p.CompanionID != cChips || p.Standalone {
		t.Errorf("chips pairing = %+v", p)
	}
	if want := "Goes with Mexican meals"; p.Reason != want {
		t.Errorf("reason = %q, want %q", p.Reason, want)
	}
	if got := pairingKeys(mealFor(t, v, pasta.ID).Pairings); slices.Contains(got, "companion:"+cChips) {
		t.Errorf("pasta got the chips: %v", got)
	}
	if len(v.GroceryItems) != 0 {
		t.Errorf("Just Suggest It added %+v on its own", v.GroceryItems)
	}

	res, err := env.svc.AcceptPairing(ctx, hhA, userAda, testWeek, tacos.ID, "companion:"+cChips)
	if err != nil || res.Status != PairingAdded || res.GroceryItem == nil || res.GroceryItem.CompanionID != cChips {
		t.Fatalf("accept = %+v, %v", res, err)
	}
	if got := fc.week(cChips); got != testWeek {
		t.Errorf("last listed week = %q, want %q", got, testWeek)
	}
	plan, _ := env.planner.Get(ctx, hhA, testWeek)
	lines, err := env.svc.GroceryExtras(ctx, plan)
	if err != nil || len(lines) != 1 || lines[0].Name != "Tortilla chips" || lines[0].Extra.Origin != grocery.OriginPairing {
		t.Fatalf("grocery extras = %+v, %v", lines, err)
	}
	if want := "Tortilla chips for Beef Tacos"; lines[0].Extra.Text != want {
		t.Errorf("text = %q, want %q", lines[0].Extra.Text, want)
	}
	// Accepted, it's on the list, so it's no longer offered.
	v, _ = env.svc.WeekPairings(ctx, hhA, userAda, testWeek, "")
	if slices.Contains(pairingKeys(mealFor(t, v, tacos.ID).Pairings), "companion:"+cChips) {
		t.Error("the chips are still offered after Add")
	}
}

func TestCompanionNotThisWeek(t *testing.T) {
	ctx := context.Background()
	env, _ := newCompanionEnv(t, chipsCompanion(companions.ModeSuggest))
	tacos := env.planMeal(t, rTacos, "tue", 2)
	v, err := env.svc.DismissPairing(ctx, hhA, userAda, testWeek, tacos.ID, "companion:"+cChips)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(pairingKeys(mealFor(t, v, tacos.ID).Pairings), "companion:"+cChips) {
		t.Error("dismissed companion still offered")
	}
}

func TestCompanionForSpecificRecipes(t *testing.T) {
	c := companions.Companion{
		ID: cChips, HouseholdID: hhA, Name: "Sour cream", Recipes: []companions.RecipeRef{{ID: rSoup, Name: "Onion Soup"}},
		Frequency: companions.FrequencyNone, Mode: companions.ModeSuggest,
	}
	env, _ := newCompanionEnv(t, c)
	soup := env.planMeal(t, rSoup, "mon", 2)
	tacos := env.planMeal(t, rTacos, "tue", 2)
	v, err := env.svc.WeekPairings(context.Background(), hhA, userAda, testWeek, "")
	if err != nil {
		t.Fatal(err)
	}
	p := companionPairing(t, mealFor(t, v, soup.ID).Pairings, "companion:"+cChips)
	if p.Reason != "Goes with Onion Soup" || p.GroceryItem.Name != "Sour cream" {
		t.Errorf("pairing = %+v", p)
	}
	if slices.Contains(pairingKeys(mealFor(t, v, tacos.ID).Pairings), "companion:"+cChips) {
		t.Error("tacos got the soup's companion")
	}
}

func TestCompanionDueByFrequencyShowsOnceWithTheFirstMeal(t *testing.T) {
	ctx := context.Background()
	towels := companions.Companion{
		ID: cTowels, HouseholdID: hhA, Name: "Paper towels", Frequency: companions.Every2Weeks, Mode: companions.ModeSuggest,
		LastListedWeek: "2026-W36",
	}
	env, fc := newCompanionEnv(t, towels)
	wed := env.planMeal(t, rTacos, "wed", 2)
	mon := env.planMeal(t, rRigatoni, "mon", 2)

	v, err := env.svc.WeekPairings(ctx, hhA, userAda, testWeek, "")
	if err != nil {
		t.Fatal(err)
	}
	p := companionPairing(t, mealFor(t, v, mon.ID).Pairings, "companion:"+cTowels)
	if !p.Standalone || p.Reason != "You get it every 2 weeks" {
		t.Errorf("towels = %+v", p)
	}
	if slices.Contains(pairingKeys(mealFor(t, v, wed.ID).Pairings), "companion:"+cTowels) {
		t.Error("towels offered twice")
	}
	res, err := env.svc.AcceptPairing(ctx, hhA, userAda, testWeek, mon.ID, "companion:"+cTowels)
	if err != nil || res.GroceryItem == nil || res.GroceryItem.Text() != "Paper towels" {
		t.Fatalf("accept = %+v, %v", res, err)
	}

	// Listed in W38, it isn't due again in W39, and is in W40.
	if fc.week(cTowels) != testWeek {
		t.Fatalf("listed = %q", fc.week(cTowels))
	}
	towels.LastListedWeek = testWeek
	for week, want := range map[string]bool{"2026-W38": true, "2026-W39": false, "2026-W40": true, "2026-W37": false} {
		w, _ := planning.ParseWeek(week)
		if got := towels.DueByFrequency(w); got != want {
			t.Errorf("due in %s = %v, want %v", week, got, want)
		}
	}
}

func TestAutoCompanionGoesOnTheList(t *testing.T) {
	ctx := context.Background()
	env, fc := newCompanionEnv(t, chipsCompanion(companions.ModeAuto))
	tacos := env.planMeal(t, rTacos, "tue", 2)
	plan, _ := env.planner.Get(ctx, hhA, testWeek)

	// Reading the list is enough: nobody opened the suggestions.
	lines, err := env.svc.GroceryExtras(ctx, plan)
	if err != nil || len(lines) != 1 || lines[0].Name != "Tortilla chips" {
		t.Fatalf("grocery extras = %+v, %v", lines, err)
	}
	if fc.week(cChips) != testWeek {
		t.Errorf("listed = %q", fc.week(cChips))
	}
	state, _ := env.pairings.GetWeekPairings(ctx, hhA, testWeek)
	if len(state.GroceryItems) != 1 || state.GroceryItems[0].AddedBy != "" || state.GroceryItems[0].ForEntryID != tacos.ID {
		t.Fatalf("state = %+v", state.GroceryItems)
	}
	// Reading again adds nothing more.
	if again, _ := env.svc.GroceryExtras(ctx, plan); len(again) != 1 {
		t.Errorf("second read = %+v", again)
	}
	// Taken off the list on purpose, it stays off this week.
	if err := env.svc.RemovePairingGroceryItem(ctx, hhA, userAda, testWeek, state.GroceryItems[0].ID); err != nil {
		t.Fatal(err)
	}
	if after, _ := env.svc.GroceryExtras(ctx, plan); len(after) != 0 {
		t.Errorf("removed item came back: %+v", after)
	}
	v, _ := env.svc.WeekPairings(ctx, hhA, userAda, testWeek, "")
	if slices.Contains(pairingKeys(mealFor(t, v, tacos.ID).Pairings), "companion:"+cChips) {
		t.Error("removed item offered again")
	}
}

func TestAutoCompanionLeavesFinalizedAndPastWeeksAlone(t *testing.T) {
	ctx := context.Background()
	env, _ := newCompanionEnv(t, chipsCompanion(companions.ModeAuto))
	env.planMeal(t, rTacos, "tue", 2)
	plan, _ := env.planner.Get(ctx, hhA, testWeek)
	plan.Status = planning.StatusFinalized
	if lines, _ := env.svc.GroceryExtras(ctx, plan); len(lines) != 0 {
		t.Errorf("finalized week got %+v", lines)
	}
	past := plan
	past.Status = planning.StatusDraft
	past.Week = plan.Week.AddWeeks(-1)
	if lines, _ := env.svc.GroceryExtras(ctx, past); len(lines) != 0 {
		t.Errorf("past week got %+v", lines)
	}
}

func TestAutoCompanionRecipeIsPlannedFromTheWeek(t *testing.T) {
	ctx := context.Background()
	bread := companions.Companion{
		ID: cBread, HouseholdID: hhA, Name: "Garlic Bread", LinkedRecipe: &companions.RecipeRef{ID: rBread, Name: "Garlic Bread"},
		Cuisines: []string{"italian"}, Frequency: companions.FrequencyNone, Mode: companions.ModeAuto,
	}
	env, fc := newCompanionEnv(t, bread)
	env.planMeal(t, rRigatoni, "mon", 2)
	plan, _ := env.planner.Get(ctx, hhA, testWeek)
	// The list never plans a recipe.
	if _, err := env.svc.GroceryExtras(ctx, plan); err != nil {
		t.Fatal(err)
	}
	if p, _ := env.planner.Get(ctx, hhA, testWeek); len(p.Entries) != 1 {
		t.Fatalf("reading the list planned %d entries", len(p.Entries))
	}
	if _, err := env.svc.WeekPairings(ctx, hhA, userAda, testWeek, ""); err != nil {
		t.Fatal(err)
	}
	p, _ := env.planner.Get(ctx, hhA, testWeek)
	if len(p.Entries) != 2 || p.Entries[1].RecipeID != rBread || p.Entries[1].Day != "mon" {
		t.Fatalf("entries = %+v", p.Entries)
	}
	if fc.week(cBread) != testWeek {
		t.Errorf("listed = %q", fc.week(cBread))
	}
	if _, err := env.svc.WeekPairings(ctx, hhA, userAda, testWeek, ""); err != nil {
		t.Fatal(err)
	}
	if p, _ := env.planner.Get(ctx, hhA, testWeek); len(p.Entries) != 2 {
		t.Errorf("planned again: %d entries", len(p.Entries))
	}
}

// A migrated add-on and the rule that already pairs it show as one
// suggestion, the rule's, exactly as before companions.
func TestMigratedAddonKeepsItsRule(t *testing.T) {
	ctx := context.Background()
	bread := companions.Companion{
		ID: cBread, HouseholdID: hhA, Name: "Garlic Bread", LinkedRecipe: &companions.RecipeRef{ID: rBread, Name: "Garlic Bread"},
		MealCategories: []string{"pasta"}, Frequency: companions.FrequencyNone, Mode: companions.ModeSuggest,
	}
	env, fc := newCompanionEnv(t, bread)
	env.saveRules(t, pastaBreadRule())
	pasta := env.planMeal(t, rRigatoni, "mon", 2)

	v, err := env.svc.WeekPairings(ctx, hhA, userAda, testWeek, "")
	if err != nil {
		t.Fatal(err)
	}
	ps := mealFor(t, v, pasta.ID).Pairings
	if len(ps) != 1 {
		t.Fatalf("pairings = %+v", ps)
	}
	p := ps[0]
	if p.Key != "recipe:"+rBread || p.Source != PairingSourceRule || p.Reason != "Your rule: pasta → Garlic Bread" || p.CompanionID != cBread {
		t.Errorf("pairing = %+v", p)
	}
	res, err := env.svc.AcceptPairing(ctx, hhA, userAda, testWeek, pasta.ID, p.Key)
	if err != nil || res.Entry == nil || res.Entry.RecipeID != rBread {
		t.Fatalf("accept = %+v, %v", res, err)
	}
	if fc.week(cBread) != testWeek {
		t.Errorf("listed = %q", fc.week(cBread))
	}
}

// Never on a migrated add-on turns off its rule and learned pairings too.
func TestNeverHidesTheAddonEverywhere(t *testing.T) {
	bread := companions.Companion{
		ID: cBread, HouseholdID: hhA, Name: "Garlic Bread", LinkedRecipe: &companions.RecipeRef{ID: rBread, Name: "Garlic Bread"},
		Frequency: companions.FrequencyNone, Mode: companions.ModeNever,
	}
	env, _ := newCompanionEnv(t, bread)
	env.saveRules(t, pastaBreadRule())
	pasta := env.planMeal(t, rRigatoni, "mon", 2)
	v, err := env.svc.WeekPairings(context.Background(), hhA, userAda, testWeek, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := pairingKeys(mealFor(t, v, pasta.ID).Pairings); slices.Contains(got, "recipe:"+rBread) {
		t.Errorf("pairings = %v; the add-on is set to Never", got)
	}
}

func TestCompanionsOff(t *testing.T) {
	// Without a companion source, pairings are as they were.
	env := newPairingEnv(t)
	env.saveRules(t, pastaBreadRule())
	pasta := env.planMeal(t, rRigatoni, "mon", 2)
	v, err := env.svc.WeekPairings(context.Background(), hhA, userAda, testWeek, "")
	if err != nil || len(mealFor(t, v, pasta.ID).Pairings) != 1 {
		t.Fatalf("pairings = %+v, %v", v.Meals, err)
	}
}

func TestCompanionHints(t *testing.T) {
	rules := []PairingRule{
		{When: PairingWhen{MealCategories: []string{"pasta"}}, Add: PairingTarget{RecipeID: rBread}},
		{When: PairingWhen{MealCategories: []string{"soup"}}, Add: PairingTarget{RecipeID: rBread}},
		{When: PairingWhen{Cuisines: []string{"italian"}}, Add: PairingTarget{RecipeID: rBread}},
		// Mixed conditions match fewer meals than either alone: nothing carries over.
		{When: PairingWhen{Cuisines: []string{"mexican"}, Proteins: []string{"beef"}}, Add: PairingTarget{RecipeID: rRolls}},
		{When: PairingWhen{MealCategories: []string{"tacos"}}, Add: PairingTarget{GroceryItem: &GroceryItem{Name: "Chips"}}},
	}
	got := companionHints(rules)
	if len(got) != 2 {
		t.Fatalf("hints = %+v", got)
	}
	if h := got[0]; h.RecipeID != rBread || !slices.Equal(h.MealCategories, []string{"pasta", "soup"}) || !slices.Equal(h.Cuisines, []string{"italian"}) {
		t.Errorf("bread hint = %+v", h)
	}
	if h := got[1]; h.RecipeID != rRolls || len(h.Cuisines)+len(h.MealCategories) != 0 {
		t.Errorf("rolls hint = %+v", h)
	}
}
