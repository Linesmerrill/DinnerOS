package shopping

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/providers"
)

// This file pre-picks products at a second store from the products a
// household already saved at its first one (docs/shopping-providers.md#starting-matches),
// so trying Fry's doesn't mean choosing every product again. Nothing here
// saves anything: a member confirms each pick, which saves it through
// PutPreference like any other product choice.

// Limits on match suggestions.
const (
	// MaxSuggestionsPerPage bounds one page, which is one product search per
	// ingredient at the store.
	MaxSuggestionsPerPage = 20
	// DefaultSuggestionsPerPage is a page when the caller doesn't say.
	DefaultSuggestionsPerPage = 12
	// suggestionAlternatives is how many runners-up come with each pick.
	suggestionAlternatives = 3
	// suggestionSearchers is how many store searches run at once.
	suggestionSearchers = 4
	// minSuggestionScore is the least a result scores to be picked: below
	// it, the member chooses. Good matches score 4 and up.
	minSuggestionScore = 2.0
	// suggestionCacheTTL keeps a household's picks while it works down the
	// list, without serving week-old prices.
	suggestionCacheTTL = 6 * time.Hour
)

// MatchSuggestion is the product picked at the target store for one
// ingredient the household saved a product for at the source store.
type MatchSuggestion struct {
	IngredientKey  string
	IngredientName string
	// From is the product saved at the source store.
	From Preference
	// Product is the pick, or nil when the store's search found nothing
	// usable; Alternatives are the next best, best first.
	Product      *SearchResult
	Alternatives []SearchResult
}

// SuggestionPage is one page of suggestions, in ingredient name order.
type SuggestionPage struct {
	Items []MatchSuggestion
	// Next is the ingredient key to pass as after for the next page, or ""
	// on the last page.
	Next string
	// Total counts every ingredient still to match, across all pages.
	Total int
}

// suggestionCache holds picks per household, store, and ingredient.
type suggestionCache struct {
	mu      sync.Mutex
	entries map[string]cachedSuggestion
}

type cachedSuggestion struct {
	suggestion MatchSuggestion
	at         time.Time
}

func (c *suggestionCache) get(key string, now time.Time) (MatchSuggestion, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok || now.Sub(e.at) > suggestionCacheTTL {
		return MatchSuggestion{}, false
	}
	return e.suggestion, true
}

func (c *suggestionCache) put(key string, s MatchSuggestion, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[string]cachedSuggestion{}
	}
	c.entries[key] = cachedSuggestion{suggestion: s, at: now}
}

