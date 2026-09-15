package baseline

// Signal names. Every recommendation carries the meal signals; the week
// signals appear when the week objective adjusted the pick.
const (
	// SignalRating is the household's mean rating mapped to -1..1.
	SignalRating = "rating"
	// SignalFeedback is structured rating feedback (make-again, kid favorite,
	// too much work, …) in -1..1.
	SignalFeedback = "feedback"
	// SignalFamiliarity is how often the meal was ordered, planned, or
	// cooked, log-scaled to 0..1.
	SignalFamiliarity = "familiarity"
	// SignalConversion is how reliably a planned meal gets cooked rather
	// than skipped, in -1..1.
	SignalConversion = "conversion"
	// SignalRecency penalizes meals had recently (-1..0) and gives a small
	// boost to familiar meals not had in a long time.
	SignalRecency = "recency"
	// SignalWeekdayAffinity is the share of the meal's planned days that
	// were this weekday, 0..1.
	SignalWeekdayAffinity = "weekdayAffinity"
	// SignalTaste is liked and disliked cuisines, tags, and proteins, in
	// -1..1.
	SignalTaste = "taste"
	// SignalRule is how well the meal matches the day's weekday rule, in
	// -1..1.
	SignalRule = "rule"
	// SignalTimeFit is how the cook time fits the day's soft limit or
	// long-cook allowance, in -1..1.
	SignalTimeFit = "timeFit"
	// SignalNovelty reflects the household's appetite for new meals, -1..1.
	SignalNovelty = "novelty"
	// SignalServingsFit penalizes meals that can't serve enough people,
	// -1..0.
	SignalServingsFit = "servingsFit"
	// SignalPantry favors meals that use ingredients running low, 0..1.
	SignalPantry = "pantry"
	// SignalAvoid penalizes meals the caller asked to steer away from, -1..0.
	SignalAvoid = "avoid"
	// SignalObjective is the bounded business boost, when present.
	SignalObjective = "objective"

	// SignalVariety is the week penalty for repeating cuisines and proteins.
	SignalVariety = "variety"
	// SignalCookTimeMix is the week penalty for too many long meals,
	// back-to-back long meals, or too few quick meals.
	SignalCookTimeMix = "cookTimeMix"
	// SignalRuleFrequency is the week penalty for a second meal matching an
	// at-most-once rule.
	SignalRuleFrequency = "ruleFrequency"
	// SignalNoveltyBudget is the week penalty for new meals beyond the
	// household's novelty budget.
	SignalNoveltyBudget = "noveltyBudget"
)

// Weights tune the baseline. A meal's score is the sum of each signal times
// its weight; a week's objective adds the week penalties. Change weights
// together with ModelVersion, so proposals and events record which weights
// produced them (docs/autopilot.md#tuning).
type Weights struct {
	// Meal signals.
	Rating          float64
	Feedback        float64
	Familiarity     float64
	Conversion      float64
	Recency         float64
	WeekdayAffinity float64
	Taste           float64
	// ColdStartTasteBoost scales Taste up when history is thin: the taste
	// weight is Taste × (1 + ColdStartTasteBoost × (1 − confidence)).
	ColdStartTasteBoost float64
	Rule                float64
	TimeFit             float64
	Novelty             float64
	ServingsFit         float64
	Pantry              float64
	Avoid               float64

	// Week objective penalties.
	// CuisineRepeat and ProteinRepeat apply per pair of the week's meals
	// sharing a cuisine or a protein.
	CuisineRepeat float64
	ProteinRepeat float64
	// ExtraLong applies per long meal beyond the week's allowance.
	ExtraLong float64
	// ConsecutiveLong applies per pair of long meals on adjacent days.
	ConsecutiveLong float64
	// MissingQuick applies per quick meal the week can no longer fit to
	// reach its minimum.
	MissingQuick float64
	// RuleRepeat applies per extra meal matching an at-most-once rule.
	RuleRepeat float64
	// NoveltyBudget applies per new meal beyond the novelty budget.
	NoveltyBudget float64
	// Unfilled applies per open day left without a meal, so the search
	// prefers filling every day.
	Unfilled float64
}

// DefaultWeights returns the weights of ModelVersion.
func DefaultWeights() Weights {
	return Weights{
		Rating:              0.30,
		Feedback:            0.20,
		Familiarity:         0.10,
		Conversion:          0.10,
		Recency:             0.25,
		WeekdayAffinity:     0.10,
		Taste:               0.25,
		ColdStartTasteBoost: 1.0,
		Rule:                0.45,
		TimeFit:             0.20,
		Novelty:             0.15,
		ServingsFit:         0.25,
		Pantry:              0.05,
		Avoid:               0.30,

		CuisineRepeat:   0.20,
		ProteinRepeat:   0.15,
		ExtraLong:       0.35,
		ConsecutiveLong: 0.25,
		MissingQuick:    0.35,
		RuleRepeat:      0.40,
		NoveltyBudget:   0.20,
		Unfilled:        1.0,
	}
}
