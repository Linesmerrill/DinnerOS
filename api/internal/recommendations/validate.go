package recommendations

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot"
	"github.com/Linesmerrill/DinnerOS/api/internal/events"
)

// Default profile values.
const (
	DefaultMealsPerWeek   = 4
	DefaultMaxLongPerWeek = 2
	DefaultNovelty        = "balanced"
)

// DefaultProfile returns the preferences of a household that hasn't set any.
func DefaultProfile(householdID string) Profile {
	return Profile{
		HouseholdID: householdID,
		Schedule: Schedule{
			PlanDays:     []string{"mon", "tue", "wed", "thu", "fri"},
			Weeknights:   []string{"mon", "tue", "wed", "thu"},
			MealsPerWeek: DefaultMealsPerWeek,
		},
		CookTime: CookTime{
			QuickMaxMinutes: autopilot.DefaultQuickMaxMinutes, MediumMaxMinutes: autopilot.DefaultMediumMaxMinutes,
			MaxLongPerWeek: DefaultMaxLongPerWeek, AvoidConsecutiveLong: true,
		},
		Novelty:  DefaultNovelty,
		Sections: map[Section]Change{},
	}
}

func invalidf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// freeValues normalizes free-text choices (cuisines, tags, ingredients) with
// canonical (canonicalCuisine, canonicalTag, or normalizeValue), then
// deduplicates and sorts them.
func freeValues(field string, values []string, maxCount, maxLength int, canonical func(string) string) ([]string, error) {
	var out []string
	for _, v := range values {
		n := canonical(v)
		switch {
		case n == "":
			continue
		case utf8.RuneCountInString(n) > maxLength:
			return nil, invalidf("%s: %q is longer than %d characters", field, v, maxLength)
		case !slices.Contains(out, n):
			out = append(out, n)
		}
	}
	if len(out) > maxCount {
		return nil, invalidf("%s: at most %d values", field, maxCount)
	}
	slices.Sort(out)
	return out, nil
}

// closedValues checks choices against a fixed list and returns them in the
// list's order.
func closedValues(field string, values []string, options []Option) ([]string, error) {
	allowed := optionValues(options)
	var picked []string
	for _, v := range values {
		n := normalizeValue(v)
		if !slices.Contains(allowed, n) {
			return nil, invalidf("%s: %q must be one of %s", field, v, strings.Join(allowed, ", "))
		}
		picked = append(picked, n)
	}
	var out []string
	for _, a := range allowed {
		if slices.Contains(picked, a) {
			out = append(out, a)
		}
	}
	return out, nil
}

func rangeCheck(field string, v, lo, hi int, zeroOK bool) error {
	if (zeroOK && v == 0) || (v >= lo && v <= hi) {
		return nil
	}
	if zeroOK {
		return invalidf("%s must be between %d and %d, or null", field, lo, hi)
	}
	return invalidf("%s must be between %d and %d", field, lo, hi)
}