// MatchSuggestions pre-picks a product at provider for each ingredient the
// household saved a product for at from but not yet at provider. Pages run
// in ingredient name order after the ingredient key after.
func (s *Service) MatchSuggestions(ctx context.Context, householdID, provider, from, after string, limit int) (SuggestionPage, error) {
	if householdID == "" {
		return SuggestionPage{}, errHouseholdRequired
	}
	switch {
	case limit <= 0:
		limit = DefaultSuggestionsPerPage
	case limit > MaxSuggestionsPerPage:
		limit = MaxSuggestionsPerPage
	}
	target, err := s.ProviderFor(ctx, householdID, provider)
	if err != nil {
		return SuggestionPage{}, err
	}
	source, err := s.Provider(from)
	if err != nil {
		return SuggestionPage{}, err
	}
	if source.Key() == target.Key() {
		return SuggestionPage{}, invalid("Pick a different store to match from.")
	}
	if _, ok := target.(providers.ProductSearcher); !ok {
		return SuggestionPage{}, invalid("%s can't suggest products. Choose them yourself.", target.Name())
	}
	saved, err := s.store.ListPreferences(ctx, householdID, source.Key())
	if err != nil {
		return SuggestionPage{}, fmt.Errorf("list %s products: %w", source.Key(), err)
	}
	have, err := s.store.ListPreferences(ctx, householdID, target.Key())
	if err != nil {
		return SuggestionPage{}, fmt.Errorf("list %s products: %w", target.Key(), err)
	}
	done := map[string]bool{}
	for _, p := range have {
		done[p.IngredientKey] = true
	}
	var todo []Preference
	for _, p := range saved {
		if !done[p.IngredientKey] {
			todo = append(todo, p)
		}
	}
	sort.SliceStable(todo, func(i, j int) bool {
		a, b := strings.ToLower(todo[i].IngredientName), strings.ToLower(todo[j].IngredientName)
		if a != b {
			return a < b
		}
		return todo[i].IngredientKey < todo[j].IngredientKey
	})
	page := SuggestionPage{Total: len(todo)}
	start := 0
	if after != "" {
		start = len(todo)
		for i, p := range todo {
			if p.IngredientKey == after {
				start = i + 1
				break
			}
		}
	}
	end := min(start+limit, len(todo))
	if end < len(todo) && end > start {
		page.Next = todo[end-1].IngredientKey
	}
	batch := todo[start:end]
	categories := s.ingredientCategories(ctx, batch)

	settings, err := s.Settings(ctx, householdID)
	if err != nil {
		return SuggestionPage{}, err
	}
	storeID := ""
	if store, ok := settings.Store(target.Key()); ok {
		storeID = store.StoreID
	}
	page.Items = make([]MatchSuggestion, len(batch))
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
		failed   int
		searched int
		slots    = make(chan struct{}, suggestionSearchers)
	)
	for i, pref := range batch {
		cacheKey := strings.Join([]string{householdID, string(target.Key()), storeID, pref.IngredientKey, pref.ProductID}, "|")
		if cached, ok := s.suggestions.get(cacheKey, s.now()); ok {
			page.Items[i] = cached
			continue
		}
		searched++
		wg.Add(1)
		go func(i int, pref Preference) {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			terms := SearchTermsFor(pref.IngredientName, categories[pref.IngredientKey])
			results, err := s.suggestionSearch(ctx, householdID, provider, terms.Query, savedProductQuery(pref))
			if err != nil {
				// One failed search leaves that ingredient without a pick;
				// only a store that fails every search fails the page.
				s.logger.WarnContext(ctx, "match suggestion search failed", "ingredientKey", pref.IngredientKey, "error", err)
				page.Items[i] = MatchSuggestion{IngredientKey: pref.IngredientKey, IngredientName: pref.IngredientName, From: pref}
				mu.Lock()
				failed++
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
				return
			}
			suggestion := pickSuggestion(pref, terms, results)
			page.Items[i] = suggestion
			s.suggestions.put(cacheKey, suggestion, s.now())
		}(i, pref)
	}
	wg.Wait()
	if searched > 0 && failed == searched {
		return SuggestionPage{}, firstErr
	}
	return page, nil
}

// suggestionSearch searches the store for the ingredient and, when the saved
// product's own words say more ("Fresh Poblano" for Long Green Pepper),
// for those too, keeping each product once at its best position.
func (s *Service) suggestionSearch(ctx context.Context, householdID, provider, query, saved string) ([]SearchResult, error) {
	queries := []string{query}
	if saved != "" && !strings.EqualFold(saved, query) {
		queries = append(queries, saved)
	}
	lists := make([][]SearchResult, 0, len(queries))
	longest := 0
	for _, q := range queries {
		results, err := s.cachedSearch(ctx, householdID, provider, q)
		if err != nil {
			return nil, err
		}
		lists = append(lists, results)
		longest = max(longest, len(results))
	}
	var merged []SearchResult
	seen := map[string]bool{}
	for i := range longest {
		for _, results := range lists {
			if i < len(results) && !seen[results[i].ProductID] {
				seen[results[i].ProductID] = true
				merged = append(merged, results[i])
			}
		}
	}
	return merged, nil
}

