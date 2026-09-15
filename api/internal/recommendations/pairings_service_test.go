package recommendations

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

func pairingKeys(pairings []Pairing) []string {
	out := make([]string, 0, len(pairings))
	for _, p := range pairings {
		out = append(out, p.Key)
	}
	return out
}

func mealFor(t *testing.T, v WeekPairingsView, entryID string) MealPairings {
	t.Helper()
	for _, m := range v.Meals {
		if m.Entry.ID == entryID {
			return m
		}
	}
	t.Fatalf("no meal for entry %s in %+v", entryID, v.Meals)
	return MealPairings{}
}

func TestWeekPairingsFromRules(t *testing.T) {
	env := newPairingEnv(t)
	ctx := context.Background()
	env.saveRules(t, pastaBreadRule(), crackersRule())
	pasta := env.planMeal(t, rRigatoni, "mon", 2)
	soup := env.planMeal(t, rSoup, "tue", 2)

	v, err := env.svc.WeekPairings(ctx, hhA, userAda, testWeek, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Meals) != 2 {
		t.Fatalf("meals = %+v", v.Meals)
	}
	pastaMeal := mealFor(t, v, pasta.ID)
	if !slices.Equal(pastaMeal.MealCategories, []string{"pasta"}) || len(pastaMeal.Pairings) != 1 {
		t.Fatalf("pasta meal = %+v", pastaMeal)
	}
	bread := pastaMeal.Pairings[0]
	if bread.Key != "recipe:"+rBread || bread.Source != PairingSourceRule || bread.Frequency != PairingAlways ||
		bread.Recipe.Name != "Garlic Bread" || bread.Servings != 2 || bread.MealCategory != "pasta" {
		t.Errorf("garlic bread pairing = %+v", bread)
	}
	if want := "Your rule: pasta → Garlic Bread"; bread.Reason != want {
		t.Errorf("reason = %q, want %q", bread.Reason, want)
	}
	crackers := mealFor(t, v, soup.ID).Pairings[0]
	if crackers.Kind != PairingKindGroceryItem || crackers.GroceryItem.Name != "Club crackers" || crackers.Key != "grocery:club crackers" {
		t.Errorf("crackers pairing = %+v", crackers)
	}

	// Every suggestion is recorded once, however often the week is read.
	if _, err := env.svc.WeekPairings(ctx, hhA, userAda, testWeek, ""); err != nil {
		t.Fatal(err)
	}
	suggested := env.events.ofType(events.TypePairingSuggested)
	if len(suggested) != 2 {
		t.Fatalf("pairing.suggested events = %d, want 2", len(suggested))
	}
	payload := suggested[0].Payload.(events.PairingSuggested)
	if payload.EntryID == "" || payload.Source != PairingSourceRule || payload.RuleID == "" || payload.Day == "" {
		t.Errorf("payload = %+v", payload)
	}

	// One meal at a time, for the app's "added a pasta dish" prompt.
	one, err := env.svc.WeekPairings(ctx, hhA, userAda, testWeek, soup.ID)
	if err != nil || len(one.Meals) != 1 || one.Meals[0].Entry.ID != soup.ID {
		t.Errorf("one meal = %+v, %v", one.Meals, err)
	}
	if _, err := env.svc.WeekPairings(ctx, hhA, userAda, testWeek, "ffffffffffffffffffffffff"); !isNotFound(err) {
		t.Errorf("unknown entry = %v, want not found", err)
	}
}

func TestWeekPairingsLearned(t *testing.T) {
	env := newPairingEnv(t)
	env.learnRolls(t)
	pasta := env.planMeal(t, rRigatoni, "mon", 2)

	v, err := env.svc.WeekPairings(context.Background(), hhA, userAda, testWeek, "")
	if err != nil {
		t.Fatal(err)
	}
	pairings := mealFor(t, v, pasta.ID).Pairings
	if len(pairings) != 1 {
		t.Fatalf("pairings = %+v", pairings)
	}
	p := pairings[0]
	if p.Source != PairingSourceLearned || p.Frequency != PairingSuggest || p.MealCategory != "pasta" || p.Confidence != 0.8 {
		t.Fatalf("learned pairing = %+v", p)
	}
	if want := "You usually have Buttery Dinner Rolls with pasta (80% of pasta weeks)"; p.Reason != want {
		t.Errorf("reason = %q, want %q", p.Reason, want)
	}
	if l := p.Learned; l == nil || l.WeeksTogether != 4 || l.CategoryWeeks != 5 || l.OtherWeeks != 5 || l.OtherRate != 0 {
		t.Errorf("learned = %+v", p.Learned)
	}
}

