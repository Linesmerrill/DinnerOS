package catalog

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/Linesmerrill/DinnerOS/api/internal/cuisine"
)

// The top of Shared Recipes (decision 710): rows of recipes, one per active
// collection, each computed here and filled from the same ranked pool, so
// every row honors the household's restrictions and marks what it already
// has. Rows with nothing to show are left out.

// SectionSize is how many recipes a row shows before See All.
const SectionSize = 12

// editorialReserve is how many recipes the rows built on saves and ratings
// leave unclaimed (at least one, at most half of what's left), so Chef’s
// Picks always has some.
const editorialReserve = 3

// trendingWindow is how far back Trending counts saves.
const trendingWindow = 7 * 24 * time.Hour

// Top Rated (decision 711) uses two signals: household ratings, joined from
// recipe_ratings through each household recipe's catalogKey (one mean score
// per household), and saves from catalog_saves. The score is a damped mean
// that starts every recipe at topRatedPrior as if rated topRatedWeight
// times, so one 5 doesn't beat many 4.5s, plus a small lift per saving
// household. A recipe nobody rated or saved is not in the row.
const (
	topRatedPrior   = 3.5
	topRatedWeight  = 2.0
	topRatedPerSave = 0.1
	topRatedMaxSave = 10
	quickMinutes    = 30
	crowdServings   = 6
)

// Section is one row and the recipes in it.
type Section struct {
	Collection
	Items []Result
	// More is true when See All has more than Items.
	More bool
}

// Home is the top of Shared Recipes.
type Home struct {
	Sections []Section
	Filters  []FilterGroup
}

// FilterGroup is one filter chip and its choices.
type FilterGroup struct {
	// ID is cuisine, time, or servings.
	ID    string
	Title string
	// Param is the list query parameter a choice's Value goes in.
	Param   string
	Options []FilterOption
}

// FilterOption is one choice under a filter chip.
type FilterOption struct {
	Value string
	Label string
}

// Home returns the household's rows.
func (s *Service) Home(ctx context.Context, householdID string) (Home, error) {
	pool, err := s.pool(ctx, householdID)
	if err != nil {
		return Home{}, err
	}
	cols, err := s.Collections(ctx)
	if err != nil {
		return Home{}, err
	}
	sig := &signals{service: s}
	now := s.now().UTC()
	out := Home{Sections: []Section{}}
	// No row repeats another (decisions 733, 764). Rows built on what
	// households did (Trending, Top Rated) claim their recipes first;
	// Chef’s Picks and the filter rows take from what’s left, in order. Rows
	// still show in their own order, and See All still lists everything.
	order := make([]int, 0, len(cols))
	for i, c := range cols {
		if c.Active(now) {
			order = append(order, i)
		}
	}
	slices.SortStableFunc(order, func(a, b int) int { return cmp.Compare(claimRank(cols[a]), claimRank(cols[b])) })
	rows := map[int]Section{}
	shownAbove := nearIndex{}
	for _, i := range order {
		c := cols[i]
		items, err := s.fill(ctx, c, pool, sig)
		if err != nil {
			return Home{}, err
		}
		row := freshOnly(items, shownAbove)
		if claimRank(c) < 2 {
			// A row built on what households did leaves some of what's
			// left for Chef’s Picks: on a small catalog where every recipe
			// has a save or a rating, it would otherwise take them all and
			// the headline row would vanish.
			fresh := len(freshOnly(pool, shownAbove))
			left := fresh - max(1, min(editorialReserve, fresh/2))
			row = row[:max(0, min(len(row), left))]
		}
		if len(row) == 0 {
			continue
		}
		row = row[:min(len(row), SectionSize)]
		for _, it := range row {
			shownAbove.add(nearKeyOf(it.Content))
		}
		rows[i] = Section{Collection: s.dressed(c), Items: row, More: len(items) > len(row)}
	}
	for i := range cols {
		if row, ok := rows[i]; ok {
			out.Sections = append(out.Sections, row)
		}
	}
	if out.Filters, err = s.FilterGroups(ctx); err != nil {
		return Home{}, err
	}
	return out, nil
}

