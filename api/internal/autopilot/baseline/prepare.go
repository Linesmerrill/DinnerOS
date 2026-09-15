package baseline

import (
	"cmp"
	"fmt"
	"hash/fnv"
	"math"
	"slices"
	"strings"
	"unicode"

	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot"
)

// model is one request, normalized and indexed.
type model struct {
	w       Weights
	version string
	beam    int
	perSlot int
	week    string
	seed    uint64

	prefs prefs
	ctx   weekCtx

	items    []*item // sorted by ID
	byID     map[string]*item
	eligible []*item // items passing the hard constraints, sorted by ID
	rejected map[string]int

	confidence float64
	coldStart  bool
	// dayCounts is how often the household planned meals on each weekday.
	dayCounts [7]int

	fixed   []fixedMeal
	fixedID map[string]bool
	avoid   map[string]bool
	boosts  map[string]float64
}

type prefs struct {
	planDays     []int
	weeknights   [7]bool
	mealsPerWeek int
	servings     int
	weeknightMax int
	likes        attrs
	dislikes     attrs
	excl         exclusions
	diets        []string
	allergens    []string
	novelty      autopilot.Novelty
	// rules holds the first rule of each day; allRules every rule.
	rules                [7]*rule
	allRules             []*rule
	bands                autopilot.TimeBands
	maxLong              int
	minQuick             int
	avoidConsecutiveLong bool
}

type attrs struct {
	cuisines, tags, proteins []string
}

type exclusions struct {
	ingredients [][]string // tokenized phrases
	cuisines    []string
	proteins    []string
	tags        []string
	spicy       bool
}

type rule struct {
	day                               int
	label                             string
	cuisines, tags, proteins, methods []string
	band                              autopilot.TimeBand
	freq                              autopilot.RuleFrequency
}

type weekCtx struct {
	skip       bool
	busy       bool
	meals      int
	maxMinutes int
	servings   int
	days       [7]dayCtx
	pantryLow  [][]string // tokenized names
}

type dayCtx struct {
	skip       bool
	maxMinutes int
	servings   int
}

type item struct {
	id                                          string
	cuisines, tags, proteins, methods, allergen []string
	diets                                       []string
	ingredients                                 [][]string // tokenized names
	minutes                                     int
	band                                        autopilot.TimeBand
	servings                                    []int
	spicy                                       bool
	tie                                         uint64
	isNew                                       bool
	st                                          stats
}

type stats struct {
	ratingSum, ratingCount int
	makeAgain, neverAgain  int
	kidFavorite            int
	kidsDisliked           int
	tooSpicy, tooBland     int
	tooMuchWork            int
	greatLeftovers         int
	ordered, planned       int
	cooked, skipped        int
	// nearest is the fewest weeks between the planned week and a week the
	// meal was ordered, planned, or cooked; -1 when never.
	nearest  int
	days     [7]int
	dayTotal int
}

type fixedMeal struct {
	it  *item // nil when the item isn't in the catalog
	day int   // -1 when unscheduled
}

// confidenceSignals is how many ratings and interactions make history fully
// trusted; below coldStartConfidence the household is in cold start.
const (
	confidenceSignals   = 40.0
	coldStartConfidence = 0.3
)