// cachedSearch is SearchProducts with a name too short to search for
// answering no results rather than an error.
func (s *Service) cachedSearch(ctx context.Context, householdID, provider, query string) ([]SearchResult, error) {
	results, err := s.SearchProducts(ctx, householdID, provider, query)
	var v *ValidationError
	if errors.As(err, &v) {
		return nil, nil
	}
	return results, err
}

// savedProductQuery is a search built from the saved product's name: its
// meaningful words in order, without brand or size, or "" when it adds
// nothing to the ingredient's name.
func savedProductQuery(pref Preference) string {
	var words []string
	for _, w := range wordSet(pref.DisplayName) {
		if len(w) < 2 || slices.Contains(brandWords, w) || strings.Trim(w, "0123456789") == "" || slices.Contains(words, w) {
			continue
		}
		words = append(words, w)
		if len(words) == 5 {
			break
		}
	}
	q := strings.Join(words, " ")
	if len(words) == 0 || strings.EqualFold(q, strings.Join(wordSet(pref.IngredientName), " ")) {
		return ""
	}
	return q
}

// ingredientCategories reads the grocery category of each catalog
// ingredient in prefs, which biases its search (SearchTermsFor). A name key,
// or a catalog that can't be read, gets no bias.
func (s *Service) ingredientCategories(ctx context.Context, prefs []Preference) map[string]string {
	out := map[string]string{}
	if s.catalog == nil {
		return out
	}
	var ids []string
	for _, p := range prefs {
		if !strings.HasPrefix(p.IngredientKey, "name:") {
			ids = append(ids, p.IngredientKey)
		}
	}
	if len(ids) == 0 {
		return out
	}
	found, err := s.catalog.IngredientsByID(ctx, ids)
	if err != nil {
		s.logger.WarnContext(ctx, "match suggestions: ingredient categories", "error", err)
		return out
	}
	for _, ing := range found {
		out[ing.ID] = ing.Category
	}
	return out
}

// scoredResult is a search result with how well it fits.
type scoredResult struct {
	result SearchResult
	score  float64
}