func TestWeekPairingsExclusions(t *testing.T) {
	ctx := context.Background()
	t.Run("an add-on planned that day", func(t *testing.T) {
		env := newPairingEnv(t)
		env.saveRules(t, pastaBreadRule())
		pasta := env.planMeal(t, rRigatoni, "mon", 2)
		env.planMeal(t, rBread, "mon", 2)
		v, err := env.svc.WeekPairings(ctx, hhA, userAda, testWeek, "")
		if err != nil {
			t.Fatal(err)
		}
		if got := mealFor(t, v, pasta.ID).Pairings; len(got) != 0 {
			t.Errorf("pairings = %v, want none", pairingKeys(got))
		}
	})
	t.Run("the same add-on on another day is still offered", func(t *testing.T) {
		env := newPairingEnv(t)
		env.saveRules(t, pastaBreadRule())
		pasta := env.planMeal(t, rRigatoni, "mon", 2)
		env.planMeal(t, rBread, "tue", 2)
		v, err := env.svc.WeekPairings(ctx, hhA, userAda, testWeek, "")
		if err != nil {
			t.Fatal(err)
		}
		if got := mealFor(t, v, pasta.ID).Pairings; len(got) != 1 {
			t.Errorf("pairings = %v, want garlic bread", pairingKeys(got))
		}
	})
	t.Run("a grocery item a planned recipe already uses", func(t *testing.T) {
		env := newPairingEnv(t)
		env.saveRules(t, crackersRule())
		env.recipe(t, rSoup).Ingredients = append(env.recipe(t, rSoup).Ingredients, recipes.RecipeIngredient{Name: "Club Crackers"})
		soup := env.planMeal(t, rSoup, "tue", 2)
		v, err := env.svc.WeekPairings(ctx, hhA, userAda, testWeek, "")
		if err != nil {
			t.Fatal(err)
		}
		if got := mealFor(t, v, soup.ID).Pairings; len(got) != 0 {
			t.Errorf("pairings = %v, want none", pairingKeys(got))
		}
	})
	t.Run("a dismissed pairing", func(t *testing.T) {
		env := newPairingEnv(t)
		env.saveRules(t, pastaBreadRule())
		pasta := env.planMeal(t, rRigatoni, "mon", 2)
		v, err := env.svc.DismissPairing(ctx, hhA, userAda, testWeek, pasta.ID, "recipe:"+rBread)
		if err != nil {
			t.Fatal(err)
		}
		if got := mealFor(t, v, pasta.ID).Pairings; len(got) != 0 {
			t.Errorf("pairings after dismissing = %v", pairingKeys(got))
		}
		dismissed := env.events.ofType(events.TypePairingDismissed)
		if len(dismissed) != 1 || dismissed[0].Payload.(events.PairingDismissed).Reason != "dismissed" {
			t.Errorf("dismiss events = %+v", dismissed)
		}
		// Dismissing again is harmless.
		if _, err := env.svc.DismissPairing(ctx, hhA, userAda, testWeek, pasta.ID, "recipe:"+rBread); err != nil {
			t.Errorf("dismissing again = %v", err)
		}
	})
}

