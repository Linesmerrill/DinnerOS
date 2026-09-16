package recommendations

import (
	"errors"
	"time"
)

// Errors returned by the service and stores.
var (
	// ErrNotFound means a proposal, slot, or recipe does not exist.
	ErrNotFound = errors.New("recommendations: not found")
	// ErrInvalid wraps a validation problem. Its message, without the
	// package prefix, is safe to show clients.
	ErrInvalid = errors.New("recommendations: invalid input")
	// ErrConflict means a stored document changed concurrently (stores) or
	// kept changing after retries (service).
	ErrConflict = errors.New("recommendations: concurrent change")
	// ErrProposalChanged means the client's proposal version is stale.
	ErrProposalChanged = errors.New("recommendations: proposal changed")
	// ErrProposalNotPending means the proposal was already accepted or
	// rejected.
	ErrProposalNotPending = errors.New("recommendations: proposal is not pending")
	// ErrNoAlternative means no other recipe fits a slot.
	ErrNoAlternative = errors.New("recommendations: no alternative")
	// ErrNothingToAccept means every meal was excluded or no longer fits.
	ErrNothingToAccept = errors.New("recommendations: nothing to accept")
	// ErrProposalStale means the proposal's recipes changed since it was
	// generated (removed, or a serving size is gone).
	ErrProposalStale = errors.New("recommendations: proposal is stale")
	// ErrPlanFinalized means the week's plan is finalized.
	ErrPlanFinalized = errors.New("recommendations: plan is finalized")
)

// Section is a part of the taste profile that is edited and tracked as a
// whole.
type Section string

// Profile sections.
const (
	SectionTaste        Section = "taste"
	SectionRestrictions Section = "restrictions"
	SectionSchedule     Section = "schedule"
	SectionCookTime     Section = "cookTime"
	SectionNovelty      Section = "novelty"
	SectionEquipment    Section = "equipment"
	SectionWeekdayRules Section = "weekdayRules"
	SectionPairings     Section = "pairings"
)

// Sections lists every section in display order.
var Sections = []Section{
	SectionTaste, SectionRestrictions, SectionSchedule, SectionCookTime, SectionNovelty, SectionEquipment, SectionWeekdayRules,
	SectionPairings,
}

// Change records who last changed something and when.
type Change struct {
	UpdatedBy string
	UpdatedAt time.Time
}

// Profile is a household's Autopilot preferences. There is at most one per
// household; a household that never set one reads the defaults.
type Profile struct {
	HouseholdID  string
	Taste        Taste
	Restrictions Restrictions
	Schedule     Schedule
	CookTime     CookTime
	Novelty      string
	Equipment    []string
	WeekdayRules []WeekdayRule
	// Pairings are the household's add-on pairing rules (pairings_rules.go).
	Pairings []PairingRule
	// Sections records who last changed each section and when. A section
	// that was never set is absent.
	Sections map[Section]Change
	// Version increases with every write. Zero means not stored.
	Version   int64
	CreatedBy string
	CreatedAt time.Time
	UpdatedBy string
	UpdatedAt time.Time
}

// Configured reports whether the household has saved a profile.
func (p Profile) Configured() bool { return p.Version > 0 }

// Taste is what the household likes and dislikes (soft preferences).
type Taste struct {
	Likes    Choices
	Dislikes Choices
}

// Choices are cuisines, tags (food types), and proteins.
type Choices struct {
	Cuisines []string
	Tags     []string
	Proteins []string
}

// Restrictions are hard constraints: a recipe that violates one is never
// suggested.
type Restrictions struct {
	Diets               []string
	Allergens           []string
	ExcludedIngredients []string
	ExcludedCuisines    []string
	ExcludedProteins    []string
	ExcludedTags        []string
	NoSpicy             bool
}

// Schedule is when and how much Autopilot plans.
type Schedule struct {
	PlanDays     []string
	Weeknights   []string
	MealsPerWeek int
	// DefaultServings is 0 to use the household's default servings.
	DefaultServings int
	// WeeknightMaxMinutes is a soft cook-time limit on weeknights; 0 means
	// none.
	WeeknightMaxMinutes int
}

// CookTime is the household's time bands and preferred weekly mix.
type CookTime struct {
	QuickMaxMinutes  int
	MediumMaxMinutes int
	// MaxLongPerWeek is 0–7; 7 means no limit.
	MaxLongPerWeek       int
	MinQuickPerWeek      int
	AvoidConsecutiveLong bool
}