// normalizeProfile validates p and puts its lists in canonical order.
func normalizeProfile(p Profile) (Profile, error) {
	var err error
	t := &p.Taste
	for _, c := range []struct {
		name string
		ch   *Choices
	}{{"taste.likes", &t.Likes}, {"taste.dislikes", &t.Dislikes}} {
		if c.ch.Cuisines, err = freeValues(c.name+".cuisines", c.ch.Cuisines, MaxListValues, MaxValueLength, canonicalCuisine); err != nil {
			return Profile{}, err
		}
		if c.ch.Tags, err = freeValues(c.name+".tags", c.ch.Tags, MaxListValues, MaxValueLength, canonicalTag); err != nil {
			return Profile{}, err
		}
		if c.ch.Proteins, err = closedValues(c.name+".proteins", c.ch.Proteins, ProteinOptions); err != nil {
			return Profile{}, err
		}
	}
	for _, pair := range [][2][]string{{t.Likes.Cuisines, t.Dislikes.Cuisines}, {t.Likes.Tags, t.Dislikes.Tags}, {t.Likes.Proteins, t.Dislikes.Proteins}} {
		for _, v := range pair[0] {
			if slices.Contains(pair[1], v) {
				return Profile{}, invalidf("taste: %q can't be both liked and disliked", v)
			}
		}
	}

	r := &p.Restrictions
	if r.Diets, err = closedValues("restrictions.diets", r.Diets, DietOptions); err != nil {
		return Profile{}, err
	}
	if r.Allergens, err = closedValues("restrictions.allergens", r.Allergens, AllergenOptions); err != nil {
		return Profile{}, err
	}
	if r.ExcludedIngredients, err = freeValues("restrictions.excludedIngredients", r.ExcludedIngredients, MaxExcludedIngredient, MaxIngredientLength, normalizeValue); err != nil {
		return Profile{}, err
	}
	if r.ExcludedCuisines, err = freeValues("restrictions.excludedCuisines", r.ExcludedCuisines, MaxListValues, MaxValueLength, canonicalCuisine); err != nil {
		return Profile{}, err
	}
	if r.ExcludedTags, err = freeValues("restrictions.excludedTags", r.ExcludedTags, MaxListValues, MaxValueLength, canonicalTag); err != nil {
		return Profile{}, err
	}
	if r.ExcludedProteins, err = closedValues("restrictions.excludedProteins", r.ExcludedProteins, ProteinOptions); err != nil {
		return Profile{}, err
	}
	for _, v := range t.Likes.Proteins {
		if slices.Contains(r.ExcludedProteins, v) {
			return Profile{}, invalidf("%q can't be both liked and excluded", v)
		}
	}
	for _, v := range t.Likes.Cuisines {
		if slices.Contains(r.ExcludedCuisines, v) {
			return Profile{}, invalidf("%q can't be both liked and excluded", v)
		}
	}

	s := &p.Schedule
	if s.PlanDays, err = closedValues("schedule.planDays", s.PlanDays, DayOptions); err != nil {
		return Profile{}, err
	}
	if len(s.PlanDays) == 0 {
		return Profile{}, invalidf("schedule.planDays must include at least one day")
	}
	if s.Weeknights, err = closedValues("schedule.weeknights", s.Weeknights, DayOptions); err != nil {
		return Profile{}, err
	}
	if err := rangeCheck("schedule.mealsPerWeek", s.MealsPerWeek, 1, len(s.PlanDays), false); err != nil {
		return Profile{}, invalidf("schedule.mealsPerWeek must be between 1 and the number of planDays (%d)", len(s.PlanDays))
	}
	if err := rangeCheck("schedule.defaultServings", s.DefaultServings, 1, MaxServings, true); err != nil {
		return Profile{}, err
	}
	if err := rangeCheck("schedule.weeknightMaxMinutes", s.WeeknightMaxMinutes, MinCookMinutes, MaxCookMinutes, true); err != nil {
		return Profile{}, err
	}

	c := p.CookTime
	switch {
	case c.QuickMaxMinutes < MinCookMinutes || c.QuickMaxMinutes > MaxCookMinutes:
		return Profile{}, invalidf("cookTime.quickMaxMinutes must be between %d and %d", MinCookMinutes, MaxCookMinutes)
	case c.MediumMaxMinutes <= c.QuickMaxMinutes || c.MediumMaxMinutes > MaxCookMinutes:
		return Profile{}, invalidf("cookTime.mediumMaxMinutes must be greater than quickMaxMinutes and at most %d", MaxCookMinutes)
	case c.MaxLongPerWeek < 0 || c.MaxLongPerWeek > 7:
		return Profile{}, invalidf("cookTime.maxLongPerWeek must be between 0 and 7")
	case c.MinQuickPerWeek < 0 || c.MinQuickPerWeek > 7:
		return Profile{}, invalidf("cookTime.minQuickPerWeek must be between 0 and 7")
	}

	if p.Novelty == "" {
		p.Novelty = DefaultNovelty
	}
	if !slices.Contains(optionValues(NoveltyOptions), p.Novelty) {
		return Profile{}, invalidf("novelty must be one of %s", strings.Join(optionValues(NoveltyOptions), ", "))
	}
	if p.Equipment, err = closedValues("equipment", p.Equipment, EquipmentOptions); err != nil {
		return Profile{}, err
	}

	if len(p.WeekdayRules) > 7 {
		return Profile{}, invalidf("weekdayRules: at most one rule per day")
	}
	rules := make([]WeekdayRule, 0, len(p.WeekdayRules))
	for i, rule := range p.WeekdayRules {
		field := fmt.Sprintf("weekdayRules[%d]", i)
		day := normalizeValue(rule.Day)
		if !slices.Contains(optionValues(DayOptions), day) {
			return Profile{}, invalidf("%s.day must be one of mon, tue, wed, thu, fri, sat, sun", field)
		}
		if slices.ContainsFunc(rules, func(r WeekdayRule) bool { return r.Day == day }) {
			return Profile{}, invalidf("weekdayRules: more than one rule for %s", day)
		}
		nr := WeekdayRule{Day: day, Label: strings.Join(strings.Fields(rule.Label), " ")}
		if utf8.RuneCountInString(nr.Label) > MaxLabelLength {
			return Profile{}, invalidf("%s.label must be at most %d characters", field, MaxLabelLength)
		}
		if nr.Cuisines, err = freeValues(field+".cuisines", rule.Cuisines, MaxRuleValues, MaxValueLength, canonicalCuisine); err != nil {
			return Profile{}, err
		}
		if nr.Tags, err = freeValues(field+".tags", rule.Tags, MaxRuleValues, MaxValueLength, canonicalTag); err != nil {
			return Profile{}, err
		}
		if nr.Proteins, err = closedValues(field+".proteins", rule.Proteins, ProteinOptions); err != nil {
			return Profile{}, err
		}
		if nr.Methods, err = closedValues(field+".methods", rule.Methods, EquipmentOptions); err != nil {
			return Profile{}, err
		}
		for _, m := range nr.Methods {
			if !slices.Contains(p.Equipment, m) {
				return Profile{}, invalidf("%s.methods: add %s to equipment first", field, m)
			}
		}
		nr.TimeBand = normalizeValue(rule.TimeBand)
		if nr.TimeBand != "" && !slices.Contains(optionValues(TimeBandOptions), nr.TimeBand) {
			return Profile{}, invalidf("%s.timeBand must be quick, medium, long, or null", field)
		}
		nr.Frequency = normalizeValue(rule.Frequency)
		if nr.Frequency == "" {
			nr.Frequency = string(autopilot.EveryWeek)
		}
		if !slices.Contains(optionValues(FrequencyOptions), nr.Frequency) {
			return Profile{}, invalidf("%s.frequency must be every_week or at_most_once", field)
		}
		if len(nr.Cuisines)+len(nr.Tags)+len(nr.Proteins)+len(nr.Methods) == 0 && nr.TimeBand == "" {
			return Profile{}, invalidf("%s needs at least one cuisine, tag, protein, method, or time band", field)
		}
		if nr.Label == "" {
			nr.Label = optionLabel(DayOptions, day)
		}
		rules = append(rules, nr)
	}
	slices.SortFunc(rules, func(a, b WeekdayRule) int {
		return slices.Index(optionValues(DayOptions), a.Day) - slices.Index(optionValues(DayOptions), b.Day)
	})
	p.WeekdayRules = rules
	if p.Pairings, err = normalizePairingRules(p.Pairings); err != nil {
		return Profile{}, err
	}
	return p, nil
}