func TestAcceptPairingRecipe(t *testing.T) {
	env := newPairingEnv(t)
	ctx := context.Background()
	env.saveRules(t, pastaBreadRule())
	// A meal for four takes the add-on's larger serving size.
	pasta := env.planMeal(t, rRigatoni, "mon", 4)

	res, err := env.svc.AcceptPairing(ctx, hhA, userAda, testWeek, pasta.ID, "recipe:"+rBread)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != PairingAdded || res.Entry == nil || res.Entry.RecipeID != rBread || string(res.Entry.Day) != "mon" ||
		res.Entry.Servings != 4 || res.Entry.Origin != planning.OriginAutopilot {
		t.Fatalf("accepted = %+v, entry %+v", res, res.Entry)
	}
	accepted := env.events.ofType(events.TypePairingAccepted)
	if len(accepted) != 1 {
		t.Fatalf("pairing.accepted events = %d", len(accepted))
	}
	if p := accepted[0].Payload.(events.PairingAccepted); p.AddedEntryID != res.Entry.ID || p.EntryID != pasta.ID || p.Kind != PairingKindRecipe {
		t.Errorf("payload = %+v", p)
	}

	// Accepting again adds nothing.
	again, err := env.svc.AcceptPairing(ctx, hhA, userAda, testWeek, pasta.ID, "recipe:"+rBread)
	if err != nil || again.Status != PairingAlreadyAdded || again.Entry == nil {
		t.Fatalf("accepting again = %+v, %v", again, err)
	}
	plan, _ := env.planner.Get(ctx, hhA, testWeek)
	if len(plan.Entries) != 2 {
		t.Errorf("plan entries = %d, want the meal and one add-on", len(plan.Entries))
	}
	if len(env.events.ofType(events.TypePairingAccepted)) != 1 {
		t.Errorf("accepting again should record nothing")
	}

	if _, err := env.svc.AcceptPairing(ctx, hhA, userAda, testWeek, pasta.ID, "recipe:ffffffffffffffffffffffff"); !isNotFound(err) {
		t.Errorf("unknown pairing = %v, want not found", err)
	}
}

func TestAcceptPairingGroceryItem(t *testing.T) {
	env := newPairingEnv(t)
	ctx := context.Background()
	env.saveRules(t, crackersRule())
	soup := env.planMeal(t, rSoup, "tue", 2)

	res, err := env.svc.AcceptPairing(ctx, hhA, userAda, testWeek, soup.ID, "grocery:club crackers")
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != PairingAdded || res.GroceryItem == nil || res.Entry != nil {
		t.Fatalf("accepted = %+v", res)
	}
	item := *res.GroceryItem
	if item.Name != "Club crackers" || item.ForEntryID != soup.ID || item.Text() != "Club crackers for Onion Soup" {
		t.Fatalf("item = %+v", item)
	}

	// It reaches the week's grocery list with its provenance.
	plan, _ := env.planner.Get(ctx, hhA, testWeek)
	lines, err := env.svc.GroceryExtras(ctx, plan)
	if err != nil || len(lines) != 1 {
		t.Fatalf("extras = %+v, %v", lines, err)
	}
	line := lines[0]
	if line.IngredientKey != "name:club crackers" || line.Extra == nil || line.Extra.Text != "Club crackers for Onion Soup" ||
		line.Sources[0].RecipeName != "Onion Soup" || line.Quantity == nil || line.UnitCode != "package" {
		t.Errorf("line = %+v, extra %+v", line, line.Extra)
	}

	// Accepting again keeps one item.
	again, err := env.svc.AcceptPairing(ctx, hhA, userAda, testWeek, soup.ID, "grocery:club crackers")
	if err != nil || again.Status != PairingAlreadyAdded || again.GroceryItem == nil {
		t.Fatalf("accepting again = %+v, %v", again, err)
	}

	// Removing the meal takes the item off the list, without deleting it.
	empty := planning.Plan{HouseholdID: hhA, Week: mustWeek(t, testWeek)}
	if lines, err := env.svc.GroceryExtras(ctx, empty); err != nil || len(lines) != 0 {
		t.Errorf("extras without the meal = %+v, %v", lines, err)
	}

	if err := env.svc.RemovePairingGroceryItem(ctx, hhA, userAda, testWeek, item.ID); err != nil {
		t.Fatal(err)
	}
	if lines, err := env.svc.GroceryExtras(ctx, plan); err != nil || len(lines) != 0 {
		t.Errorf("extras after removing = %+v, %v", lines, err)
	}
	if err := env.svc.RemovePairingGroceryItem(ctx, hhA, userAda, testWeek, item.ID); !isNotFound(err) {
		t.Errorf("removing again = %v, want not found", err)
	}
}

