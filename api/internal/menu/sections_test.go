package menu

import (
	"slices"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/ratings"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
	"github.com/Linesmerrill/DinnerOS/api/internal/recommendations"
)

func TestUpcomingSectionsInOrder(t *testing.T) {
	catalog := []recipes.Recipe{
		newRecipe("r01", "Beef Tacos", withIngredients("Ground Beef"), ordered(6, "2026-W36")),
		newRecipe("r02", "Speedy Shrimp", cookMinutes(15), withIngredients("Shrimp"), ordered(1, "2026-W20")),
		newRecipe("r03", "Brand New Curry", withIngredients("Chickpeas")),
		newRecipe("r04", "Braised Short Ribs", cookMinutes(180), withIngredients("Short Rib")),
		newRecipe("r05", "Garlic Bread", addon(), ordered(4, "2026-W37")),
	}
	snap := newSnapshot(input(catalog...))

	got := snap.sections(TimingUpcoming)
	want := []string{SectionFavorites, SectionQuick, SectionNewToYou, SectionLongCooks, SectionSides}
	if !slices.Equal(sectionIDs(got), want) {
		t.Fatalf("sections = %v, want %v", sectionIDs(got), want)
	}
	for _, sec := range got {
		if sec.Kind != KindCarousel || sec.Title == "" || sec.Subtitle == "" {
			t.Errorf("section %s = %+v", sec.ID, sec)
		}
	}
	quick, _ := findSection(got, SectionQuick)
	if quick.Subtitle != "Ready in 20 minutes or less" || quick.MoreQuery["maxMinutes"] != "20" {
		t.Errorf("quick section = %+v", quick)
	}
	if favorites, _ := findSection(got, SectionFavorites); favorites.MoreQuery["sort"] != string(SortPopular) {
		t.Errorf("favorites moreQuery = %v", favorites.MoreQuery)
	}
	// The current week shows the same sections as an upcoming one.
	if ids := sectionIDs(snap.sections(TimingCurrent)); !slices.Equal(ids, want) {
		t.Errorf("current-week sections = %v, want %v", ids, want)
	}
}

// A section with nothing to show is left out.
func TestEmptySectionsAreOmitted(t *testing.T) {
	// A medium-length meal nobody has ordered belongs to New to You alone.
	snap := newSnapshot(input(newRecipe("r01", "Only Meal")))
	if ids := sectionIDs(snap.sections(TimingUpcoming)); !slices.Equal(ids, []string{SectionNewToYou}) {
		t.Errorf("sections = %v, want only new_to_you", ids)
	}
	// One ordered medium-length meal matches no section at all.
	once := newSnapshot(input(newRecipe("r01", "Only Meal", ordered(1, "2026-W20"))))
	if ids := sectionIDs(once.sections(TimingUpcoming)); len(ids) != 0 {
		t.Errorf("sections = %v, want none", ids)
	}
	if ids := sectionIDs(newSnapshot(input()).sections(TimingUpcoming)); len(ids) != 0 {
		t.Errorf("an empty catalog has sections %v", ids)
	}
}

func TestFavoritesRankingAndReasons(t *testing.T) {
	in := input(
		newRecipe("r01", "A Five Star", ordered(1, "2021-W10")),
		newRecipe("r02", "B Frequent", ordered(10, "2026-W37")),
		newRecipe("r03", "C Stale", ordered(4, "2023-W01")),
		newRecipe("r04", "D Never Again", ordered(8, "2026-W37")),
		newRecipe("r05", "E Add-on", addon(), ordered(9, "2026-W37")),
		newRecipe("r06", "F Low Rated", ordered(5, "2026-W37")),
		newRecipe("r07", "G Kid Pleaser"),
	)
	in.Ratings = []ratings.Rating{
		rate("r01", "me", 5, ratings.TagMakeAgain),
		rate("r04", "them", 2, ratings.TagNeverAgain),
		rate("r06", "them", 2),
		rate("r07", "them", 3, ratings.TagKidFavorite),
	}
	sec, ok := findSection(newSnapshot(in).sections(TimingUpcoming), SectionFavorites)
	if !ok {
		t.Fatal("no favorites section")
	}
	// Stale orders, never-again, low ratings, and add-ons are all left out.
	if want := []string{"A Five Star", "B Frequent", "G Kid Pleaser"}; !slices.Equal(cardNames(sec.Cards), want) {
		t.Errorf("favorites = %v, want %v", cardNames(sec.Cards), want)
	}
	if want := []string{"You rated this 5★", "Ordered 10 times", "A kid favorite"}; !slices.Equal(cardReasons(sec.Cards), want) {
		t.Errorf("reasons = %v, want %v", cardReasons(sec.Cards), want)
	}
}

