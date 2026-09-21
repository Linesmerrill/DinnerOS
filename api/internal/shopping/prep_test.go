package shopping

import (
	"strings"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/providers"
)

func packOf(t *testing.T, name, category string, size, need string) BulkPack {
	t.Helper()
	line := boughtLine("l1", name, category, providers.CoveragePerWeek,
		&PackageSize{Quantity: size, Unit: "oz"}, 1, Amount{need, "oz"})
	pack, ok := bulkPackOf(line)
	if !ok {
		t.Fatalf("%s %s oz for %s oz is not a bulk pack", name, size, need)
	}
	return pack
}

// The case the whole feature exists for. A portion is one meal's worth,
// because the household's own Thursday says a meal is ten ounces.
func TestPortionPlanCutsTheLoinIntoMeals(t *testing.T) {
	plan := portionPlanFor(packOf(t, "Pork Loin", ingredients.CategoryMeatSeafood, "64", "10"), 1)
	switch {
	case plan.Reserved != "10":
		t.Errorf("Reserved = %q, want 10: Thursday's meal stays out of the freezer", plan.Reserved)
	case plan.Surplus != "54":
		t.Errorf("Surplus = %q, want 54", plan.Surplus)
	case plan.TypicalMeal != "10" || plan.Basis != BasisMeal:
		t.Errorf("typical meal = %q (%s), want 10 from the week's meals", plan.TypicalMeal, plan.Basis)
	case plan.Portions != 5:
		t.Errorf("Portions = %d, want 5: 54 oz is five more dinners", plan.Portions)
	case plan.PortionSize != "54/5":
		t.Errorf("PortionSize = %q, want 54/5 (10.8 oz)", plan.PortionSize)
	}
	// 10.8 oz of meat at five hours a pound is 3.375 hours. The old button
	// recorded one 54 oz portion and quoted seventeen.
	if plan.Thaw.Hours != 3 || !plan.Thaw.Measured {
		t.Errorf("thaw = %d hours (measured %v), want 3", plan.Thaw.Hours, plan.Thaw.Measured)
	}
	if whole := thawForPortions(packOf(t, "Pork Loin", ingredients.CategoryMeatSeafood, "64", "10"), 1); whole.Hours != 17 {
		t.Errorf("one whole portion = %d hours, want 17; the test no longer shows what portioning buys", whole.Hours)
	}
}

// Two meals of beef say a meal is eighteen ounces, not thirty-six.
func TestPortionPlanDividesByTheMealsThatNeedIt(t *testing.T) {
	plan := portionPlanFor(packOf(t, "Ground Beef", ingredients.CategoryMeatSeafood, "128", "36"), 2)
	switch {
	case plan.TypicalMeal != "18":
		t.Errorf("TypicalMeal = %q, want 18", plan.TypicalMeal)
	case plan.Portions != 5:
		t.Errorf("Portions = %d, want 5: 92 oz is about five 18 oz dinners", plan.Portions)
	case plan.PortionSize != "92/5":
		t.Errorf("PortionSize = %q, want 92/5", plan.PortionSize)
	case plan.Thaw.Hours != 6:
		t.Errorf("thaw = %d hours, want 6", plan.Thaw.Hours)
	}
}

// Without recipes on the line there is nothing to divide by, so the week's
// whole need stands in for a meal and the basis says so rather than
// pretending to know more.
func TestPortionPlanFallsBackToTheWeeksNeed(t *testing.T) {
	plan := portionPlanFor(packOf(t, "Pork Loin", ingredients.CategoryMeatSeafood, "64", "16"), 0)
	if plan.Basis != BasisWeek || plan.TypicalMeal != "16" || plan.Portions != 3 {
		t.Errorf("plan = %s basis, typical %q, %d portions; want week / 16 / 3",
			plan.Basis, plan.TypicalMeal, plan.Portions)
	}
}

// Every offered count carries its own size and thaw time. That is what stops
// a stepper from showing the five-portion thaw beside a two-portion cut.
func TestPortionOptionsEachCarryTheirOwnThaw(t *testing.T) {
	plan := portionPlanFor(packOf(t, "Pork Loin", ingredients.CategoryMeatSeafood, "64", "10"), 1)
	if len(plan.Options) != MaxPrepPortions {
		t.Fatalf("%d options, want %d", len(plan.Options), MaxPrepPortions)
	}
	for _, want := range []struct {
		portions, hours int
		size            string
	}{{1, 17, "54"}, {2, 8, "27"}, {5, 3, "54/5"}, {12, 2, "9/2"}} {
		o := plan.Options[want.portions-1]
		if o.Portions != want.portions || o.Size != want.size || o.Thaw.Hours != want.hours {
			t.Errorf("option %d = %q, %d hours; want %q, %d hours",
				o.Portions, o.Size, o.Thaw.Hours, want.size, want.hours)
		}
	}
}

// A chosen count is honoured, and the thaw estimate follows it.
func TestApplyPortionsRecomputesTheThaw(t *testing.T) {
	pack := packOf(t, "Pork Loin", ingredients.CategoryMeatSeafood, "64", "10")
	plan := applyPortions(portionPlanFor(pack, 1), pack, 2)
	if plan.Portions != 2 || plan.PortionSize != "27" || plan.Thaw.Hours != 8 {
		t.Errorf("two portions = %q, %d hours; want 27, 8", plan.PortionSize, plan.Thaw.Hours)
	}
}

