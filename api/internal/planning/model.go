package planning

import (
	"errors"
	"fmt"
	"time"
)

// Errors returned by stores and the service.
var (
	// ErrNotFound means the plan or entry does not exist.
	ErrNotFound = errors.New("planning: not found")
	// ErrInvalidWeek means a week is not a valid ISO week.
	ErrInvalidWeek = errors.New("planning: invalid week")
	// ErrInvalidRange means a week range is missing, reversed, or too long.
	ErrInvalidRange = errors.New("planning: invalid range")
	// ErrInvalidEntry means an entry field is invalid.
	ErrInvalidEntry = errors.New("planning: invalid entry")
	// ErrInvalidStatus means a plan status is unknown.
	ErrInvalidStatus = errors.New("planning: invalid status")
	// ErrRecipeNotFound means an entry names a recipe that is not in the
	// household.
	ErrRecipeNotFound = errors.New("planning: recipe not found in this household")
	// ErrFinalized means the plan is finalized, so its entries can't change.
	ErrFinalized = errors.New("planning: plan is finalized")
	// ErrPlanFull means the week already has MaxEntriesPerWeek entries.
	ErrPlanFull = errors.New("planning: plan is full")

	errHouseholdRequired = errors.New("planning: household id is required")
)

// Status is a week plan's state.
type Status string

// Plan statuses. A finalized plan's entries are locked; set it back to draft
// to change them.
const (
	StatusDraft     Status = "draft"
	StatusFinalized Status = "finalized"
)

// ParseStatus validates a status.
func ParseStatus(s string) (Status, error) {
	switch Status(s) {
	case StatusDraft, StatusFinalized:
		return Status(s), nil
	}
	return "", fmt.Errorf("%w: status must be draft or finalized", ErrInvalidStatus)
}

// Origin says how an entry got into a plan.
type Origin string

// Entry origins.
const (
	// OriginManual entries were added by a member.
	OriginManual Origin = "manual"
	// OriginAutopilot entries were added by accepting an Autopilot proposal.
	OriginAutopilot Origin = "autopilot"
)

// parseOrigin validates an origin; empty means manual.
func parseOrigin(o Origin) (Origin, error) {
	switch o {
	case "":
		return OriginManual, nil
	case OriginManual, OriginAutopilot:
		return o, nil
	}
	return "", fmt.Errorf("%w: origin must be manual or autopilot", ErrInvalidEntry)
}

// Plan is a household's plan for one ISO week. (HouseholdID, Week) is unique.
// A week nobody has planned is returned as an empty draft with a zero
// CreatedAt.
type Plan struct {
	HouseholdID string
	Week        Week
	Status      Status
	Entries     []Entry
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Entry is a recipe planned for the week.
type Entry struct {
	ID       string
	RecipeID string
	// RecipeName and RecipeImageURL are copied from the recipe when the entry
	// is added, so a plan renders without loading recipes. Details (and the
	// grocery list) always use the live recipe.
	RecipeName     string
	RecipeImageURL string
	// Day is empty for "this week, not scheduled".
	Day Day
	// Servings is one of the recipe's authored serving sizes.
	Servings int
	Note     string
	AddedBy  string
	AddedAt  time.Time
	// Origin is how the entry was added. Entries stored before origins
	// existed read as OriginManual.
	Origin Origin
	// ProposalID is the Autopilot proposal an autopilot entry came from.
	ProposalID string
	// Customizations are the member's protein choices for this meal (package
	// customize). Empty when the meal is cooked as written.
	Customizations []Customization
}

// Customization is one customized ingredient line of a planned meal: the
// line's key (catalog ID or "name:<key>") and the chosen choice ("double",
// "swap:ground-beef"). Package customize validates and applies them; planning
// only stores them.
type Customization struct {
	IngredientKey string
	ChoiceID      string
	// Label is the choice's display label when it was chosen ("2x Ground Beef").
	Label string
}

// EntryChanges is a partial update of an entry. Nil fields are unchanged; a
// Day pointing at "" unschedules the entry.
type EntryChanges struct {
	Day      *Day
	Servings *int
	Note     *string
	// Customizations replaces the entry's customizations; an empty slice
	// removes them.
	Customizations *[]Customization
}

// Summary describes one week in a range.
type Summary struct {
	Week       Week
	Status     Status
	EntryCount int
	// RecipeIDs are the week's entries' recipes, in entry order. Planning
	// doesn't know which are add-ons; callers that care (the menu's week
	// strip) decide with the catalog. Empty for a week without entries.
	RecipeIDs []string
	// UpdatedAt is zero for weeks nobody has planned.
	UpdatedAt time.Time
}

// entry returns the entry with id, if present.
func (p Plan) entry(id string) (Entry, bool) {
	for _, e := range p.Entries {
		if e.ID == id {
			return e, true
		}
	}
	return Entry{}, false
}
