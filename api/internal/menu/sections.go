package menu

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/ratings"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
	"github.com/Linesmerrill/DinnerOS/api/internal/recommendations"
)

// Section kinds.
const (
	KindCarousel = "carousel"
	KindHistory  = "history"
)

// Section IDs. Weekday rule sections are "rule_<day>" ("rule_sun"); a second
// rule on the same day is "rule_sun_2".
const (
	SectionFavorites      = "favorites"
	SectionQuick          = "quick"
	SectionNewToYou       = "new_to_you"
	SectionLongCooks      = "long_cooks"
	SectionSides          = "sides"
	SectionHistoryPlanned = "history_planned"
	SectionHistoryOrdered = "history_ordered"
)

// Section is a titled group of cards.
type Section struct {
	ID       string
	Kind     string
	Title    string
	Subtitle string
	Cards    []Card
	// MoreQuery holds GET .../menu/recipes query parameters that list more
	// like the section, or nil when there is no such list.
	MoreQuery map[string]string
}

// Card is a recipe on the menu.
type Card struct {
	Recipe recipes.Recipe
	// Rating is the household aggregate with the caller's own rating.
	Rating ratings.Summary
	// Badges are at most two, most important first.
	Badges []Badge
	// Reason is a short explanation, or "".
	Reason string
	// InPlan is true when the week's plan has the recipe; PlanEntryIDs are
	// those entries.
	InPlan       bool
	PlanEntryIDs []string
}

func (s *snapshot) card(it *item, reason, suppress, promote string) Card {
	ids := slices.Clone(s.planEntries[it.recipe.ID])
	return Card{
		Recipe: it.recipe, Rating: it.rating, Badges: s.badges(it, suppress, promote), Reason: reason,
		InPlan: len(ids) > 0, PlanEntryIDs: ids,
	}
}

// sections returns the week's non-empty sections in display order.
func (s *snapshot) sections(timing Timing) []Section {
	var out []Section
	add := func(sec Section) {
		if len(sec.Cards) > 0 {
			out = append(out, sec)
		}
	}
	if timing == TimingPast {
		add(s.historyPlanned())
		add(s.historyOrdered())
		add(s.favorites(true))
		return out
	}
	add(s.favorites(false))
	add(s.quickSection())
	add(s.newToYou())
	for _, sec := range s.ruleSections() {
		add(sec)
	}
	add(s.longCooks())
	add(s.sides())
	return out
}

// favorites: main meals with a high household rating (average ≥ 4), a
// make-again or kid-favorite tag, 3+ orders within two years, or 2+ cooks,
// by favorite score. Past weeks call it "Cook It Again".
func (s *snapshot) favorites(past bool) Section {
	sec := Section{
		ID: SectionFavorites, Kind: KindCarousel, Title: "Your Favorites", Subtitle: "Based on what you rate and reorder",
		MoreQuery: map[string]string{"sort": string(SortPopular)},
	}
	if past {
		sec.Title, sec.Subtitle = "Cook It Again", "Favorites worth another round"
	}
	for _, it := range top(s.items,
		func(it *item) bool { return it.main() && it.isFavorite },
		func(a, b *item) int { return higher(a.favorite, b.favorite) },
	) {
		sec.Cards = append(sec.Cards, s.card(it, favoriteReason(it), "", ""))
	}
	return sec
}

// quickSection: suggestible main meals with a known cook time within the
// quick limit, by favorite score, then shortest.
func (s *snapshot) quickSection() Section {
	sec := Section{
		ID: SectionQuick, Kind: KindCarousel, Title: "Quick & Easy", Subtitle: fmt.Sprintf("Ready in %d minutes or less", s.quick),
		MoreQuery: map[string]string{"maxMinutes": strconv.Itoa(s.quick), "sort": string(SortRecommended)},
	}
	for _, it := range top(s.items,
		func(it *item) bool { return it.idea() && it.cook > 0 && it.cook <= s.quick },
		func(a, b *item) int {
			if c := higher(a.favorite, b.favorite); c != 0 {
				return c
			}
			return cmp.Compare(a.cook, b.cook)
		},
	) {
		sec.Cards = append(sec.Cards, s.card(it, fmt.Sprintf("Ready in %d min", it.cook), BadgeQuick, ""))
	}
	return sec
}

// newToYou: suggestible main meals never ordered or cooked, by taste fit.
func (s *snapshot) newToYou() Section {
	sec := Section{ID: SectionNewToYou, Kind: KindCarousel, Title: "New to You", Subtitle: "Meals you haven't had yet"}
	if s.in.Profile.Configured() {
		sec.Subtitle = "Picked for your taste"
	}
	for _, it := range top(s.items,
		func(it *item) bool { return it.idea() && it.isNew() },
		func(a, b *item) int { return higher(a.fit, b.fit) },
	) {
		sec.Cards = append(sec.Cards, s.card(it, it.fitReason, BadgeNew, ""))
	}
	return sec
}