func (p *Provider) prepare(in autopilot.Input, attempt int) (*model, error) {
	if _, err := autopilot.WeekStart(in.Week); err != nil {
		return nil, err
	}
	m := &model{
		w: p.weights, version: p.version, beam: p.beam, perSlot: p.perSlot, week: in.Week,
		seed:     hashString(fmt.Sprintf("%s|%s|%d|%s", in.HouseholdID, in.Week, attempt, p.version)),
		byID:     map[string]*item{},
		rejected: map[string]int{},
		fixedID:  map[string]bool{},
		avoid:    map[string]bool{},
		boosts:   map[string]float64{},
	}
	var err error
	if m.prefs, err = normalizePrefs(in.Preferences); err != nil {
		return nil, err
	}
	if m.ctx, err = normalizeContext(in.Context); err != nil {
		return nil, err
	}

	catalog := slices.Clone(in.Catalog)
	slices.SortStableFunc(catalog, func(a, b autopilot.Item) int { return cmp.Compare(a.ID, b.ID) })
	for _, ci := range catalog {
		if ci.ID == "" {
			return nil, fmt.Errorf("%w: every catalog item needs an id", autopilot.ErrInvalidRequest)
		}
		if m.byID[ci.ID] != nil {
			continue
		}
		it := &item{
			id: ci.ID, cuisines: normAll(ci.Cuisines), tags: normAll(ci.Tags), proteins: normAll(ci.Proteins),
			methods: normAll(ci.Methods), allergen: normAll(ci.Allergens), diets: normAll(ci.Diets),
			minutes: max(ci.CookMinutes, 0), servings: sortedPositive(ci.Servings),
			tie: hashString(fmt.Sprintf("%d|%s", m.seed, ci.ID)), st: stats{nearest: -1},
		}
		it.spicy = ci.Spicy || slices.Contains(it.tags, "spicy")
		it.band = m.prefs.bands.Of(it.minutes)
		for _, name := range ci.Ingredients {
			if tokens := tokenize(name); len(tokens) > 0 {
				it.ingredients = append(it.ingredients, tokens)
			}
		}
		m.items = append(m.items, it)
		m.byID[it.id] = it
	}

	signals := 0
	for _, r := range in.Ratings {
		it := m.byID[r.ItemID]
		if it == nil || r.Score < 1 || r.Score > 5 {
			continue
		}
		signals++
		it.st.ratingSum += r.Score
		it.st.ratingCount++
		for _, tag := range r.Tags {
			switch norm(tag) {
			case autopilot.FeedbackMakeAgain:
				it.st.makeAgain++
			case autopilot.FeedbackNeverAgain:
				it.st.neverAgain++
			case autopilot.FeedbackKidFavorite:
				it.st.kidFavorite++
			case autopilot.FeedbackKidsDisliked:
				it.st.kidsDisliked++
			case autopilot.FeedbackTooSpicy:
				it.st.tooSpicy++
			case autopilot.FeedbackTooBland:
				it.st.tooBland++
			case autopilot.FeedbackTooMuchWork:
				it.st.tooMuchWork++
			case autopilot.FeedbackGreatLeftovers:
				it.st.greatLeftovers++
			}
		}
	}

	distances := map[string]int{}
	for _, h := range in.History {
		d, ok := distances[h.Week]
		if !ok {
			between, valid := autopilot.WeeksBetween(in.Week, h.Week)
			d = -1
			if valid {
				d = abs(between)
			}
			distances[h.Week] = d
		}
		if d < 0 {
			continue
		}
		day := h.Day.Index()
		if h.Kind == autopilot.KindPlanned && day >= 0 {
			m.dayCounts[day]++
		}
		it := m.byID[h.ItemID]
		if it == nil {
			continue
		}
		switch h.Kind {
		case autopilot.KindOrdered:
			it.st.ordered++
		case autopilot.KindPlanned:
			it.st.planned++
			if day >= 0 {
				it.st.days[day]++
				it.st.dayTotal++
			}
		case autopilot.KindCooked:
			it.st.cooked++
		case autopilot.KindSkipped:
			it.st.skipped++
		default:
			continue
		}
		signals++
		if h.Kind != autopilot.KindSkipped && (it.st.nearest < 0 || d < it.st.nearest) {
			it.st.nearest = d
		}
	}
	m.confidence = math.Min(1, float64(signals)/confidenceSignals)
	m.coldStart = m.confidence < coldStartConfidence
	for _, it := range m.items {
		st := it.st
		it.isNew = st.ratingCount == 0 && st.ordered+st.planned+st.cooked+st.skipped == 0
	}

	for _, f := range in.Fixed {
		day := -1
		if f.Day != "" {
			if day = f.Day.Index(); day < 0 {
				return nil, fmt.Errorf("%w: fixed meal day %q is not a weekday", autopilot.ErrInvalidRequest, f.Day)
			}
		}
		m.fixed = append(m.fixed, fixedMeal{it: m.byID[f.ItemID], day: day})
		m.fixedID[f.ItemID] = true
	}
	slices.SortStableFunc(m.fixed, func(a, b fixedMeal) int {
		if c := cmp.Compare(a.day, b.day); c != 0 {
			return c
		}
		return cmp.Compare(itemID(a.it), itemID(b.it))
	})
	for _, id := range in.Avoid {
		m.avoid[id] = true
	}
	for _, o := range in.Objectives {
		m.boosts[o.ItemID] = math.Max(-autopilot.MaxObjectiveBoost, math.Min(autopilot.MaxObjectiveBoost, o.Boost))
	}

	for _, it := range m.items {
		if reason := m.reject(it); reason != "" {
			m.rejected[reason]++
			continue
		}
		m.eligible = append(m.eligible, it)
	}
	return m, nil
}

