package autopilot

// Day is a weekday, Monday first as in ISO weeks.
type Day string

// Days of the week.
const (
	Monday    Day = "mon"
	Tuesday   Day = "tue"
	Wednesday Day = "wed"
	Thursday  Day = "thu"
	Friday    Day = "fri"
	Saturday  Day = "sat"
	Sunday    Day = "sun"
)

// Days lists the days in week order.
var Days = []Day{Monday, Tuesday, Wednesday, Thursday, Friday, Saturday, Sunday}

// Index returns the day's position in the week (0 for Monday), or -1.
func (d Day) Index() int {
	for i, known := range Days {
		if known == d {
			return i
		}
	}
	return -1
}

// Valid reports whether d is a known day.
func (d Day) Valid() bool { return d.Index() >= 0 }

// Plural returns the day's plural display name ("Tuesdays").
func (d Day) Plural() string { return d.Name() + "s" }

// Name returns the day's display name ("Tuesday").
func (d Day) Name() string {
	switch d {
	case Monday:
		return "Monday"
	case Tuesday:
		return "Tuesday"
	case Wednesday:
		return "Wednesday"
	case Thursday:
		return "Thursday"
	case Friday:
		return "Friday"
	case Saturday:
		return "Saturday"
	case Sunday:
		return "Sunday"
	}
	return string(d)
}

// Item is a catalog item: something the household can have for dinner.
// String attributes are compared case-insensitively.
type Item struct {
	ID       string
	Cuisines []string
	// CuisineRegions are broader groups the cuisines belong to ("italian" →
	// "southern european", "european"). Likes, dislikes, exclusions, and
	// weekday rules match them too; variety compares only Cuisines.
	CuisineRegions []string
	Tags           []string
	// Proteins are protein codes ("chicken", "pork", "tofu").
	Proteins []string
	// Methods are cooking methods or equipment the item suits ("smoker",
	// "grill"), after any household overrides.
	Methods []string
	// CookMinutes is the effective cook time; 0 means unknown.
	CookMinutes int
	// Servings are the serving sizes the item can be made in.
	Servings []int
	// Allergens are allergen codes the item contains.
	Allergens []string
	// Diets are the diets the item satisfies ("vegetarian").
	Diets []string
	// Ingredients are normalized ingredient names.
	Ingredients []string
	Spicy       bool
}

// Rating is one member's rating of an item.
type Rating struct {
	ItemID   string
	MemberID string
	// Score is 1–5.
	Score int
	// Tags are structured feedback such as FeedbackMakeAgain.
	Tags []string
}

// Feedback tags the baseline understands.
const (
	FeedbackMakeAgain      = "make-again"
	FeedbackNeverAgain     = "never-again"
	FeedbackKidFavorite    = "kid-favorite"
	FeedbackKidsDisliked   = "kids-disliked"
	FeedbackTooSpicy       = "too-spicy"
	FeedbackTooBland       = "too-bland"
	FeedbackTooMuchWork    = "too-much-work"
	FeedbackGreatLeftovers = "great-leftovers"
)

// InteractionKind is what happened with an item.
type InteractionKind string

// Interaction kinds.
const (
	// KindOrdered means the household bought or received the item.
	KindOrdered InteractionKind = "ordered"
	// KindPlanned means the item was on a week plan.
	KindPlanned InteractionKind = "planned"
	KindCooked  InteractionKind = "cooked"
	KindSkipped InteractionKind = "skipped"
)

// Interaction is one past event involving an item.
type Interaction struct {
	ItemID string
	Kind   InteractionKind
	// Week is the ISO week it concerned.
	Week string
	// Day is the weekday, when known (planned meals with a day).
	Day Day
}

// Assignment is a meal fixed on a day of the week being planned. Day is empty
// for a meal planned for the week without a day.
type Assignment struct {
	ItemID string
	Day    Day
}

// Objective is a business boost for one item.
type Objective struct {
	ItemID string
	// Boost is added to the item's score, clamped to ±MaxObjectiveBoost.
	Boost float64
}

// Novelty is the household's appetite for new meals.
type Novelty string

// Novelty levels.
const (
	NoveltyFavorites   Novelty = "favorites"
	NoveltyBalanced    Novelty = "balanced"
	NoveltyAdventurous Novelty = "adventurous"
)

// TimeBand groups meals by effective cook time.
type TimeBand string

// Time bands.
const (
	BandQuick  TimeBand = "quick"
	BandMedium TimeBand = "medium"
	BandLong   TimeBand = "long"
)