// ruleSections returns one section per weekday rule, Monday first.
func (s *snapshot) ruleSections() []Section {
	rules := slices.Clone(s.in.Profile.WeekdayRules)
	slices.SortStableFunc(rules, func(a, b recommendations.WeekdayRule) int { return cmp.Compare(dayIndex(a.Day), dayIndex(b.Day)) })
	perDay := map[string]int{}
	var out []Section
	for _, rule := range rules {
		perDay[rule.Day]++
		id := "rule_" + rule.Day
		if n := perDay[rule.Day]; n > 1 {
			id = fmt.Sprintf("%s_%d", id, n)
		}
		out = append(out, s.ruleSection(id, rule))
	}
	return out
}

type ruleMatch struct {
	it       *item
	fraction float64
	bandFit  int
	labels   []string
}

// ruleSection lists suggestible main meals that match a weekday rule as
// Autopilot matches it: each group the rule sets (methods, proteins,
// cuisines or regions, tags) matches when any value does. A recipe qualifies
// when it matches at least half the groups, and the methods group whenever
// the rule has one. A rule with only a time band lists meals in that band.
// Order: groups matched, fitting the time band, favorite score.
func (s *snapshot) ruleSection(id string, rule recommendations.WeekdayRule) Section {
	label := rule.Label
	if label == "" {
		label = dayName(rule.Day)
	}
	sec := Section{ID: id, Kind: KindCarousel, Title: titleWords(label) + " Ideas", Subtitle: "For " + dayName(rule.Day)}
	promote := ""
	if slices.Contains(s.ruleMethods(rule), "smoker") {
		promote = BadgeSmokerFriendly
	}
	var matches []ruleMatch
	for _, it := range s.items {
		if !it.idea() {
			continue
		}
		if m, ok := s.matchRule(it, rule); ok {
			matches = append(matches, m)
		}
	}
	slices.SortFunc(matches, func(a, b ruleMatch) int {
		if c := higher(a.fraction, b.fraction); c != 0 {
			return c
		}
		if c := higher(a.bandFit, b.bandFit); c != 0 {
			return c
		}
		if c := higher(a.it.favorite, b.it.favorite); c != 0 {
			return c
		}
		return byName(a.it, b.it)
	})
	for _, m := range matches[:min(len(matches), sectionLimit)] {
		sec.Cards = append(sec.Cards, s.card(m.it, strings.Join(m.labels[:min(len(m.labels), 2)], " · "), "", promote))
	}
	return sec
}

// ruleMethods are the rule's methods the household actually has equipment
// for. A rule naming a method the household dropped is scored as if it had no
// methods, as Autopilot scores it (docs/autopilot.md#taste-profile).
func (s *snapshot) ruleMethods(rule recommendations.WeekdayRule) []string {
	var out []string
	for _, m := range rule.Methods {
		if slices.Contains(s.in.Profile.Equipment, m) {
			out = append(out, m)
		}
	}
	return out
}

func (s *snapshot) matchRule(it *item, rule recommendations.WeekdayRule) (ruleMatch, bool) {
	m := ruleMatch{it: it}
	groups, matched := 0, 0
	methodsOK := true
	if methods := s.ruleMethods(rule); len(methods) > 0 {
		groups++
		i := slices.IndexFunc(methods, it.attrs.Suits)
		if i >= 0 {
			matched++
			m.labels = append(m.labels, methodName(methods[i]))
		} else {
			methodsOK = false
		}
	}
	for _, g := range []struct {
		have, want []string
		label      func(string) string
	}{
		{it.attrs.Proteins, rule.Proteins, proteinName},
		{it.withRegions, rule.Cuisines, cuisineName},
		{it.attrs.Tags, rule.Tags, titleWords},
	} {
		if len(g.want) == 0 {
			continue
		}
		groups++
		if v := intersect(g.have, g.want); v != "" {
			matched++
			m.labels = append(m.labels, g.label(v))
		}
	}
	switch rule.TimeBand {
	case "long":
		if it.cook > s.medium {
			m.bandFit = 1
			m.labels = append(m.labels, "Long cook")
		}
	case "quick":
		if it.cook > 0 && it.cook <= s.quick {
			m.bandFit = 1
		}
	case "medium":
		if it.cook > 0 && it.cook <= s.medium {
			m.bandFit = 1
		}
	}
	if groups == 0 {
		m.fraction = 1
		return m, rule.TimeBand != "" && m.bandFit == 1
	}
	m.fraction = float64(matched) / float64(groups)
	return m, methodsOK && m.fraction >= 0.5
}

