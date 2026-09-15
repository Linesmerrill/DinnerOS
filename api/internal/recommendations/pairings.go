package recommendations

import (
	"errors"
	"time"
)

// Add-on pairings suggest an add-on recipe ("Garlic Bread") or a grocery item
// ("Club crackers") with a main meal, from the household's pairing rules and
// from what it usually has (docs/autopilot.md#add-on-pairings). This file
// holds the models; pairings_*.go the rest.

// Pairing is a suggested pairing for one main meal.
type Pairing struct {
	// Key identifies the target (PairingTarget.Key).
	Key  string
	Kind string
	// Recipe is set for an add-on recipe, GroceryItem for a grocery item.
	Recipe      *PairingRecipe
	GroceryItem *GroceryItem
	// Servings is the add-on's serving size for the meal: the smallest
	// authored size that feeds the meal's servings, or its largest.
	Servings int
	// Source is PairingSourceRule or PairingSourceLearned.
	Source string
	// Frequency is the rule's; learned pairings are PairingSuggest.
	Frequency    string
	MealCategory string
	RuleID       string
	// Confidence is the learned confidence, when history supports the
	// pairing (also for rules); 0 otherwise.
	Confidence float64
	Learned    *LearnedPairing
	Reason     string
	// InPlan: the add-on is already planned for the meal's day, or the
	// grocery item is on the week's list. Computed, not stored.
	InPlan bool
	// Dismissed for this meal this week. Computed, not stored.
	Dismissed bool
	// Included: accepting the proposal adds it unless the member leaves it
	// out. Proposal slots only.
	Included bool
}

// PairingRecipe is a snapshot of an add-on recipe for display.
type PairingRecipe struct {
	ID              string
	Name            string
	Headline        string
	ImageURL        string
	CookMinutes     int
	TimesOrdered    int
	LastOrderedWeek string
	Tags            []string
}

// LearnedPairing is the history behind a learned pairing (PairingStats).
type LearnedPairing struct {
	WeeksTogether int
	CategoryWeeks int
	OtherWeeks    int
	OtherRate     float64
}

// MaxPairingsPerMeal bounds the pairings offered with one meal.
const MaxPairingsPerMeal = 3

// WeekPairings is a household's pairing state for one ISO week: decisions on
// suggestions for plan entries, and the grocery items pairings added.
type WeekPairings struct {
	HouseholdID  string
	Week         string
	Decisions    []PairingDecision
	GroceryItems []WeekGroceryItem
	// Version increases with every write. Zero means not stored.
	Version   int64
	UpdatedAt time.Time
}

// Decision statuses.
const (
	DecisionAccepted  = "accepted"
	DecisionDismissed = "dismissed"
)

// Bounds for a week's pairing state.
const (
	MaxPairingDecisions = 200
	MaxWeekGroceryItems = 50
)

// PairingDecision is a member's answer to a pairing for a plan entry.
type PairingDecision struct {
	EntryID string
	Key     string
	Status  string
	// AddedEntryID is the add-on's plan entry; GroceryItemID the grocery item.
	AddedEntryID  string
	GroceryItemID string
	DecidedBy     string
	DecidedAt     time.Time
}

// WeekGroceryItem is a paired grocery item on the week's grocery list.
type WeekGroceryItem struct {
	ID  string
	Key string
	GroceryItem
	// ForEntryID is the main meal's plan entry; the item leaves the list when
	// that entry is removed.
	ForEntryID    string
	ForRecipeID   string
	ForRecipeName string
	Source        string
	RuleID        string
	AddedBy       string
	AddedAt       time.Time
}

// Text is the item's provenance: "Club crackers for Chicken Noodle Soup".
func (g WeekGroceryItem) Text() string { return g.Name + " for " + g.ForRecipeName }

func (wp WeekPairings) decision(entryID, key string) int {
	for i, d := range wp.Decisions {
		if d.EntryID == entryID && d.Key == key {
			return i
		}
	}
	return -1
}

func (wp WeekPairings) groceryItem(id string) int {
	for i, g := range wp.GroceryItems {
		if g.ID == id {
			return i
		}
	}
	return -1
}

func (wp WeekPairings) groceryItemByKey(key string) int {
	for i, g := range wp.GroceryItems {
		if g.Key == key {
			return i
		}
	}
	return -1
}

// ErrPairingsUnavailable means the service runs without a pairing store.
var ErrPairingsUnavailable = errors.New("recommendations: pairings are unavailable")

func isNotFound(err error) bool { return errors.Is(err, ErrNotFound) }