// normalizeWeekContext validates a week context.
func normalizeWeekContext(c WeekContext) (WeekContext, error) {
	if err := rangeCheck("mealsPerWeek", c.MealsPerWeek, 1, 7, true); err != nil {
		return WeekContext{}, err
	}
	if err := rangeCheck("maxMinutes", c.MaxMinutes, MinCookMinutes, MaxCookMinutes, true); err != nil {
		return WeekContext{}, err
	}
	if err := rangeCheck("servings", c.Servings, 1, MaxServings, true); err != nil {
		return WeekContext{}, err
	}
	c.Note = strings.TrimSpace(c.Note)
	if utf8.RuneCountInString(c.Note) > MaxNoteLength {
		return WeekContext{}, invalidf("note must be at most %d characters", MaxNoteLength)
	}
	var days []DayOverride
	for i, d := range c.Days {
		field := fmt.Sprintf("days[%d]", i)
		d.Day = normalizeValue(d.Day)
		if !slices.Contains(optionValues(DayOptions), d.Day) {
			return WeekContext{}, invalidf("%s.day must be one of mon, tue, wed, thu, fri, sat, sun", field)
		}
		if slices.ContainsFunc(days, func(o DayOverride) bool { return o.Day == d.Day }) {
			return WeekContext{}, invalidf("days: %s appears more than once", d.Day)
		}
		if err := rangeCheck(field+".maxMinutes", d.MaxMinutes, MinCookMinutes, MaxCookMinutes, true); err != nil {
			return WeekContext{}, err
		}
		if err := rangeCheck(field+".servings", d.Servings, 1, MaxServings, true); err != nil {
			return WeekContext{}, err
		}
		if d.Skip || d.MaxMinutes > 0 || d.Servings > 0 {
			days = append(days, d)
		}
	}
	slices.SortFunc(days, func(a, b DayOverride) int {
		return slices.Index(optionValues(DayOptions), a.Day) - slices.Index(optionValues(DayOptions), b.Day)
	})
	c.Days = days
	return c, nil
}

