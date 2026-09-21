package recommendations

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot"
	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// "Try something similar": a member long-presses a planned meal and re-picks
// it. Alternatives offers a few meals that resemble it; SwapEntry applies one.
// Both keep the day, the entry, and the rest of the week as they are.

// Limits on the alternatives offered.
const (
	// DefaultAlternatives is how many candidates an ask returns.
	DefaultAlternatives = 3
	// MaxAlternatives caps the request.
	MaxAlternatives = 8
	// MaxSeenRecipes bounds the "show me different ones" list a client sends
	// back, so the query can't grow without end.
	MaxSeenRecipes = 40
)

// Message codes for alternatives.
const (
	// MessageNoSimilar: nothing in the library resembles the meal, so the
	// best remaining fits are offered instead.
	MessageNoSimilar = "no_similar"
	// MessageNoAlternatives: the household has nothing else to offer for
	// this day.
	MessageNoAlternatives = "no_alternatives"
	// MessageSeenAll: every similar meal has been shown already.
	MessageSeenAll = "seen_all"
)

// ErrMealCooked means the meal has already been cooked, so replacing it would
// rewrite what the household did.
var ErrMealCooked = errors.New("recommendations: the meal is already cooked")

// Alternative is one replacement offered for a planned meal.
type Alternative struct {
	RecipeID       string
	RecipeName     string
	RecipeImageURL string
	CookMinutes    int
	TimeBand       string
	// Servings is what the meal would be cooked in: the entry's servings when
	// the recipe is authored in that size, and otherwise the nearest size.
	Servings int
	// Similarity is 0..1 (similar.go); Score is the provider's fit for the day.
	Similarity float64
	Score      float64
	// Reasons say why this one resembles the meal ("Also Thai", "30 min"),
	// then why it fits the day at all.
	Reasons []Reason
	// order is how the candidates are sorted: similarity mixed with the
	// provider's fit for the day.
	order float64
}

// AlternativesResult is what a planned meal could be swapped for.
type AlternativesResult struct {
	EntryID string
	Day     string
	Week    string
	// Recipe is the meal being replaced.
	RecipeID     string
	RecipeName   string
	Servings     int
	Alternatives []Alternative
	Messages     []Message
	ModelVersion string
}

// AlternativeOptions narrows an ask for alternatives.
type AlternativeOptions struct {
	// Limit is how many to return; 0 means DefaultAlternatives.
	Limit int
	// Seen are recipes the member has already been offered for this meal and
	// wants replaced by others ("show me something different").
	Seen []string
}

