package recommendations

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot"
	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

func attrsOf(t *testing.T, id string) RecipeAttributes {
	t.Helper()
	for _, r := range testRecipes() {
		if r.ID == id {
			return similarAttributes(r, nil, autopilot.TimeBands{QuickMaxMinutes: 20, MediumMaxMinutes: 35})
		}
	}
	t.Fatalf("no recipe %s", id)
	return RecipeAttributes{}
}

func TestSimilarity(t *testing.T) {
	tacos, burger := attrsOf(t, rTacos), attrsOf(t, rBurger)
	curry, stirFry := attrsOf(t, rCurry), attrsOf(t, rStirFry)
	salmon := attrsOf(t, rSalmon)

	// Two ground-beef meals of about the same length beat a fish dinner.
	beef := similarity(tacos, burger)
	fish := similarity(tacos, salmon)
	if beef.Score <= fish.Score {
		t.Errorf("beef %.2f is not more like tacos than salmon %.2f", beef.Score, fish.Score)
	}
	if beef.Score < MinSimilarity {
		t.Errorf("two ground-beef dinners scored %.2f, below the %.2f bar", beef.Score, MinSimilarity)
	}
	if !slices.ContainsFunc(beef.Reasons, func(r Reason) bool { return r.Code == ReasonSimilarProtein }) {
		t.Errorf("reasons = %+v; want the shared protein", beef.Reasons)
	}
	if len(beef.Reasons) > maxSimilarReasons {
		t.Errorf("reasons = %+v; want at most %d", beef.Reasons, maxSimilarReasons)
	}

	// Asian vegetarian mains share a region and a cook-time band.
	if got := similarity(curry, stirFry); got.Score <= similarity(curry, burger).Score {
		t.Errorf("curry/stir fry %.2f is not more alike than curry/burger %.2f", got.Score, similarity(curry, burger).Score)
	}
	// A meal is as similar to itself as anything can be.
	if self := similarity(tacos, tacos); self.Score < 0.9 {
		t.Errorf("a meal is %.2f similar to itself", self.Score)
	}
	// The reason reads the way the app shows it.
	if got := similarity(curry, curry); got.Reasons[0].Text != "Also Indian" {
		t.Errorf("first reason = %q; want the shared cuisine", got.Reasons[0].Text)
	}
}

func TestSimilarityUnknownFacetsAreNeutral(t *testing.T) {
	// Nothing known about either meal: every facet is unknown, not different.
	blank := RecipeAttributes{}
	if got := similarity(blank, blank); got.Score < 0.49 || got.Score > 0.51 {
		t.Errorf("two unknown meals scored %.2f; want neutral", got.Score)
	}
	// One side knows its cuisine and the other doesn't: that facet is a miss,
	// not a match.
	known := RecipeAttributes{Cuisines: []string{"thai"}}
	if got := similarity(known, blank); got.Score >= similarity(known, known).Score {
		t.Errorf("an unknown meal scored %.2f, as well as an identical one", got.Score)
	}
}

func TestTimeSimilarity(t *testing.T) {
	quick := RecipeAttributes{CookMinutes: 20, TimeBand: "quick"}
	alsoQuick := RecipeAttributes{CookMinutes: 15, TimeBand: "quick"}
	long := RecipeAttributes{CookMinutes: 120, TimeBand: "long"}
	if got := timeSimilarity(quick, alsoQuick); got != 1 {
		t.Errorf("same band = %.2f; want 1", got)
	}
	if got := timeSimilarity(quick, long); got != 0 {
		t.Errorf("20 vs 120 minutes = %.2f; want 0", got)
	}
	medium := RecipeAttributes{CookMinutes: 35, TimeBand: "medium"}
	if got := timeSimilarity(quick, medium); got <= 0 || got >= 1 {
		t.Errorf("15 minutes apart = %.2f; want partial credit", got)
	}
	if got := timeSimilarity(RecipeAttributes{}, quick); got != neutralFacet {
		t.Errorf("unknown time = %.2f; want neutral", got)
	}
}

// plannedWeek puts entries in the week and returns their IDs by recipe.
func plannedWeek(t *testing.T, env *testEnv, entries ...planning.Entry) planning.Plan {
	t.Helper()
	p := planning.Plan{HouseholdID: hhA, Week: mustWeek(t, testWeek), Status: planning.StatusDraft, Entries: entries}
	env.planner.set(p)
	return p
}

