package baseline

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot"
)

// slot is an open day of the week being planned.
type slot struct {
	day int
	// hardCap is the day's hard cook-time cap in minutes; 0 means none.
	hardCap int
	// softLimit is the day's soft cook-time limit; soft says where it came
	// from (busy, rule, weeknight).
	softLimit int
	soft      string
	// longOK means the day's rule allows a long cook.
	longOK   bool
	servings int
	// guests is true when the week or day asks for more servings than usual.
	guests bool
	rule   *rule
	cands  []cand
}

// cand is a meal scored for a slot.
type cand struct {
	it       *item
	base     float64
	servings int
	signals  map[string]float64
	reasons  []reason
}

type reason struct {
	code  string
	text  string
	value float64
	// priority reasons (the day's rule, time caps) come first.
	priority bool
}

func (m *model) newSlot(day int) *slot {
	dc := m.ctx.days[day]
	s := &slot{
		day:      day,
		hardCap:  minPositive(m.ctx.maxMinutes, dc.maxMinutes),
		rule:     m.prefs.rules[day],
		servings: firstPositive(dc.servings, m.ctx.servings, m.prefs.servings),
	}
	s.guests = s.servings > m.prefs.servings
	switch {
	case s.rule != nil && s.rule.band == autopilot.BandLong:
		s.longOK = true
	case m.busyOn(day):
		s.softLimit, s.soft = m.prefs.bands.QuickMaxMinutes, "busy"
	case s.rule != nil && s.rule.band == autopilot.BandQuick:
		s.softLimit, s.soft = m.prefs.bands.QuickMaxMinutes, "rule"
	case s.rule != nil && s.rule.band == autopilot.BandMedium:
		s.softLimit, s.soft = m.prefs.bands.MediumMaxMinutes, "rule"
	case m.prefs.weeknights[day] && m.prefs.weeknightMax > 0:
		s.softLimit, s.soft = m.prefs.weeknightMax, "weeknight"
	}
	return s
}

// fits reports whether the item passes the slot's hard cook-time cap. Items
// with an unknown cook time never pass a cap.
func (s *slot) fits(it *item) bool {
	return s.hardCap == 0 || (it.minutes > 0 && it.minutes <= s.hardCap)
}