// --- diffs ----------------------------------------------------------------------

type differ struct {
	changes []events.FieldChange
}

func truncate(s string) string {
	if utf8.RuneCountInString(s) <= 100 {
		return s
	}
	return string([]rune(s)[:99]) + "…"
}

func (d *differ) list(field string, old, next []string) {
	var added, removed []string
	for _, v := range next {
		if !slices.Contains(old, v) {
			added = append(added, truncate(v))
		}
	}
	for _, v := range old {
		if !slices.Contains(next, v) {
			removed = append(removed, truncate(v))
		}
	}
	if len(added)+len(removed) == 0 {
		return
	}
	d.changes = append(d.changes, events.FieldChange{
		Field: field, Added: added[:min(len(added), events.MaxChangeValues)], Removed: removed[:min(len(removed), events.MaxChangeValues)],
	})
}

func (d *differ) scalar(field, old, next string) {
	if old != next {
		d.changes = append(d.changes, events.FieldChange{Field: field, From: truncate(old), To: truncate(next)})
	}
}

func intText(n int) string {
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n)
}

func boolText(b bool) string { return strconv.FormatBool(b) }

func (r WeekdayRule) summary() string {
	parts := []string{r.Label}
	for _, values := range [][]string{r.Proteins, r.Methods, r.Cuisines, r.Tags} {
		if len(values) > 0 {
			parts = append(parts, strings.Join(values, "/"))
		}
	}
	if r.TimeBand != "" {
		parts = append(parts, r.TimeBand)
	}
	return strings.Join(append(parts, r.Frequency), " · ")
}

