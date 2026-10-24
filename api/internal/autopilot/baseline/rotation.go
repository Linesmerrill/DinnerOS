package baseline

import "github.com/Linesmerrill/DinnerOS/api/internal/autopilot"

// The rotation (decision 647): a meal the household had goes to the bottom of
// its kind's stack, and comes back up only after enough other meals of that
// kind have been had since. Rating, familiarity, and habit all favor meals
// already had, so without it the same favorites win every week and the loop
// feeds itself.
//
// Each kind keeps its own stack, named by the most specific thing the meal
// is: its dishes ("taco"), else its meal categories ("pasta"), else its
// cuisines, else the whole library. A pasta waits for other pastas, not for
// tacos, so cooking every kind but pasta doesn't bury the pastas. A meal on
// several stacks of that level is only as ready as its least-turned one.
//
// A buried meal stays buried until its turn: its signal is −1 just had and
// rises only to −0.5 while it waits, so a five-star favorite can't climb
// back over the meals ahead of it. Then it is 0, like a meal never had.
const (
	// rotationDepth is how many other meals of a kind must come between
	// repeats when the library has that many; a smaller kind cycles through
	// all of its meals.
	rotationDepth = 6
	// rotationWeeks is how long it takes a meal to come back on time alone,
	// for a kind the household rarely has.
	rotationWeeks = 16
)

// had is when the household last had a meal: weeks from the planned week
// (negative is before it), then the day; zero when never.
type had struct {
	week, day int
	ok        bool
}

func (h had) after(o had) bool {
	switch {
	case !h.ok:
		return false
	case !o.ok:
		return true
	case h.week != o.week:
		return h.week > o.week
	}
	return h.day > o.day
}

// noteHad records an interaction that puts a meal on its stacks: ordered,
// planned, or cooked by the planned week, or planned for a coming week
// before it. Skips, views, and weeks after the planned one don't.
func (it *item) noteHad(h autopilot.Interaction, weeks int) {
	switch h.Kind {
	case autopilot.KindOrdered, autopilot.KindPlanned, autopilot.KindCooked:
		if weeks > 0 {
			return
		}
	case autopilot.KindNearby:
		if weeks >= 0 {
			return
		}
	default:
		return
	}
	at := had{week: weeks, day: h.Day.Index() + 1, ok: true}
	if at.after(it.last) {
		it.last = at
	}
}

// rotationKinds are the stacks a meal sits on.
func rotationKinds(it *item) []string {
	for _, level := range []struct {
		prefix string
		values []string
	}{{"dish:", it.dishes}, {"category:", it.categories}, {"cuisine:", it.cuisines}} {
		if len(level.values) == 0 {
			continue
		}
		kinds := make([]string, len(level.values))
		for i, v := range level.values {
			kinds[i] = level.prefix + v
		}
		return kinds
	}
	return []string{"all"}
}

// rotate sets how far each eligible meal has come back up its stacks, 0
// (just had) to 1 (its turn).
func (m *model) rotate() {
	stacks := map[string][]*item{}
	for _, it := range m.eligible {
		for _, k := range rotationKinds(it) {
			stacks[k] = append(stacks[k], it)
		}
	}
	for _, it := range m.eligible {
		it.ready = 1
		if !it.last.ok {
			continue
		}
		for _, k := range rotationKinds(it) {
			stack := stacks[k]
			need := min(len(stack)-1, rotationDepth)
			if need == 0 {
				continue
			}
			since := 0
			for _, other := range stack {
				if other.last.after(it.last) {
					since++
				}
			}
			it.ready = min(it.ready, float64(min(since, need))/float64(need))
		}
		// A kind the household rarely has still comes back in time.
		it.ready = max(it.ready, min(1, float64(-it.last.week)/rotationWeeks))
	}
}

// rotationSignal is the meal's rotation signal: 0 on its turn, else −1 to
// −0.5 as it comes back up.
func rotationSignal(it *item) float64 {
	if it.ready >= 1 {
		return 0
	}
	return -1 + it.ready/2
}
