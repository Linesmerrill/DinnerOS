package autopilot

import (
	"context"
	"errors"
	"time"
)

// RecommendationProvider builds recommendations for one household's week.
// Implementations must be deterministic: the same request and model version
// always produce the same result.
type RecommendationProvider interface {
	// RankMeals ranks the catalog for one day of the week, taking the week's
	// fixed meals into account (variety, cook-time mix, rule frequency). It
	// powers "swap this meal".
	RankMeals(ctx context.Context, req RankRequest) (RankResult, error)
	// GenerateWeek chooses the best set of meals for the week's open days.
	GenerateWeek(ctx context.Context, req WeekRequest) (WeekResult, error)
}

// ErrInvalidRequest wraps a problem with a request. Its message is safe to
// show to callers.
var ErrInvalidRequest = errors.New("autopilot: invalid request")

// Input is what every request carries.
type Input struct {
	// HouseholdID is an opaque identifier, used only to seed tie-breaking.
	HouseholdID string
	// Week is the ISO week being planned ("2026-W38").
	Week string
	// Catalog is every item the household could eat. Order doesn't matter.
	Catalog     []Item
	Preferences Preferences
	Context     WeekContext
	// Ratings are members' current opinions of items.
	Ratings []Rating
	// History is past interactions with items (ordered, planned, cooked,
	// skipped), excluding meals already fixed in this week.
	History []Interaction
	// Fixed are meals already planned for this week. They occupy their days,
	// count toward the week's meals, and take part in variety and cook-time
	// balance. Items must be in Catalog to be considered.
	Fixed []Assignment
	// Avoid are items to steer away from, such as the picks of a proposal
	// that is being regenerated. It is a soft penalty, not a filter.
	Avoid []string
	// Objectives are tenant-supplied business boosts, applied after hard
	// constraints and bounded by MaxObjectiveBoost.
	Objectives []Objective
	// LearningSince, when set, is when the household last reset what the
	// provider learned: interactions before it still feed the standard
	// signals (ratings, recency, conversion) but no learned adjustment.
	LearningSince time.Time
}

// WeekRequest asks for a week.
type WeekRequest struct {
	Input
	// Attempt numbers generations of the same week (1, 2, …) and is part of
	// the tie-breaking seed.
	Attempt int
}

// RankRequest asks for a ranking of meals for one day.
type RankRequest struct {
	Input
	Day Day
	// Exclude are items that must not be returned (the current pick and
	// alternatives already rejected).
	Exclude []string
	// Limit caps the result; 0 means DefaultRankLimit.
	Limit int
}

// Rank limits.
const (
	DefaultRankLimit = 10
	MaxRankLimit     = 50
)

// MaxObjectiveBoost bounds a business objective's effect on a score.
const MaxObjectiveBoost = 0.2

// Reason is a short human-readable explanation of a pick.
type Reason struct {
	// Code is stable and machine-readable ("rating", "rule", "busyWeek").
	Code string
	// Text is for display ("You rated this 5★").
	Text string
}

// Message is a week-level note ("Only 3 quick recipes match").
type Message struct {
	Code string
	Text string
}

// Recommendation is one ranked item for a day.
type Recommendation struct {
	ItemID string
	// Score is the weighted sum of the signals plus week-level adjustments.
	// Higher is better; it isn't bounded to 0–1.
	Score float64
	// Servings is the authored serving size to cook.
	Servings int
	TimeBand TimeBand
	// Signals are the feature values behind the score, by name.
	Signals map[string]float64
	// Reasons explain the pick in words, most important first: at most 3,
	// plus learned and context reasons, which are always shown (at most 5 in
	// all).
	Reasons []Reason
}

// Slot is a day of the week with its chosen meal.
type Slot struct {
	Day Day
	Recommendation
}

// Unfilled is a day the week wanted planned but couldn't fill.
type Unfilled struct {
	Day  Day
	Code string
	Text string
}

// WeekScore breaks down the week objective of a result.
type WeekScore struct {
	// Meals is the sum of the chosen meals' scores.
	Meals float64
	// Variety, CookTime, Rules, and Novelty are the week-level penalties
	// (zero or negative).
	Variety  float64
	CookTime float64
	Rules    float64
	Novelty  float64
	Total    float64
}

// WeekResult is a proposed week.
type WeekResult struct {
	ModelVersion string
	// Seed is the tie-breaking seed derived from the request.
	Seed uint64
	// Requested is how many meals the week calls for (preferences or
	// context), including fixed meals. Planned is how many slots were filled.
	Requested int
	Planned   int
	// Candidates is how many catalog items passed the hard constraints.
	Candidates int
	// ColdStart is true when there is little history, so the taste profile
	// and catalog attributes carry more weight.
	ColdStart bool
	Slots     []Slot
	Unfilled  []Unfilled
	Messages  []Message
	Score     WeekScore
}

// LearningReporter is implemented by providers that learn per-household
// adjustments from interactions and can list them. It is optional: callers
// check for it with a type assertion.
type LearningReporter interface {
	// Learning returns what the provider learned from the input's history,
	// strongest first. Like the rest of the provider it is deterministic.
	Learning(ctx context.Context, in Input) (LearningResult, error)
}

// Learned adjustment kinds.
const (
	LearnedItem         = "item"
	LearnedCuisine      = "cuisine"
	LearnedProtein      = "protein"
	LearnedMealCategory = "mealCategory"
	LearnedTimeBand     = "timeBand"
	// LearnedBusySkips applies only on busy weeks and busy days.
	LearnedBusySkips = "busySkips"
)

// LearnedAdjustment is one thing a provider learned about a household.
type LearnedAdjustment struct {
	// Kind is one of the Learned constants.
	Kind string
	// Key is the item ID for the item and busySkips kinds, otherwise the
	// attribute value ("mexican", "pork", "long").
	Key string
	// Value is -1..1: negative steers away, positive toward.
	Value float64
	// Text explains it ("You swapped this out twice recently").
	Text string
	// Evidence is how many interactions support it.
	Evidence int
}

// LearningResult lists learned adjustments.
type LearningResult struct {
	ModelVersion string
	// Interactions is how many interactions were eligible for learning.
	Interactions int
	Adjustments  []LearnedAdjustment
}

// RankResult is a ranking for one day.
type RankResult struct {
	ModelVersion string
	// Eligible is how many items passed the hard constraints for the day.
	Eligible int
	Items    []Recommendation
	Messages []Message
}
