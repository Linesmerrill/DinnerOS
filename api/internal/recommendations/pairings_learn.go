package recommendations

import (
	"cmp"
	"slices"

	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// Learned pairings come from the household's history: which add-on recipes it
// had in the same ISO weeks as main meals of a category, from order history
// (recipes.orderWeeks) and planned weeks (plan entries).
// docs/autopilot.md#learned-pairings explains the math.
//
// Weeks are the unit, and each is counted once: a recipe's order weeks are
// deduplicated, a week that is both ordered and planned is one week, and weeks
// without a main meal don't count. Raw confidence (the share of a category's
// weeks that had the add-on) is inflated for an add-on the household has
// nearly every week, whatever it eats: imported add-ons can collapse many
// weekly menu aliases into one recipe with an order week almost every week.
// So a pairing must also beat the add-on's rate in the weeks without the
// category by MinPairingLead.

// Learned pairing thresholds.
const (
	// MinPairingSupport is the fewest weeks with both the category and the
	// add-on.
	MinPairingSupport = 3
	// MinPairingConfidence is the lowest share of the category's weeks that
	// had the add-on.
	MinPairingConfidence = 0.5
	// MinPairingLead is how much higher that share must be than the add-on's
	// share of the weeks without the category.
	MinPairingLead = 0.2
	// MinPairingComparisonWeeks is the fewest weeks without the category
	// needed to compare against.
	MinPairingComparisonWeeks = 4
)

// PairingStats is how often an add-on came with a meal category.
type PairingStats struct {
	Category string
	AddonID  string
	// Together counts weeks with the category and the add-on; CategoryWeeks
	// weeks with the category; AddonWeeks weeks with a main meal and the
	// add-on; TotalWeeks weeks with a main meal.
	Together, CategoryWeeks, AddonWeeks, TotalWeeks int
}

// Confidence is the share of the category's weeks that had the add-on.
func (st PairingStats) Confidence() float64 { return ratio(st.Together, st.CategoryWeeks) }

// OtherWeeks counts weeks with a main meal but not the category.
func (st PairingStats) OtherWeeks() int { return st.TotalWeeks - st.CategoryWeeks }

// OtherRate is the share of OtherWeeks that had the add-on.
func (st PairingStats) OtherRate() float64 { return ratio(st.AddonWeeks-st.Together, st.OtherWeeks()) }

// BaseRate is the share of all weeks with a main meal that had the add-on.
func (st PairingStats) BaseRate() float64 { return ratio(st.AddonWeeks, st.TotalWeeks) }

// Lift is Confidence over BaseRate.
func (st PairingStats) Lift() float64 {
	if base := st.BaseRate(); base > 0 {
		return st.Confidence() / base
	}
	return 0
}

// Qualified reports whether the pairing is strong and specific enough to
// suggest.
func (st PairingStats) Qualified() bool {
	return st.Together >= MinPairingSupport && st.Confidence() >= MinPairingConfidence &&
		st.OtherWeeks() >= MinPairingComparisonWeeks && st.Confidence()-st.OtherRate() >= MinPairingLead
}

func ratio(n, d int) float64 {
	if d <= 0 {
		return 0
	}
	return float64(n) / float64(d)
}

// pairingHistory collects, per ISO week, the categories of the main meals and
// the add-ons the household had.
type pairingHistory struct {
	weeks map[string]*historyWeek
}

type historyWeek struct {
	main       bool
	categories map[string]bool
	addons     map[string]bool
}

func newPairingHistory() *pairingHistory { return &pairingHistory{weeks: map[string]*historyWeek{}} }

func (h *pairingHistory) week(w string) *historyWeek {
	hw, ok := h.weeks[w]
	if !ok {
		hw = &historyWeek{categories: map[string]bool{}, addons: map[string]bool{}}
		h.weeks[w] = hw
	}
	return hw
}

// addMain records a main meal, with its categories (possibly none), in week w.
func (h *pairingHistory) addMain(w string, categories []string) {
	hw := h.week(w)
	hw.main = true
	for _, c := range categories {
		hw.categories[c] = true
	}
}

// addAddon records an add-on in week w.
func (h *pairingHistory) addAddon(w, recipeID string) { h.week(w).addons[recipeID] = true }

// stats returns every category and add-on seen in the same week, ordered by
// category (vocabulary order), then Qualified first, confidence, together, and
// add-on ID.
func (h *pairingHistory) stats() []PairingStats {
	total := 0
	categoryWeeks, addonWeeks := map[string]int{}, map[string]int{}
	type pair struct{ category, addon string }
	together := map[pair]int{}
	for _, hw := range h.weeks {
		if !hw.main {
			continue
		}
		total++
		for c := range hw.categories {
			categoryWeeks[c]++
		}
		for a := range hw.addons {
			addonWeeks[a]++
			for c := range hw.categories {
				together[pair{c, a}]++
			}
		}
	}
	out := make([]PairingStats, 0, len(together))
	for p, n := range together {
		out = append(out, PairingStats{
			Category: p.category, AddonID: p.addon, Together: n,
			CategoryWeeks: categoryWeeks[p.category], AddonWeeks: addonWeeks[p.addon], TotalWeeks: total,
		})
	}
	order := optionValues(MealCategoryOptions)
	slices.SortFunc(out, func(a, b PairingStats) int {
		if c := cmp.Compare(slices.Index(order, a.Category), slices.Index(order, b.Category)); c != 0 {
			return c
		}
		if a.Qualified() != b.Qualified() {
			if a.Qualified() {
				return -1
			}
			return 1
		}
		if c := cmp.Compare(b.Confidence(), a.Confidence()); c != 0 {
			return c
		}
		if c := cmp.Compare(b.Together, a.Together); c != 0 {
			return c
		}
		return cmp.Compare(a.AddonID, b.AddonID)
	})
	return out
}

// learned returns the qualified pairings by category, best first.
func (h *pairingHistory) learned() map[string][]PairingStats {
	out := map[string][]PairingStats{}
	for _, st := range h.stats() {
		if st.Qualified() {
			out[st.Category] = append(out[st.Category], st)
		}
	}
	return out
}

// buildPairingHistory reads order weeks from the catalog and planned weeks
// from plans, leaving out skipWeek (the week being suggested for).
func buildPairingHistory(catalog []recipes.Recipe, overrides map[string]RecipeOverride, plans []planning.Plan, skipWeek string) *pairingHistory {
	h := newPairingHistory()
	byID := make(map[string]recipes.Recipe, len(catalog))
	categories := make(map[string][]string, len(catalog))
	add := func(w string, r recipes.Recipe) {
		if w == skipWeek {
			return
		}
		if r.IsAddon {
			h.addAddon(w, r.ID)
		} else {
			h.addMain(w, categories[r.ID])
		}
	}
	for _, r := range catalog {
		byID[r.ID] = r
		if !r.IsAddon {
			var o *RecipeOverride
			if found, ok := overrides[r.ID]; ok {
				o = &found
			}
			categories[r.ID] = recipeMealCategories(r, o)
		}
		for _, w := range r.OrderWeeks {
			add(w, r)
		}
	}
	for _, p := range plans {
		for _, e := range p.Entries {
			if r, ok := byID[e.RecipeID]; ok {
				add(p.Week.String(), r)
			}
		}
	}
	return h
}