// reject returns why a hard constraint removes the item, or "".
func (m *model) reject(it *item) string {
	x := m.prefs.excl
	switch {
	case len(it.servings) == 0:
		return "noServings"
	case it.st.neverAgain > 0:
		return "neverAgain"
	case intersect(it.allergen, m.prefs.allergens) != "":
		return "allergen"
	case !containsAll(it.diets, m.prefs.diets):
		return "diet"
	case intersect(it.cuisines, x.cuisines) != "", intersect(it.proteins, x.proteins) != "",
		intersect(it.tags, x.tags) != "", x.spicy && it.spicy:
		return "exclusion"
	}
	for _, phrase := range x.ingredients {
		for _, name := range it.ingredients {
			if containsPhrase(name, phrase) {
				return "exclusion"
			}
		}
	}
	return ""
}

func normalizePrefs(in autopilot.Preferences) (prefs, error) {
	p := prefs{
		mealsPerWeek: in.MealsPerWeek, servings: in.DefaultServings, weeknightMax: max(in.WeeknightMaxMinutes, 0),
		likes:    attrs{normAll(in.Likes.Cuisines), normAll(in.Likes.Tags), normAll(in.Likes.Proteins)},
		dislikes: attrs{normAll(in.Dislikes.Cuisines), normAll(in.Dislikes.Tags), normAll(in.Dislikes.Proteins)},
		excl: exclusions{
			cuisines: normAll(in.Exclusions.Cuisines), proteins: normAll(in.Exclusions.Proteins),
			tags: normAll(in.Exclusions.Tags), spicy: in.Exclusions.Spicy,
		},
		diets: normAll(in.Diets), allergens: normAll(in.Allergens), novelty: in.Novelty,
		bands:                in.CookTime.Bands,
		maxLong:              in.CookTime.MaxLongPerWeek,
		minQuick:             min(max(in.CookTime.MinQuickPerWeek, 0), 7),
		avoidConsecutiveLong: in.CookTime.AvoidConsecutiveLong,
	}
	for _, phrase := range in.Exclusions.Ingredients {
		if tokens := tokenize(phrase); len(tokens) > 0 {
			p.excl.ingredients = append(p.excl.ingredients, tokens)
		}
	}
	days, err := dayIndexes(in.PlanDays, "planDays")
	if err != nil {
		return prefs{}, err
	}
	if len(days) == 0 {
		days = []int{0, 1, 2, 3, 4, 5, 6}
	}
	p.planDays = days
	nights, err := dayIndexes(in.Weeknights, "weeknights")
	if err != nil {
		return prefs{}, err
	}
	if in.Weeknights == nil {
		nights = []int{0, 1, 2, 3}
	}
	for _, d := range nights {
		p.weeknights[d] = true
	}
	if p.mealsPerWeek <= 0 {
		p.mealsPerWeek = min(4, len(p.planDays))
	}
	p.mealsPerWeek = min(p.mealsPerWeek, 7)
	if p.servings <= 0 {
		p.servings = 2
	}
	switch p.novelty {
	case "":
		p.novelty = autopilot.NoveltyBalanced
	case autopilot.NoveltyFavorites, autopilot.NoveltyBalanced, autopilot.NoveltyAdventurous:
	default:
		return prefs{}, fmt.Errorf("%w: novelty %q is unknown", autopilot.ErrInvalidRequest, p.novelty)
	}
	if p.bands.QuickMaxMinutes <= 0 {
		p.bands.QuickMaxMinutes = autopilot.DefaultQuickMaxMinutes
	}
	if p.bands.MediumMaxMinutes <= p.bands.QuickMaxMinutes {
		p.bands.MediumMaxMinutes = max(autopilot.DefaultMediumMaxMinutes, p.bands.QuickMaxMinutes+1)
	}
	if p.maxLong < 0 {
		p.maxLong = 7
	}

	for _, r := range in.Rules {
		day := r.Day.Index()
		if day < 0 {
			return prefs{}, fmt.Errorf("%w: rule day %q is not a weekday", autopilot.ErrInvalidRequest, r.Day)
		}
		nr := &rule{
			day: day, label: strings.TrimSpace(r.Label), cuisines: normAll(r.Cuisines), tags: normAll(r.Tags),
			proteins: normAll(r.Proteins), methods: normAll(r.Methods), band: r.TimeBand, freq: r.Frequency,
		}
		switch nr.band {
		case "", autopilot.BandQuick, autopilot.BandMedium, autopilot.BandLong:
		default:
			return prefs{}, fmt.Errorf("%w: rule time band %q is unknown", autopilot.ErrInvalidRequest, nr.band)
		}
		switch nr.freq {
		case "":
			nr.freq = autopilot.EveryWeek
		case autopilot.EveryWeek, autopilot.AtMostOnce:
		default:
			return prefs{}, fmt.Errorf("%w: rule frequency %q is unknown", autopilot.ErrInvalidRequest, nr.freq)
		}
		if nr.label == "" {
			nr.label = r.Day.Name()
		}
		p.allRules = append(p.allRules, nr)
	}
	slices.SortStableFunc(p.allRules, func(a, b *rule) int {
		if c := cmp.Compare(a.day, b.day); c != 0 {
			return c
		}
		return cmp.Compare(a.label, b.label)
	})
	for _, r := range p.allRules {
		if p.rules[r.day] == nil {
			p.rules[r.day] = r
		}
	}
	return p, nil
}