func TestMakePairingRule(t *testing.T) {
	env := newPairingEnv(t)
	ctx := context.Background()
	env.learnRolls(t)
	pasta := env.planMeal(t, rRigatoni, "mon", 2)
	key := "recipe:" + rRolls

	res, err := env.svc.MakePairingRule(ctx, hhA, userAda, testWeek, MakePairingRuleInput{EntryID: pasta.ID, Key: key})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != RuleCreated || res.Rule.Add.RecipeID != rRolls || res.Rule.Frequency != PairingSuggest ||
		!slices.Equal(res.Rule.When.MealCategories, []string{"pasta"}) || len(res.Profile.Pairings) != 1 {
		t.Fatalf("rule = %+v", res)
	}
	created := env.events.ofType(events.TypePairingRuleCreated)
	if len(created) != 1 {
		t.Fatalf("pairing.rule_created events = %d", len(created))
	}
	if p := created[0].Payload.(events.PairingRuleCreated); p.RuleID != res.Rule.ID || p.MealCategory != "pasta" || p.Merged {
		t.Errorf("payload = %+v", p)
	}

	// The suggestion now comes from the rule, so making it again changes
	// nothing.
	again, err := env.svc.MakePairingRule(ctx, hhA, userAda, testWeek, MakePairingRuleInput{EntryID: pasta.ID, Key: key})
	if err != nil || again.Status != RuleUnchanged || len(again.Profile.Pairings) != 1 {
		t.Fatalf("making the rule again = %+v, %v", again, err)
	}

	// The same add-on for another category joins the rule it already has.
	soup := env.planMeal(t, rSoup, "tue", 2)
	env.recipe(t, rRolls).OrderWeeks = append(env.recipe(t, rRolls).OrderWeeks, week(20), week(21), week(22))
	env.recipe(t, rSoup).OrderWeeks = []string{week(20), week(21), week(22)}
	merged, err := env.svc.MakePairingRule(ctx, hhA, userAda, testWeek, MakePairingRuleInput{EntryID: soup.ID, Key: key})
	if err != nil {
		t.Fatal(err)
	}
	if merged.Status != RuleMerged || len(merged.Profile.Pairings) != 1 ||
		!slices.Equal(merged.Profile.Pairings[0].When.MealCategories, []string{"pasta", "soup"}) {
		t.Fatalf("merged rule = %+v", merged.Profile.Pairings)
	}
}

func TestRecipePairings(t *testing.T) {
	env := newPairingEnv(t)
	ctx := context.Background()
	env.saveRules(t, pastaBreadRule())

	// Not planned: the carousel still shows the pairing.
	res, err := env.svc.RecipePairings(ctx, hhA, rRigatoni, testWeek)
	if err != nil {
		t.Fatal(err)
	}
	if res.Entry != nil || len(res.Pairings) != 1 || res.Pairings[0].InPlan || !slices.Equal(res.MealCategories, []string{"pasta"}) {
		t.Fatalf("pairings = %+v, entry %+v", res.Pairings, res.Entry)
	}
	if res.Pairings[0].Recipe.CookMinutes == 0 || res.Pairings[0].Recipe.Name != "Garlic Bread" {
		t.Errorf("target = %+v", res.Pairings[0].Recipe)
	}

	// Planned, with the add-on already in the week: inPlan, not hidden.
	pasta := env.planMeal(t, rRigatoni, "mon", 2)
	env.planMeal(t, rBread, "mon", 2)
	res, err = env.svc.RecipePairings(ctx, hhA, rRigatoni, testWeek)
	if err != nil {
		t.Fatal(err)
	}
	if res.Entry == nil || res.Entry.ID != pasta.ID || len(res.Pairings) != 1 || !res.Pairings[0].InPlan {
		t.Fatalf("planned pairings = %+v, entry %+v", res.Pairings, res.Entry)
	}

	// Without a week nothing is in the plan.
	if res, err = env.svc.RecipePairings(ctx, hhA, rRigatoni, ""); err != nil || res.Week != nil || res.Pairings[0].InPlan {
		t.Errorf("without a week = %+v, %v", res, err)
	}
	// Add-ons have no pairings of their own.
	if res, err = env.svc.RecipePairings(ctx, hhA, rBread, testWeek); err != nil || len(res.Pairings) != 0 {
		t.Errorf("add-on pairings = %+v, %v", res.Pairings, err)
	}
	if _, err := env.svc.RecipePairings(ctx, hhA, "ffffffffffffffffffffffff", testWeek); !isNotFound(err) {
		t.Errorf("unknown recipe = %v, want not found", err)
	}
}

// --- proposals --------------------------------------------------------------------

