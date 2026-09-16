package baseline

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"math"
	"slices"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot"
)

// Learning from feedback (docs/autopilot.md#learning-from-feedback).
//
// The household's responses to Autopilot and to its own plans become small,
// bounded adjustments, recomputed from the request's history on every call:
//
//   - per meal: swapped out or left out (away), accepted or looked at
//     (toward);
//   - per cuisine, protein, meal category, and cook-time band: how the
//     household's cooks, skips, swaps, rejections, accepts, and ratings of
//     meals with that attribute compare with its usual response;
//   - skipped on busy weeks: meals skipped at least twice on a busy week, or
//     for lack of time, steer away on busy weeks and busy days only.
//
// Evidence decays with a half-life of learnHalfLifeWeeks and is ignored after
// learnWindowWeeks. Every adjustment that moves a score is named in the meal's
// explanation, and the total is bounded twice: by the Learned weight, and so
// that it can shrink but never reverse the household's stated likes and
// dislikes. Hard constraints are applied before scoring, so learning can't
// reintroduce a meal they removed.
const (
	learnWindowWeeks   = 26
	learnHalfLifeWeeks = 8.0
	// A feature needs this much evidence, and to differ this much from the
	// household's usual response, to be learned.
	minFeatureEvidence = 3
	minFeatureAffinity = 0.15
	// featureShrink pulls affinities with little evidence toward zero:
	// affinity × n/(n+featureShrink).
	featureShrink = 4.0
	// busySkipsNeeded is how many busy-week skips make a meal "often skipped
	// on busy weeks".
	busySkipsNeeded = 2
	// minLearnedContribution is the smallest score change a learned component
	// makes; smaller ones are dropped rather than applied without a reason.
	minLearnedContribution = 0.005

	learnedItemShare    = 0.6
	learnedFeatureShare = 0.4
	learnedBusyPenalty  = -0.6
)

// itemLearning is what was learned about one meal.
type itemLearning struct {
	// value is -1..1 from swaps, rejections, accepts, and views.
	value                          float64
	swaps, rejects, accepts, views int
	busySkips                      int
}

// featureLearning is a learned affinity for one attribute value.
type featureLearning struct {
	kind, key string
	affinity  float64
	evidence  int
}

type learning struct {
	interactions int
	items        map[string]*itemLearning
	// features by kind+"\x00"+key.
	features map[string]*featureLearning
}

func featureID(kind, key string) string { return kind + "\x00" + key }

// itemFeatures lists the attribute values learning is kept for.
func itemFeatures(it *item, fn func(kind, key string)) {
	for _, c := range it.cuisines {
		fn(autopilot.LearnedCuisine, c)
	}
	for _, p := range it.proteins {
		fn(autopilot.LearnedProtein, p)
	}
	for _, c := range it.categories {
		fn(autopilot.LearnedMealCategory, c)
	}
	if it.minutes > 0 {
		fn(autopilot.LearnedTimeBand, string(it.band))
	}
}

