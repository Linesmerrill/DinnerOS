// Package shelflife answers "how long does this keep?" for a food, where it's
// stored, and when it was put away: a recommended best-by date for the
// pantry.
//
// The library is the USDA's FoodKeeper data (the source FoodSafety.gov's
// storage times come from), embedded as a seed, plus entries added later to
// the shelf_life_entries collection, which win over the seed. A name the
// library doesn't know gets its food category's typical time, marked as an
// estimate, and is recorded in shelf_life_misses so the library can grow
// toward what households actually buy. Dates are recommendations for
// quality; the app says so and leaves the call to the cook.
package shelflife

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
)

// Storage is where a food is kept.
type Storage string

// Storages.
const (
	Pantry  Storage = "pantry"
	Fridge  Storage = "fridge"
	Freezer Storage = "freezer"
)

// Valid reports whether s is a known storage.
func (s Storage) Valid() bool { return s == Pantry || s == Fridge || s == Freezer }

// Range is a shelf life in days, shortest to longest.
type Range [2]int

// Entry is one food in the library.
type Entry struct {
	ID       int      `json:"id" bson:"id"`
	Category string   `json:"category" bson:"category"`
	Name     string   `json:"name" bson:"name"`
	Detail   string   `json:"detail,omitempty" bson:"detail,omitempty"`
	Keywords []string `json:"keywords,omitempty" bson:"keywords,omitempty"`
	Pantry   *Range   `json:"pantry,omitempty" bson:"pantry,omitempty"`
	Fridge   *Range   `json:"fridge,omitempty" bson:"fridge,omitempty"`
	Freezer  *Range   `json:"freezer,omitempty" bson:"freezer,omitempty"`
	// Source names where the times come from; the seed's own when empty.
	Source string `json:"source,omitempty" bson:"source,omitempty"`
}

// In returns the entry's shelf life in storage, if it has one.
func (e Entry) In(s Storage) (Range, bool) {
	var r *Range
	switch s {
	case Pantry:
		r = e.Pantry
	case Fridge:
		r = e.Fridge
	case Freezer:
		r = e.Freezer
	}
	if r == nil || r[0] <= 0 {
		return Range{}, false
	}
	return *r, true
}

// Label is how the entry reads: "Pork, ground", "Carrots, parsnips".
func (e Entry) Label() string {
	if e.Detail == "" {
		return e.Name
	}
	return e.Name + ", " + e.Detail
}

//go:embed seed/foodkeeper.json
var seedJSON []byte

type seedFile struct {
	Source  string  `json:"source"`
	Entries []Entry `json:"entries"`
}

// SeedSource names the seed's data.
var SeedSource string

var seed = func() []Entry {
	var f seedFile
	if err := json.Unmarshal(seedJSON, &f); err != nil {
		panic("shelflife: seed: " + err.Error())
	}
	SeedSource = f.Source
	for i := range f.Entries {
		if f.Entries[i].Source == "" {
			f.Entries[i].Source = f.Source
		}
	}
	return f.Entries
}()

// Seed returns the embedded library.
func Seed() []Entry { return slices.Clone(seed) }

// --- Matching -----------------------------------------------------------------

// Field weights: a word in the food's name counts most, its detail next
// ("ground" in "Pork, ground"), a keyword least.
const (
	nameWeight    = 2.0
	detailWeight  = 1.5
	keywordWeight = 1.0
	// extraWordCost is taken for each word of the food's name the query
	// doesn't have: "Stuffed, raw chicken breasts" is further from "Chicken
	// Breasts" than "Chicken parts, breast halves" is.
	extraWordCost = 0.75
)

// Match finds the entry in entries that best fits name. It doesn't look at
// storage: a food the library knows but has no time for in the storage asked
// about gets its category's estimate, never another food's time. Ties go to
// the entry with a time for storage, then the shorter time, to stay on the
// safe side. ok is false when nothing fits.
func Match(entries []Entry, name string, storage Storage) (Entry, bool) {
	query := words(aliased(name))
	if len(query) == 0 {
		return Entry{}, false
	}
	best, bestScore, found := Entry{}, 0.0, false
	better := func(e Entry, score float64) bool {
		if !found || score > bestScore+1e-9 {
			return true
		}
		if score < bestScore-1e-9 {
			return false
		}
		er, eok := e.In(storage)
		br, bok := best.In(storage)
		if eok != bok {
			return eok
		}
		return eok && er[0] < br[0]
	}
	for _, e := range entries {
		score, ok := scoreEntry(e, query)
		if ok && better(e, score) {
			best, bestScore, found = e, score, true
		}
	}
	return best, found
}