// Alternatives offers replacements for one planned meal that resemble it.
//
// The candidates come from the same provider call an Autopilot proposal swap
// makes: the meal's own day is ranked with every other meal in the week fixed,
// so the household's restrictions, never-include rules, the day's time cap,
// variety, and the cook-time mix all apply. This only reorders that ranking
// towards meals like the one being replaced (similarity(), weighted
// similarityShare against the provider's own fit) and explains each pick.
//
// A finalized week fails with ErrPlanFinalized and a cooked meal with
// ErrMealCooked: the grocery list is out, or the meal already happened.
func (s *Service) Alternatives(ctx context.Context, householdID, week, entryID string, opts AlternativeOptions) (AlternativesResult, error) {
	if strings.TrimSpace(householdID) == "" {
		return AlternativesResult{}, invalidf("household id is required")
	}
	limit := opts.Limit
	switch {
	case limit <= 0:
		limit = DefaultAlternatives
	case limit > MaxAlternatives:
		return AlternativesResult{}, invalidf("limit must be between 1 and %d", MaxAlternatives)
	}
	if len(opts.Seen) > MaxSeenRecipes {
		return AlternativesResult{}, invalidf("seen must have at most %d recipes", MaxSeenRecipes)
	}
	w, plan, entry, err := s.swappableEntry(ctx, householdID, week, entryID)
	if err != nil {
		return AlternativesResult{}, err
	}
	in, data, err := s.entryRankInput(ctx, householdID, w, plan, entry)
	if err != nil {
		return AlternativesResult{}, err
	}
	exclude, err := s.excludedForEntry(ctx, householdID, w, plan, entry, opts.Seen)
	if err != nil {
		return AlternativesResult{}, err
	}
	res, err := s.provider.RankMeals(ctx, autopilot.RankRequest{
		Input: in, Day: autopilot.Day(entry.Day), Exclude: exclude, Limit: autopilot.MaxRankLimit,
	})
	if err != nil {
		return AlternativesResult{}, fmt.Errorf("rank meals: %w", err)
	}
	out := AlternativesResult{
		EntryID: entry.ID, Day: string(entry.Day), Week: w.String(), RecipeID: entry.RecipeID,
		RecipeName: entry.RecipeName, Servings: entry.Servings, ModelVersion: res.ModelVersion,
	}
	bands, err := s.TimeBands(ctx, householdID)
	if err != nil {
		return AlternativesResult{}, err
	}
	overrides, err := s.overridesByRecipe(ctx, householdID)
	if err != nil {
		return AlternativesResult{}, err
	}
	target := similarAttributes(data.byID[entry.RecipeID], overrides[entry.RecipeID], bands)
	if _, ok := data.byID[entry.RecipeID]; !ok {
		// The meal was removed from the library since it was planned; without
		// it there is nothing to be similar to, so the ranking stands as is.
		target = RecipeAttributes{}
	}
	ranked := make([]Alternative, 0, len(res.Items))
	for i, item := range res.Items {
		r, ok := data.byID[item.ItemID]
		if !ok {
			continue
		}
		sim := similarity(target, similarAttributes(r, overrides[item.ItemID], bands))
		alt := Alternative{
			RecipeID: r.ID, RecipeName: r.Name, RecipeImageURL: r.ImageURL, CookMinutes: r.CookMinutes(),
			TimeBand: string(item.TimeBand), Servings: nearestServings(r, entry.Servings),
			Similarity: sim.Score, Score: item.Score, Reasons: sim.Reasons,
		}
		// The provider's own reason for the day comes after the similarity
		// ones, so a card reads "Also Thai · 30 min · You rated this 5★".
		for _, reason := range item.Reasons {
			if len(alt.Reasons) > maxSimilarReasons {
				break
			}
			alt.Reasons = append(alt.Reasons, Reason{Code: reason.Code, Text: reason.Text})
		}
		// order mixes similarity with the provider's fit, which is its
		// position in this ranking (rankFit) rather than its unbounded score.
		alt.order = similarityShare*sim.Score + (1-similarityShare)*rankFit(i, len(res.Items))
		ranked = append(ranked, alt)
	}
	slices.SortStableFunc(ranked, func(a, b Alternative) int {
		switch {
		case a.order > b.order:
			return -1
		case a.order < b.order:
			return 1
		}
		return strings.Compare(a.RecipeID, b.RecipeID)
	})
	similarOnes := make([]Alternative, 0, limit)
	rest := make([]Alternative, 0, limit)
	for _, alt := range ranked {
		switch {
		case alt.Similarity >= MinSimilarity && len(similarOnes) < limit:
			similarOnes = append(similarOnes, alt)
		case len(rest) < limit:
			rest = append(rest, alt)
		}
	}
	switch {
	case len(similarOnes) > 0:
		out.Alternatives = similarOnes
	case len(rest) > 0:
		out.Alternatives = rest
		out.Messages = append(out.Messages, Message{Code: MessageNoSimilar, Text: "Nothing in your library is much like this one, so these are the best fits for the day."})
	case len(opts.Seen) > 0:
		out.Messages = append(out.Messages, Message{Code: MessageSeenAll, Text: "That's everything that fits this day. Try Add a Meal to choose one yourself."})
	default:
		text := "No other recipe in your library fits this day."
		if len(res.Messages) > 0 {
			text = res.Messages[0].Text
		}
		out.Messages = append(out.Messages, Message{Code: MessageNoAlternatives, Text: text})
	}
	return out, nil
}

// SwapResult is a planned meal replaced by another.
type SwapResult struct {
	Plan planning.Plan
	// Previous is the entry as it was; Entry is the same entry with its new
	// recipe.
	Previous planning.Entry
	Entry    planning.Entry
}