// TimeBands are the upper bounds, in minutes, of the quick and medium bands.
// Anything longer is long.
type TimeBands struct {
	QuickMaxMinutes  int
	MediumMaxMinutes int
}

// Default time band bounds.
const (
	DefaultQuickMaxMinutes  = 20
	DefaultMediumMaxMinutes = 35
)

// Of returns the band of a cook time. An unknown time (0) is medium.
func (b TimeBands) Of(minutes int) TimeBand {
	quick, medium := b.QuickMaxMinutes, b.MediumMaxMinutes
	if quick <= 0 {
		quick = DefaultQuickMaxMinutes
	}
	if medium <= quick {
		medium = max(DefaultMediumMaxMinutes, quick)
	}
	switch {
	case minutes <= 0:
		return BandMedium
	case minutes <= quick:
		return BandQuick
	case minutes <= medium:
		return BandMedium
	}
	return BandLong
}

// Attributes are sets of cuisines, tags, and proteins.
type Attributes struct {
	Cuisines []string
	Tags     []string
	Proteins []string
}

// Exclusions are hard constraints chosen by the household.
type Exclusions struct {
	// Ingredients exclude items with an ingredient whose name contains any
	// of these words ("mushroom" excludes "cremini mushrooms").
	Ingredients []string
	Cuisines    []string
	Proteins    []string
	Tags        []string
	// Spicy excludes spicy items.
	Spicy bool
}

// CookTimeMix is the household's preferred balance of cook times in a week.
type CookTimeMix struct {
	Bands TimeBands
	// MaxLongPerWeek is the most long meals wanted in a week; days whose rule
	// allows a long cook don't count. Negative means no limit.
	MaxLongPerWeek int
	// MinQuickPerWeek is the fewest quick meals wanted.
	MinQuickPerWeek int
	// AvoidConsecutiveLong penalizes long meals on back-to-back days.
	AvoidConsecutiveLong bool
}

// RuleFrequency says how often a weekday rule applies.
type RuleFrequency string

// Rule frequencies.
const (
	// EveryWeek applies the rule on its day every week.
	EveryWeek RuleFrequency = "every_week"
	// AtMostOnce prefers the rule on its day, and allows at most one meal
	// matching it per week.
	AtMostOnce RuleFrequency = "at_most_once"
)

// WeekdayRule is a soft preference for one day ("Sunday smoker night: pork
// or chicken, long cook OK").
type WeekdayRule struct {
	Day   Day
	Label string
	// A meal matches the rule when it matches every non-empty group; within
	// a group, any value matches. Partial matches earn partial credit.
	Cuisines []string
	Tags     []string
	Proteins []string
	Methods  []string
	// TimeBand is "" for no preference, quick or medium to prefer meals at or
	// under that band, or long to allow a long cook that day.
	TimeBand  TimeBand
	Frequency RuleFrequency
}

// Preferences are the household's standing preferences.
type Preferences struct {
	// PlanDays are the days Autopilot may plan.
	PlanDays []Day
	// Weeknights are the days WeeknightMaxMinutes applies to.
	Weeknights   []Day
	MealsPerWeek int
	// DefaultServings is how many people a meal is usually for.
	DefaultServings int
	// WeeknightMaxMinutes is a soft cook-time limit on weeknights; 0 means
	// none.
	WeeknightMaxMinutes int
	Likes               Attributes
	Dislikes            Attributes
	Exclusions          Exclusions
	// Diets must all be satisfied (hard).
	Diets []string
	// Allergens must all be absent (hard).
	Allergens []string
	Novelty   Novelty
	// Equipment is the cooking equipment the household has.
	Equipment []string
	Rules     []WeekdayRule
	CookTime  CookTimeMix
}

// WeekContext is what's special about the week being planned.
type WeekContext struct {
	// Skip means nothing should be planned.
	Skip bool
	// Busy prefers quick meals and no long ones, without a hard cap.
	Busy bool
	// Meals overrides Preferences.MealsPerWeek when positive.
	Meals int
	// MaxMinutes is a hard cook-time cap for every day; 0 means none.
	// Items with an unknown cook time don't pass a cap.
	MaxMinutes int
	// Servings overrides Preferences.DefaultServings when positive.
	Servings int
	Days     []DayContext
	// PantryLow are normalized names of ingredients running low at home.
	PantryLow []string
}

// DayContext overrides the week context for one day.
type DayContext struct {
	Day  Day
	Skip bool
	// MaxMinutes is a hard cap for the day (the tighter of it and the week's
	// cap applies).
	MaxMinutes int
	Servings   int
}