// pickSuggestion ranks a store's search results for one saved product: in
// stock first, then the words the ingredient and the saved product share
// with the result, then the closest package size. A result that is only a
// form the search warned against ("dried" for fresh produce) sinks. Results
// out of stock are kept as alternatives, never picked.
func pickSuggestion(pref Preference, terms SearchTerms, results []SearchResult) MatchSuggestion {
	out := MatchSuggestion{IngredientKey: pref.IngredientKey, IngredientName: pref.IngredientName, From: pref}
	if len(results) == 0 {
		return out
	}
	want := matchWords(pref.IngredientName)
	saved := matchWords(pref.DisplayName)
	alsoWant := synonymWords(want)
	known := map[string]bool{}
	for _, set := range []map[string]bool{want, saved, alsoWant} {
		for w := range set {
			known[w] = true
		}
	}
	nameWords := wordSet(pref.IngredientName)
	savedSize := amountFromSize(pref.PackageSize)
	// What the saved product says beyond the ingredient ("poblano" for Long
	// Green Pepper) is what the household actually buys.
	var savedOnly []string
	for w := range saved {
		if !want[w] && !alsoWant[w] && !slices.Contains(genericWords, w) {
			savedOnly = append(savedOnly, w)
		}
	}
	scored := make([]scoredResult, 0, len(results))
	for rank, r := range results {
		words := matchWords(r.Name)
		wantShare := math.Max(overlap(want, words), overlap(alsoWant, words))
		savedShare := overlap(saved, words)
		if sameWords(saved, want) {
			// A saved product named like the ingredient says nothing more.
			savedShare = wantShare
		}
		score := 3.5*wantShare + 3*savedShare
		// The store's own order is a weak signal, but a signal.
		score += 0.6 * math.Max(0, 5-float64(rank)) / 5
		// Words the ingredient and the saved product don't have usually
		// mean a different product that mentions this one ("Balsamic
		// Vinegar & Basil Crackers").
		extra := 0
		for w := range words {
			if !known[w] && !partOfKnown(w, known) {
				extra++
			}
		}
		score -= math.Min(3.5, 0.7*float64(extra))
		// A fresh saved product wants a fresh one, not a jar or can of it.
		if containsPhrase(wordSet(pref.DisplayName), "fresh") {
			for _, form := range processedForms {
				if containsPhrase(wordSet(r.Name), form) && !containsPhrase(nameWords, form) {
					score -= 2.5
					break
				}
			}
		}
		for _, form := range terms.Avoid {
			if containsPhrase(wordSet(r.Name), form) && !containsPhrase(nameWords, form) {
				score -= 3
			}
		}
		missing := 0
		for _, w := range savedOnly {
			if !words[w] {
				missing++
			}
		}
		// A short saved name ("Fresh Poblano", "Fresh Yellow Onion") names the
		// kind the household buys; a long one is mostly brand and flavor text
		// ("McCormick Gourmet Organic Crushed Rosemary"), so it doesn't count.
		if len(savedOnly) > 0 && len(saved) <= 3 {
			score -= 3 * float64(missing) / float64(len(savedOnly))
		}
		score += sizeCloseness(savedSize, amountFromSize(r.PackageSize))
		switch r.Stock {
		case providers.StockOut:
			score -= 100
		case providers.StockLow:
			score -= 0.5
		}
		if !namesTheSameThing(want, alsoWant, saved, words) {
			score -= 50
		}
		scored = append(scored, scoredResult{result: r, score: score})
	}
	sort.SliceStable(scored, func(i, j int) bool { return scored[i].score > scored[j].score })
	rest := scored
	if scored[0].score >= minSuggestionScore {
		best := scored[0].result
		out.Product = &best
		rest = scored[1:]
	}
	for _, r := range rest {
		if len(out.Alternatives) == suggestionAlternatives {
			break
		}
		out.Alternatives = append(out.Alternatives, r.result)
	}
	return out
}

// processedForms are words that mark a jarred, canned, or otherwise prepared
// form of a fresh product.
var processedForms = []string{"diced", "canned", "can", "jarred", "jar", "chopped", "dried", "frozen", "pickled", "chowder", "soup"}

// genericWords say what kind of product something is, not which: a result
// must share the other words to count as the same thing.
var genericWords = []string{
	"seasoning", "blend", "sauce", "base", "mix", "bottled", "ground", "dried", "powder", "spice", "paste",
	"chopped", "diced", "sliced", "shredded", "minced", "crushed", "strip", "breast",
}

// namesTheSameThing reports whether a result's words name the product the
// household wants: at least half of the ingredient's specific words, or
// every specific word of the saved product. "Roasted Beef Base" is not
// "Smoky Chipotle Bouillon Base"; "Fresh Poblano Peppers" is the saved
// "Fresh Poblano" for Long Green Pepper.
func namesTheSameThing(want, alsoWant, saved, words map[string]bool) bool {
	specific := func(set map[string]bool) []string {
		var out []string
		for w := range set {
			if !slices.Contains(genericWords, w) {
				out = append(out, w)
			}
		}
		return out
	}
	share := func(list []string) float64 {
		if len(list) == 0 {
			return 1
		}
		n := 0
		for _, w := range list {
			if words[w] {
				n++
			}
		}
		return float64(n) / float64(len(list))
	}
	if s := specific(saved); len(s) > 0 && share(s) == 1 {
		return true
	}
	return share(specific(want)) >= 0.5 || (len(alsoWant) > 0 && share(specific(alsoWant)) >= 0.5)
}

// synonyms are grocery names that mean the same product.
var synonyms = map[string]string{
	"scallion":    "green onion",
	"green onion": "scallion",
	"cilantro":    "coriander",
	"garbanzo":    "chickpea",
	"chickpea":    "garbanzo",
}