// score computes a meal's signals and base score for a slot.
func (m *model) score(s *slot, it *item) cand {
	w, st := m.w, &it.st
	c := cand{it: it, servings: pickServings(it.servings, s.servings), signals: make(map[string]float64, 16)}
	add := func(signal string, value, weight float64) float64 {
		c.signals[signal] = round3(value)
		c.base += value * weight
		return value * weight
	}
	explain := func(code, text string, contribution float64, priority bool) {
		if text != "" && (contribution > 0 || priority) {
			c.reasons = append(c.reasons, reason{code: code, text: text, value: contribution, priority: priority})
		}
	}
	day := autopilot.Days[s.day]
	historyCount := st.ordered + st.planned + st.cooked

	// Household rating.
	var rating float64
	var ratingText string
	if st.ratingCount > 0 {
		mean := float64(st.ratingSum) / float64(st.ratingCount)
		rating = (mean - 3) / 2
		if mean >= 4 {
			if st.ratingCount == 1 {
				ratingText = fmt.Sprintf("You rated this %d★", st.ratingSum)
			} else {
				ratingText = fmt.Sprintf("Rated %s★ by your household", strconv.FormatFloat(math.Round(mean*10)/10, 'f', -1, 64))
			}
		}
	}
	explain("rating", ratingText, add(SignalRating, rating, w.Rating), false)

	// Structured feedback.
	weeknight := m.prefs.weeknights[s.day]
	feedback := 0.6*flag(st.makeAgain) + 0.4*flag(st.kidFavorite) + 0.2*flag(st.greatLeftovers) -
		0.4*flag(st.kidsDisliked) - 0.3*flag(st.tooSpicy) - 0.2*flag(st.tooBland)
	if weeknight {
		feedback -= 0.6 * flag(st.tooMuchWork)
	} else {
		feedback -= 0.3 * flag(st.tooMuchWork)
	}
	var feedbackText string
	switch {
	case st.makeAgain > 0:
		feedbackText = "Marked make-again"
	case st.kidFavorite > 0:
		feedbackText = "A kid favorite"
	case st.greatLeftovers > 0:
		feedbackText = "Great leftovers"
	}
	explain("feedback", feedbackText, add(SignalFeedback, clamp(feedback), w.Feedback), false)

	// Familiarity.
	familiarity := math.Min(1, math.Log2(1+float64(historyCount))/math.Log2(9))
	var familiarText string
	if historyCount >= 3 {
		familiarText = fmt.Sprintf("A household regular (%d times)", historyCount)
	}
	explain("familiar", familiarText, add(SignalFamiliarity, familiarity, w.Familiarity), false)

	// Planned → cooked conversion.
	var conversion float64
	var conversionText string
	if n := st.cooked + st.skipped; n > 0 {
		conversion = 2*(float64(st.cooked)+1)/(float64(n)+2) - 1
		if conversion >= 0.3 {
			conversionText = "You usually cook it when it's planned"
		}
	}
	explain("conversion", conversionText, add(SignalConversion, conversion, w.Conversion), false)

	// Recency and repetition.
	var recency float64
	var recencyText string
	switch d := st.nearest; {
	case d < 0:
	case d <= 1:
		recency = -1
	case d == 2:
		recency = -0.6
	case d == 3:
		recency = -0.4
	case d == 4:
		recency = -0.25
	case d <= 8:
		recency = -0.1
	case d >= 10 && historyCount >= 2 && (st.ratingCount == 0 || float64(st.ratingSum)/float64(st.ratingCount) >= 3.5):
		recency = 0.3
		recencyText = fmt.Sprintf("Haven't had it in %d weeks", d)
	}
	explain("notRecently", recencyText, add(SignalRecency, recency, w.Recency), false)

	// Weekday affinity.
	var affinity float64
	var affinityText string
	if st.dayTotal >= 2 {
		affinity = float64(st.days[s.day]) / float64(st.dayTotal)
		if affinity >= 0.5 {
			affinityText = "Often on " + day.Plural()
		}
	}
	explain("weekday", affinityText, add(SignalWeekdayAffinity, affinity, w.WeekdayAffinity), false)

	// Taste profile. Its weight grows when history is thin (cold start).
	var taste float64
	var tasteText string
	for _, g := range []struct {
		have, want []string
		value      float64
	}{
		{it.withRegions, m.prefs.likes.cuisines, 0.4},
		{it.tags, m.prefs.likes.tags, 0.4},
		{it.proteins, m.prefs.likes.proteins, 0.3},
	} {
		if v := intersect(g.have, g.want); v != "" {
			taste += g.value
			if tasteText == "" {
				tasteText = "You like " + displayName(v)
			}
		}
	}
	for _, g := range [][2][]string{
		{it.withRegions, m.prefs.dislikes.cuisines}, {it.tags, m.prefs.dislikes.tags}, {it.proteins, m.prefs.dislikes.proteins},
	} {
		if intersect(g[0], g[1]) != "" {
			taste -= 0.5
		}
	}
	tasteWeight := w.Taste * (1 + w.ColdStartTasteBoost*(1-m.confidence))
	explain("taste", tasteText, add(SignalTaste, clamp(taste), tasteWeight), false)

	// Weekday rule. A rule's methods (already limited to the household's
	// equipment) dominate: a meal that doesn't suit them earns no rule credit
	// however well its protein, cuisine, or tags match, and an every-week
	// rule penalizes it.
	var ruleValue float64
	var ruleText string
	if r := s.rule; r != nil {
		groups, matched := 0, 0
		var parts []string
		// The label names the method ("smoker night"), so explanations list
		// the matched protein, cuisine, or tag.
		for _, g := range []struct {
			want  []string
			match func() string
			// quiet groups aren't listed: the label already names the method.
			quiet bool
		}{
			{r.proteins, func() string { return intersect(it.proteins, r.proteins) }, false},
			{r.methods, func() string { return intersect(it.methods, r.methods) }, true},
			{r.cuisines, func() string { return ruleCuisine(it, r) }, false},
			{r.tags, func() string { return intersect(it.tags, r.tags) }, false},
		} {
			if len(g.want) == 0 {
				continue
			}
			groups++
			if v := g.match(); v != "" {
				matched++
				if !g.quiet {
					parts = append(parts, displayName(v))
				}
			}
		}
		methodMiss := len(r.methods) > 0 && !suitsMethod(it, r)
		switch {
		case methodMiss && r.freq == autopilot.EveryWeek:
			ruleValue = -1
		case methodMiss:
		case groups > 0 && matched == 0 && r.freq == autopilot.EveryWeek:
			ruleValue = -0.3
		case groups > 0:
			ruleValue = float64(matched) / float64(groups)
		}
		if s.longOK && it.band == autopilot.BandLong {
			parts = append(parts, "Long cook OK")
		}
		// The label says the meal is what the rule is about: it suits the
		// rule's method, or matches every group of a rule without methods.
		// Other positive matches explain only the parts that match.
		switch {
		case methodMiss:
		case len(r.methods) > 0 || matched == groups:
			if ruleValue > 0 || len(parts) > 0 {
				ruleText = strings.Join(append([]string{r.label}, parts...), " · ")
			}
		case ruleValue > 0:
			ruleText = strings.Join(parts, " · ")
		}
	}
	c.signals[SignalRule] = round3(ruleValue)
	c.base += ruleValue * w.Rule
	if ruleText != "" {
		c.reasons = append(c.reasons, reason{code: "rule", text: ruleText, value: ruleValue * w.Rule, priority: true})
	}

	// Cook time: hard caps are already enforced; soft limits and long-cook
	// allowances are scored.
	var timeFit float64
	switch {
	case s.longOK:
		if it.band == autopilot.BandLong {
			timeFit = 0.5
		}
	case s.softLimit > 0 && it.minutes <= 0:
		timeFit = -0.1
	case s.softLimit > 0 && it.minutes <= s.softLimit:
		timeFit = 0.5 + 0.5*(1-float64(it.minutes)/float64(s.softLimit))
	case s.softLimit > 0:
		timeFit = -math.Min(1, 2*float64(it.minutes-s.softLimit)/float64(s.softLimit))
	}
	add(SignalTimeFit, timeFit, w.TimeFit)
	switch {
	case s.hardCap > 0 && s.hardCap == m.ctx.maxMinutes && weeknight:
		c.reasons = append(c.reasons, reason{code: "busyWeek", text: fmt.Sprintf("Quick for your busy week (%d min)", it.minutes), priority: true})
	case s.hardCap > 0:
		c.reasons = append(c.reasons, reason{code: "dayLimit", text: fmt.Sprintf("Ready in %d min for %s", it.minutes, day.Name()), priority: true})
	case s.soft == "busy" && timeFit > 0:
		c.reasons = append(c.reasons, reason{code: "busyWeek", text: fmt.Sprintf("Quick for your busy week (%d min)", it.minutes), priority: true})
	case s.soft == "weeknight" && timeFit > 0:
		explain("weeknight", fmt.Sprintf("%d min, easy for a weeknight", it.minutes), timeFit*w.TimeFit, false)
	case s.soft == "rule" && timeFit > 0:
		explain("quick", fmt.Sprintf("Ready in %d min", it.minutes), timeFit*w.TimeFit, false)
	}

	// Novelty appetite.
	var novelty float64
	var noveltyText string
	switch m.prefs.novelty {
	case autopilot.NoveltyFavorites:
		novelty = 0.2
		if it.isNew {
			novelty = -0.6
		}
	case autopilot.NoveltyAdventurous:
		novelty = -0.1
		if it.isNew {
			novelty, noveltyText = 0.6, "Something new to try"
		}
	default:
		if it.isNew {
			novelty, noveltyText = 0.1, "Something new to try"
		}
	}
	explain("new", noveltyText, add(SignalNovelty, novelty, w.Novelty), false)

	// Servings.
	var servingsFit float64
	if largest := it.servings[len(it.servings)-1]; largest < s.servings {
		servingsFit = -math.Min(1, float64(s.servings-largest)/float64(s.servings))
	}
	add(SignalServingsFit, servingsFit, w.ServingsFit)
	if s.guests && c.servings >= s.servings {
		c.reasons = append(c.reasons, reason{code: "guests", text: fmt.Sprintf("Serves %d for your guests", c.servings), value: 0.05})
	}

	// Pantry items running low.
	var pantry float64
	var pantryText string
	matches := 0
	for _, low := range m.ctx.pantryLow {
		for _, name := range it.ingredients {
			if containsPhrase(name, low) {
				if matches == 0 {
					pantryText = "Uses up the " + strings.Join(low, " ") + " running low"
				}
				matches++
				break
			}
		}
	}
	pantry = math.Min(1, float64(matches)/2)
	explain("pantry", pantryText, add(SignalPantry, pantry, w.Pantry), false)

	// Caller-requested avoidance (a regenerated proposal's picks).
	var avoid float64
	if m.avoid[it.id] {
		avoid = -1
	}
	add(SignalAvoid, avoid, w.Avoid)

	// Business objectives: bounded, after hard constraints.
	if boost, ok := m.boosts[it.id]; ok && boost != 0 {
		c.signals[SignalObjective] = round3(boost)
		c.base += boost
	}
	return c
}