// claimRank orders rows for claiming recipes: the ones built from what
// households did come first, narrowest first (this week's saves, then
// ratings), because no other row can show those recipes in their place.
func claimRank(c Collection) int {
	switch {
	case len(c.RecipeIDs) > 0:
		return 2
	case c.Rule == RuleTrending:
		return 0
	case c.Rule == RuleTopRated:
		return 1
	}
	return 2
}

// Section returns one row in full, a page at a time, for See All.
func (s *Service) Section(ctx context.Context, householdID, id string, q Query) (Collection, Page, error) {
	f, err := q.validate()
	if err != nil {
		return Collection{}, Page{}, err
	}
	cols, err := s.Collections(ctx)
	if err != nil {
		return Collection{}, Page{}, err
	}
	i := slices.IndexFunc(cols, func(c Collection) bool { return c.ID == id })
	if i < 0 || !cols[i].Active(s.now().UTC()) {
		return Collection{}, Page{}, ErrNotFound
	}
	pool, err := s.pool(ctx, householdID)
	if err != nil {
		return Collection{}, Page{}, err
	}
	items, err := s.fill(ctx, cols[i], pool, &signals{service: s})
	if err != nil {
		return Collection{}, Page{}, err
	}
	return s.dressed(cols[i]), page(items, f), nil
}

// FilterGroups returns the filter chips: the cuisines Shared Recipes has,
// and fixed time and servings choices.
func (s *Service) FilterGroups(ctx context.Context) ([]FilterGroup, error) {
	stored, err := s.store.Cuisines(ctx)
	if err != nil {
		return nil, fmt.Errorf("list cuisines: %w", err)
	}
	// Only standard cuisines are choices (decision 732): "North America"
	// and "American" are one American chip, and a region like "Southern
	// Europe" is none.
	group := FilterGroup{ID: "cuisine", Title: "Cuisine", Param: "cuisine", Options: []FilterOption{}}
	seen := map[string]bool{}
	for _, v := range stored {
		c := cuisine.Canonical(v)
		if !cuisine.IsStandard(c) || seen[c] {
			continue
		}
		seen[c] = true
		group.Options = append(group.Options, FilterOption{Value: c, Label: cuisine.Label(c)})
	}
	slices.SortFunc(group.Options, func(a, b FilterOption) int { return cmp.Compare(a.Label, b.Label) })
	out := []FilterGroup{}
	if len(group.Options) > 0 {
		out = append(out, group)
	}
	out = append(out,
		FilterGroup{ID: "time", Title: "Time", Param: "maxMinutes", Options: []FilterOption{
			{Value: "30", Label: "30 min or less"}, {Value: "45", Label: "45 min or less"}, {Value: "60", Label: "1 hour or less"},
		}},
		FilterGroup{ID: "servings", Title: "Servings", Param: "minServings", Options: []FilterOption{
			{Value: "2", Label: "2+"}, {Value: "4", Label: "4+"}, {Value: "6", Label: "6+"},
		}},
	)
	return out, nil
}

// pool is every recipe the household may see, ranked for it with its
// restrictions applied, and marked when it already has one.
func (s *Service) pool(ctx context.Context, householdID string) ([]Result, error) {
	entries, err := s.store.Scan(ctx, ScanLimit)
	if err != nil {
		return nil, fmt.Errorf("scan catalog: %w", err)
	}
	entries = withoutHidden(withoutTestRecipes(shown(entries)), s.hidden(ctx, householdID))
	profile, bands := s.tasteOf(ctx, householdID)
	ranked := rank(entries, profile, bands)
	if err := s.mark(ctx, householdID, ranked); err != nil {
		return nil, err
	}
	return ranked, nil
}

// dressed fills in words a row computes: the season's subtitle.
func (s *Service) dressed(c Collection) Collection {
	if c.Rule == RuleSeasonal && c.Subtitle == "" && len(c.RecipeIDs) == 0 {
		c.Subtitle = "Just right for " + seasonOf(s.now())
	}
	return c
}