// aliases are names the library knows by another name.
var aliases = map[string]string{
	"scallion": "green onion", "scallions": "green onions", "jalapeno": "hot pepper", "jalapeño": "hot pepper",
	"jalapenos": "hot peppers", "jalapeños": "hot peppers", "serrano": "hot pepper", "habanero": "hot pepper",
	"shallot": "onion", "shallots": "onions",
}

func aliased(name string) string {
	lower := strings.ToLower(name)
	for from, to := range aliases {
		for _, w := range strings.Fields(lower) {
			if w == from {
				lower = strings.Replace(lower, from, to, 1)
			}
		}
	}
	return lower
}

// scoreEntry weighs how well e fits the query words. ok is false unless at
// least half the query matched, or every word of the food's name did:
// "Long Green Pepper" fits "Peppers".
func scoreEntry(e Entry, query []string) (float64, bool) {
	name, detail := words(e.Name), words(e.Detail)
	keywords := map[string]bool{}
	for _, k := range e.Keywords {
		for _, w := range words(k) {
			keywords[w] = true
		}
	}
	score, matched := 0.0, 0
	for _, q := range query {
		switch {
		case slices.Contains(name, q):
			score += nameWeight
		case slices.Contains(detail, q):
			score += detailWeight
		case keywords[q]:
			score += keywordWeight
		default:
			continue
		}
		matched++
	}
	extra, cost := 0, 0.0
	part := closestPart(e.Name, query)
	for _, n := range name {
		if slices.Contains(query, n) {
			continue
		}
		// "Carrot juice" isn't carrots, and "Stuffed, raw chicken breasts"
		// aren't chicken breasts.
		if formWords[n] {
			cost += formWordCost
		}
		// "Parsnips" in "Carrots, parsnips" is another food on the list, not
		// a word the query is missing.
		if slices.Contains(part, n) || formWords[n] {
			extra++
			cost += extraWordCost
		}
	}
	if matched == 0 || matched*2 < len(query) && extra > 0 {
		return 0, false
	}
	// A name that is exactly the query ("Carrots") beats one that lists it
	// ("Carrots, parsnips").
	if extra == 0 && matched == len(query) && len(name) == len(query) {
		score += exactBonus
	}
	// The last word of a name is the food ("Long Green Pepper" is a pepper).
	if slices.Contains(name, query[len(query)-1]) {
		score += headWordBonus
	}
	// A plain entry beats a qualified one: "Eggs, in shell" over "Eggs, raw
	// whites, yolks"; "Bacon" over "Bacon, uncured".
	for _, d := range detail {
		if !slices.Contains(query, d) {
			cost += detailWordCost
		}
	}
	return score - cost, true
}

// closestPart is the words of the part of a name listing several foods
// ("Carrots, parsnips") that shares the most words with the query: the other
// foods in the list aren't words the query is missing.
func closestPart(name string, query []string) []string {
	var best []string
	bestHits := -1
	for _, part := range strings.Split(name, ",") {
		w := words(part)
		hits := 0
		for _, q := range query {
			if slices.Contains(w, q) {
				hits++
			}
		}
		if hits > bestHits {
			best, bestHits = w, hits
		}
	}
	return best
}

// formWords name a prepared form of a food; an entry named for one needs
// the query to say so.
var formWords = map[string]bool{
	"juice": true, "sauce": true, "paste": true, "oil": true, "powder": true, "soup": true, "salad": true,
	"dried": true, "canned": true, "pickled": true, "chip": true, "bread": true, "cake": true, "pie": true,
	"extract": true, "flavor": true, "spread": true, "syrup": true, "jam": true, "jelly": true, "dip": true,
	"concentrate": true, "mix": true, "drink": true, "cider": true, "vinegar": true, "butter": true,
	"stuffed": true, "kabob": true, "fried": true, "rotisserie": true, "nugget": true, "patty": true, "dish": true,
	"leftover": true, "casserole": true, "substitute": true,
}

const (
	formWordCost   = 2.0
	detailWordCost = 0.1
	headWordBonus  = 0.5
	exactBonus     = 0.25
)