func generateWithPairings(t *testing.T, env *pairingEnv) Proposal {
	t.Helper()
	p, err := env.svc.Generate(context.Background(), hhA, userAda, testWeek, GenerateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func slotWithPairings(t *testing.T, p Proposal) Slot {
	t.Helper()
	for _, sl := range p.Slots {
		if len(sl.Pairings) > 0 {
			return sl
		}
	}
	t.Fatalf("no slot carries pairings: %+v", p.Slots)
	return Slot{}
}

func TestProposalCarriesPairings(t *testing.T) {
	env := newPairingEnv(t)
	ctx := context.Background()
	env.saveRules(t, tacosBreadRule())

	p := generateWithPairings(t, env)
	sl := slotWithPairings(t, p)
	if sl.RecipeID != rTacos {
		t.Fatalf("pairings belong to the tacos slot, not %+v", sl)
	}
	pairing := sl.Pairings[0]
	if pairing.Key != "recipe:"+rBread || !pairing.Included || pairing.Frequency != PairingAlways {
		t.Fatalf("slot pairing = %+v", pairing)
	}
	suggested := env.events.ofType(events.TypePairingSuggested)
	if len(suggested) != 1 {
		t.Fatalf("pairing.suggested = %d", len(suggested))
	}
	if payload := suggested[0].Payload.(events.PairingSuggested); payload.ProposalID != p.ID || payload.SlotID != sl.ID {
		t.Errorf("payload = %+v", payload)
	}

	// Accepting adds the meals and the included pairings in one change.
	res, err := env.svc.Accept(ctx, hhA, userAda, testWeek, p.Version, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.PairingsAdded) != 1 || res.PairingsAdded[0].Entry == nil || res.PairingsAdded[0].Entry.RecipeID != rBread {
		t.Fatalf("pairings added = %+v", res.PairingsAdded)
	}
	added := res.PairingsAdded[0]
	if string(added.Entry.Day) != sl.Day || added.Entry.Origin != planning.OriginAutopilot || added.ID != ProposalPairingID(sl.ID, added.Pairing.Key) {
		t.Errorf("added pairing entry = %+v", added)
	}
	if len(res.Added) != len(p.Slots) {
		t.Errorf("added meals = %d, want %d; add-ons don't count as meals", len(res.Added), len(p.Slots))
	}
	if accepted := env.events.ofType(events.TypeWeekAccepted); accepted[0].Payload.(events.WeekAccepted).Added != len(p.Slots) {
		t.Errorf("week.accepted added = %+v", accepted[0].Payload)
	}
}

func TestProposalPairingsChosenAtAccept(t *testing.T) {
	ctx := context.Background()
	t.Run("an empty list leaves them all out", func(t *testing.T) {
		env := newPairingEnv(t)
		env.saveRules(t, tacosBreadRule())
		p := generateWithPairings(t, env)
		res, err := env.svc.AcceptWithPairings(ctx, hhA, userAda, testWeek, p.Version, nil, &[]string{})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.PairingsAdded) != 0 {
			t.Fatalf("added = %+v", res.PairingsAdded)
		}
		dismissed := env.events.ofType(events.TypePairingDismissed)
		if len(dismissed) != 1 || dismissed[0].Payload.(events.PairingDismissed).Reason != "excluded" {
			t.Errorf("dismiss events = %+v", dismissed)
		}
	})
	t.Run("an explicit list", func(t *testing.T) {
		env := newPairingEnv(t)
		env.saveRules(t, tacosBreadRule())
		p := generateWithPairings(t, env)
		sl := slotWithPairings(t, p)
		id := ProposalPairingID(sl.ID, sl.Pairings[0].Key)
		res, err := env.svc.AcceptWithPairings(ctx, hhA, userAda, testWeek, p.Version, nil, &[]string{id})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.PairingsAdded) != 1 || res.PairingsAdded[0].ID != id {
			t.Errorf("added = %+v", res.PairingsAdded)
		}
	})
	t.Run("an unknown id", func(t *testing.T) {
		env := newPairingEnv(t)
		env.saveRules(t, tacosBreadRule())
		p := generateWithPairings(t, env)
		_, err := env.svc.AcceptWithPairings(ctx, hhA, userAda, testWeek, p.Version, nil, &[]string{"mon/recipe:nope"})
		if !isInvalid(err) {
			t.Errorf("error = %v, want invalid", err)
		}
	})
	t.Run("an excluded slot drops its pairings", func(t *testing.T) {
		env := newPairingEnv(t)
		env.saveRules(t, tacosBreadRule())
		p := generateWithPairings(t, env)
		sl := slotWithPairings(t, p)
		res, err := env.svc.Accept(ctx, hhA, userAda, testWeek, p.Version, []string{sl.ID})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.PairingsAdded) != 0 {
			t.Errorf("added = %+v", res.PairingsAdded)
		}
	})
}