// longCooks: suggestible main meals longer than the medium limit, by
// favorite score.
func (s *snapshot) longCooks() Section {
	sec := Section{ID: SectionLongCooks, Kind: KindCarousel, Title: "Worth the Wait", Subtitle: "Longer cooks for slower days"}
	for _, it := range top(s.items,
		func(it *item) bool { return it.idea() && it.cook > s.medium },
		func(a, b *item) int { return higher(a.favorite, b.favorite) },
	) {
		sec.Cards = append(sec.Cards, s.card(it, fmt.Sprintf("Takes %d min", it.cook), "", ""))
	}
	return sec
}

// sides: add-ons nobody marked never-again, most ordered first, then most
// recently ordered.
func (s *snapshot) sides() Section {
	sec := Section{
		ID: SectionSides, Kind: KindCarousel, Title: "Sides & Add-ons", Subtitle: "Your most-ordered extras",
		MoreQuery: map[string]string{"addons": "true", "sort": string(SortPopular)},
	}
	for _, it := range top(s.items,
		func(it *item) bool { return !it.main() && !it.neverAgain },
		func(a, b *item) int {
			if c := higher(a.recipe.TimesOrdered, b.recipe.TimesOrdered); c != 0 {
				return c
			}
			return higher(a.recipe.LastOrderedWeek, b.recipe.LastOrderedWeek)
		},
	) {
		sec.Cards = append(sec.Cards, s.card(it, historyReason(it), "", ""))
	}
	return sec
}

// historyPlanned: the week's plan entries by day (unscheduled last), one card
// per recipe carrying all its entry IDs. Add-ons are included: it is a record
// of the plan.
func (s *snapshot) historyPlanned() Section {
	sec := Section{ID: SectionHistoryPlanned, Kind: KindHistory, Title: "You Planned", Subtitle: "What was on the plan"}
	entries := slices.Clone(s.in.Plan.Entries)
	slices.SortStableFunc(entries, func(a, b planning.Entry) int { return cmp.Compare(dayIndex(string(a.Day)), dayIndex(string(b.Day))) })
	week := s.in.Week.String()
	seen := map[string]bool{}
	for _, e := range entries {
		it := s.byID[e.RecipeID]
		if it == nil || seen[e.RecipeID] || len(sec.Cards) == sectionLimit {
			continue
		}
		seen[e.RecipeID] = true
		reason := "Planned"
		switch {
		case it.cookedWeeks[week]:
			reason = "Cooked"
		case e.Day != "":
			reason = "Planned for " + dayName(string(e.Day))
		}
		sec.Cards = append(sec.Cards, s.card(it, reason, "", ""))
	}
	return sec
}

// historyOrdered: recipes delivered that week (orderWeeks has it), main meals
// first, then add-ons, each by name.
func (s *snapshot) historyOrdered() Section {
	sec := Section{ID: SectionHistoryOrdered, Kind: KindHistory, Title: "You Ordered", Subtitle: "Delivered that week"}
	week := s.in.Week.String()
	for _, it := range top(s.items,
		func(it *item) bool { return slices.Contains(it.recipe.OrderWeeks, week) },
		func(a, b *item) int { return higher(boolInt(a.main()), boolInt(b.main())) },
	) {
		reason := fmt.Sprintf("Ordered %d times", it.recipe.TimesOrdered)
		if len(it.recipe.OrderWeeks) > 0 && it.recipe.OrderWeeks[0] == week {
			reason = "First time ordered"
		}
		sec.Cards = append(sec.Cards, s.card(it, reason, "", ""))
	}
	return sec
}

// --- reasons ------------------------------------------------------------------

// favoriteReason prefers a high rating, then a tag, then order history.
func favoriteReason(it *item) string {
	if m := it.rating.Mine; m != nil && m.Score >= 4 {
		return fmt.Sprintf("You rated this %d★", m.Score)
	}
	if avg, ok := it.rating.Average(); ok && avg >= 4 {
		return "Rated " + strconv.FormatFloat(math.Round(avg*10)/10, 'f', -1, 64) + "★"
	}
	switch {
	case it.makeAgain:
		return "Marked Make Again"
	case it.kidFavorite:
		return "A kid favorite"
	}
	return historyReason(it)
}

func historyReason(it *item) string {
	switch n := it.recipe.TimesOrdered; {
	case n >= 2:
		return fmt.Sprintf("Ordered %d times", n)
	case it.cooked >= 2:
		return fmt.Sprintf("Cooked %d times", it.cooked)
	case n == 1:
		return "Ordered once"
	case it.cooked == 1:
		return "Cooked once"
	}
	return ""
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
