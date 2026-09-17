package menu

import (
	"cmp"
	"math"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot"
	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/ratings"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
	"github.com/Linesmerrill/DinnerOS/api/internal/recommendations"
)

// Scoring constants (docs/api.md#menu). Scores are deterministic functions
// of the household's data and the current week.
const (
	// recentOrderWeeks is how long a delivery counts as recent.
	recentOrderWeeks = 52
	// favoriteOrderWeeks is how long frequent orders keep a recipe a favorite.
	favoriteOrderWeeks = 104
	// favoriteMinOrders is how many orders make a recipe a favorite.
	favoriteMinOrders = 3
	// restrictedPenalty lowers recipes that break a hard restriction in the
	// recommended sort, which lists them rather than hiding them.
	restrictedPenalty = 3
)

// snapshotInput is everything a snapshot reads.
type snapshotInput struct {
	UserID   string
	Week     planning.Week
	Current  planning.Week
	Location *time.Location
	// FirstDay is the household's first day of the week; empty means Monday.
	FirstDay planning.Day
	// Catalog is the household's recipes ordered by ID.
	Catalog   []recipes.Recipe
	Ratings   []ratings.Rating
	Overrides []recommendations.RecipeOverride
	Profile   recommendations.Profile
	// Cooked are recipe.cooked events.
	Cooked   []events.Event
	Plan     planning.Plan
	Proposal *recommendations.Proposal
}

// item is a recipe with the signals sections rank by.
type item struct {
	recipe recipes.Recipe
	attrs  recommendations.RecipeAttributes
	// withRegions is attrs.Cuisines followed by attrs.CuisineRegions.
	withRegions []string
	cook        int
	rating      ratings.Summary
	makeAgain   bool
	kidFavorite bool
	neverAgain  bool
	// cooked counts recipe.cooked events; cookedWeeks are their weeks.
	cooked      int
	cookedWeeks map[string]bool
	// weeksSinceOrder is -1 when never ordered.
	weeksSinceOrder int
	favorite        float64
	isFavorite      bool
	fit             float64
	fitReason       string
	restricted      bool
}

func (it *item) main() bool { return !it.recipe.IsAddon }

// idea reports whether the recipe may be suggested: a main meal nobody marked
// never-again that breaks no hard restriction.
func (it *item) idea() bool { return it.main() && !it.neverAgain && !it.restricted }

func (it *item) isNew() bool { return it.recipe.TimesOrdered == 0 && it.cooked == 0 }

// snapshot is one household's catalog, scored for one week.
type snapshot struct {
	in    snapshotInput
	bands autopilot.TimeBands
	quick int
	// medium is the upper bound of the medium band; longer is long.
	medium int
	items  []*item
	byID   map[string]*item
	// planEntries maps a recipe ID to the week's plan entry IDs, in order.
	planEntries map[string][]string
	// autopilotPicks are recipes in the week's pending proposal or added to
	// the plan from a proposal.
	autopilotPicks map[string]bool
	hasSmoker      bool
}

func newSnapshot(in snapshotInput) *snapshot {
	if in.Location == nil {
		in.Location = time.UTC
	}
	s := &snapshot{
		in: in, bands: in.Profile.TimeBands(), byID: make(map[string]*item, len(in.Catalog)),
		planEntries: map[string][]string{}, autopilotPicks: map[string]bool{},
		hasSmoker: slices.Contains(in.Profile.Equipment, "smoker"),
	}
	s.quick, s.medium = bandLimits(s.bands)

	overrides := make(map[string]*recommendations.RecipeOverride, len(in.Overrides))
	for i := range in.Overrides {
		overrides[in.Overrides[i].RecipeID] = &in.Overrides[i]
	}
	for _, r := range in.Catalog {
		it := &item{recipe: r, attrs: recommendations.AttributesOf(r, overrides[r.ID], s.bands), cook: r.CookMinutes(), weeksSinceOrder: -1}
		it.withRegions = slices.Concat(it.attrs.Cuisines, it.attrs.CuisineRegions)
		if w, err := planning.ParseWeek(r.LastOrderedWeek); err == nil {
			it.weeksSinceOrder = max(w.WeeksUntil(in.Current), 0)
		}
		s.items = append(s.items, it)
		s.byID[r.ID] = it
	}
	for _, rt := range in.Ratings {
		it := s.byID[rt.RecipeID]
		if it == nil {
			continue
		}
		it.rating.Count++
		it.rating.Sum += rt.Score
		if rt.UserID == in.UserID {
			mine := rt
			it.rating.Mine = &mine
		}
		it.makeAgain = it.makeAgain || slices.Contains(rt.Tags, ratings.TagMakeAgain)
		it.kidFavorite = it.kidFavorite || slices.Contains(rt.Tags, ratings.TagKidFavorite)
		it.neverAgain = it.neverAgain || slices.Contains(rt.Tags, ratings.TagNeverAgain)
	}
	for _, e := range in.Cooked {
		if it := s.byID[e.RecipeID]; it != nil {
			it.cooked++
			if it.cookedWeeks == nil {
				it.cookedWeeks = map[string]bool{}
			}
			it.cookedWeeks[eventWeek(e, in.Location, in.FirstDay)] = true
		}
	}
	for _, e := range in.Plan.Entries {
		s.planEntries[e.RecipeID] = append(s.planEntries[e.RecipeID], e.ID)
		if e.Origin == planning.OriginAutopilot {
			s.autopilotPicks[e.RecipeID] = true
		}
	}
	if p := in.Proposal; p != nil && p.Status == recommendations.StatusProposed {
		for _, slot := range p.Slots {
			s.autopilotPicks[slot.RecipeID] = true
		}
	}

	aff := s.orderAffinity()
	for _, it := range s.items {
		s.score(it, aff)
	}
	return s
}

