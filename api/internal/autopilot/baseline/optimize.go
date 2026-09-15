package baseline

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot"
)

// state is a partial week in the beam search.
type state struct {
	picks  []*cand      // per slot; nil while unfilled
	pens   [][4]float64 // per slot: variety, cook time, rules, novelty
	chosen []chosenMeal // fixed meals and picks
	long   int          // long meals counted against the allowance
	quick  int
	fresh  int // new meals
	hits   []int
	bands  [7]autopilot.TimeBand
	// remaining is how many slots are still to be decided.
	remaining int
	total     float64
	tie       uint64
}

type chosenMeal struct {
	it  *item
	day int
}

func (st *state) clone() state {
	c := *st
	c.picks = slices.Clone(st.picks)
	c.pens = slices.Clone(st.pens)
	c.chosen = slices.Clone(st.chosen)
	c.hits = slices.Clone(st.hits)
	return c
}

func (st *state) has(id string) bool {
	return slices.ContainsFunc(st.chosen, func(c chosenMeal) bool { return c.it.id == id })
}

// requestedMeals is how many meals the week calls for.
func (m *model) requestedMeals() int {
	return firstPositive(m.ctx.meals, m.prefs.mealsPerWeek)
}

// maxLong is the week's long-meal allowance; a busy week allows none.
func (m *model) maxLong() int {
	if m.ctx.busy {
		return 0
	}
	return m.prefs.maxLong
}

// minQuick is the fewest quick meals wanted; a busy week wants at least half.
func (m *model) minQuick() int {
	if m.ctx.busy {
		return max(m.prefs.minQuick, (m.requestedMeals()+1)/2)
	}
	return m.prefs.minQuick
}

// noveltyBudget is how many new meals the week takes before penalties.
func (m *model) noveltyBudget() int {
	n := m.requestedMeals()
	switch m.prefs.novelty {
	case autopilot.NoveltyFavorites:
		return n / 5
	case autopilot.NoveltyAdventurous:
		return n
	}
	return (n + 1) / 2
}

func (m *model) longOKOn(day int) bool {
	if day < 0 || m.ctx.busy {
		return false
	}
	r := m.prefs.rules[day]
	return r != nil && r.band == autopilot.BandLong
}

// fullMatch reports whether the item matches every group the rule specifies.
func fullMatch(it *item, r *rule) bool {
	groups := 0
	for _, g := range [][2][]string{{it.proteins, r.proteins}, {it.methods, r.methods}, {it.cuisines, r.cuisines}, {it.tags, r.tags}} {
		if len(g[1]) == 0 {
			continue
		}
		groups++
		if intersect(g[0], g[1]) == "" {
			return false
		}
	}
	return groups > 0
}

func (m *model) initialState(slots int) state {
	st := state{picks: make([]*cand, slots), pens: make([][4]float64, slots), hits: make([]int, len(m.prefs.allRules)), remaining: slots}
	for _, f := range m.fixed {
		if f.it != nil {
			m.apply(&st, f.it, f.day, m.longOKOn(f.day))
		}
	}
	return st
}

// apply adds a meal to the state's week.
func (m *model) apply(st *state, it *item, day int, longOK bool) {
	st.chosen = append(st.chosen, chosenMeal{it: it, day: day})
	switch it.band {
	case autopilot.BandLong:
		if !longOK {
			st.long++
		}
	case autopilot.BandQuick:
		st.quick++
	}
	if day >= 0 {
		st.bands[day] = it.band
	}
	if it.isNew {
		st.fresh++
	}
	for i, r := range m.prefs.allRules {
		if r.freq == autopilot.AtMostOnce && fullMatch(it, r) {
			st.hits[i]++
		}
	}
}

