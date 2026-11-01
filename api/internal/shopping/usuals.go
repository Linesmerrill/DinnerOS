package shopping

import (
	"slices"
	"sort"
	"strings"
	"time"
)

// This file holds the pure half of "Your usuals" (decision 651): items a
// member adds to the week's list by hand (Add Item), and the usuals worked
// out from them. It has no I/O.
//
// Everything here is keyed to the DinnerOS household. Nothing reads or
// stores a store account's profile: Kroger's terms forbid using its profile
// ID to map or keep customer data, so the history is DinnerOS's own.
//
// A usual is an item the household added in at least UsualMinWeeks of its
// last UsualWindow shopping weeks, or one a member pinned. A shopping week is
// one before the week being planned in which the household added an item or
// sent its list to a store's cart (Walmart's links count as much as Fry's
// cart). "Not a usual" hides an item the counting would otherwise suggest.

// Limits and windows for added items and usuals.
const (
	// UsualWindow is how many recent shopping weeks are looked at.
	UsualWindow = 6
	// UsualMinWeeks is how many of them an item must be added in.
	UsualMinWeeks = 3
	// AddedItemRetention is how long added items are kept: long enough to
	// cover UsualWindow shopping weeks with gaps between them.
	AddedItemRetention = 26 * 7 * 24 * time.Hour
	// MaxAddedItemsPerWeek bounds one week's added items.
	MaxAddedItemsPerWeek = 100
	// MaxAddedItemsPerRequest bounds one request, such as "Add your usuals".
	MaxAddedItemsPerRequest = 50
	// MaxAddedItemCount is the most of one item a member can add.
	MaxAddedItemCount = 20
	// MaxAddedItemName bounds an item's name.
	MaxAddedItemName = 80
	// MaxUsualPrefs bounds the pins and "not a usual" marks a household keeps.
	MaxUsualPrefs = 200
)

// AddedItem is one item a member added to a week's list by hand.
type AddedItem struct {
	ID          string
	HouseholdID string
	Week        string
	// IngredientKey is the catalog ingredient the name resolves to, or
	// "name:" and the normalized name, as on the grocery list.
	IngredientKey string
	Name          string
	// Count is how many to buy: the packages sent to the cart.
	Count int
	// FromUsuals is true when it came from "Add your usuals".
	FromUsuals bool
	AddedBy    string
	AddedAt    time.Time
}

// AddItemInput is one item to add.
type AddItemInput struct {
	Name string
	// Count defaults to 1.
	Count int
}

// UsualPref is a member's choice about one item: pinned, so it is always
// offered, or marked "not a usual", so counting never offers it.
type UsualPref struct {
	HouseholdID   string
	IngredientKey string
	Name          string
	// Count is the amount to offer for a pinned item; 0 uses the usual one.
	Count     int
	Pinned    bool
	NotUsual  bool
	UpdatedBy string
	UpdatedAt time.Time
}

// UsualsPrompt records that a week's "Add your usuals?" was answered (added
// or Not Now), so it isn't asked again that week.
type UsualsPrompt struct {
	HouseholdID string
	Week        string
	AnsweredBy  string
	AnsweredAt  time.Time
}

// Usual is one of the household's usuals.
type Usual struct {
	IngredientKey string
	Name          string
	// Count is the amount to offer: the pin's, else the one added most often.
	Count  int
	Pinned bool
	// Weeks is how many of the shopping weeks looked at it was added in.
	Weeks int
	// OnList is true when the week's list already has it.
	OnList bool
}

// UsualsView is a week's usuals.
type UsualsView struct {
	Week string
	// Items are the usuals, pinned first, then by name.
	Items []Usual
	// ShoppingWeeks is how many shopping weeks were looked at (at most
	// UsualWindow).
	ShoppingWeeks int
	// Prompt is true when the week should ask "Add your usuals?": it hasn't
	// been answered and some usual isn't on the list yet.
	Prompt bool
	// NotUsual are the items marked "not a usual", by name, so they can be
	// put back.
	NotUsual []UsualPref
}

