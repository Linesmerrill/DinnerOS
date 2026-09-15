package grocery

import "sort"

// Extra is a grocery line that no recipe's ingredients ask for: an item added
// to the week directly, such as a grocery item Autopilot pairs with a meal
// ("Club crackers for Chicken Noodle Soup"). The line's Sources name the
// recipes it is for.
type Extra struct {
	// ID identifies the extra where it is stored, so it can be removed.
	ID string
	// Origin says where it came from: OriginPairing.
	Origin string
	// Text explains it, for display.
	Text string
}

// Extra origins.
const (
	// OriginPairing extras were added by accepting an Autopilot pairing.
	OriginPairing = "pairing"
)

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