// penalties returns the week objective's change from adding the item on day:
// variety, cook-time mix, rule frequency, and novelty budget (all ≤ 0).
func (m *model) penalties(st *state, it *item, day int, longOK bool) [4]float64 {
	var p [4]float64
	w := m.w
	for _, c := range st.chosen {
		if intersect(it.cuisines, c.it.cuisines) != "" {
			p[0] -= w.CuisineRepeat
		}
		if intersect(it.proteins, c.it.proteins) != "" {
			p[0] -= w.ProteinRepeat
		}
	}
	if it.band == autopilot.BandLong {
		if !longOK && st.long >= m.maxLong() {
			p[1] -= w.ExtraLong
		}
		if m.prefs.avoidConsecutiveLong && day >= 0 {
			for _, n := range []int{day - 1, day + 1} {
				if n >= 0 && n < 7 && st.bands[n] == autopilot.BandLong {
					p[1] -= w.ConsecutiveLong
				}
			}
		}
	}
	if minQuick := m.minQuick(); minQuick > 0 {
		before := max(0, minQuick-st.quick-st.remaining)
		quick := st.quick
		if it.band == autopilot.BandQuick {
			quick++
		}
		after := max(0, minQuick-quick-(st.remaining-1))
		p[1] -= w.MissingQuick * float64(after-before)
	}
	for i, r := range m.prefs.allRules {
		if r.freq == autopilot.AtMostOnce && st.hits[i] > 0 && fullMatch(it, r) {
			p[2] -= w.RuleRepeat
		}
	}
	if it.isNew && st.fresh >= m.noveltyBudget() {
		p[3] -= w.NoveltyBudget
	}
	return p
}

// candidates scores every eligible, unfixed item that fits the slot, best
// first.
func (m *model) candidates(s *slot) {
	for _, it := range m.eligible {
		if m.fixedID[it.id] || !s.fits(it) {
			continue
		}
		s.cands = append(s.cands, m.score(s, it))
	}
	slices.SortStableFunc(s.cands, func(a, b cand) int {
		if a.base != b.base {
			return cmp.Compare(b.base, a.base)
		}
		return cmp.Compare(a.it.tie, b.it.tie)
	})
}

// chooseDays picks which open days get meals when there are more open days
// than meals: days with rules first, then the weekdays the household plans
// most, then week order.
func (m *model) chooseDays(open []int, needed int) []int {
	if needed >= len(open) {
		return open
	}
	total := 0
	for _, n := range m.dayCounts {
		total += n
	}
	score := func(day int) float64 {
		var s float64
		if r := m.prefs.rules[day]; r != nil {
			s = 1
			if r.freq == autopilot.EveryWeek {
				s = 2
			}
		}
		if total > 0 {
			s += float64(m.dayCounts[day]) / float64(total)
		}
		return s
	}
	ranked := slices.Clone(open)
	slices.SortStableFunc(ranked, func(a, b int) int {
		if sa, sb := score(a), score(b); sa != sb {
			return cmp.Compare(sb, sa)
		}
		return cmp.Compare(a, b)
	})
	chosen := ranked[:needed]
	slices.Sort(chosen)
	return chosen
}

// search runs the beam search over slots, most constrained first.
func (m *model) search(slots []*slot) state {
	order := make([]int, len(slots))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int {
		if c := cmp.Compare(len(slots[a].cands), len(slots[b].cands)); c != 0 {
			return c
		}
		return cmp.Compare(slots[a].day, slots[b].day)
	})
	beam := []state{m.initialState(len(slots))}
	for _, si := range order {
		s := slots[si]
		var next []state
		for i := range beam {
			st := &beam[i]
			tried := 0
			for ci := range s.cands {
				c := &s.cands[ci]
				if st.has(c.it.id) {
					continue
				}
				if tried == m.perSlot {
					break
				}
				tried++
				pen := m.penalties(st, c.it, s.day, s.longOK)
				ns := st.clone()
				ns.picks[si], ns.pens[si] = c, pen
				m.apply(&ns, c.it, s.day, s.longOK)
				ns.remaining--
				ns.total += c.base + pen[0] + pen[1] + pen[2] + pen[3]
				ns.tie = ns.tie*1099511628211 ^ c.it.tie
				next = append(next, ns)
			}
			if tried == 0 {
				ns := st.clone()
				ns.remaining--
				ns.total -= m.w.Unfilled
				next = append(next, ns)
			}
		}
		slices.SortStableFunc(next, func(a, b state) int {
			if a.total != b.total {
				return cmp.Compare(b.total, a.total)
			}
			return cmp.Compare(a.tie, b.tie)
		})
		beam = m.selectDiverse(next)
	}
	return beam[0]
}