// stopWords carry no meaning for matching.
var stopWords = map[string]bool{"and": true, "or": true, "the": true, "a": true, "of": true, "with": true, "such": true, "as": true, "etc": true, "in": true}

// words are the lowercase, singular words of s, without stop words.
func words(s string) []string {
	var out []string
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) }) {
		if stopWords[w] || len(w) < 2 {
			continue
		}
		w = singular(w)
		if !slices.Contains(out, w) {
			out = append(out, w)
		}
	}
	return out
}

func singular(w string) string {
	switch {
	case strings.HasSuffix(w, "ies") && len(w) > 4:
		return w[:len(w)-3] + "y"
	case strings.HasSuffix(w, "oes") && len(w) > 4:
		return w[:len(w)-2]
	case strings.HasSuffix(w, "ches"), strings.HasSuffix(w, "shes"), strings.HasSuffix(w, "sses"), strings.HasSuffix(w, "xes"):
		return w[:len(w)-2]
	case strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss") && !strings.HasSuffix(w, "us") && len(w) > 3:
		return w[:len(w)-1]
	}
	return w
}

// --- Estimates ----------------------------------------------------------------

// estimates are a food category's typical times, in days, for a name the
// library doesn't know. They are deliberately on the short side.
var estimates = map[string]map[Storage]Range{
	"produce":      {Pantry: {3, 7}, Fridge: {5, 7}, Freezer: {240, 360}},
	"meat-seafood": {Fridge: {1, 2}, Freezer: {90, 120}},
	"dairy-eggs":   {Fridge: {7, 14}, Freezer: {60, 90}},
	"bakery":       {Pantry: {3, 7}, Fridge: {7, 14}, Freezer: {60, 90}},
	"deli":         {Fridge: {3, 5}, Freezer: {30, 60}},
	"frozen":       {Freezer: {240, 360}},
}

// defaultEstimate covers any other category: shelf-stable goods.
var defaultEstimate = map[Storage]Range{Pantry: {180, 365}, Fridge: {30, 60}, Freezer: {180, 360}}

// Estimate is the typical time for category in storage.
func Estimate(category string, storage Storage) (Range, bool) {
	if byStorage, ok := estimates[category]; ok {
		r, ok := byStorage[storage]
		return r, ok
	}
	r, ok := defaultEstimate[storage]
	return r, ok
}

// --- Dates and words ----------------------------------------------------------

// Wraps, matching households.Household.Wrap: vacuum-sealed meat keeps the
// long end of its freezer time.
const (
	WrapVacuum = "vacuum_sealed"
	// wrapUnknown is food frozen however it came.
	wrapUnknown = "unknown"
)

// Days picks the end of r a best-by date uses: the short end, except frozen
// food sealed without air, which keeps the long end.
func Days(r Range, storage Storage, wrap string) int {
	if storage == Freezer && (wrap == WrapVacuum || wrap == "") {
		return r[1]
	}
	return r[0]
}

// BestBy is storedOn plus days, counted in whole months or years when the
// time is given that way, so "4 months" from Sep 29 is Jan 29.
func BestBy(storedOn time.Time, days int) time.Time {
	switch {
	case days >= 365 && days%365 == 0:
		return storedOn.AddDate(days/365, 0, 0)
	case days >= 30 && days%30 == 0:
		return storedOn.AddDate(0, days/30, 0)
	}
	return storedOn.AddDate(0, 0, days)
}

// Text says r for people: "2–3 weeks", "3–4 months", "1 year".
func Text(r Range) string {
	unit, size := "day", 1
	switch {
	case r[0]%365 == 0 && r[1]%365 == 0:
		unit, size = "year", 365
	case r[0] >= 30 && r[1] >= 30:
		unit, size = "month", 30
	case r[0]%7 == 0 && r[1]%7 == 0:
		unit, size = "week", 7
	}
	// A year is 12 months, not 12 and a sixth.
	count := func(d int) int {
		if size == 30 && d%365 == 0 {
			return d / 365 * 12
		}
		return (d + size/2) / size
	}
	lo, hi := count(r[0]), count(r[1])
	plural := func(n int) string {
		if n == 1 {
			return unit
		}
		return unit + "s"
	}
	if lo == hi {
		return fmt.Sprintf("%d %s", lo, plural(lo))
	}
	return fmt.Sprintf("%d–%d %s", lo, hi, plural(hi))
}
