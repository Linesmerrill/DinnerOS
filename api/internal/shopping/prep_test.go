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
	plan := portionPlanFor(packOf(t, "Pork Loin", ingredients.CategoryMeatSeafood, "64", "10"), 1, nil)
	switch {
	case plan.Reserved != "10":
		t.Errorf("Reserved = %q, want 10: Thursday's meal stays out of the freezer", plan.Reserved)
	case plan.Surplus != "54":
		t.Errorf("Surplus = %q, want 54", plan.Surplus)
	case plan.TypicalMeal != "10" || plan.Basis != BasisMeal:
		t.Errorf("typical meal = %q (%s), want 10 from the week's meals", plan.TypicalMeal, plan.Basis)
	case plan.Portions != 5:
		t.Errorf("Portions = %d, want 5: 54 oz holds five whole 10 oz dinners", plan.Portions)
	case plan.PortionSize != "10":
		t.Errorf("PortionSize = %q, want 10: a bag is one dinner, not the surplus split evenly", plan.PortionSize)
	case plan.Frozen != "50" || plan.Leftover != "4":
		t.Errorf("frozen %q, leftover %q; want 50 and 4", plan.Frozen, plan.Leftover)
	}
	// 10 oz of meat at five hours a pound is about three hours. The old button
	// recorded one 54 oz portion and quoted seventeen.
	if plan.Thaw.Hours != 3 || !plan.Thaw.Measured {
		t.Errorf("thaw = %d hours (measured %v), want 3", plan.Thaw.Hours, plan.Thaw.Measured)
	}
	if whole := thawForPortion(packOf(t, "Pork Loin", ingredients.CategoryMeatSeafood, "64", "10"), ratOfExact("54")); whole.Hours != 17 {
		t.Errorf("one whole portion = %d hours, want 17; the test no longer shows what portioning buys", whole.Hours)
	}
}

// Two meals of beef say a meal is eighteen ounces, not thirty-six.
func TestPortionPlanDividesByTheMealsThatNeedIt(t *testing.T) {
	plan := portionPlanFor(packOf(t, "Ground Beef", ingredients.CategoryMeatSeafood, "128", "36"), 2, nil)
	switch {
	case plan.TypicalMeal != "18":
		t.Errorf("TypicalMeal = %q, want 18", plan.TypicalMeal)
	case plan.Portions != 5:
		t.Errorf("Portions = %d, want 5: 92 oz holds five whole 18 oz dinners", plan.Portions)
	case plan.PortionSize != "18" || plan.Leftover != "2":
		t.Errorf("PortionSize = %q, leftover %q; want 18 and 2", plan.PortionSize, plan.Leftover)
	case plan.Thaw.Hours != 6:
		t.Errorf("thaw = %d hours, want 6", plan.Thaw.Hours)
	}
}

// Without recipes on the line there is nothing to divide by, so the week's
// whole need stands in for a meal and the basis says so rather than
// pretending to know more.
func TestPortionPlanFallsBackToTheWeeksNeed(t *testing.T) {
	plan := portionPlanFor(packOf(t, "Pork Loin", ingredients.CategoryMeatSeafood, "64", "16"), 0, nil)
	if plan.Basis != BasisWeek || plan.TypicalMeal != "16" || plan.Portions != 3 {
		t.Errorf("plan = %s basis, typical %q, %d portions; want week / 16 / 3",
			plan.Basis, plan.TypicalMeal, plan.Portions)
	}
}

// The stepper offers every whole number of dinners the surplus holds, each
// bag one dinner's worth with that dinner's thaw time — never a bigger bag.
func TestPortionOptionsAreWholeDinners(t *testing.T) {
	plan := portionPlanFor(packOf(t, "Pork Loin", ingredients.CategoryMeatSeafood, "64", "10"), 1, nil)
	if len(plan.Options) != 5 {
		t.Fatalf("%d options, want 5: 54 oz holds five 10 oz dinners", len(plan.Options))
	}
	for _, o := range plan.Options {
		if o.Size != "10" || o.Thaw.Hours != 3 {
			t.Errorf("option %d = %q, %d hours; want 10 oz, 3 hours", o.Portions, o.Size, o.Thaw.Hours)
		}
	}
}

// Fewer bags than the surplus holds leaves more over; more is capped at
// what it holds.
func TestApplyPortionsKeepsBagsDinnerSized(t *testing.T) {
	pack := packOf(t, "Pork Loin", ingredients.CategoryMeatSeafood, "64", "10")
	two := applyPortions(portionPlanFor(pack, 1, nil), pack, 2)
	if two.Portions != 2 || two.PortionSize != "10" || two.Frozen != "20" || two.Leftover != "34" {
		t.Errorf("two bags = %+v, want 2 × 10 frozen and 34 left", two)
	}
	if many := applyPortions(portionPlanFor(pack, 1, nil), pack, 9); many.Portions != 5 {
		t.Errorf("nine bags of a 54 oz surplus = %d, want 5", many.Portions)
	}
}