// selectDiverse keeps the beam's partial weeks: first the best week for each
// distinct cook-time and rule pattern, then the best of the rest. Without it
// the beam fills with weeks that differ only by interchangeable meals and
// loses arrangements such as alternating long cooks. sorted is best first.
func (m *model) selectDiverse(sorted []state) []state {
	if len(sorted) <= m.beam {
		return sorted
	}
	out := make([]state, 0, m.beam)
	taken := make([]bool, len(sorted))
	seen := map[string]bool{}
	for i := range sorted {
		if len(out) == m.beam {
			break
		}
		if sig := sorted[i].signature(); !seen[sig] {
			seen[sig], taken[i] = true, true
			out = append(out, sorted[i])
		}
	}
	for i := range sorted {
		if len(out) == m.beam {
			break
		}
		if !taken[i] {
			out = append(out, sorted[i])
		}
	}
	slices.SortStableFunc(out, func(a, b state) int {
		if a.total != b.total {
			return cmp.Compare(b.total, a.total)
		}
		return cmp.Compare(a.tie, b.tie)
	})
	return out
}

// signature describes what a partial week means for the days still open.
func (st *state) signature() string {
	return fmt.Sprint(st.bands, st.hits, st.fresh, st.quick, st.long)
}

// evaluate computes the week objective of picks (one per slot, nil for
// unfilled), adding meals in day order, and each slot's week penalties.
func (m *model) evaluate(slots []*slot, picks []*cand) (float64, [][4]float64) {
	st := m.initialState(len(slots))
	pens := make([][4]float64, len(slots))
	var total float64
	for i, s := range slots {
		c := picks[i]
		if c == nil {
			st.remaining--
			total -= m.w.Unfilled
			continue
		}
		pen := m.penalties(&st, c.it, s.day, s.longOK)
		pens[i] = pen
		m.apply(&st, c.it, s.day, s.longOK)
		st.remaining--
		total += c.base + pen[0] + pen[1] + pen[2] + pen[3]
	}
	return total, pens
}

// improve refines the beam's best week with deterministic local search:
// replace one day's meal with an unused candidate, or swap two days' meals,
// whenever that raises the week objective, until no move does. The beam keeps
// few partial weeks, and many of them differ only by interchangeable meals,
// so this recovers arrangements (such as alternating long cooks) it dropped.
func (m *model) improve(slots []*slot, picks []*cand) []*cand {
	const maxRounds = 50
	index := make([]map[string]*cand, len(slots))
	for i, s := range slots {
		index[i] = make(map[string]*cand, len(s.cands))
		for ci := range s.cands {
			index[i][s.cands[ci].it.id] = &s.cands[ci]
		}
	}
	picks = slices.Clone(picks)
	best, _ := m.evaluate(slots, picks)
	used := func(id string) bool {
		return slices.ContainsFunc(picks, func(c *cand) bool { return c != nil && c.it.id == id })
	}
	try := func(trial []*cand) bool {
		if total, _ := m.evaluate(slots, trial); total > best+1e-9 {
			best, picks = total, trial
			return true
		}
		return false
	}
	for range maxRounds {
		improved := false
		for i, s := range slots {
			tried := 0
			for ci := range s.cands {
				c := &s.cands[ci]
				if used(c.it.id) {
					continue
				}
				if tried == m.perSlot {
					break
				}
				tried++
				trial := slices.Clone(picks)
				trial[i] = c
				improved = try(trial) || improved
			}
			for j := i + 1; j < len(slots); j++ {
				if picks[i] == nil || picks[j] == nil {
					continue
				}
				a, b := index[j][picks[i].it.id], index[i][picks[j].it.id]
				if a == nil || b == nil {
					continue
				}
				trial := slices.Clone(picks)
				trial[i], trial[j] = b, a
				improved = try(trial) || improved
			}
		}
		if !improved {
			break
		}
	}
	return picks
}

