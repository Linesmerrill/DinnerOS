package grocery

import "sort"

// Extra is a grocery line that no recipe's ingredients ask for: an item added
// to the week directly, such as a grocery item Autopilot pairs with a meal
// ("Club crackers for Chicken Noodle Soup"), or one a member added (Add Item).
// A pairing's Sources name the recipes it is for; an added item is for no
// meal and has none.
type Extra struct {
	// ID identifies the extra where it is stored, so it can be removed.
	ID string
	// Origin says where it came from: OriginPairing or OriginAdded.
	Origin string
	// Text explains it, for display.
	Text string
}

// Extra origins.
const (
	// OriginPairing extras were added by accepting an Autopilot pairing.
	OriginPairing = "pairing"
	// OriginAdded extras were added by a member, one at a time or from the
	// household's usuals. They are for no meal.
	OriginAdded = "added"
)

// forNoMeal reports whether a line is an added item, which keeps no
// sources rather than taking its selection's.
func (l Line) forNoMeal() bool {
	return l.Extra != nil && l.Extra.Origin == OriginAdded && len(l.Sources) == 0
}

func (a *accumulator) addExtra(e *Extra) {
	if e == nil {
		return
	}
	if a.extras == nil {
		a.extras = map[string]Extra{}
	}
	a.extras[e.ID] = *e
}

// itemExtras returns the extras in ID order, or nil.
func (a *accumulator) itemExtras() []Extra {
	if len(a.extras) == 0 {
		return nil
	}
	out := make([]Extra, 0, len(a.extras))
	for _, e := range a.extras {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