func normalizeContext(in autopilot.WeekContext) (weekCtx, error) {
	c := weekCtx{
		skip: in.Skip, busy: in.Busy, meals: min(max(in.Meals, 0), 7),
		maxMinutes: max(in.MaxMinutes, 0), servings: max(in.Servings, 0),
	}
	for _, d := range in.Days {
		i := d.Day.Index()
		if i < 0 {
			return weekCtx{}, fmt.Errorf("%w: context day %q is not a weekday", autopilot.ErrInvalidRequest, d.Day)
		}
		c.days[i] = dayCtx{skip: d.Skip, maxMinutes: max(d.MaxMinutes, 0), servings: max(d.Servings, 0)}
	}
	for _, name := range in.PantryLow {
		if tokens := tokenize(name); len(tokens) > 0 {
			c.pantryLow = append(c.pantryLow, tokens)
		}
	}
	return c, nil
}

// --- small helpers ------------------------------------------------------------

func itemID(it *item) string {
	if it == nil {
		return ""
	}
	return it.id
}

func dayIndexes(days []autopilot.Day, field string) ([]int, error) {
	var out []int
	for _, d := range days {
		i := d.Index()
		if i < 0 {
			return nil, fmt.Errorf("%w: %s: %q is not a weekday", autopilot.ErrInvalidRequest, field, d)
		}
		if !slices.Contains(out, i) {
			out = append(out, i)
		}
	}
	slices.Sort(out)
	return out, nil
}

func norm(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// normAll lowercases, trims, dedupes, and sorts values.
func normAll(values []string) []string {
	var out []string
	for _, v := range values {
		if v = norm(v); v != "" && !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	slices.Sort(out)
	return out
}

func sortedPositive(values []int) []int {
	var out []int
	for _, v := range values {
		if v > 0 && !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	slices.Sort(out)
	return out
}

// intersect returns the first value of a (in order) that is also in b.
func intersect(a, b []string) string {
	for _, v := range a {
		if slices.Contains(b, v) {
			return v
		}
	}
	return ""
}

func containsAll(have, want []string) bool {
	for _, v := range want {
		if !slices.Contains(have, v) {
			return false
		}
	}
	return true
}

// tokenize splits a name into lowercase words.
func tokenize(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// containsPhrase reports whether the words of phrase appear consecutively in
// name, allowing a plural "s" or "es" on each word ("mushroom" matches
// "cremini mushrooms").
func containsPhrase(name, phrase []string) bool {
	for start := 0; start+len(phrase) <= len(name); start++ {
		match := true
		for i, word := range phrase {
			if got := name[start+i]; got != word && got != word+"s" && got != word+"es" {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func hashString(s string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return h.Sum64()
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