// synonymWords is want with its name swapped for a synonym, or nil.
func synonymWords(want map[string]bool) map[string]bool {
	var words []string
	for w := range want {
		words = append(words, w)
	}
	sort.Strings(words)
	joined := strings.Join(words, " ")
	for from, to := range synonyms {
		if strings.Contains(joined, from) {
			return matchWords(strings.Replace(joined, from, to, 1))
		}
	}
	return nil
}

// sameWords reports whether a and b hold the same words.
func sameWords(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for w := range a {
		if !b[w] {
			return false
		}
	}
	return true
}

// partOfKnown reports whether w is half of a compound the household's words
// spell as one ("bread" and "crumb" of "breadcrumb").
func partOfKnown(w string, known map[string]bool) bool {
	for k := range known {
		if len(k) > len(w) && strings.Contains(k, w) {
			return true
		}
	}
	return false
}

// brandWords are words in a saved product's name that say whose product it
// is, not what it is, so they don't count towards a match at another store.
var brandWords = []string{
	"great", "value", "marketside", "freshness", "guaranteed", "equate", "sams", "choice", "member", "mark",
	"kroger", "simple", "truth", "private", "selection", "organic", "brand", "store",
	"fresh", "whole", "premium", "quality", "natural", "large", "jumbo", "original", "classic", "rich",
	"oz", "lb", "lbs", "ct", "count", "pack", "fl", "each", "bag", "jar", "can", "bottle", "box",
}

// matchWords is the set of meaningful lower-case words in a product or
// ingredient name: no brand or size words, no numbers, simple plurals folded.
func matchWords(name string) map[string]bool {
	out := map[string]bool{}
	words := wordSet(name)
	for i, w := range words {
		if len(w) < 2 || slices.Contains(brandWords, w) || strings.Trim(w, "0123456789") == "" {
			continue
		}
		out[singular(w)] = true
		// "Bread Crumbs" is also "breadcrumbs", so it meets a name that
		// spells it as one word.
		if i+1 < len(words) && len(words[i+1]) > 2 && !slices.Contains(brandWords, words[i+1]) {
			if compound := singular(w + words[i+1]); breadcrumbLike(compound) {
				out[compound] = true
			}
		}
	}
	return out
}

// compounds are two-word names a store may spell as one.
var compounds = []string{"breadcrumb", "greenonion", "springroll", "cornstarch", "buttermilk", "eggroll"}

func breadcrumbLike(w string) bool { return slices.Contains(compounds, w) }

func singular(w string) string {
	switch {
	case strings.HasSuffix(w, "ies") && len(w) > 4:
		return strings.TrimSuffix(w, "ies") + "y"
	case strings.HasSuffix(w, "oes") && len(w) > 4:
		return strings.TrimSuffix(w, "es")
	case strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss") && len(w) > 3:
		return strings.TrimSuffix(w, "s")
	}
	return w
}

// overlap is the share of want's words that words contains, 0 to 1.
func overlap(want, words map[string]bool) float64 {
	if len(want) == 0 {
		return 0
	}
	n := 0
	for w := range want {
		if words[w] {
			n++
		}
	}
	return float64(n) / float64(len(want))
}

// sizeCloseness scores how near a result's package is to the saved one: up
// to 1.2 for the same size, falling to 0 at four times larger or smaller, and
// nothing when either size is unknown or the units don't convert.
func sizeCloseness(saved, result *ingredients.Amount) float64 {
	if saved == nil || result == nil {
		return 0
	}
	q, err := ingredients.Convert(result.Quantity, result.Unit, saved.Unit)
	if err != nil {
		return 0
	}
	a, b := saved.Quantity.Float64(), q.Float64()
	if a <= 0 || b <= 0 {
		return 0
	}
	ratio := math.Abs(math.Log2(b / a))
	return math.Max(0, 1.2*(1-ratio/2))
}