func TestProposalGroceryPairingsAtAccept(t *testing.T) {
	env := newPairingEnv(t)
	ctx := context.Background()
	env.saveRules(t, tacosChipsRule())

	p := generateWithPairings(t, env)
	sl := slotWithPairings(t, p)
	if sl.RecipeID != rTacos {
		t.Fatalf("pairings belong to the tacos slot, not %+v", sl)
	}
	res, err := env.svc.Accept(ctx, hhA, userAda, testWeek, p.Version, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.PairingsAdded) != 1 || res.PairingsAdded[0].GroceryItem == nil {
		t.Fatalf("added = %+v", res.PairingsAdded)
	}
	item := res.PairingsAdded[0].GroceryItem
	if item.ForRecipeName != "Beef Tacos" || item.ForEntryID == "" {
		t.Errorf("item = %+v", item)
	}
	plan, _ := env.planner.Get(ctx, hhA, testWeek)
	if lines, err := env.svc.GroceryExtras(ctx, plan); err != nil || len(lines) != 1 {
		t.Errorf("extras = %+v, %v", lines, err)
	}
}

// An accepted add-on shares its meal's day: it must not take the day, or
// count as a meal, when the week is generated again.
func TestAddonEntriesDoNotOccupyDays(t *testing.T) {
	env := newPairingEnv(t)
	ctx := context.Background()
	env.saveRules(t, pastaBreadRule())
	env.planMeal(t, rRigatoni, "mon", 2)
	env.planMeal(t, rBread, "mon", 2)

	p, err := env.svc.Generate(ctx, hhA, userAda, testWeek, GenerateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(p.Slots, func(s Slot) bool { return s.Day == "mon" }) {
		t.Errorf("Monday already has a meal: %+v", p.Slots)
	}
	// The week wants 4 meals. Monday's pasta dish is one of them, so 3 are
	// planned; the add-on sharing Monday is not a meal (it would leave 2).
	if p.Planned != 3 {
		t.Errorf("planned = %d, want 3", p.Planned)
	}
}

func TestPairingsNeedAStore(t *testing.T) {
	env := newTestEnv(t) // no pairing store
	ctx := context.Background()
	if _, err := env.svc.WeekPairings(ctx, hhA, userAda, testWeek, ""); !errors.Is(err, ErrPairingsUnavailable) {
		t.Errorf("error = %v, want unavailable", err)
	}
	// Generating still works; the proposal simply has no pairings.
	p, err := env.svc.Generate(ctx, hhA, userAda, testWeek, GenerateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, sl := range p.Slots {
		if len(sl.Pairings) > 0 {
			t.Errorf("slot = %+v", sl)
		}
	}
	plan, _ := env.planner.Get(ctx, hhA, testWeek)
	if lines, err := env.svc.GroceryExtras(ctx, plan); err != nil || lines != nil {
		t.Errorf("extras = %+v, %v", lines, err)
	}
}

func TestPairingRuleMatchingUsesCuisinesTagsAndProteins(t *testing.T) {
	env := newPairingEnv(t)
	ctx := context.Background()
	env.saveRules(t, PairingRule{
		When: PairingWhen{Cuisines: []string{"european"}}, Add: PairingTarget{RecipeID: rBread}, Frequency: PairingSuggest,
	})
	// Onion Soup is French: a like of the region matches it.
	soup := env.planMeal(t, rSoup, "tue", 2)
	v, err := env.svc.WeekPairings(ctx, hhA, userAda, testWeek, "")
	if err != nil {
		t.Fatal(err)
	}
	pairings := mealFor(t, v, soup.ID).Pairings
	if len(pairings) != 1 || pairings[0].MealCategory != "" {
		t.Fatalf("pairings = %+v", pairings)
	}
	if !strings.HasPrefix(pairings[0].Reason, "Your rule: European →") {
		t.Errorf("reason = %q", pairings[0].Reason)
	}
}