// SwapEntry replaces one planned meal with recipeID, keeping the entry, its
// day, and its servings (the nearest authored size when the new recipe isn't
// made in the entry's). The week's grocery list is derived from the plan, so
// it follows on the next read.
//
// It records meal.swapped, with the replaced meal as previousRecipeId, and
// recipe.planned for the new one with origin autopilot — the same pair an
// Autopilot swap and accept record, so learning reads "not this one" about
// the meal that was dropped and "kept" about the one chosen.
func (s *Service) SwapEntry(ctx context.Context, householdID, userID, week, entryID, recipeID string) (SwapResult, error) {
	if err := required(householdID, userID); err != nil {
		return SwapResult{}, err
	}
	if strings.TrimSpace(recipeID) == "" {
		return SwapResult{}, invalidf("recipeId is required")
	}
	w, plan, entry, err := s.swappableEntry(ctx, householdID, week, entryID)
	if err != nil {
		return SwapResult{}, err
	}
	if entry.RecipeID == recipeID {
		return SwapResult{}, invalidf("that meal is already planned for this day")
	}
	for _, e := range plan.Entries {
		if e.ID != entry.ID && e.RecipeID == recipeID {
			return SwapResult{}, invalidf("that meal is already planned this week")
		}
	}
	p, previous, next, err := s.plans.ReplaceEntryRecipe(ctx, householdID, w.String(), entryID, recipeID, planning.OriginAutopilot)
	switch {
	case errors.Is(err, planning.ErrFinalized):
		return SwapResult{}, ErrPlanFinalized
	case errors.Is(err, planning.ErrNotFound), errors.Is(err, planning.ErrRecipeNotFound):
		return SwapResult{}, fmt.Errorf("%w: %s", ErrNotFound, "that recipe isn't in this household")
	case errors.Is(err, planning.ErrInvalidEntry):
		return SwapResult{}, fmt.Errorf("%w: %s", ErrInvalid, err.Error())
	case err != nil:
		return SwapResult{}, err
	}
	now := s.now().UTC()
	s.record(ctx, events.Event{
		HouseholdID: householdID, UserID: userID, Type: events.TypeMealSwapped, RecipeID: next.RecipeID,
		Week: w.String(), OccurredAt: now,
		Payload: events.MealSwapped{
			EntryID: next.ID, SlotID: string(next.Day), Day: string(next.Day), Date: p.DateOf(next.Day),
			PreviousRecipeID: previous.RecipeID, ModelVersion: s.weekModelVersion(ctx, householdID, w), SwapNumber: 1,
		},
	})
	s.record(ctx, events.Event{
		HouseholdID: householdID, UserID: userID, Type: events.TypeRecipePlanned, RecipeID: next.RecipeID,
		Week: w.String(), OccurredAt: now,
		Payload: events.RecipePlanned{
			EntryID: next.ID, Day: string(next.Day), Date: p.DateOf(next.Day), Servings: next.Servings,
			Origin: string(planning.OriginAutopilot),
		},
	})
	return SwapResult{Plan: p, Previous: previous, Entry: next}, nil
}

// swappableEntry loads the week's plan and the entry, refusing a finalized
// week, an add-on, and a meal the household has already cooked.
func (s *Service) swappableEntry(ctx context.Context, householdID, week, entryID string) (planning.Week, planning.Plan, planning.Entry, error) {
	if strings.TrimSpace(entryID) == "" {
		return planning.Week{}, planning.Plan{}, planning.Entry{}, invalidf("entryId is required")
	}
	w, plan, err := s.loadWeek(ctx, householdID, week)
	if err != nil {
		return planning.Week{}, planning.Plan{}, planning.Entry{}, err
	}
	i := slices.IndexFunc(plan.Entries, func(e planning.Entry) bool { return e.ID == entryID })
	if i < 0 {
		return planning.Week{}, planning.Plan{}, planning.Entry{}, fmt.Errorf("%w: the week has no meal %q", ErrNotFound, entryID)
	}
	entry := plan.Entries[i]
	if entry.Day == "" {
		return planning.Week{}, planning.Plan{}, planning.Entry{}, invalidf("give the meal a day before swapping it, so alternatives can fit that night")
	}
	if entry.RecipeIsAddon {
		return planning.Week{}, planning.Plan{}, planning.Entry{}, invalidf("add-ons are swapped with their meal, not on their own")
	}
	cooked, err := s.cookedEntries(ctx, householdID, w)
	if err != nil {
		return planning.Week{}, planning.Plan{}, planning.Entry{}, err
	}
	if cooked[entry.ID] || cooked[entry.RecipeID] {
		return planning.Week{}, planning.Plan{}, planning.Entry{}, ErrMealCooked
	}
	return w, plan, entry, nil
}

// entryRankInput builds provider input for ranking the entry's day, with the
// entry itself left out so its day is open and its own attributes no longer
// count against variety.
func (s *Service) entryRankInput(ctx context.Context, householdID string, w planning.Week, plan planning.Plan, entry planning.Entry) (autopilot.Input, inputData, error) {
	without := plan
	without.Entries = slices.DeleteFunc(slices.Clone(plan.Entries), func(e planning.Entry) bool { return e.ID == entry.ID })
	device := DeviceSignals{}
	if p, err := s.store.GetProposal(ctx, householdID, w.String()); err == nil {
		// The week's proposal kept the device's calendar and weather signals;
		// a swap uses them too, as a proposal swap does.
		device = p.Context.Device
	} else if !errors.Is(err, ErrNotFound) {
		return autopilot.Input{}, inputData{}, err
	}
	in, data, err := s.prepareInput(ctx, householdID, w, without, device)
	if err != nil {
		return autopilot.Input{}, inputData{}, err
	}
	// The meal being replaced is still in the library, so keep it available
	// for the similarity comparison even though it can't be picked again.
	if r, err := s.recipe(ctx, householdID, entry.RecipeID); err == nil {
		data.byID[r.ID] = r
	}
	return in, data, nil
}