// bandLimits resolves the quick and medium limits as autopilot.TimeBands.Of
// does.
func bandLimits(b autopilot.TimeBands) (quick, medium int) {
	quick, medium = b.QuickMaxMinutes, b.MediumMaxMinutes
	if quick <= 0 {
		quick = autopilot.DefaultQuickMaxMinutes
	}
	if medium <= quick {
		medium = max(autopilot.DefaultMediumMaxMinutes, quick)
	}
	return quick, medium
}

// eventWeek is the week a cooked event belongs to: its week, else the week
// of its payload date, else of when it occurred in the household's time zone,
// with weeks starting on first.
func eventWeek(e events.Event, loc *time.Location, first planning.Day) string {
	if e.Week != "" {
		return e.Week
	}
	if p, ok := e.Payload.(events.RecipeCooked); ok && p.Date != "" {
		if d, err := time.Parse(time.DateOnly, p.Date); err == nil {
			return planning.WeekOfOn(d, first).String()
		}
	}
	return planning.WeekOfOn(e.OccurredAt.In(loc), first).String()
}

// affinity is the share of the household's main-meal orders that had each
// protein or cuisine: an implicit taste profile from order history.
type affinity struct {
	proteins map[string]float64
	cuisines map[string]float64
}

func (s *snapshot) orderAffinity() affinity {
	a := affinity{proteins: map[string]float64{}, cuisines: map[string]float64{}}
	total := 0.0
	for _, it := range s.items {
		n := float64(it.recipe.TimesOrdered)
		if !it.main() || n == 0 {
			continue
		}
		total += n
		for _, p := range it.attrs.Proteins {
			a.proteins[p] += n
		}
		for _, c := range it.attrs.Cuisines {
			a.cuisines[c] += n
		}
	}
	if total > 0 {
		for k := range a.proteins {
			a.proteins[k] /= total
		}
		for k := range a.cuisines {
			a.cuisines[k] /= total
		}
	}
	return a
}

// score sets an item's favorite score, favorite status, taste fit, and
// restriction.
//
// favorite = (average rating − 3) + 1.5 make-again + 0.75 kid-favorite
// + 0.8·log2(1 + times ordered) + recency (1 → 0 over 52 weeks since the last
// order) + 0.5·log2(1 + times cooked), − 5 when anyone marked it never-again.
func (s *snapshot) score(it *item, aff affinity) {
	avg, rated := it.rating.Average()
	f := 0.0
	if rated {
		f += avg - 3
	}
	if it.makeAgain {
		f += 1.5
	}
	if it.kidFavorite {
		f += 0.75
	}
	f += 0.8 * math.Log2(1+float64(it.recipe.TimesOrdered))
	if d := it.weeksSinceOrder; d >= 0 && d < recentOrderWeeks {
		f += 1 - float64(d)/recentOrderWeeks
	}
	f += 0.5 * math.Log2(1+float64(it.cooked))
	if it.neverAgain {
		f -= 5
	}
	it.favorite = round6(f)

	frequent := it.recipe.TimesOrdered >= favoriteMinOrders && it.weeksSinceOrder >= 0 && it.weeksSinceOrder <= favoriteOrderWeeks
	it.isFavorite = !it.neverAgain && (!rated || avg >= 3) &&
		((rated && avg >= 4) || it.makeAgain || it.kidFavorite || frequent || it.cooked >= 2)
	it.fit, it.fitReason = s.tasteFit(it, aff)
	it.restricted = s.restricted(it)
}