// WeekdayRule is a soft preference for one day of the week.
type WeekdayRule struct {
	Day      string
	Label    string
	Cuisines []string
	Tags     []string
	Proteins []string
	// Methods are cooking methods or equipment; each must be in the
	// profile's Equipment.
	Methods []string
	// TimeBand is "" (no preference), quick, medium, or long (long cook OK).
	TimeBand  string
	Frequency string
}

// WeekContext is what's special about one ISO week.
type WeekContext struct {
	HouseholdID  string
	Week         string
	Skip         bool
	Busy         bool
	MealsPerWeek int
	MaxMinutes   int
	Servings     int
	Days         []DayOverride
	// Note is free text for the household; V1 stores it without
	// interpreting it.
	Note      string
	Version   int64
	UpdatedBy string
	UpdatedAt time.Time
}

// Configured reports whether the week's context was saved.
func (c WeekContext) Configured() bool { return c.Version > 0 }

// DayOverride changes the week context for one day.
type DayOverride struct {
	Day        string
	Skip       bool
	MaxMinutes int
	Servings   int
}

// ProposalStatus is a proposal's state.
type ProposalStatus string

// Proposal statuses.
const (
	StatusProposed ProposalStatus = "proposed"
	StatusAccepted ProposalStatus = "accepted"
	StatusRejected ProposalStatus = "rejected"
)

// Proposal is an Autopilot week. There is at most one per household and week:
// generating again replaces it.
type Proposal struct {
	ID           string
	HouseholdID  string
	Week         string
	Status       ProposalStatus
	Version      int64
	Attempt      int
	ModelVersion string
	// InputsHash fingerprints the provider request, so identical inputs are
	// recognizable.
	InputsHash string
	Requested  int
	Planned    int
	Candidates int
	ColdStart  bool
	Slots      []Slot
	Unfilled   []Unfilled
	Messages   []Message
	Objective  Objective
	// Context is the season, holidays, order date, and device signals the
	// week was planned with.
	Context   ProposalContext
	SwapCount int
	// ExcludedSlots are the slots left out when the proposal was accepted.
	ExcludedSlots []string
	GeneratedBy   string
	GeneratedAt   time.Time
	UpdatedAt     time.Time
	DecidedBy     string
	DecidedAt     time.Time
}

// slot returns the index of the slot with id, or -1.
func (p Proposal) slot(id string) int {
	for i, s := range p.Slots {
		if s.ID == id {
			return i
		}
	}
	return -1
}

// Slot is one proposed meal. Its ID is its day code, since a proposal has at
// most one meal per day.
type Slot struct {
	ID             string
	Day            string
	RecipeID       string
	RecipeName     string
	RecipeImageURL string
	CookMinutes    int
	TimeBand       string
	Servings       int
	Score          float64
	Signals        map[string]float64
	Reasons        []Reason
	SwapCount      int
	// RejectedRecipeIDs were swapped out of this slot, most recent last.
	RejectedRecipeIDs []string
	// Pairings are add-ons and grocery items offered with the meal.
	Pairings []Pairing
}

// Reason explains a pick.
type Reason struct {
	Code string
	Text string
}

// Message is a week-level note.
type Message struct {
	Code string
	Text string
}

// Unfilled is a day the week wanted but couldn't fill.
type Unfilled struct {
	Day  string
	Code string
	Text string
}

// Objective breaks down the week objective.
type Objective struct {
	Meals    float64
	Variety  float64
	CookTime float64
	Rules    float64
	Novelty  float64
	Total    float64
}

// RecipeOverride is the household's say on which cooking methods a recipe
// suits, overriding the heuristic.
type RecipeOverride struct {
	HouseholdID string
	RecipeID    string
	// Methods maps a method to whether the recipe suits it.
	Methods map[string]bool
	// MealCategories maps a meal category to whether the recipe is in it.
	MealCategories map[string]bool
	UpdatedBy      string
	UpdatedAt      time.Time
}

// Limits.
const (
	MaxListValues         = 30
	MaxExcludedIngredient = 50
	MaxValueLength        = 40
	MaxIngredientLength   = 60
	MaxRuleValues         = 10
	MaxLabelLength        = 40
	MaxNoteLength         = 500
	MaxServings           = 12
	MinCookMinutes        = 5
	MaxCookMinutes        = 480
	// MaxRejectedPerSlot bounds a slot's swap history.
	MaxRejectedPerSlot = 20
	// MaxHistoryItems bounds preference history reads.
	MaxHistoryItems     = 100
	DefaultHistoryItems = 50
)