// signals reads the ratings and saves a request needs, once each.
type signals struct {
	service *Service
	ratings map[string]RatingStat
	saves   map[string]int
	recent  map[string]int
	read    map[string]bool
}

func (sg *signals) load(ctx context.Context, what string) error {
	if sg.read == nil {
		sg.read = map[string]bool{}
	}
	if sg.read[what] {
		return nil
	}
	sg.read[what] = true
	s := sg.service
	var err error
	switch what {
	case "ratings":
		if s.ratings != nil {
			sg.ratings, err = s.ratings.CatalogRatings(ctx)
		}
	case "saves":
		if s.saves != nil {
			sg.saves, err = s.saves.Counts(ctx, time.Time{})
		}
	case "recent":
		if s.saves != nil {
			sg.recent, err = s.saves.Counts(ctx, s.now().UTC().Add(-trendingWindow))
		}
	}
	return err
}

// fill returns a row's recipes from the pool, the household's new ones
// first: a row is a place to find something, so what it already has goes
// last rather than away.
func (s *Service) fill(ctx context.Context, c Collection, pool []Result, sig *signals) ([]Result, error) {
	var items []Result
	var err error
	if len(c.RecipeIDs) > 0 {
		items, err = s.picked(ctx, c.RecipeIDs, pool)
	} else {
		items, err = byRule(ctx, c.Rule, pool, sig, s.now())
	}
	if err != nil {
		return nil, err
	}
	// The pool is ranked, so the best-ranked of near copies stays.
	items = withoutNearCopies(items)
	slices.SortStableFunc(items, func(a, b Result) int {
		return cmp.Compare(boolInt(a.LibraryRecipeID != ""), boolInt(b.LibraryRecipeID != ""))
	})
	return items, nil
}

// picked returns the pool's recipes for ids, in their order. An ID of any
// copy of a recipe picks the recipe.
func (s *Service) picked(ctx context.Context, ids []string, pool []Result) ([]Result, error) {
	byID, byGroup := map[string]int{}, map[string]int{}
	for i, r := range pool {
		byID[r.ID] = i
		byGroup[groupKey(r.Recipe)] = i
	}
	var missing []string
	for _, id := range ids {
		if _, ok := byID[id]; !ok {
			missing = append(missing, id)
		}
	}
	groups := map[string]string{}
	if len(missing) > 0 {
		var err error
		if groups, err = s.store.Groups(ctx, missing); err != nil {
			return nil, fmt.Errorf("read groups: %w", err)
		}
	}
	var out []Result
	used := map[int]bool{}
	for _, id := range ids {
		i, ok := byID[id]
		if !ok {
			i, ok = byGroup[groups[id]]
		}
		if ok && !used[i] {
			used[i] = true
			out = append(out, pool[i])
		}
	}
	return out, nil
}

// byRule computes a row from the ranked pool.
func byRule(ctx context.Context, rule string, pool []Result, sig *signals, now time.Time) ([]Result, error) {
	keep := func(ok func(Result) bool) []Result {
		var out []Result
		for _, r := range pool {
			if ok(r) {
				out = append(out, r)
			}
		}
		return out
	}
	switch rule {
	case RuleBest:
		return slices.Clone(pool), nil
	case RuleQuick:
		return keep(func(r Result) bool { return withinMinutes(r.Content, quickMinutes) }), nil
	case RuleCrowd:
		out := keep(func(r Result) bool { return servesAtLeast(r.Content, crowdServings) })
		// Big pots first: chili, soup, stew.
		slices.SortStableFunc(out, func(a, b Result) int {
			return cmp.Compare(boolInt(!bigPot(a)), boolInt(!bigPot(b)))
		})
		return out, nil
	case RuleSeasonal:
		season := seasonOf(now)
		return keep(func(r Result) bool { return inSeason(r, season) }), nil
	case RuleTrending:
		if err := sig.load(ctx, "recent"); err != nil {
			return nil, err
		}
		out := keep(func(r Result) bool { return sig.recent[groupKey(r.Recipe)] > 0 })
		slices.SortStableFunc(out, func(a, b Result) int {
			return cmp.Compare(sig.recent[groupKey(b.Recipe)], sig.recent[groupKey(a.Recipe)])
		})
		return out, nil
	case RuleTopRated:
		if err := sig.load(ctx, "ratings"); err != nil {
			return nil, err
		}
		if err := sig.load(ctx, "saves"); err != nil {
			return nil, err
		}
		scores := map[string]float64{}
		out := keep(func(r Result) bool {
			score, ok := topRated(r, sig.ratings, sig.saves[groupKey(r.Recipe)])
			scores[r.ID] = score
			return ok
		})
		slices.SortStableFunc(out, func(a, b Result) int { return cmp.Compare(scores[b.ID], scores[a.ID]) })
		return out, nil
	}
	return nil, nil
}

