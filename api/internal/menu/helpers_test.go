package menu

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/ratings"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
	"github.com/Linesmerrill/DinnerOS/api/internal/recommendations"
)

// current is the household's current week in unit tests.
var current = week("2026-W38")

func week(s string) planning.Week {
	w, err := planning.ParseWeek(s)
	if err != nil {
		panic(err)
	}
	return w
}

type recipeOpt func(*recipes.Recipe)

// newRecipe returns a made-up main meal: 30 minutes, never ordered, with one
// ingredient that has no protein.
func newRecipe(id, name string, opts ...recipeOpt) recipes.Recipe {
	r := recipes.Recipe{ID: id, Name: name, Servings: []int{2, 4}, PrepMinutes: 30, Ingredients: []recipes.RecipeIngredient{{Name: "Onion"}}}
	for _, o := range opts {
		o(&r)
	}
	return r
}

func cookMinutes(n int) recipeOpt {
	return func(r *recipes.Recipe) { r.PrepMinutes, r.TotalMinutes = n, 0 }
}

// ordered sets n order weeks ending at last, one week apart.
func ordered(n int, last string) recipeOpt {
	return func(r *recipes.Recipe) {
		r.OrderWeeks = nil
		for i := n - 1; i >= 0; i-- {
			r.OrderWeeks = append(r.OrderWeeks, week(last).AddWeeks(-i).String())
		}
		r.TimesOrdered, r.LastOrderedWeek = n, ""
		if n > 0 {
			r.LastOrderedWeek = r.OrderWeeks[n-1]
		}
	}
}

func orderWeeks(weeks ...string) recipeOpt {
	return func(r *recipes.Recipe) {
		r.OrderWeeks = slices.Sorted(slices.Values(weeks))
		r.TimesOrdered = len(weeks)
		r.LastOrderedWeek = r.OrderWeeks[len(weeks)-1]
	}
}

func addon() recipeOpt { return func(r *recipes.Recipe) { r.IsAddon = true } }

func withIngredients(names ...string) recipeOpt {
	return func(r *recipes.Recipe) {
		r.Ingredients = nil
		for _, n := range names {
			r.Ingredients = append(r.Ingredients, recipes.RecipeIngredient{Name: n})
		}
	}
}

func withCuisines(values ...string) recipeOpt { return func(r *recipes.Recipe) { r.Cuisines = values } }

func withTags(values ...string) recipeOpt { return func(r *recipes.Recipe) { r.Tags = values } }

func rate(recipeID, userID string, score int, tags ...ratings.Tag) ratings.Rating {
	return ratings.Rating{RecipeID: recipeID, UserID: userID, Score: score, Tags: tags}
}

func input(catalog ...recipes.Recipe) snapshotInput {
	slices.SortFunc(catalog, func(a, b recipes.Recipe) int { return strings.Compare(a.ID, b.ID) })
	return snapshotInput{UserID: "me", Week: current, Current: current, Location: time.UTC, Catalog: catalog, Profile: recommendations.DefaultProfile("hh")}
}

// configured returns a saved profile.
func configured(edit func(p *recommendations.Profile)) recommendations.Profile {
	p := recommendations.DefaultProfile("hh")
	p.Version = 1
	if edit != nil {
		edit(&p)
	}
	return p
}

func sectionIDs(sections []Section) []string {
	out := []string{}
	for _, s := range sections {
		out = append(out, s.ID)
	}
	return out
}

func findSection(sections []Section, id string) (Section, bool) {
	i := slices.IndexFunc(sections, func(s Section) bool { return s.ID == id })
	if i < 0 {
		return Section{}, false
	}
	return sections[i], true
}

func cardNames(cards []Card) []string {
	out := []string{}
	for _, c := range cards {
		out = append(out, c.Recipe.Name)
	}
	return out
}

func cardReasons(cards []Card) []string {
	out := []string{}
	for _, c := range cards {
		out = append(out, c.Reason)
	}
	return out
}

func badgeCodes(badges []Badge) []string {
	out := []string{}
	for _, b := range badges {
		out = append(out, b.Code)
	}
	return out
}

// --- fakes for Service tests ---------------------------------------------------

type fakeSources struct {
	household households.Household
	catalog   []recipes.Recipe
	ratings   []ratings.Rating
	plans     map[string]planning.Plan
	profile   recommendations.Profile
	proposals map[string]recommendations.Proposal
	events    []events.Event
	// listRanges records Plans.List calls as "from..to".
	listRanges []string
}

func (f *fakeSources) options(now time.Time) Options {
	return Options{Recipes: f, Ratings: f, Plans: f, Autopilot: f, Events: eventLog{f}, Households: f, Now: func() time.Time { return now }}
}

func (f *fakeSources) GetHousehold(context.Context, string) (households.Household, error) {
	return f.household, nil
}

func (f *fakeSources) MenuCatalog(context.Context, string) ([]recipes.Recipe, error) {
	return slices.Clone(f.catalog), nil
}

func (f *fakeSources) HouseholdRatings(context.Context, string) ([]ratings.Rating, error) {
	return f.ratings, nil
}

func (f *fakeSources) Get(_ context.Context, householdID, w string) (planning.Plan, error) {
	if p, ok := f.plans[w]; ok {
		return p, nil
	}
	return planning.Plan{HouseholdID: householdID, Week: week(w), Status: planning.StatusDraft}, nil
}

func (f *fakeSources) List(_ context.Context, _ string, from, to string) ([]planning.Summary, error) {
	fw, tw := week(from), week(to)
	if n := fw.WeeksUntil(tw) + 1; n < 1 || n > planning.MaxRangeWeeks {
		return nil, fmt.Errorf("range %s..%s covers %d weeks", from, to, n)
	}
	f.listRanges = append(f.listRanges, from+".."+to)
	var out []planning.Summary
	for w := fw; w.WeeksUntil(tw) >= 0; w = w.AddWeeks(1) {
		sum := planning.Summary{Week: w, Status: planning.StatusDraft}
		if p, ok := f.plans[w.String()]; ok {
			sum = planning.Summary{Week: w, Status: p.Status, EntryCount: len(p.Entries), UpdatedAt: p.UpdatedAt}
		}
		out = append(out, sum)
	}
	return out, nil
}

func (f *fakeSources) EarliestPlannedWeek(context.Context, string) (planning.Week, bool, error) {
	var earliest planning.Week
	found := false
	for _, p := range f.plans {
		if len(p.Entries) > 0 && (!found || p.Week.WeeksUntil(earliest) > 0) {
			earliest, found = p.Week, true
		}
	}
	return earliest, found, nil
}

func (f *fakeSources) Profile(context.Context, string) (recommendations.Profile, error) {
	return f.profile, nil
}

func (f *fakeSources) RecipeOverrides(context.Context, string) ([]recommendations.RecipeOverride, error) {
	return nil, nil
}

func (f *fakeSources) Proposal(_ context.Context, _ string, w string) (recommendations.Proposal, error) {
	if p, ok := f.proposals[w]; ok {
		return p, nil
	}
	return recommendations.Proposal{}, recommendations.ErrNotFound
}

// eventLog adapts fakeSources to EventSource (List is taken by PlanSource).
type eventLog struct{ f *fakeSources }

func (e eventLog) List(_ context.Context, q events.Query) ([]events.Event, error) {
	var out []events.Event
	for _, ev := range e.f.events {
		if !q.Since.IsZero() && ev.OccurredAt.Before(q.Since) {
			continue
		}
		out = append(out, ev)
	}
	return out, nil
}