func TestSectionLimitAndDeterministicOrder(t *testing.T) {
	var catalog []recipes.Recipe
	for i := range 30 {
		name := "Quick Meal"
		if i%2 == 0 {
			name = "quick meal" // same name, different case
		}
		catalog = append(catalog, newRecipe(pad(i), name, cookMinutes(10)))
	}
	snap := newSnapshot(input(catalog...))
	sec, _ := findSection(snap.sections(TimingUpcoming), SectionQuick)
	if len(sec.Cards) != sectionLimit {
		t.Fatalf("quick section has %d cards, want %d", len(sec.Cards), sectionLimit)
	}
	// Identical signals and names: ID order decides, case-insensitively.
	var ids []string
	for _, c := range sec.Cards {
		ids = append(ids, c.Recipe.ID)
	}
	want := []string{"r00", "r01", "r02", "r03", "r04", "r05", "r06", "r07", "r08", "r09", "r10", "r11"}
	if !slices.Equal(ids, want) {
		t.Errorf("quick section = %v, want %v", ids, want)
	}
	again, _ := findSection(newSnapshot(input(catalog...)).sections(TimingUpcoming), SectionQuick)
	if !slices.Equal(cardNames(again.Cards), cardNames(sec.Cards)) {
		t.Error("sections are not deterministic")
	}
}

func pad(i int) string {
	const digits = "0123456789"
	return "r" + string([]byte{digits[i/10], digits[i%10]})
}

// Add-ons appear only in Sides & Add-ons.
func TestAddonsOnlyAppearInSides(t *testing.T) {
	snap := newSnapshot(input(
		newRecipe("r01", "Side Salad", addon(), cookMinutes(10)),
		newRecipe("r02", "Dinner Rolls", addon(), cookMinutes(200), ordered(3, "2026-W37")),
		newRecipe("r03", "Real Dinner", cookMinutes(10)),
	))
	for _, sec := range snap.sections(TimingUpcoming) {
		for _, c := range sec.Cards {
			if c.Recipe.IsAddon && sec.ID != SectionSides {
				t.Errorf("add-on %q in section %s", c.Recipe.Name, sec.ID)
			}
		}
	}
	sides, _ := findSection(snap.sections(TimingUpcoming), SectionSides)
	if want := []string{"Dinner Rolls", "Side Salad"}; !slices.Equal(cardNames(sides.Cards), want) {
		t.Errorf("sides = %v, want most ordered first %v", cardNames(sides.Cards), want)
	}
}

func TestPastWeekSections(t *testing.T) {
	past := week("2026-W30")
	in := input(
		newRecipe("r01", "Tuesday Tacos", orderWeeks("2026-W20", "2026-W30")),
		newRecipe("r02", "Sunday Soup"),
		newRecipe("r03", "Side Fries", addon(), orderWeeks("2026-W30")),
		newRecipe("r04", "Old Favorite", ordered(5, "2026-W29")),
		newRecipe("r05", "Dinner Rolls", addon()),
	)
	in.Week = past
	in.Plan = planning.Plan{
		Week: past, Status: planning.StatusFinalized, CreatedAt: time.Now(),
		Entries: []planning.Entry{
			{ID: "e1", RecipeID: "r01", Day: planning.Tuesday},
			{ID: "e2", RecipeID: "r02"},
			{ID: "e3", RecipeID: "r01", Day: planning.Thursday},
			{ID: "e4", RecipeID: "r05", Day: planning.Monday},
		},
	}
	in.Cooked = []events.Event{{RecipeID: "r02", Week: past.String(), Payload: events.RecipeCooked{EntryID: "e2"}}}

	got := newSnapshot(in).sections(TimingPast)
	want := []string{SectionHistoryPlanned, SectionHistoryOrdered, SectionFavorites}
	if !slices.Equal(sectionIDs(got), want) {
		t.Fatalf("past sections = %v, want %v", sectionIDs(got), want)
	}
	planned, _ := findSection(got, SectionHistoryPlanned)
	if planned.Kind != KindHistory || planned.Title != "You Planned" {
		t.Errorf("history_planned = %+v", planned)
	}
	if want := []string{"Dinner Rolls", "Tuesday Tacos", "Sunday Soup"}; !slices.Equal(cardNames(planned.Cards), want) {
		t.Errorf("planned = %v, want day order %v", cardNames(planned.Cards), want)
	}
	if want := []string{"Planned for Monday", "Planned for Tuesday", "Cooked"}; !slices.Equal(cardReasons(planned.Cards), want) {
		t.Errorf("planned reasons = %v, want %v", cardReasons(planned.Cards), want)
	}
	// One card per recipe, carrying every entry, and the week's plan is the
	// one inPlan refers to.
	tacos := planned.Cards[1]
	if !slices.Equal(tacos.PlanEntryIDs, []string{"e1", "e3"}) || !tacos.InPlan {
		t.Errorf("tacos card = %+v", tacos)
	}

	ordered, _ := findSection(got, SectionHistoryOrdered)
	if want := []string{"Tuesday Tacos", "Side Fries"}; !slices.Equal(cardNames(ordered.Cards), want) {
		t.Errorf("ordered = %v, want main meals first, add-ons included %v", cardNames(ordered.Cards), want)
	}
	if want := []string{"Ordered 2 times", "First time ordered"}; !slices.Equal(cardReasons(ordered.Cards), want) {
		t.Errorf("ordered reasons = %v, want %v", cardReasons(ordered.Cards), want)
	}
	if favorites, _ := findSection(got, SectionFavorites); favorites.Title != "Cook It Again" || !slices.Equal(cardNames(favorites.Cards), []string{"Old Favorite"}) {
		t.Errorf("past favorites = %+v", favorites)
	}
}