// explanation returns up to three reasons: priority reasons in order, then
// the largest contributions.
func (c *cand) explanation() []autopilot.Reason {
	sorted := slices.Clone(c.reasons)
	slices.SortStableFunc(sorted, func(a, b reason) int {
		if a.priority != b.priority {
			if a.priority {
				return -1
			}
			return 1
		}
		if a.priority {
			return 0
		}
		if a.value != b.value {
			return cmp.Compare(b.value, a.value)
		}
		return cmp.Compare(a.code, b.code)
	})
	var out []autopilot.Reason
	for _, r := range sorted {
		if len(out) == 3 {
			break
		}
		out = append(out, autopilot.Reason{Code: r.code, Text: r.text})
	}
	if len(out) == 0 {
		text := "Adds variety to your week"
		if c.it.minutes > 0 {
			text = fmt.Sprintf("Ready in %d min", c.it.minutes)
		}
		out = append(out, autopilot.Reason{Code: "fit", Text: text})
	}
	return out
}

// pickServings returns the smallest authored size that feeds want, or the
// largest size when none does.
func pickServings(sizes []int, want int) int {
	for _, s := range sizes {
		if s >= want {
			return s
		}
	}
	return sizes[len(sizes)-1]
}

func flag(n int) float64 {
	if n > 0 {
		return 1
	}
	return 0
}

func clamp(v float64) float64 { return math.Max(-1, math.Min(1, v)) }

func round3(v float64) float64 { return math.Round(v*1000) / 1000 }

func minPositive(values ...int) int {
	out := 0
	for _, v := range values {
		if v > 0 && (out == 0 || v < out) {
			out = v
		}
	}
	return out
}

func firstPositive(values ...int) int {
	for _, v := range values {
		if v > 0 {
			return v
		}
	}
	return 0
}

// displayName capitalizes each word ("middle eastern" → "Middle Eastern").
func displayName(v string) string {
	words := strings.Fields(v)
	for i, word := range words {
		words[i] = strings.ToUpper(word[:1]) + word[1:]
	}
	return strings.Join(words, " ")
}