// learn derives the household's learned adjustments from history. It sorts
// the interactions it uses, so float sums don't depend on input order.
func (m *model) learn(history []autopilot.Interaction, since time.Time) {
	l := learning{items: map[string]*itemLearning{}, features: map[string]*featureLearning{}}
	m.learned = l
	if m.w.Learned == 0 {
		return
	}
	var used []autopilot.Interaction
	for _, h := range history {
		switch h.Kind {
		case autopilot.KindCooked, autopilot.KindSkipped, autopilot.KindSwappedOut, autopilot.KindRejected,
			autopilot.KindAccepted, autopilot.KindViewed, autopilot.KindRated:
		default:
			continue
		}
		if !since.IsZero() && (h.At.IsZero() || h.At.Before(since)) {
			continue
		}
		if m.byID[h.ItemID] == nil {
			continue
		}
		used = append(used, h)
	}
	slices.SortFunc(used, func(a, b autopilot.Interaction) int {
		return cmp.Or(cmp.Compare(a.ItemID, b.ItemID), cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.Week, b.Week),
			a.At.Compare(b.At), cmp.Compare(a.Reason, b.Reason), cmp.Compare(a.Score, b.Score))
	})

	type acc struct {
		sum, weight float64
		n           int
	}
	features := map[string]*acc{}
	var global acc
	sums := map[string]float64{}
	viewSums := map[string]float64{}
	for _, h := range used {
		between, ok := autopilot.WeeksBetween(m.week, h.Week)
		if !ok || abs(between) > learnWindowWeeks {
			continue
		}
		decay := math.Pow(0.5, float64(abs(between))/learnHalfLifeWeeks)
		it := m.byID[h.ItemID]
		il := l.items[it.id]
		if il == nil {
			il = &itemLearning{}
			l.items[it.id] = il
		}
		value, feature := 0.0, true
		switch h.Kind {
		case autopilot.KindCooked:
			value = 1
		case autopilot.KindSkipped:
			if h.Busy || norm(h.Reason) == autopilot.SkipNoTime {
				il.busySkips++
			}
			if !skipAboutTheMeal(h.Reason) {
				feature = false
			}
			value = -1
		case autopilot.KindSwappedOut:
			value = -1
			il.swaps++
			sums[it.id] -= decay
		case autopilot.KindRejected:
			value = -1
			il.rejects++
			sums[it.id] -= decay
		case autopilot.KindAccepted:
			value = 0.5
			il.accepts++
			sums[it.id] += 0.5 * decay
		case autopilot.KindViewed:
			feature = false
			il.views++
			viewSums[it.id] += 0.1 * decay
		case autopilot.KindRated:
			if h.Score < 1 || h.Score > 5 {
				continue
			}
			value = float64(h.Score-3) / 2
		}
		l.interactions++
		if !feature {
			continue
		}
		global.sum += value * decay
		global.weight += decay
		global.n++
		itemFeatures(it, func(kind, key string) {
			a := features[featureID(kind, key)]
			if a == nil {
				a = &acc{}
				features[featureID(kind, key)] = a
			}
			a.sum += value * decay
			a.weight += decay
			a.n++
		})
	}
	for id, il := range l.items {
		il.value = math.Tanh((sums[id] + math.Min(viewSums[id], 0.3)) / 1.5)
	}
	if global.weight > 0 {
		usual := global.sum / global.weight
		for id, a := range features {
			if a.n < minFeatureEvidence || a.weight == 0 {
				continue
			}
			affinity := clamp((a.sum/a.weight - usual) * float64(a.n) / (float64(a.n) + featureShrink))
			if math.Abs(affinity) < minFeatureAffinity {
				continue
			}
			var kind, key string
			for i := range len(id) {
				if id[i] == 0 {
					kind, key = id[:i], id[i+1:]
					break
				}
			}
			l.features[id] = &featureLearning{kind: kind, key: key, affinity: round3(affinity), evidence: a.n}
		}
	}
	m.learned = l
}

// stated reports whether the household stated a like or dislike for the
// feature. Stated preferences win, so the feature isn't learned on top.
func (m *model) stated(kind, key string) bool {
	var lists [][]string
	switch kind {
	case autopilot.LearnedCuisine:
		lists = [][]string{m.prefs.likes.cuisines, m.prefs.dislikes.cuisines}
	case autopilot.LearnedProtein:
		lists = [][]string{m.prefs.likes.proteins, m.prefs.dislikes.proteins}
	}
	return slices.ContainsFunc(lists, func(list []string) bool { return slices.Contains(list, key) })
}

// busyContext reports whether a day is busy: a busy week's weeknight, or a day
// whose evening is known to be busy.
func (m *model) busyContext(day int) bool {
	return m.busyOn(day) || m.ctx.days[day].sig.busyness == autopilot.BusynessBusy
}

// learnedComponent is one part of a meal's learned adjustment.
type learnedComponent struct {
	code, text string
	value      float64 // before the Learned weight
}

// learnedFor returns the learned signal (-1..1) for a meal on a slot and the
// reasons that explain it, given the meal's taste contribution: learning may
// shrink a stated like or dislike by at most half, never reverse it.
func (m *model) learnedFor(s *slot, it *item, tasteContribution float64) (float64, []reason) {
	w := m.w.Learned
	if w == 0 {
		return 0, nil
	}
	var parts []learnedComponent
	if il := m.learned.items[it.id]; il != nil {
		if v := learnedItemShare * il.value; math.Abs(v*w) >= minLearnedContribution {
			parts = append(parts, learnedComponent{code: "learnedMeal", text: itemText(il, v), value: v})
		}
		if il.busySkips >= busySkipsNeeded && m.busyContext(s.day) {
			parts = append(parts, learnedComponent{code: "busySkips", text: "Often skipped on busy weeks", value: learnedBusyPenalty})
		}
	}
	var sum float64
	var n int
	var strongest *featureLearning
	itemFeatures(it, func(kind, key string) {
		f := m.learned.features[featureID(kind, key)]
		if f == nil || m.stated(kind, key) {
			return
		}
		sum += f.affinity
		n++
		if strongest == nil || math.Abs(f.affinity) > math.Abs(strongest.affinity) {
			strongest = f
		}
	})
	if n > 0 {
		if v := learnedFeatureShare * sum / float64(n); math.Abs(v*w) >= minLearnedContribution {
			parts = append(parts, learnedComponent{code: "learnedTaste", text: featureText(strongest.kind, strongest.key, v), value: v})
		}
	}
	if len(parts) == 0 {
		return 0, nil
	}
	total := 0.0
	for _, p := range parts {
		total += p.value
	}
	value := clamp(total)
	// Stated preferences win: learning can take back at most half of what
	// the taste profile gave or took.
	contribution := value * w
	switch {
	case tasteContribution > 0 && contribution < -tasteContribution/2:
		contribution = -tasteContribution / 2
	case tasteContribution < 0 && contribution > -tasteContribution/2:
		contribution = -tasteContribution / 2
	}
	scale := 1.0
	if total != 0 {
		scale = contribution / (total * w)
	}
	slices.SortStableFunc(parts, func(a, b learnedComponent) int {
		return cmp.Or(cmp.Compare(math.Abs(b.value), math.Abs(a.value)), cmp.Compare(a.code, b.code))
	})
	var reasons []reason
	for _, p := range parts[:min(len(parts), 2)] {
		reasons = append(reasons, reason{code: p.code, text: p.text, value: p.value * w * scale, forced: true})
	}
	return contribution / w, reasons
}