// recommendation turns a scored candidate and its week penalties into the
// public type.
func recommendation(c *cand, pen [4]float64) autopilot.Recommendation {
	signals := make(map[string]float64, len(c.signals)+4)
	for k, v := range c.signals {
		signals[k] = v
	}
	for i, name := range []string{SignalVariety, SignalCookTimeMix, SignalRuleFrequency, SignalNoveltyBudget} {
		if pen[i] != 0 {
			signals[name] = round3(pen[i])
		}
	}
	return autopilot.Recommendation{
		ItemID:   c.it.id,
		Score:    round3(c.base + pen[0] + pen[1] + pen[2] + pen[3]),
		Servings: c.servings,
		TimeBand: c.it.band,
		Signals:  signals,
		Reasons:  c.explanation(),
	}
}

func (m *model) generate() autopilot.WeekResult {
	res := autopilot.WeekResult{ModelVersion: m.version, Seed: m.seed, Candidates: len(m.eligible), ColdStart: m.coldStart}
	res.Requested = m.requestedMeals()
	if m.ctx.maxMinutes > 0 {
		// A week-wide cap is a hard constraint too.
		res.Candidates = 0
		for _, it := range m.eligible {
			if it.minutes > 0 && it.minutes <= m.ctx.maxMinutes {
				res.Candidates++
			}
		}
	}
	switch {
	case m.ctx.skip:
		res.Requested = 0
		res.Messages = append(res.Messages, autopilot.Message{Code: "week_skipped", Text: "You're skipping this week, so there's nothing to plan."})
		return res
	case len(m.items) == 0:
		res.Messages = append(res.Messages, autopilot.Message{Code: "empty_catalog", Text: "Add or import recipes so Autopilot has meals to choose from."})
		return res
	}

	var occupied [7]bool
	for _, f := range m.fixed {
		if f.day >= 0 {
			occupied[f.day] = true
		}
	}
	var open []int
	for _, d := range m.prefs.planDays {
		if !m.ctx.days[d].skip && !occupied[d] {
			open = append(open, d)
		}
	}
	needed := res.Requested - len(m.fixed)
	if needed <= 0 {
		res.Messages = append(res.Messages, autopilot.Message{
			Code: "week_full", Text: fmt.Sprintf("This week already has %s planned.", plural(len(m.fixed), "meal", "meals")),
		})
		return res
	}
	days := m.chooseDays(open, needed)
	slots := make([]*slot, 0, len(days))
	for _, d := range days {
		s := m.newSlot(d)
		m.candidates(s)
		slots = append(slots, s)
	}
	picks := m.improve(slots, m.search(slots).picks)
	_, pens := m.evaluate(slots, picks)

	for si, s := range slots {
		c := picks[si]
		day := autopilot.Days[s.day]
		if c == nil {
			u := autopilot.Unfilled{Day: day, Code: "no_candidates", Text: fmt.Sprintf("No remaining recipe fits %s.", day.Name())}
			if s.hardCap > 0 {
				u.Code, u.Text = "no_quick_candidates", fmt.Sprintf("No remaining recipe is ready within %d minutes on %s.", s.hardCap, day.Name())
			}
			res.Unfilled = append(res.Unfilled, u)
			continue
		}
		pen := pens[si]
		res.Slots = append(res.Slots, autopilot.Slot{Day: day, Recommendation: recommendation(c, pen)})
		res.Score.Meals += c.base
		res.Score.Variety += pen[0]
		res.Score.CookTime += pen[1]
		res.Score.Rules += pen[2]
		res.Score.Novelty += pen[3]
	}
	res.Planned = len(res.Slots)
	res.Score.Meals, res.Score.Variety, res.Score.CookTime = round3(res.Score.Meals), round3(res.Score.Variety), round3(res.Score.CookTime)
	res.Score.Rules, res.Score.Novelty = round3(res.Score.Rules), round3(res.Score.Novelty)
	res.Score.Total = round3(res.Score.Meals + res.Score.Variety + res.Score.CookTime + res.Score.Rules + res.Score.Novelty)

	if len(res.Unfilled) > 0 {
		capMinutes := 0
		for si, s := range slots {
			if picks[si] == nil && s.hardCap > 0 && (capMinutes == 0 || s.hardCap < capMinutes) {
				capMinutes = s.hardCap
			}
		}
		matching := 0
		for _, it := range m.eligible {
			if !m.fixedID[it.id] && (capMinutes == 0 || (it.minutes > 0 && it.minutes <= capMinutes)) {
				matching++
			}
		}
		nights := fmt.Sprintf("planned %d of %d nights", res.Planned, len(slots))
		msg := autopilot.Message{Code: "not_enough_candidates"}
		switch {
		case capMinutes > 0:
			msg.Text = fmt.Sprintf("Only %s (≤%d min) %s; %s.", plural(matching, "quick recipe", "quick recipes"), capMinutes, verb(matching), nights)
		default:
			msg.Text = fmt.Sprintf("Only %s %s your preferences; %s.", plural(matching, "recipe", "recipes"), verb(matching), nights)
		}
		res.Messages = append(res.Messages, msg)
	}
	if len(days) < needed {
		res.Messages = append(res.Messages, autopilot.Message{
			Code: "not_enough_days",
			Text: fmt.Sprintf("Only %s open this week; planned %d of %d meals.", plural(len(open), "day is", "days are"), res.Planned+len(m.fixed), res.Requested),
		})
	}
	if len(m.fixed) > 0 {
		res.Messages = append(res.Messages, autopilot.Message{
			Code: "already_planned", Text: fmt.Sprintf("%s already planned, so Autopilot filled the rest.", plural(len(m.fixed), "meal was", "meals were")),
		})
	}
	if m.coldStart {
		res.Messages = append(res.Messages, autopilot.Message{
			Code: "cold_start", Text: "Autopilot is leaning on your taste profile until you rate and cook more meals.",
		})
	}
	return res
}

