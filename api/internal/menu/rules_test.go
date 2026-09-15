package menu

import (
	"slices"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
	"github.com/Linesmerrill/DinnerOS/api/internal/recommendations"
)

func smokerNight() recommendations.WeekdayRule {
	return recommendations.WeekdayRule{
		Day: "sun", Label: "Smoker night", Proteins: []string{"chicken", "pork"},
		Methods: []string{"smoker"}, TimeBand: "long", Frequency: "every_week",
	}
}

func ruleCatalog() []recipes.Recipe {
	return []recipes.Recipe{
		newRecipe("r01", "Smoked Pork Shoulder", cookMinutes(360), withIngredients("Pork Shoulder"), ordered(3, "2026-W36")),
		newRecipe("r02", "Pork Tenderloin Dinner", cookMinutes(120), withIngredients("Pork Tenderloin")),
		newRecipe("r03", "Chicken Pot Pie", cookMinutes(60), withIngredients("Chicken Breast")),
		newRecipe("r04", "Ground Pork Tacos", cookMinutes(25), withIngredients("Ground Pork")),
		newRecipe("r05", "Brisket Plate", cookMinutes(400), withIngredients("Beef Brisket")),
	}
}

// A rule's methods dominate, as they do in Autopilot: a meal that suits none
// is left out however well its protein matches.
func TestRuleSectionMatchesMethodsFirst(t *testing.T) {
	in := input(ruleCatalog()...)
	in.Profile = configured(func(p *recommendations.Profile) {
		p.Equipment = []string{"smoker"}
		p.WeekdayRules = []recommendations.WeekdayRule{smokerNight()}
	})
	sections := newSnapshot(in).sections(TimingUpcoming)

	sec, ok := findSection(sections, "rule_sun")
	if !ok {
		t.Fatalf("no rule_sun section in %v", sectionIDs(sections))
	}
	if sec.Title != "Smoker Night Ideas" || sec.Subtitle != "For Sunday" || sec.Kind != KindCarousel {
		t.Errorf("section = %+v", sec)
	}
	// Pot pie and tacos are dish shapes the smoker heuristic rejects, so they
	// are out; brisket suits the smoker but misses the proteins, so it comes
	// after the full matches.
	want := []string{"Smoked Pork Shoulder", "Pork Tenderloin Dinner", "Brisket Plate"}
	if !slices.Equal(cardNames(sec.Cards), want) {
		t.Fatalf("rule section = %v, want %v", cardNames(sec.Cards), want)
	}
	if want := []string{"Smoker · Pork", "Smoker · Pork", "Smoker · Long cook"}; !slices.Equal(cardReasons(sec.Cards), want) {
		t.Errorf("reasons = %v, want %v", cardReasons(sec.Cards), want)
	}
	// The section is about the smoker, so that badge leads.
	if got := badgeCodes(sec.Cards[0].Badges); len(got) == 0 || got[0] != BadgeSmokerFriendly {
		t.Errorf("first card badges = %v, want smoker_friendly first", got)
	}
}

// A rule naming equipment the household doesn't have is scored as if it had
// no methods.
func TestRuleSectionIgnoresMethodsWithoutEquipment(t *testing.T) {
	in := input(ruleCatalog()...)
	in.Profile = configured(func(p *recommendations.Profile) {
		p.Equipment = nil
		p.WeekdayRules = []recommendations.WeekdayRule{smokerNight()}
	})
	sec, ok := findSection(newSnapshot(in).sections(TimingUpcoming), "rule_sun")
	if !ok {
		t.Fatal("no rule_sun section")
	}
	// Only the proteins group is left, so pork and chicken meals qualify.
	want := []string{"Smoked Pork Shoulder", "Chicken Pot Pie", "Ground Pork Tacos", "Pork Tenderloin Dinner"}
	got := cardNames(sec.Cards)
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("rule section = %v, want %v", got, want)
	}
	for _, c := range sec.Cards {
		if slices.Contains(badgeCodes(c.Badges), BadgeSmokerFriendly) {
			t.Errorf("%q has a smoker badge without a smoker", c.Recipe.Name)
		}
	}
}

// One section per rule, Monday first, with a distinct ID per rule.
func TestRuleSectionsPerDay(t *testing.T) {
	in := input(
		newRecipe("r01", "Taco Tuesday Beef", withIngredients("Ground Beef"), withCuisines("Mexican")),
		newRecipe("r02", "Sunday Pork Shoulder", cookMinutes(300), withIngredients("Pork Shoulder")),
		newRecipe("r03", "Pasta Night", withCuisines("Italian")),
	)
	in.Profile = configured(func(p *recommendations.Profile) {
		p.Equipment = []string{"smoker"}
		p.WeekdayRules = []recommendations.WeekdayRule{
			smokerNight(),
			{Day: "tue", Label: "Taco Tuesday", Cuisines: []string{"mexican"}, Frequency: "every_week"},
			{Day: "tue", Label: "Pasta too", Cuisines: []string{"italian"}, Frequency: "at_most_once"},
		}
	})
	got := sectionIDs(newSnapshot(in).sections(TimingUpcoming))
	// Rule sections come between New to You and Worth the Wait, earliest
	// weekday first, and a second rule on one day gets its own ID.
	for _, want := range []string{"rule_tue", "rule_tue_2", "rule_sun"} {
		if !slices.Contains(got, want) {
			t.Errorf("sections = %v, want %s", got, want)
		}
	}
	if i, j := slices.Index(got, "rule_tue"), slices.Index(got, "rule_sun"); i > j {
		t.Errorf("sections = %v, want Tuesday before Sunday", got)
	}
}

// A rule with only a time band lists meals in that band.
func TestRuleSectionWithOnlyATimeBand(t *testing.T) {
	in := input(
		newRecipe("r01", "Long Braise", cookMinutes(200)),
		newRecipe("r02", "Fast Skillet", cookMinutes(15)),
	)
	in.Profile = configured(func(p *recommendations.Profile) {
		p.WeekdayRules = []recommendations.WeekdayRule{{Day: "sat", Label: "Project cook", TimeBand: "long", Frequency: "at_most_once"}}
	})
	sec, ok := findSection(newSnapshot(in).sections(TimingUpcoming), "rule_sat")
	if !ok {
		t.Fatal("no rule_sat section")
	}
	if want := []string{"Long Braise"}; !slices.Equal(cardNames(sec.Cards), want) {
		t.Errorf("rule section = %v, want %v", cardNames(sec.Cards), want)
	}
	if sec.Title != "Project Cook Ideas" || sec.Subtitle != "For Saturday" {
		t.Errorf("section = %+v", sec)
	}
}