// The count is clamped, never zero and never absurd.
func TestPortionPlanClamps(t *testing.T) {
	// Barely any surplus: 16 oz for 8 oz is one more dinner, not none.
	plan := portionPlanFor(packOf(t, "Ground Beef", ingredients.CategoryMeatSeafood, "16", "8"), 1)
	if plan.Portions != 1 {
		t.Errorf("Portions = %d, want 1", plan.Portions)
	}
	// A tiny per-meal amount against a huge pack would run away.
	big := portionPlanFor(packOf(t, "Ground Beef", ingredients.CategoryMeatSeafood, "512", "1"), 1)
	if big.Portions != MaxPrepPortions {
		t.Errorf("Portions = %d, want the cap %d", big.Portions, MaxPrepPortions)
	}
}

// Reassurance is only ever offered for a reminder that exists. A card that
// freezes nothing promises nothing.
func TestPrepReminderPromisesOnlyWhatExists(t *testing.T) {
	// Produce never reaches a card today (a big produce leftover is tracked
	// pantry stock, so it is not a bulk pack at all), but the branch exists
	// so that a category that freezes badly can never be told to freeze.
	pack := BulkPack{
		LineSource: LineSource{IngredientKey: "name:carrots", Name: "Carrots", Category: ingredients.CategoryProduce},
		LineID:     "l1", Unit: "oz", Bought: "64", Needed: "10", Surplus: "54", SurplusPercent: 84,
	}
	card := prepCardFor("h1", "walmart", pack, nil, "2026-09-15")
	if card.Reminder != PrepReminderNone || card.ReminderText != "" {
		t.Errorf("unfreezable card promises %q: %q", card.Reminder, card.ReminderText)
	}

	meat := packOf(t, "Pork Loin", ingredients.CategoryMeatSeafood, "64", "10")
	meat.Recipes = []RecipeRef{{ID: "r1", Name: "Tuscan Pork"}}
	meals := map[string][]PrepMeal{"r1": {{RecipeID: "r1", RecipeName: "Tuscan Pork", Day: "thu", Date: "2026-09-17"}}}
	ahead := prepCardFor("h1", "walmart", meat, meals, "2026-09-15")
	if ahead.Reminder != PrepReminderThaw || !strings.Contains(ahead.ReminderText, "Thursday") {
		t.Errorf("a meal still ahead = %q: %q", ahead.Reminder, ahead.ReminderText)
	}
	if !strings.Contains(ahead.Instruction, "Thursday's Tuscan Pork") {
		t.Errorf("instruction = %q, want Thursday's meal named", ahead.Instruction)
	}

	// Come back on Saturday and Thursday has gone. The freezer still keeps
	// it off the list, and the thaw reminder still starts the day something
	// is planned — so the copy says that instead of naming a past day.
	meals["r1"][0].Past = true
	late := prepCardFor("h1", "walmart", meat, meals, "2026-09-19")
	if late.Reminder != PrepReminderList || strings.Contains(late.ReminderText, "Thursday") {
		t.Errorf("a week that has gone = %q: %q", late.Reminder, late.ReminderText)
	}
}

func TestMealsText(t *testing.T) {
	for _, tc := range []struct {
		meals []PrepMeal
		want  string
	}{
		{nil, ""},
		{[]PrepMeal{{RecipeName: "Tuscan Pork", Day: "thu"}}, "Thursday's Tuscan Pork"},
		{[]PrepMeal{{RecipeName: "Chili"}}, "Chili"},
		{[]PrepMeal{{RecipeName: "Tacos", Day: "mon"}, {RecipeName: "Chili", Day: "wed"}},
			"Monday's Tacos and Wednesday's Chili"},
		{[]PrepMeal{{RecipeName: "A", Day: "mon"}, {RecipeName: "B", Day: "tue"}, {RecipeName: "C", Day: "wed"}},
			"Monday's A, Tuesday's B, and Wednesday's C"},
	} {
		if got := mealsText(tc.meals); got != tc.want {
			t.Errorf("mealsText = %q, want %q", got, tc.want)
		}
	}
}

// The session's own copy has to be right when there is nothing to do: an
// empty checklist is a good outcome, not a blank screen.
func TestPrepSessionStates(t *testing.T) {
	empty := PrepSession{}
	empty.count()
	if empty.State != PrepNothingToDo || !strings.Contains(empty.Headline, "Nothing to prep") {
		t.Errorf("empty session = %s %q", empty.State, empty.Headline)
	}
	ready := PrepSession{Cards: []PrepCard{{Status: PrepPending}, {Status: PrepDone}}}
	ready.count()
	if ready.State != PrepReady || ready.Pending != 1 || ready.Done != 1 || ready.Headline != "One thing to put away." {
		t.Errorf("ready session = %s %d/%d %q", ready.State, ready.Pending, ready.Done, ready.Headline)
	}
	done := PrepSession{Cards: []PrepCard{{Status: PrepSkipped}, {Status: PrepDone}}}
	done.count()
	if done.State != PrepFinished || done.Skipped != 1 || done.Headline != "Everything's put away." {
		t.Errorf("finished session = %s %q", done.State, done.Headline)
	}
}