// The count never runs away, and a surplus under one dinner freezes nothing.
func TestPortionPlanClamps(t *testing.T) {
	plan := portionPlanFor(packOf(t, "Ground Beef", ingredients.CategoryMeatSeafood, "16", "8"), 1, nil)
	if plan.Portions != 1 || plan.Leftover != "0" {
		t.Errorf("16 oz for 8 oz = %d portions, leftover %q; want exactly one more dinner", plan.Portions, plan.Leftover)
	}
	big := portionPlanFor(packOf(t, "Ground Beef", ingredients.CategoryMeatSeafood, "512", "1"), 1, nil)
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
	card := prepCardFor("h1", "walmart", pack, nil, "2026-09-15", nil)
	if card.Reminder != PrepReminderNone || card.ReminderText != "" {
		t.Errorf("unfreezable card promises %q: %q", card.Reminder, card.ReminderText)
	}

	meat := packOf(t, "Pork Loin", ingredients.CategoryMeatSeafood, "64", "10")
	meat.Recipes = []RecipeRef{{ID: "r1", Name: "Tuscan Pork"}}
	meals := map[string][]PrepMeal{"r1": {{RecipeID: "r1", RecipeName: "Tuscan Pork", Day: "thu", Date: "2026-09-17"}}}
	ahead := prepCardFor("h1", "walmart", meat, meals, "2026-09-15", nil)
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
	late := prepCardFor("h1", "walmart", meat, meals, "2026-09-19", nil)
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

// The card reads in ounces a scale shows and freezes only whole dinners. The
// case that prompted it: a 1.5 lb pack of pork bought for a 10 oz Tuesday
// said "Keep ⅝ lb out … cut the rest into 1 portions of about ⅞ lb" — a 14 oz
// bag that would be thawed for a 10 oz dinner. The household's library uses
// 10 oz of ground pork in 53 recipes and never 4 oz.
func TestPrepFreezesWholeDinnersAndReadsInOunces(t *testing.T) {
	pork := BulkPack{Needed: "5/8", Surplus: "7/8", Unit: "lb", Freezable: true}
	pork.Name, pork.Category = "Ground Pork", ingredients.CategoryMeatSeafood
	plan := portionPlanFor(pork, 1, nil)
	if plan.Portions != 1 || plan.PortionSize != "5/8" || plan.Frozen != "5/8" || plan.Leftover != "1/4" {
		t.Fatalf("plan = %+v, want 1 portion of 5/8 lb frozen and 1/4 lb left over", plan)
	}
	card := PrepCard{Pack: pork, Meals: []PrepMeal{{RecipeName: "Citrus Pork Tacos", Day: "tue"}}, Portions: plan}
	want := "Keep 10 oz out for Tuesday's Citrus Pork Tacos, then freeze another 10 oz in one bag for a future dinner. 4 oz left over. Throw it in for a little more protein, or toss it."
	if got := prepInstruction(card); got != want {
		t.Errorf("instruction:\n got %q\nwant %q", got, want)
	}
	if got := frozenAmount(plan, plan.Portions); got != "5/8" {
		t.Errorf("frozen amount = %q, want 5/8: only the whole portion is sealed", got)
	}

	loin := BulkPack{Needed: "10", Surplus: "54", Unit: "oz", Freezable: true}
	loin.Name, loin.Category = "Pork Loin", ingredients.CategoryMeatSeafood
	plan = portionPlanFor(loin, 1, nil)
	if plan.Portions != 5 || plan.Leftover != "4" || len(plan.Options) != 5 {
		t.Fatalf("plan = %+v, want 5 bags of 10 oz, 4 oz left, options 1..5", plan)
	}
	card = PrepCard{Pack: loin, Meals: []PrepMeal{{RecipeName: "Tuscan Pork", Day: "thu"}}, Portions: plan}
	want = "Keep 10 oz out for Thursday's Tuscan Pork, then freeze 5 bags of 10 oz, one per future dinner. 4 oz left over. Throw it in for a little more protein, or toss it."
	if got := prepInstruction(card); got != want {
		t.Errorf("instruction:\n got %q\nwant %q", got, want)
	}

	// A member asking for more bags than the surplus holds gets what it holds.
	if capped := applyPortions(plan, loin, 9); capped.Portions != 5 {
		t.Errorf("9 portions of a 54 oz surplus = %d, want 5", capped.Portions)
	}

	// Less than a dinner left: nothing to freeze.
	small := BulkPack{Needed: "10", Surplus: "4", Unit: "oz", Freezable: true}
	small.Name = "Ground Beef"
	plan = portionPlanFor(small, 1, nil)
	if plan.Portions != 0 || plan.Frozen != "0" {
		t.Fatalf("plan = %+v, want nothing frozen", plan)
	}
}

// The household's bag size replaces the recipe's dinner, converted to the
// pack's unit; less than a bag is thrown in or tossed. The case that prompted
// it: 19 oz of chicken for a 10 oz dinner left 9 oz and got no card at all.
func TestPrepBagSizeIsTheHouseholdsFutureDinner(t *testing.T) {
	chicken := BulkPack{Needed: "10", Surplus: "9", Unit: "oz", Freezable: true}
	chicken.Name, chicken.Category = "Chicken Breast", ingredients.CategoryMeatSeafood
	plan := portionPlanFor(chicken, 1, nil)
	if plan.Portions != 0 || plan.Leftover != "9" {
		t.Fatalf("default plan = %+v, want no bag and 9 oz left", plan)
	}
	card := PrepCard{Pack: chicken, Meals: []PrepMeal{{RecipeName: "Tuscan Chicken", Day: "mon"}}, Portions: plan}
	want := "Keep 10 oz out for Monday's Tuscan Chicken. 9 oz left over. Throw it in for a little more protein, or toss it."
	if got := prepInstruction(card); got != want {
		t.Errorf("instruction:\n got %q\nwant %q", got, want)
	}

	eight := 8
	plan = portionPlanFor(chicken, 1, &eight)
	if plan.Portions != 1 || plan.PortionSize != "8" || plan.Leftover != "1" {
		t.Fatalf("8 oz plan = %+v, want one 8 oz bag and 1 oz left", plan)
	}
	if got := frozenAmount(plan, 1); got != "8" {
		t.Errorf("frozen amount = %q, want 8", got)
	}

	// A pack in pounds: 8 oz is half a pound.
	beef := BulkPack{Needed: "5/8", Surplus: "3/2", Unit: "lb", Freezable: true}
	beef.Name, beef.Category = "Ground Beef", ingredients.CategoryMeatSeafood
	plan = portionPlanFor(beef, 1, &eight)
	if plan.Portions != 3 || plan.PortionSize != "1/2" || plan.Leftover != "0" {
		t.Fatalf("lb plan = %+v, want 3 bags of 1/2 lb", plan)
	}

	// Not a weight: the recipe's dinner stands.
	buns := BulkPack{Needed: "4", Surplus: "8", Unit: "count", Freezable: true}
	buns.Name, buns.Category = "Buns", ingredients.CategoryBakery
	if plan = portionPlanFor(buns, 1, &eight); plan.PortionSize != "4" || plan.Portions != 2 {
		t.Fatalf("count plan = %+v, want 2 bags of 4", plan)
	}
}

func TestWeightTextRoundsToTheOunce(t *testing.T) {
	cases := []struct{ exact, unit, want string }{
		{"5/8", "lb", "10 oz"},
		{"7/8", "lb", "14 oz"},
		{"18", "oz", "18 oz"},
		{"4", "lb", "4 lb"},
		{"54", "oz", "3 lb 6 oz"},
		{"283", "g", "10 oz"},
		{"2", "tbsp", "2 Tbsp"}, // not a weight: left to the unit's own label
	}
	for _, c := range cases {
		if got := amountText(c.exact, c.unit); got != c.want {
			t.Errorf("amountText(%s %s) = %q, want %q", c.exact, c.unit, got, c.want)
		}
	}
}

// Every bag count carries its own leftover, so the line under the bag cards
// changes with the count and never shows another count's number.
func TestPortionOptionsCarryTheirLeftover(t *testing.T) {
	loin := BulkPack{Needed: "10", Surplus: "54", Unit: "oz", Freezable: true}
	loin.Name = "Pork Loin"
	resp := portionPlanResponse(portionPlanFor(loin, 1, nil))
	want := map[int]string{1: "44 oz", 2: "34 oz", 3: "24 oz", 4: "14 oz", 5: "4 oz"}
	for _, o := range resp.Options {
		if o.LeftoverText != want[o.Portions] {
			t.Errorf("%d bags leave %q, want %q", o.Portions, o.LeftoverText, want[o.Portions])
		}
	}
	if resp.LeftoverText != "4 oz" || resp.LeftoverValue != 4 {
		t.Errorf("suggested count leaves %q (%v), want 4 oz", resp.LeftoverText, resp.LeftoverValue)
	}

	// Nothing left over reads as nothing, not "0 oz".
	even := BulkPack{Needed: "10", Surplus: "20", Unit: "oz", Freezable: true}
	even.Name = "Ground Turkey"
	if got := portionPlanResponse(portionPlanFor(even, 1, nil)).LeftoverText; got != "" {
		t.Errorf("an even split leaves %q, want empty", got)
	}

	// Under a dinner: no bags, and the whole surplus is the leftover.
	small := BulkPack{Needed: "10", Surplus: "4", Unit: "oz", Freezable: true}
	small.Name = "Ground Beef"
	resp = portionPlanResponse(portionPlanFor(small, 1, nil))
	if resp.Portions != 0 || len(resp.Options) != 0 || resp.LeftoverText != "4 oz" {
		t.Errorf("small surplus = %d bags, %d options, leftover %q; want 0, 0, 4 oz",
			resp.Portions, len(resp.Options), resp.LeftoverText)
	}
}