// tasteFit scores a recipe against the taste profile like Autopilot's taste
// signal (liked cuisine or region +0.4, tag +0.4, protein +0.3, each dislike
// −0.5), plus 0.3 × the order-history share of its best protein and of its
// best cuisine.
func (s *snapshot) tasteFit(it *item, aff affinity) (float64, string) {
	fit, reason := 0.0, ""
	if s.in.Profile.Configured() {
		t := s.in.Profile.Taste
		for _, g := range []struct {
			have, want []string
			value      float64
			label      func(string) string
		}{
			{it.withRegions, t.Likes.Cuisines, 0.4, cuisineName},
			{it.attrs.Tags, t.Likes.Tags, 0.4, titleWords},
			{it.attrs.Proteins, t.Likes.Proteins, 0.3, proteinName},
		} {
			if v := intersect(g.have, g.want); v != "" {
				fit += g.value
				if reason == "" {
					reason = "You like " + g.label(v)
				}
			}
		}
		for _, g := range [][2][]string{
			{it.withRegions, t.Dislikes.Cuisines}, {it.attrs.Tags, t.Dislikes.Tags}, {it.attrs.Proteins, t.Dislikes.Proteins},
		} {
			if intersect(g[0], g[1]) != "" {
				fit -= 0.5
			}
		}
	}
	best := 0.0
	for _, p := range it.attrs.Proteins {
		best = max(best, aff.proteins[p])
	}
	fit += 0.3 * best
	best = 0
	for _, c := range it.attrs.Cuisines {
		best = max(best, aff.cuisines[c])
	}
	fit += 0.3 * best
	return round6(fit), reason
}

// restricted reports whether a recipe breaks one of the profile's hard
// restrictions, read the way Autopilot reads them: every diet must be
// satisfied, and no allergen, excluded cuisine (or region), protein, tag,
// ingredient phrase, or spicy recipe when "no spicy" is set.
func (s *snapshot) restricted(it *item) bool {
	if !s.in.Profile.Configured() {
		return false
	}
	r, a := s.in.Profile.Restrictions, it.attrs
	for _, d := range r.Diets {
		if !slices.Contains(a.Diets, d) {
			return true
		}
	}
	if intersect(a.Allergens, r.Allergens) != "" || intersect(it.withRegions, r.ExcludedCuisines) != "" ||
		intersect(a.Proteins, r.ExcludedProteins) != "" || intersect(a.Tags, r.ExcludedTags) != "" || (r.NoSpicy && a.Spicy) {
		return true
	}
	for _, phrase := range r.ExcludedIngredients {
		want := words(phrase)
		for _, line := range it.recipe.Ingredients {
			if containsWords(words(line.Name), want) {
				return true
			}
		}
	}
	return false
}

// --- ordering helpers ---------------------------------------------------------

// sectionLimit caps a section's items.
const sectionLimit = 12

// top returns up to sectionLimit items that keep accepts, ordered by order
// and then by name and ID.
func top(items []*item, keep func(*item) bool, order func(a, b *item) int) []*item {
	var out []*item
	for _, it := range items {
		if keep(it) {
			out = append(out, it)
		}
	}
	slices.SortFunc(out, func(a, b *item) int {
		if c := order(a, b); c != 0 {
			return c
		}
		return byName(a, b)
	})
	return out[:min(len(out), sectionLimit)]
}

func byName(a, b *item) int {
	if c := cmp.Compare(strings.ToLower(a.recipe.Name), strings.ToLower(b.recipe.Name)); c != 0 {
		return c
	}
	return cmp.Compare(a.recipe.ID, b.recipe.ID)
}

// higher orders larger values first.
func higher[T cmp.Ordered](a, b T) int { return cmp.Compare(b, a) }

func round6(v float64) float64 { return math.Round(v*1e6) / 1e6 }

// --- text helpers ---------------------------------------------------------------

func intersect(have, want []string) string {
	for _, w := range want {
		if slices.Contains(have, w) {
			return w
		}
	}
	return ""
}

// titleWords capitalizes the first letter of each word ("smoker night" →
// "Smoker Night").
func titleWords(s string) string {
	fields := strings.Fields(s)
	for i, f := range fields {
		r := []rune(f)
		r[0] = unicode.ToUpper(r[0])
		fields[i] = string(r)
	}
	return strings.Join(fields, " ")
}

func optionLabel(options []recommendations.Option, value string) string {
	for _, o := range options {
		if o.Value == value {
			return o.Label
		}
	}
	return titleWords(value)
}

func proteinName(v string) string { return optionLabel(recommendations.ProteinOptions, v) }

func methodName(v string) string { return optionLabel(recommendations.EquipmentOptions, v) }

func dayName(v string) string { return optionLabel(recommendations.DayOptions, v) }

// dayIndex is day's position in the household's week (0 for its first day),
// with unknown and empty days last.
func (s *snapshot) dayIndex(day string) int {
	if i := planning.Day(day).OrderOn(s.in.FirstDay); i >= 0 {
		return i
	}
	return len(recommendations.DayOptions)
}

func cuisineName(v string) string {
	if label := recommendations.CuisineLabel(v); label != "" {
		return label
	}
	return titleWords(v)
}

// words splits text into lowercase letter-and-digit words.
func words(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// containsWords reports whether want appears consecutively in name, allowing
// plurals ("thighs", "anchovies").
func containsWords(name, want []string) bool {
	if len(want) == 0 {
		return false
	}
	for start := 0; start+len(want) <= len(name); start++ {
		ok := true
		for i, w := range want {
			got := name[start+i]
			if got != w && got != w+"s" && got != w+"es" && !(strings.HasSuffix(w, "y") && got == strings.TrimSuffix(w, "y")+"ies") {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}