// excludedForEntry is everything that must not be offered for this meal: the
// meal itself, everything else planned this week, every meal already swapped
// away this week, and whatever the member has already been shown.
func (s *Service) excludedForEntry(ctx context.Context, householdID string, w planning.Week, plan planning.Plan, entry planning.Entry, seen []string) ([]string, error) {
	exclude := []string{entry.RecipeID}
	add := func(id string) {
		if id != "" && !slices.Contains(exclude, id) {
			exclude = append(exclude, id)
		}
	}
	for _, e := range plan.Entries {
		add(e.RecipeID)
	}
	swappedAway, err := s.swappedAwayThisWeek(ctx, householdID, w)
	if err != nil {
		return nil, err
	}
	for _, id := range swappedAway {
		add(id)
	}
	for _, id := range seen {
		add(id)
	}
	return exclude, nil
}

// swappedAwayThisWeek are the meals the household swapped out of this week,
// from proposals or from the plan. A meal someone has just said no to
// shouldn't come straight back as a suggestion for the same week.
func (s *Service) swappedAwayThisWeek(ctx context.Context, householdID string, w planning.Week) ([]string, error) {
	list, err := s.weekEvents(ctx, householdID, w, events.TypeMealSwapped)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range list {
		if payload, ok := e.Payload.(events.MealSwapped); ok && payload.PreviousRecipeID != "" {
			out = append(out, payload.PreviousRecipeID)
		}
	}
	return out, nil
}

// cookedEntries are the entry IDs (and recipe IDs) the household has already
// cooked this week.
func (s *Service) cookedEntries(ctx context.Context, householdID string, w planning.Week) (map[string]bool, error) {
	list, err := s.weekEvents(ctx, householdID, w, events.TypeRecipeCooked)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(list))
	for _, e := range list {
		if payload, ok := e.Payload.(events.RecipeCooked); ok && payload.EntryID != "" {
			out[payload.EntryID] = true
		}
	}
	return out, nil
}

// weekEvents reads one type of event for a week. Events carry their week, and
// the read starts a week early so an event stamped from a date still lands.
func (s *Service) weekEvents(ctx context.Context, householdID string, w planning.Week, t events.Type) ([]events.Event, error) {
	first, err := s.FirstDay(ctx, householdID)
	if err != nil {
		return nil, err
	}
	list, err := s.events.List(ctx, events.Query{
		HouseholdID: householdID, Types: []events.Type{t},
		Since: w.StartOn(first).AddDate(0, 0, -7), Limit: events.MaxListLimit, Newest: true,
	})
	if err != nil {
		return nil, fmt.Errorf("load %s events: %w", t, err)
	}
	out := list[:0]
	for _, e := range list {
		if e.Week == "" || e.Week == w.String() {
			out = append(out, e)
		}
	}
	return out, nil
}

// overridesByRecipe indexes the household's recipe overrides.
func (s *Service) overridesByRecipe(ctx context.Context, householdID string) (map[string]*RecipeOverride, error) {
	list, err := s.store.ListOverrides(ctx, householdID)
	if err != nil {
		return nil, fmt.Errorf("load recipe overrides: %w", err)
	}
	out := make(map[string]*RecipeOverride, len(list))
	for _, o := range list {
		out[o.RecipeID] = &o
	}
	return out, nil
}

// weekModelVersion is the model version the week's proposal was built with,
// or "" when the week was never generated. It only stamps events, so an
// unknown version is harmless.
func (s *Service) weekModelVersion(ctx context.Context, householdID string, w planning.Week) string {
	p, err := s.store.GetProposal(ctx, householdID, w.String())
	if err != nil {
		return ""
	}
	return p.ModelVersion
}

// nearestServings mirrors planning's: the entry's servings when the recipe is
// authored in that size, and otherwise the closest size it has.
func nearestServings(r recipes.Recipe, want int) int {
	best := 0
	for _, size := range r.Servings {
		switch {
		case size == want:
			return want
		case best == 0, absInt(size-want) < absInt(best-want), absInt(size-want) == absInt(best-want) && size < best:
			best = size
		}
	}
	return best
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