func TestNewToYouRanksByTasteFit(t *testing.T) {
	in := input(
		newRecipe("r01", "Pasta Night", withCuisines("Italian"), withIngredients("Chicken Breast")),
		newRecipe("r02", "Pad Thai", withCuisines("Thai"), withIngredients("Tofu")),
		newRecipe("r03", "Shrimp Scampi", withCuisines("Italian"), withIngredients("Shrimp")),
		newRecipe("r04", "Chicken Enchiladas", withCuisines("Mexican"), withIngredients("Chicken Thighs"), ordered(8, "2026-W37")),
	)
	in.Profile = configured(func(p *recommendations.Profile) {
		p.Taste.Likes.Cuisines = []string{"italian"}
		p.Restrictions.ExcludedProteins = []string{"shellfish"}
	})
	sec, ok := findSection(newSnapshot(in).sections(TimingUpcoming), SectionNewToYou)
	if !ok {
		t.Fatal("no new_to_you section")
	}
	// Ordered meals are not new, and a hard restriction rules one out.
	if want := []string{"Pasta Night", "Pad Thai"}; !slices.Equal(cardNames(sec.Cards), want) {
		t.Fatalf("new_to_you = %v, want %v", cardNames(sec.Cards), want)
	}
	if sec.Subtitle != "Picked for your taste" || sec.Cards[0].Reason != "You like Italian" {
		t.Errorf("section = %+v, first reason %q", sec, sec.Cards[0].Reason)
	}
	// Without a profile the section still works, and nothing is restricted.
	plain := newSnapshot(input(
		newRecipe("r01", "Pasta Night", withCuisines("Italian")),
		newRecipe("r03", "Shrimp Scampi", withIngredients("Shrimp")),
	))
	sec, _ = findSection(plain.sections(TimingUpcoming), SectionNewToYou)
	if len(sec.Cards) != 2 || sec.Subtitle != "Meals you haven't had yet" {
		t.Errorf("new_to_you without a profile = %+v", sec)
	}
}

func TestBadges(t *testing.T) {
	in := input(
		newRecipe("r01", "Quick Favorite", cookMinutes(15), ordered(6, "2026-W37")),
		newRecipe("r02", "Plain Quick", cookMinutes(10)),
		newRecipe("r03", "Proposed Meal", ordered(5, "2026-W37")),
	)
	in.Ratings = []ratings.Rating{rate("r01", "me", 5, ratings.TagMakeAgain), rate("r01", "them", 5)}
	in.Proposal = &recommendations.Proposal{
		ID: "p1", Status: recommendations.StatusProposed, Slots: []recommendations.Slot{{RecipeID: "r03"}},
	}
	snap := newSnapshot(in)

	tests := []struct {
		id       string
		suppress string
		promote  string
		want     []string
	}{
		{id: "r01", want: []string{BadgeMakeAgain, BadgeTopRated}},
		{id: "r02", want: []string{BadgeQuick, BadgeNew}},
		// A section suppresses the badge that repeats its title.
		{id: "r02", suppress: BadgeQuick, want: []string{BadgeNew}},
		{id: "r02", suppress: BadgeNew, want: []string{BadgeQuick}},
		{id: "r03", want: []string{BadgeAutopilotPick, BadgeOftenOrdered}},
		// A promoted badge leads when it is earned.
		{id: "r01", promote: BadgeQuick, want: []string{BadgeQuick, BadgeMakeAgain}},
		{id: "r03", promote: BadgeSmokerFriendly, want: []string{BadgeAutopilotPick, BadgeOftenOrdered}},
	}
	for _, tt := range tests {
		got := badgeCodes(snap.badges(snap.byID[tt.id], tt.suppress, tt.promote))
		if !slices.Equal(got, tt.want) {
			t.Errorf("badges(%s, suppress %q, promote %q) = %v, want %v", tt.id, tt.suppress, tt.promote, got, tt.want)
		}
		if len(got) > maxBadges {
			t.Errorf("badges(%s) returned %d badges", tt.id, len(got))
		}
	}
}