// Offered returns the usuals the prompt offers: those not on the list.
func (v UsualsView) Offered() []Usual {
	var out []Usual
	for _, u := range v.Items {
		if !u.OnList {
			out = append(out, u)
		}
	}
	return out
}

// shoppingWeeks returns the UsualWindow most recent weeks before week in
// which the household added an item or sent its list to a cart, newest
// first. Week strings are ISO ("2026-W44"), which sort in time order.
func shoppingWeeks(week string, added []AddedItem, sentWeeks []string) []string {
	seen := map[string]bool{}
	var weeks []string
	consider := func(w string) {
		if w != "" && w < week && !seen[w] {
			seen[w] = true
			weeks = append(weeks, w)
		}
	}
	for _, a := range added {
		consider(a.Week)
	}
	for _, w := range sentWeeks {
		consider(w)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(weeks)))
	if len(weeks) > UsualWindow {
		weeks = weeks[:UsualWindow]
	}
	return weeks
}

// findUsuals works out a week's usuals from the household's added items
// (any weeks; only those in the window count), the weeks it sent a list to a
// cart, its pins and "not a usual" marks, and the keys already on the week's
// list. Prompt is left for the caller.
func findUsuals(week string, added []AddedItem, sentWeeks []string, prefs []UsualPref, onList map[string]bool) UsualsView {
	window := shoppingWeeks(week, added, sentWeeks)
	inWindow := map[string]bool{}
	for _, w := range window {
		inWindow[w] = true
	}

	type tally struct {
		name      string
		latest    time.Time
		weeks     map[string]bool
		counts    map[int]int
		countSeen map[int]time.Time
	}
	tallies := map[string]*tally{}
	for _, a := range added {
		if !inWindow[a.Week] {
			continue
		}
		t := tallies[a.IngredientKey]
		if t == nil {
			t = &tally{weeks: map[string]bool{}, counts: map[int]int{}, countSeen: map[int]time.Time{}}
			tallies[a.IngredientKey] = t
		}
		if a.AddedAt.After(t.latest) || t.name == "" {
			t.name, t.latest = a.Name, a.AddedAt
		}
		t.weeks[a.Week] = true
		count := max(a.Count, 1)
		t.counts[count]++
		if a.AddedAt.After(t.countSeen[count]) {
			t.countSeen[count] = a.AddedAt
		}
	}
	// usualCount is the amount added most often; a tie goes to the latest.
	usualCount := func(t *tally) int {
		best := 1
		for c, n := range t.counts {
			if n > t.counts[best] || (n == t.counts[best] && t.countSeen[c].After(t.countSeen[best])) {
				best = c
			}
		}
		return best
	}

	byKey := map[string]UsualPref{}
	for _, p := range prefs {
		byKey[p.IngredientKey] = p
	}
	view := UsualsView{Week: week, ShoppingWeeks: len(window), Items: []Usual{}}
	for key, t := range tallies {
		pref := byKey[key]
		if pref.Pinned || pref.NotUsual || len(t.weeks) < UsualMinWeeks {
			continue
		}
		view.Items = append(view.Items, Usual{IngredientKey: key, Name: t.name, Count: usualCount(t), Weeks: len(t.weeks), OnList: onList[key]})
	}
	for _, p := range prefs {
		switch {
		case p.Pinned:
			u := Usual{IngredientKey: p.IngredientKey, Name: p.Name, Count: p.Count, Pinned: true, OnList: onList[p.IngredientKey]}
			if t := tallies[p.IngredientKey]; t != nil {
				u.Weeks = len(t.weeks)
				if u.Count == 0 {
					u.Count = usualCount(t)
				}
			}
			u.Count = max(u.Count, 1)
			view.Items = append(view.Items, u)
		case p.NotUsual:
			view.NotUsual = append(view.NotUsual, p)
		}
	}
	slices.SortFunc(view.Items, func(a, b Usual) int {
		if a.Pinned != b.Pinned {
			if a.Pinned {
				return -1
			}
			return 1
		}
		if c := strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); c != 0 {
			return c
		}
		return strings.Compare(a.IngredientKey, b.IngredientKey)
	})
	slices.SortFunc(view.NotUsual, func(a, b UsualPref) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return view
}