// diffProfile summarizes how next differs from old, by section.
func diffProfile(old, next Profile) map[Section][]events.FieldChange {
	out := map[Section][]events.FieldChange{}
	add := func(section Section, d *differ) {
		if len(d.changes) > 0 {
			out[section] = d.changes
		}
	}
	var d differ
	d.list("taste.likes.cuisines", old.Taste.Likes.Cuisines, next.Taste.Likes.Cuisines)
	d.list("taste.likes.tags", old.Taste.Likes.Tags, next.Taste.Likes.Tags)
	d.list("taste.likes.proteins", old.Taste.Likes.Proteins, next.Taste.Likes.Proteins)
	d.list("taste.dislikes.cuisines", old.Taste.Dislikes.Cuisines, next.Taste.Dislikes.Cuisines)
	d.list("taste.dislikes.tags", old.Taste.Dislikes.Tags, next.Taste.Dislikes.Tags)
	d.list("taste.dislikes.proteins", old.Taste.Dislikes.Proteins, next.Taste.Dislikes.Proteins)
	add(SectionTaste, &d)

	d = differ{}
	or, nr := old.Restrictions, next.Restrictions
	d.list("restrictions.diets", or.Diets, nr.Diets)
	d.list("restrictions.allergens", or.Allergens, nr.Allergens)
	d.list("restrictions.excludedIngredients", or.ExcludedIngredients, nr.ExcludedIngredients)
	d.list("restrictions.excludedCuisines", or.ExcludedCuisines, nr.ExcludedCuisines)
	d.list("restrictions.excludedProteins", or.ExcludedProteins, nr.ExcludedProteins)
	d.list("restrictions.excludedTags", or.ExcludedTags, nr.ExcludedTags)
	d.scalar("restrictions.noSpicy", boolText(or.NoSpicy), boolText(nr.NoSpicy))
	add(SectionRestrictions, &d)

	d = differ{}
	os, ns := old.Schedule, next.Schedule
	d.list("schedule.planDays", os.PlanDays, ns.PlanDays)
	d.list("schedule.weeknights", os.Weeknights, ns.Weeknights)
	d.scalar("schedule.mealsPerWeek", intText(os.MealsPerWeek), intText(ns.MealsPerWeek))
	d.scalar("schedule.defaultServings", intText(os.DefaultServings), intText(ns.DefaultServings))
	d.scalar("schedule.weeknightMaxMinutes", intText(os.WeeknightMaxMinutes), intText(ns.WeeknightMaxMinutes))
	add(SectionSchedule, &d)

	d = differ{}
	oc, nc := old.CookTime, next.CookTime
	d.scalar("cookTime.quickMaxMinutes", intText(oc.QuickMaxMinutes), intText(nc.QuickMaxMinutes))
	d.scalar("cookTime.mediumMaxMinutes", intText(oc.MediumMaxMinutes), intText(nc.MediumMaxMinutes))
	d.scalar("cookTime.maxLongPerWeek", strconv.Itoa(oc.MaxLongPerWeek), strconv.Itoa(nc.MaxLongPerWeek))
	d.scalar("cookTime.minQuickPerWeek", strconv.Itoa(oc.MinQuickPerWeek), strconv.Itoa(nc.MinQuickPerWeek))
	d.scalar("cookTime.avoidConsecutiveLong", boolText(oc.AvoidConsecutiveLong), boolText(nc.AvoidConsecutiveLong))
	add(SectionCookTime, &d)

	d = differ{}
	d.scalar("novelty", old.Novelty, next.Novelty)
	add(SectionNovelty, &d)

	d = differ{}
	d.list("equipment", old.Equipment, next.Equipment)
	add(SectionEquipment, &d)

	d = differ{}
	for _, day := range optionValues(DayOptions) {
		var o, n string
		for _, r := range old.WeekdayRules {
			if r.Day == day {
				o = r.summary()
			}
		}
		for _, r := range next.WeekdayRules {
			if r.Day == day {
				n = r.summary()
			}
		}
		d.scalar("weekdayRules."+day, o, n)
	}
	add(SectionWeekdayRules, &d)

	if changes := diffPairingRules(old.Pairings, next.Pairings); len(changes) > 0 {
		out[SectionPairings] = changes
	}
	return out
}

func (o DayOverride) summary() string {
	var parts []string
	if o.Skip {
		parts = append(parts, "skip")
	}
	if o.MaxMinutes > 0 {
		parts = append(parts, fmt.Sprintf("≤%d min", o.MaxMinutes))
	}
	if o.Servings > 0 {
		parts = append(parts, fmt.Sprintf("serves %d", o.Servings))
	}
	return strings.Join(parts, ", ")
}

