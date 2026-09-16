package baseline

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

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
	// A busy week wants busyWant quick meals on its busy weeknights; busyQuick
	// is how many it has, and busyLeft how many busy slots are undecided.
	busyWant, busyQuick, busyLeft int
	total                         float64
	tie                           uint64
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

// busyOn reports whether a busy week's quick preference applies on day. A busy
// week is about weeknights: other days keep their usual cook-time handling,
// and a day whose rule allows a long cook keeps it. Hard caps apply anyway.
func (m *model) busyOn(day int) bool {
	return m.ctx.busy && day >= 0 && m.prefs.weeknights[day] && !m.longOKOn(day)
}

// overLong reports whether another long meal on day, not allowed by the day's
// rule, exceeds the week's long-meal allowance. Busy weeknights allow none.
func (m *model) overLong(st *state, day int) bool {
	return m.busyOn(day) || st.long >= m.prefs.maxLong
}

// missingQuick is how many more quick meals a week can no longer fit after
// adding a meal: it wants want, has have so far, and left slots (including
// this one) are undecided.
func missingQuick(want, have, left int, quick bool) int {
	before := max(0, want-have-left)
	if quick {
		have++
	}
	return max(0, want-have-(left-1)) - before
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

// longOKOn reports whether day's rule allows a long cook, busy week or not.
func (m *model) longOKOn(day int) bool {
	if day < 0 {
		return false
	}
	r := m.prefs.rules[day]
	return r != nil && r.band == autopilot.BandLong
}

// ruleCuisine returns the value by which the item matches the rule's cuisines,
// or "". Matching runs one step in each direction, but never sideways:
//
//   - the item carries the rule's cuisine or descends from it, so a rule for
//     "european" matches an Italian recipe (intersect with withRegions);
//   - the item carries only a region the rule's cuisine belongs to, so a rule
//     for "italian" matches a recipe the catalog labeled just "southern
//     european" — it could be Italian.
//
// The second test reads the item's own cuisines, not its regions, so a rule
// for "italian" does not match a French recipe just because both are
// European.
func ruleCuisine(it *item, r *rule) string {
	if v := intersect(it.withRegions, r.cuisines); v != "" {
		return v
	}
	return intersect(it.cuisines, r.regions)
}

// fullMatch reports whether the item matches every group the rule specifies.
func fullMatch(it *item, r *rule) bool {
	groups := 0
	for _, g := range [][2][]string{{it.proteins, r.proteins}, {it.methods, r.methods}, {it.tags, r.tags}} {
		if len(g[1]) == 0 {
			continue
		}
		groups++
		if intersect(g[0], g[1]) == "" {
			return false
		}
	}
	if len(r.cuisines) > 0 {
		groups++
		if ruleCuisine(it, r) == "" {
			return false
		}
	}
	return groups > 0
}

// suitsMethod reports whether the item suits any of the rule's methods.
func suitsMethod(it *item, r *rule) bool {
	return intersect(it.methods, r.methods) != ""
}

// methodUnmet explains a rule day filled with a meal that doesn't suit the
// rule's methods: none of the day's candidates did, or those that did lost to
// the rest of the week (recently had, repeated, or already planned).
func methodUnmet(s *slot) autopilot.Message {
	names := make([]string, len(s.rule.methods))
	for i, method := range s.rule.methods {
		names[i] = strings.ReplaceAll(method, "-", " ")
	}
	kind, day := strings.Join(names, " or ")+"-friendly", autopilot.Days[s.day].Name()
	text := fmt.Sprintf("No %s recipe fits %s; picked the best alternative.", kind, day)
	if slices.ContainsFunc(s.cands, func(c cand) bool { return suitsMethod(c.it, s.rule) }) {
		text = fmt.Sprintf("%s's %s recipes didn't fit this week; picked the best alternative.", day, kind)
	}
	return autopilot.Message{Code: "rule_method_unmet", Text: text}
}

func (m *model) initialState(slots []*slot) state {
	n := len(slots)
	st := state{picks: make([]*cand, n), pens: make([][4]float64, n), hits: make([]int, len(m.prefs.allRules)), remaining: n}
	for _, s := range slots {
		if m.busyOn(s.day) {
			st.busyLeft++
		}
	}
	busyMeals := st.busyLeft
	for _, f := range m.fixed {
		if f.it != nil {
			if m.busyOn(f.day) {
				busyMeals++
			}
			m.apply(&st, f.it, f.day, m.longOKOn(f.day))
		}
	}
	st.busyWant = (busyMeals + 1) / 2
	return st
}

// decided marks the slot on day as decided, filled or not.
func (m *model) decided(st *state, day int) {
	st.remaining--
	if m.busyOn(day) {
		st.busyLeft--
	}
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
		if m.busyOn(day) {
			st.busyQuick++
		}
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
		// A shared cuisine is a full repeat; sharing only a region ("italian"
		// and "greek" are both southern european) is a partial one. They never
		// stack: a shared cuisine already implies a shared region.
		switch {
		case intersect(it.cuisines, c.it.cuisines) != "":
			p[0] -= w.CuisineRepeat
		case intersect(it.withRegions, c.it.withRegions) != "":
			p[0] -= w.CuisineRegionRepeat
		}
		// Two pastas repeat even when their cuisine labels differ or are
		// missing, which is what "the same kind of meal twice" means.
		if intersect(it.categories, c.it.categories) != "" {
			p[0] -= w.MealCategoryRepeat
		}
		if intersect(it.proteins, c.it.proteins) != "" {
			p[0] -= w.ProteinRepeat
		}
	}
	if it.band == autopilot.BandLong {
		if !longOK && m.overLong(st, day) {
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
	quick := it.band == autopilot.BandQuick
	p[1] -= w.MissingQuick * float64(missingQuick(m.prefs.minQuick, st.quick, st.remaining, quick))
	if m.busyOn(day) {
		p[1] -= w.MissingQuick * float64(missingQuick(st.busyWant, st.busyQuick, st.busyLeft, quick))
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
	beam := []state{m.initialState(slots)}
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
				m.decided(&ns, s.day)
				ns.total += c.base + pen[0] + pen[1] + pen[2] + pen[3]
				ns.tie = ns.tie*1099511628211 ^ c.it.tie
				next = append(next, ns)
			}
			if tried == 0 {
				ns := st.clone()
				m.decided(&ns, s.day)
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
	return fmt.Sprint(st.bands, st.hits, st.fresh, st.quick, st.busyQuick, st.long)
}

// evaluate computes the week objective of picks (one per slot, nil for
// unfilled), adding meals in day order, and each slot's week penalties.
func (m *model) evaluate(slots []*slot, picks []*cand) (float64, [][4]float64) {
	st := m.initialState(slots)
	pens := make([][4]float64, len(slots))
	var total float64
	for i, s := range slots {
		c := picks[i]
		if c == nil {
			m.decided(&st, s.day)
			total -= m.w.Unfilled
			continue
		}
		pen := m.penalties(&st, c.it, s.day, s.longOK)
		pens[i] = pen
		m.apply(&st, c.it, s.day, s.longOK)
		m.decided(&st, s.day)
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
	for si, s := range slots {
		if c := picks[si]; c != nil && s.rule != nil && len(s.rule.methods) > 0 && !suitsMethod(c.it, s.rule) {
			res.Messages = append(res.Messages, methodUnmet(s))
		}
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
	st := m.initialState([]*slot{s})
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