func (m *model) rank(day autopilot.Day, exclude []string, limit int) autopilot.RankResult {
	res := autopilot.RankResult{ModelVersion: m.version}
	s := m.newSlot(day.Index())
	st := m.initialState(1)
	excluded := map[string]bool{}
	for _, id := range exclude {
		excluded[id] = true
	}
	type ranked struct {
		c     cand
		pen   [4]float64
		total float64
	}
	var list []ranked
	for _, it := range m.eligible {
		if m.fixedID[it.id] || !s.fits(it) {
			continue
		}
		res.Eligible++
		if excluded[it.id] {
			continue
		}
		c := m.score(s, it)
		pen := m.penalties(&st, it, s.day, s.longOK)
		list = append(list, ranked{c: c, pen: pen, total: c.base + pen[0] + pen[1] + pen[2] + pen[3]})
	}
	slices.SortStableFunc(list, func(a, b ranked) int {
		if a.total != b.total {
			return cmp.Compare(b.total, a.total)
		}
		return cmp.Compare(a.c.it.tie, b.c.it.tie)
	})
	switch {
	case limit <= 0:
		limit = autopilot.DefaultRankLimit
	case limit > autopilot.MaxRankLimit:
		limit = autopilot.MaxRankLimit
	}
	for i := range list[:min(len(list), limit)] {
		res.Items = append(res.Items, recommendation(&list[i].c, list[i].pen))
	}
	if len(res.Items) == 0 {
		msg := autopilot.Message{Code: "no_alternative", Text: fmt.Sprintf("No other recipe fits %s.", day.Name())}
		if s.hardCap > 0 {
			msg.Text = fmt.Sprintf("No other recipe is ready within %d minutes on %s.", s.hardCap, day.Name())
		}
		res.Messages = append(res.Messages, msg)
	}
	return res
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

func verb(n int) string {
	if n == 1 {
		return "matches"
	}
	return "match"
}