// topRated scores one recipe from every household's rating of any copy of
// it and its saves. ok is false when it has neither.
func topRated(r Result, ratings map[string]RatingStat, saves int) (float64, bool) {
	var sum float64
	var n int
	for _, k := range r.keys() {
		st := ratings[k]
		sum += st.Sum
		n += st.Households
	}
	if n == 0 && saves == 0 {
		return 0, false
	}
	score := (sum + topRatedPrior*topRatedWeight) / (float64(n) + topRatedWeight)
	return score + topRatedPerSave*float64(min(saves, topRatedMaxSave)), true
}

// seasonOf is the season at now in the northern hemisphere, where almost
// every household is; Autopilot's own season (recommendations/context.go)
// knows the household's time zone, which the catalog does not.
func seasonOf(now time.Time) string {
	switch now.Month() {
	case time.December, time.January, time.February:
		return "winter"
	case time.March, time.April, time.May:
		return "spring"
	case time.June, time.July, time.August:
		return "summer"
	}
	return "fall"
}

// seasonWords are the words in a name or tag that make a recipe
// a season's favorite.
var seasonWords = map[string][]string{
	"winter": {"winter", "soup", "stew", "chili", "braise", "braised", "pot roast", "pot pie", "casserole", "chowder",
		"short rib", "short ribs", "ragu", "goulash", "stroganoff", "shepherd's pie", "cottage pie", "lasagna", "comfort food"},
	"fall": {"fall", "autumn", "pumpkin", "butternut", "squash", "sweet potato", "sweet potatoes", "apple", "apples", "sage",
		"maple", "cranberry", "pecan", "brussels sprouts", "harvest", "chili", "stew", "soup"},
	"spring": {"spring", "asparagus", "peas", "snap peas", "lemon", "radish", "artichoke", "strawberry", "strawberries",
		"mint", "leek", "leeks", "fennel", "primavera"},
	"summer": {"summer", "grill", "grilled", "bbq", "barbecue", "salad", "corn", "tomato", "tomatoes", "zucchini", "peach",
		"peaches", "watermelon", "berries", "kebab", "kebabs", "skewers", "cucumber", "gazpacho", "caprese", "mango"},
}

func inSeason(r Result, season string) bool {
	// The name and tags, not every ingredient: a tomato in a winter stew
	// doesn't make it a summer dinner.
	parts := append([]string{r.Content.Name}, r.Content.Tags...)
	hay := " " + words(strings.Join(parts, " ")) + " "
	for _, w := range seasonWords[season] {
		if strings.Contains(hay, " "+w+" ") {
			return true
		}
	}
	return false
}

// bigPot reports whether a recipe is a chili, soup, or stew.
func bigPot(r Result) bool {
	hay := " " + words(r.Content.Name) + " "
	for _, w := range []string{"chili", "soup", "stew", "chowder"} {
		if strings.Contains(hay, " "+w+" ") {
			return true
		}
	}
	return false
}

// words lowercases s and keeps its letters, digits, and apostrophes, with
// single spaces between words.
func words(s string) string {
	return strings.Join(strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '\''
	}), " ")
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ParseFilterInt reads a whole-number filter; "" is 0.
func ParseFilterInt(v string) (int, error) {
	if v == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("%w: must be a positive whole number", ErrInvalidQuery)
	}
	return n, nil
}