func TestAlternativesOffersSimilarMeals(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	plannedWeek(t, env,
		planning.Entry{ID: "e-tacos", RecipeID: rTacos, RecipeName: "Beef Tacos", Day: planning.Tuesday, Servings: 2, Origin: planning.OriginManual},
		planning.Entry{ID: "e-soup", RecipeID: rSoup, RecipeName: "Onion Soup", Day: planning.Wednesday, Servings: 2, Origin: planning.OriginManual},
	)
	res, err := env.svc.Alternatives(ctx, hhA, testWeek, "e-tacos", AlternativeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.EntryID != "e-tacos" || res.Day != "tue" || res.RecipeID != rTacos || res.Servings != 2 {
		t.Fatalf("result = %+v", res)
	}
	if len(res.Alternatives) == 0 || len(res.Alternatives) > DefaultAlternatives {
		t.Fatalf("offered %d alternatives", len(res.Alternatives))
	}
	for _, a := range res.Alternatives {
		if a.RecipeID == rTacos || a.RecipeID == rSoup {
			t.Errorf("offered a meal already in the week: %s", a.RecipeName)
		}
		if a.RecipeID == rBread {
			t.Error("offered an add-on as a meal")
		}
		if a.RecipeID == rBobs {
			t.Error("offered another household's recipe")
		}
		if a.RecipeName == "" || a.Servings == 0 || len(a.Reasons) == 0 {
			t.Errorf("alternative = %+v", a)
		}
	}
	// The beef burger is the closest thing to beef tacos in this library.
	if res.Alternatives[0].RecipeID != rBurger {
		t.Errorf("first alternative = %s; want the other ground-beef meal", res.Alternatives[0].RecipeName)
	}
	if res.Alternatives[0].Similarity < MinSimilarity {
		t.Errorf("top similarity = %.2f", res.Alternatives[0].Similarity)
	}
	// Asking again with the ones already seen offers different meals.
	seen := []string{res.Alternatives[0].RecipeID}
	again, err := env.svc.Alternatives(ctx, hhA, testWeek, "e-tacos", AlternativeOptions{Seen: seen, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range again.Alternatives {
		if slices.Contains(seen, a.RecipeID) {
			t.Errorf("offered %s again after it was seen", a.RecipeName)
		}
	}
	if len(again.Alternatives) > 2 {
		t.Errorf("limit ignored: %d alternatives", len(again.Alternatives))
	}
}

func TestAlternativesRespectRestrictionsAndPastSwaps(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	if _, err := env.svc.UpdateProfile(ctx, hhA, userAda, ProfileUpdate{
		Restrictions: &Restrictions{ExcludedProteins: []string{"pork"}, ExcludedIngredients: []string{"salmon"}},
	}, false); err != nil {
		t.Fatal(err)
	}
	plannedWeek(t, env,
		planning.Entry{ID: "e-thighs", RecipeID: rThighs, RecipeName: "Low and Slow Chicken", Day: planning.Sunday, Servings: 2},
	)
	// The household already swapped the burger away from this week.
	env.events.add(events.Event{
		HouseholdID: hhA, UserID: userAda, Type: events.TypeMealSwapped, RecipeID: rSoup, Week: testWeek,
		OccurredAt: testNow, Payload: events.MealSwapped{PreviousRecipeID: rBurger, Day: "sun"},
	})
	res, err := env.svc.Alternatives(ctx, hhA, testWeek, "e-thighs", AlternativeOptions{Limit: MaxAlternatives})
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range res.Alternatives {
		switch a.RecipeID {
		case rTenderloin:
			t.Error("offered pork to a household that excludes it")
		case rSalmon:
			t.Error("offered an excluded ingredient")
		case rBurger:
			t.Error("offered a meal the household swapped away this week")
		}
	}
	if len(res.Alternatives) == 0 {
		t.Fatal("no alternatives at all")
	}
}

func TestAlternativesWithNothingSimilar(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	// A library of two very different meals: the omelet is all that's left.
	env.recipes.list = []recipes.Recipe{}
	for _, r := range testRecipes() {
		if r.ID == rThighs || r.ID == rOmelet {
			env.recipes.list = append(env.recipes.list, r)
		}
	}
	plannedWeek(t, env,
		planning.Entry{ID: "e-thighs", RecipeID: rThighs, RecipeName: "Low and Slow Chicken", Day: planning.Sunday, Servings: 2},
	)
	res, err := env.svc.Alternatives(ctx, hhA, testWeek, "e-thighs", AlternativeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Alternatives) != 1 || res.Alternatives[0].RecipeID != rOmelet {
		t.Fatalf("alternatives = %+v", res.Alternatives)
	}
	if len(res.Messages) != 1 || res.Messages[0].Code != MessageNoSimilar {
		t.Errorf("messages = %+v; want %s", res.Messages, MessageNoSimilar)
	}
	// With nothing left at all, the answer says so rather than failing.
	empty, err := env.svc.Alternatives(ctx, hhA, testWeek, "e-thighs", AlternativeOptions{Seen: []string{rOmelet}})
	if err != nil {
		t.Fatal(err)
	}
	if len(empty.Alternatives) != 0 || len(empty.Messages) != 1 || empty.Messages[0].Code != MessageSeenAll {
		t.Errorf("empty = %+v", empty)
	}
}

func TestSwapEntryReplacesTheMealAndTeachesAutopilot(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	plannedWeek(t, env,
		planning.Entry{ID: "e-tacos", RecipeID: rTacos, RecipeName: "Beef Tacos", Day: planning.Tuesday, Servings: 2, Origin: planning.OriginManual},
	)
	res, err := env.svc.SwapEntry(ctx, hhA, userAlan, testWeek, "e-tacos", rBurger)
	if err != nil {
		t.Fatal(err)
	}
	if res.Entry.ID != "e-tacos" || res.Entry.RecipeID != rBurger || res.Entry.Day != planning.Tuesday || res.Entry.Servings != 2 {
		t.Fatalf("entry = %+v", res.Entry)
	}
	if res.Previous.RecipeID != rTacos || res.Entry.Origin != planning.OriginAutopilot {
		t.Errorf("previous = %+v, origin = %s", res.Previous, res.Entry.Origin)
	}
	if len(res.Plan.Entries) != 1 {
		t.Errorf("the week gained or lost meals: %+v", res.Plan.Entries)
	}
	swapped := env.events.ofType(events.TypeMealSwapped)
	if len(swapped) != 1 || swapped[0].RecipeID != rBurger {
		t.Fatalf("meal.swapped = %+v", swapped)
	}
	payload := swapped[0].Payload.(events.MealSwapped)
	if payload.PreviousRecipeID != rTacos || payload.EntryID != "e-tacos" || payload.Day != "tue" {
		t.Errorf("meal.swapped payload = %+v", payload)
	}
	planned := env.events.ofType(events.TypeRecipePlanned)
	if len(planned) != 1 || planned[0].RecipeID != rBurger || planned[0].Payload.(events.RecipePlanned).Origin != string(planning.OriginAutopilot) {
		t.Errorf("recipe.planned = %+v", planned)
	}
	// Learning reads the swapped-away meal as "not this one".
	in, _, err := env.svc.prepareInput(ctx, hhA, mustWeek(t, testWeek), res.Plan, DeviceSignals{})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(in.History, func(h autopilot.Interaction) bool {
		return h.ItemID == rTacos && h.Kind == autopilot.KindSwappedOut
	}) {
		t.Error("the swapped-away meal isn't in learning's history")
	}
	// It doesn't come straight back as a suggestion for the same week.
	next, err := env.svc.Alternatives(ctx, hhA, testWeek, "e-tacos", AlternativeOptions{Limit: MaxAlternatives})
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range next.Alternatives {
		if a.RecipeID == rTacos {
			t.Error("the meal that was just swapped away is offered again this week")
		}
	}
}

func TestSwapEntryRefusals(t *testing.T) {
	ctx := context.Background()

	t.Run("finalized week", func(t *testing.T) {
		env := newTestEnv(t)
		env.planner.set(planning.Plan{HouseholdID: hhA, Week: mustWeek(t, testWeek), Status: planning.StatusFinalized, Entries: []planning.Entry{
			{ID: "e-tacos", RecipeID: rTacos, Day: planning.Tuesday, Servings: 2},
		}})
		if _, err := env.svc.Alternatives(ctx, hhA, testWeek, "e-tacos", AlternativeOptions{}); !errors.Is(err, ErrPlanFinalized) {
			t.Errorf("Alternatives(finalized) error = %v", err)
		}
		if _, err := env.svc.SwapEntry(ctx, hhA, userAda, testWeek, "e-tacos", rBurger); !errors.Is(err, ErrPlanFinalized) {
			t.Errorf("SwapEntry(finalized) error = %v", err)
		}
	})

	t.Run("cooked meal", func(t *testing.T) {
		env := newTestEnv(t)
		plannedWeek(t, env, planning.Entry{ID: "e-tacos", RecipeID: rTacos, Day: planning.Tuesday, Servings: 2})
		env.events.add(events.Event{
			HouseholdID: hhA, UserID: userAda, Type: events.TypeRecipeCooked, RecipeID: rTacos, Week: testWeek,
			OccurredAt: testNow, Payload: events.RecipeCooked{EntryID: "e-tacos", Date: "2026-09-15"},
		})
		if _, err := env.svc.Alternatives(ctx, hhA, testWeek, "e-tacos", AlternativeOptions{}); !errors.Is(err, ErrMealCooked) {
			t.Errorf("Alternatives(cooked) error = %v", err)
		}
		if _, err := env.svc.SwapEntry(ctx, hhA, userAda, testWeek, "e-tacos", rBurger); !errors.Is(err, ErrMealCooked) {
			t.Errorf("SwapEntry(cooked) error = %v", err)
		}
	})

	t.Run("add-on, unknown entry, and duplicates", func(t *testing.T) {
		env := newTestEnv(t)
		plannedWeek(t, env,
			planning.Entry{ID: "e-tacos", RecipeID: rTacos, Day: planning.Tuesday, Servings: 2},
			planning.Entry{ID: "e-bread", RecipeID: rBread, RecipeIsAddon: true, Day: planning.Tuesday, Servings: 2},
			planning.Entry{ID: "e-burger", RecipeID: rBurger, Day: planning.Friday, Servings: 2},
		)
		if _, err := env.svc.Alternatives(ctx, hhA, testWeek, "e-bread", AlternativeOptions{}); !errors.Is(err, ErrInvalid) {
			t.Errorf("Alternatives(add-on) error = %v", err)
		}
		if _, err := env.svc.Alternatives(ctx, hhA, testWeek, "e-nope", AlternativeOptions{}); !errors.Is(err, ErrNotFound) {
			t.Errorf("Alternatives(unknown entry) error = %v", err)
		}
		if _, err := env.svc.SwapEntry(ctx, hhA, userAda, testWeek, "e-tacos", rBurger); !errors.Is(err, ErrInvalid) {
			t.Errorf("SwapEntry(already planned) error = %v", err)
		}
		if _, err := env.svc.SwapEntry(ctx, hhA, userAda, testWeek, "e-tacos", rTacos); !errors.Is(err, ErrInvalid) {
			t.Errorf("SwapEntry(same meal) error = %v", err)
		}
		if _, err := env.svc.Alternatives(ctx, hhA, testWeek, "e-tacos", AlternativeOptions{Limit: MaxAlternatives + 1}); !errors.Is(err, ErrInvalid) {
			t.Errorf("Alternatives(limit too high) error = %v", err)
		}
	})
}

func TestAlternativeReasonsReadWell(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	plannedWeek(t, env, planning.Entry{ID: "e-curry", RecipeID: rCurry, RecipeName: "Chickpea Curry", Day: planning.Tuesday, Servings: 2})
	res, err := env.svc.Alternatives(ctx, hhA, testWeek, "e-curry", AlternativeOptions{Limit: MaxAlternatives})
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range res.Alternatives {
		texts := make([]string, 0, len(a.Reasons))
		for _, reason := range a.Reasons {
			if reason.Text == "" || reason.Code == "" {
				t.Errorf("empty reason on %s: %+v", a.RecipeName, a.Reasons)
			}
			texts = append(texts, reason.Text)
		}
		joined := strings.Join(texts, " · ")
		if len(joined) > 120 {
			t.Errorf("reasons are too long for a card: %q", joined)
		}
	}
}
