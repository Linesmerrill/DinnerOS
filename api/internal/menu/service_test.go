package menu

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
	"github.com/Linesmerrill/DinnerOS/api/internal/recommendations"
)

func testSources() *fakeSources {
	return &fakeSources{
		household: households.Household{ID: "hh", TimeZone: "America/Denver", DefaultServings: 2},
		profile:   recommendations.DefaultProfile("hh"),
		catalog: []recipes.Recipe{
			newRecipe("r01", "Beef Tacos", withIngredients("Ground Beef"), ordered(6, "2026-W36")),
			newRecipe("r02", "Speedy Shrimp", cookMinutes(15), withIngredients("Shrimp")),
		},
		plans:     map[string]planning.Plan{},
		proposals: map[string]recommendations.Proposal{},
	}
}

// The current week is the household's, not UTC's: 03:00 UTC on Monday is
// still Sunday, the last day of the previous week, in Denver.
func TestMenuUsesTheHouseholdTimeZone(t *testing.T) {
	f := testSources()
	svc := NewService(f.options(time.Date(2026, 9, 14, 3, 0, 0, 0, time.UTC)))

	m, err := svc.Menu(context.Background(), "hh", "me", "")
	if err != nil {
		t.Fatal(err)
	}
	if m.Week != week("2026-W37") || m.CurrentWeek != week("2026-W37") || m.Timing != TimingCurrent {
		t.Errorf("menu = week %s, current %s, timing %s; want 2026-W37 and current", m.Week, m.CurrentWeek, m.Timing)
	}
}

func TestMenuWeekTimingPlanAndProposal(t *testing.T) {
	f := testSources()
	stored := time.Date(2026, 9, 14, 18, 0, 0, 0, time.UTC)
	f.plans["2026-W38"] = planning.Plan{
		HouseholdID: "hh", Week: current, Status: planning.StatusDraft, CreatedAt: stored, UpdatedAt: stored,
		Entries: []planning.Entry{{ID: "e1", RecipeID: "r01", Day: planning.Tuesday, Servings: 2}},
	}
	f.proposals["2026-W39"] = recommendations.Proposal{ID: "p1", Status: recommendations.StatusProposed, Version: 3, Planned: 5}
	svc := NewService(f.options(time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)))
	ctx := context.Background()

	m, err := svc.Menu(ctx, "hh", "me", "")
	if err != nil {
		t.Fatal(err)
	}
	if m.Plan == nil || len(m.Plan.Entries) != 1 || m.Proposal != nil {
		t.Errorf("current week = plan %+v, proposal %+v", m.Plan, m.Proposal)
	}
	// The week's plan decides inPlan and planEntryIds.
	favorites, ok := findSection(m.Sections, SectionFavorites)
	if !ok || !favorites.Cards[0].InPlan || len(favorites.Cards[0].PlanEntryIDs) != 1 {
		t.Errorf("favorites card = %+v", favorites.Cards)
	}

	next, err := svc.Menu(ctx, "hh", "me", "2026-W39")
	if err != nil {
		t.Fatal(err)
	}
	if next.Timing != TimingUpcoming || next.Plan != nil {
		t.Errorf("2026-W39 = timing %s, plan %+v; want upcoming with no stored plan", next.Timing, next.Plan)
	}
	if p := next.Proposal; p == nil || p.ID != "p1" || p.Version != 3 || p.Planned != 5 {
		t.Errorf("proposal = %+v", p)
	}
	if card, _ := findSection(next.Sections, SectionFavorites); card.Cards[0].InPlan {
		t.Error("a card should not be in the plan of a week with no plan")
	}

	past, err := svc.Menu(ctx, "hh", "me", "2026-W30")
	if err != nil {
		t.Fatal(err)
	}
	if past.Timing != TimingPast {
		t.Errorf("2026-W30 timing = %s, want past", past.Timing)
	}
	for _, sec := range past.Sections {
		if sec.ID == SectionQuick || sec.ID == SectionNewToYou {
			t.Errorf("past week has the forward-looking section %s", sec.ID)
		}
	}

	if _, err := svc.Menu(ctx, "hh", "me", "2026-W99"); !errors.Is(err, ErrInvalid) {
		t.Errorf("Menu() with an invalid week = %v, want ErrInvalid", err)
	}
}

func TestRecipesUsesTheWeekForInPlan(t *testing.T) {
	f := testSources()
	stored := time.Date(2026, 9, 14, 18, 0, 0, 0, time.UTC)
	f.plans["2026-W38"] = planning.Plan{
		HouseholdID: "hh", Week: current, Status: planning.StatusDraft, CreatedAt: stored, UpdatedAt: stored,
		Entries: []planning.Entry{{ID: "e1", RecipeID: "r02", Servings: 2}},
	}
	svc := NewService(f.options(time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)))
	ctx := context.Background()

	page, err := svc.Recipes(ctx, "hh", "me", ListQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Cards) != 2 {
		t.Fatalf("cards = %v", cardNames(page.Cards))
	}
	for _, c := range page.Cards {
		if want := c.Recipe.ID == "r02"; c.InPlan != want {
			t.Errorf("%q inPlan = %v, want %v", c.Recipe.Name, c.InPlan, want)
		}
	}
	if page.NextCursor != "" {
		t.Errorf("nextCursor = %q, want none on the last page", page.NextCursor)
	}

	other, err := svc.Recipes(ctx, "hh", "me", ListQuery{Week: "2026-W39"})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range other.Cards {
		if c.InPlan {
			t.Errorf("%q is in the plan of a week with no plan", c.Recipe.Name)
		}
	}
}