// diffWeekContext summarizes how next differs from old.
func diffWeekContext(old, next WeekContext) []events.FieldChange {
	var d differ
	d.scalar("skip", boolText(old.Skip), boolText(next.Skip))
	d.scalar("busy", boolText(old.Busy), boolText(next.Busy))
	d.scalar("mealsPerWeek", intText(old.MealsPerWeek), intText(next.MealsPerWeek))
	d.scalar("maxMinutes", intText(old.MaxMinutes), intText(next.MaxMinutes))
	d.scalar("servings", intText(old.Servings), intText(next.Servings))
	for _, day := range optionValues(DayOptions) {
		var o, n string
		for _, x := range old.Days {
			if x.Day == day {
				o = x.summary()
			}
		}
		for _, x := range next.Days {
			if x.Day == day {
				n = x.summary()
			}
		}
		d.scalar("days."+day, o, n)
	}
	d.scalar("note", old.Note, next.Note)
	return d.changes
}

// --- provider inputs ------------------------------------------------------------

func days(values []string) []autopilot.Day {
	out := make([]autopilot.Day, 0, len(values))
	for _, v := range values {
		out = append(out, autopilot.Day(v))
	}
	return out
}

func (p Profile) bands() autopilot.TimeBands {
	return autopilot.TimeBands{QuickMaxMinutes: p.CookTime.QuickMaxMinutes, MediumMaxMinutes: p.CookTime.MediumMaxMinutes}
}

// preferences converts a profile for the provider. householdServings is the
// household's default servings.
func (p Profile) preferences(householdServings int) autopilot.Preferences {
	servings := p.Schedule.DefaultServings
	if servings == 0 {
		servings = householdServings
	}
	maxLong := p.CookTime.MaxLongPerWeek
	if maxLong >= 7 {
		maxLong = -1
	}
	out := autopilot.Preferences{
		PlanDays: days(p.Schedule.PlanDays), Weeknights: days(p.Schedule.Weeknights),
		MealsPerWeek: p.Schedule.MealsPerWeek, DefaultServings: servings, WeeknightMaxMinutes: p.Schedule.WeeknightMaxMinutes,
		Likes:    autopilot.Attributes{Cuisines: p.Taste.Likes.Cuisines, Tags: p.Taste.Likes.Tags, Proteins: p.Taste.Likes.Proteins},
		Dislikes: autopilot.Attributes{Cuisines: p.Taste.Dislikes.Cuisines, Tags: p.Taste.Dislikes.Tags, Proteins: p.Taste.Dislikes.Proteins},
		Exclusions: autopilot.Exclusions{
			Ingredients: p.Restrictions.ExcludedIngredients, Cuisines: p.Restrictions.ExcludedCuisines,
			Proteins: p.Restrictions.ExcludedProteins, Tags: p.Restrictions.ExcludedTags, Spicy: p.Restrictions.NoSpicy,
		},
		Diets: p.Restrictions.Diets, Allergens: p.Restrictions.Allergens,
		Novelty: autopilot.Novelty(p.Novelty), Equipment: p.Equipment,
		CookTime: autopilot.CookTimeMix{
			Bands: p.bands(), MaxLongPerWeek: maxLong, MinQuickPerWeek: p.CookTime.MinQuickPerWeek,
			AvoidConsecutiveLong: p.CookTime.AvoidConsecutiveLong,
		},
	}
	if out.Weeknights == nil {
		out.Weeknights = []autopilot.Day{}
	}
	for _, r := range p.WeekdayRules {
		out.Rules = append(out.Rules, autopilot.WeekdayRule{
			Day: autopilot.Day(r.Day), Label: r.Label, Cuisines: r.Cuisines, Tags: r.Tags, Proteins: r.Proteins,
			Methods: r.Methods, TimeBand: autopilot.TimeBand(r.TimeBand), Frequency: autopilot.RuleFrequency(r.Frequency),
		})
	}
	return out
}

// providerContext converts a week context for the provider.
func (c WeekContext) providerContext(pantryLow []string) autopilot.WeekContext {
	out := autopilot.WeekContext{
		Skip: c.Skip, Busy: c.Busy, Meals: c.MealsPerWeek, MaxMinutes: c.MaxMinutes, Servings: c.Servings, PantryLow: pantryLow,
	}
	for _, d := range c.Days {
		out.Days = append(out.Days, autopilot.DayContext{Day: autopilot.Day(d.Day), Skip: d.Skip, MaxMinutes: d.MaxMinutes, Servings: d.Servings})
	}
	return out
}