func times(n int) string {
	switch n {
	case 1:
		return "once"
	case 2:
		return "twice"
	}
	return fmt.Sprintf("%d times", n)
}

func itemText(il *itemLearning, value float64) string {
	switch {
	case value < 0 && il.swaps > 0 && il.rejects > 0:
		return "You swapped this out " + times(il.swaps) + " and left it out " + times(il.rejects) + " recently"
	case value < 0 && il.swaps >= il.rejects:
		return "You swapped this out " + times(il.swaps) + " recently"
	case value < 0:
		return "You left this out " + times(il.rejects) + " recently"
	case il.accepts == 1:
		return "You kept it when Autopilot suggested it"
	case il.accepts > 1:
		return fmt.Sprintf("You kept it the last %d times Autopilot suggested it", il.accepts)
	}
	return fmt.Sprintf("You've looked at it %s lately", times(il.views))
}

func featureLabel(kind, key string) string {
	if kind == autopilot.LearnedTimeBand {
		switch autopilot.TimeBand(key) {
		case autopilot.BandQuick:
			return "quick meals"
		case autopilot.BandLong:
			return "long cooks"
		}
		return "medium-length cooks"
	}
	if kind == autopilot.LearnedMealCategory {
		return key
	}
	return displayName(key)
}

func featureText(kind, key string, value float64) string {
	if value < 0 {
		return "Lately you pass on " + featureLabel(kind, key)
	}
	return "Lately you favor " + featureLabel(kind, key)
}

// Learning implements autopilot.LearningReporter.
func (p *Provider) Learning(ctx context.Context, in autopilot.Input) (autopilot.LearningResult, error) {
	if err := ctx.Err(); err != nil {
		return autopilot.LearningResult{}, err
	}
	m, err := p.prepare(in, 0)
	if err != nil {
		return autopilot.LearningResult{}, err
	}
	res := autopilot.LearningResult{ModelVersion: m.version, Interactions: m.learned.interactions}
	for _, id := range slices.Sorted(maps.Keys(m.learned.items)) {
		il := m.learned.items[id]
		if v := learnedItemShare * il.value; math.Abs(v*m.w.Learned) >= minLearnedContribution {
			res.Adjustments = append(res.Adjustments, autopilot.LearnedAdjustment{
				Kind: autopilot.LearnedItem, Key: id, Value: round3(il.value), Text: itemText(il, v), Evidence: il.swaps + il.rejects + il.accepts + il.views,
			})
		}
		if il.busySkips >= busySkipsNeeded {
			res.Adjustments = append(res.Adjustments, autopilot.LearnedAdjustment{
				Kind: autopilot.LearnedBusySkips, Key: id, Value: learnedBusyPenalty, Text: "Often skipped on busy weeks", Evidence: il.busySkips,
			})
		}
	}
	for _, f := range m.learned.features {
		if m.stated(f.kind, f.key) {
			continue
		}
		res.Adjustments = append(res.Adjustments, autopilot.LearnedAdjustment{
			Kind: f.kind, Key: f.key, Value: f.affinity, Text: featureText(f.kind, f.key, f.affinity), Evidence: f.evidence,
		})
	}
	slices.SortFunc(res.Adjustments, func(a, b autopilot.LearnedAdjustment) int {
		return cmp.Or(cmp.Compare(math.Abs(b.Value), math.Abs(a.Value)), cmp.Compare(b.Evidence, a.Evidence),
			cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.Key, b.Key))
	})
	return res, nil
}
