package events

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Errors returned by the service and stores.
var (
	// ErrInvalidEvent wraps a validation problem with one event. Its message,
	// without the package prefix, is safe to show clients.
	ErrInvalidEvent = errors.New("events: invalid event")
	// ErrInvalidBatch means a client batch as a whole is unusable (empty or
	// too large).
	ErrInvalidBatch = errors.New("events: invalid batch")
)

// Type names what happened. Types are dotted, lowercase, and stable: they are
// stored and later forwarded to Autopilot.
type Type string

// Event types. Clients may send only the types ClientTypes lists; the rest are
// recorded by server code.
const (
	TypeRecipeViewed       Type = "recipe.viewed"
	TypeRecipeRated        Type = "recipe.rated"
	TypeRecipeUnrated      Type = "recipe.unrated"
	TypeRecipePlanned      Type = "recipe.planned"
	TypeRecipeUnplanned    Type = "recipe.unplanned"
	TypeRecipeCooked       Type = "recipe.cooked"
	TypeRecipeSkipped      Type = "recipe.skipped"
	TypeGroceryItemChecked Type = "grocery.item_checked"
	TypeImportCompleted    Type = "import.completed"

	// Autopilot preference changes (who changed what, as a diff summary).
	TypeAutopilotPreferencesUpdated    Type = "autopilot.preferences_updated"
	TypeAutopilotWeekContextUpdated    Type = "autopilot.week_context_updated"
	TypeAutopilotRecipeOverrideUpdated Type = "autopilot.recipe_override_updated"

	// Autopilot week generation and the household's response to it.
	TypeWeekGenerated Type = "week.generated"
	TypeWeekAccepted  Type = "week.accepted"
	TypeWeekRejected  Type = "week.rejected"
	TypeMealSwapped   Type = "meal.swapped"
	TypeMealRejected  Type = "meal.rejected"

	// Autopilot add-on pairings: offered with a main meal, added, dismissed,
	// and turned into household rules.
	TypePairingSuggested   Type = "pairing.suggested"
	TypePairingAccepted    Type = "pairing.accepted"
	TypePairingDismissed   Type = "pairing.dismissed"
	TypePairingRuleCreated Type = "pairing.rule_created"

	// TypeMealCustomized: a member customized a planned meal's protein
	// (swapped it or doubled it).
	TypeMealCustomized Type = "meal.customized"

	// TypeShoppingHandoffCreated: a member handed the week's list to a
	// shopping provider.
	TypeShoppingHandoffCreated Type = "shopping.handoff_created"
	// TypeShoppingOrderConfirmed: a member confirmed what was ordered from a
	// handoff.
	TypeShoppingOrderConfirmed Type = "shopping.order_confirmed"
)

// Source says who observed an event.
type Source string

// Event sources.
const (
	// SourceAPI events are recorded by DinnerOS server code, including
	// operator commands such as importrecipes.
	SourceAPI Source = "api"
	// SourceClient events were observed by an app and sent to the ingestion
	// endpoint. They are only as trustworthy as the signed-in member.
	SourceClient Source = "client"
)

// Event is one household behavior record.
type Event struct {
	ID          string
	HouseholdID string
	// UserID is the member who acted. It is empty for operator actions.
	UserID string
	Type   Type
	// RecipeID is required for recipe.* events and absent otherwise.
	RecipeID string
	// Week is the ISO week ("2026-W38") the event concerns, when it concerns
	// one (a planned week, a grocery list).
	Week string
	// Payload is the type-specific detail. Its concrete type must match Type
	// (see Payload); nil means the type's zero payload.
	Payload Payload
	// ClientEventID is the app's idempotency key: a retried client event with
	// the same key is stored once. Server events leave it empty.
	ClientEventID string
	// OccurredAt is when it happened (client clock for client events).
	OccurredAt time.Time
	// RecordedAt is when the server stored it.
	RecordedAt time.Time
	Source     Source
}

// Limits for event fields.
const (
	MaxClientEventIDLength = 64
	maxShortText           = 100
	maxServings            = 12
)

// Payload is the typed detail of one event type. The set is closed: each
// event type has exactly one payload type, defined in this package.
type Payload interface {
	// EventType returns the event type this payload belongs to.
	EventType() Type
	validate() error
}

// RecipeViewed is the payload of recipe.viewed.
type RecipeViewed struct {
	// Surface is where the recipe was seen: detail, plan, search, or
	// recommendation. Optional.
	Surface string `json:"surface,omitempty" bson:"surface,omitempty"`
}

// RecipeRated is the payload of recipe.rated. Comments are deliberately not
// copied into events.
type RecipeRated struct {
	Score int `json:"score" bson:"score"`
	// PreviousScore is the score this rating replaced; 0 for a first rating.
	PreviousScore int      `json:"previousScore,omitempty" bson:"previousScore,omitempty"`
	Tags          []string `json:"tags,omitempty" bson:"tags,omitempty"`
}

// RecipeUnrated is the payload of recipe.unrated (a member removed a rating).
type RecipeUnrated struct {
	PreviousScore int `json:"previousScore" bson:"previousScore"`
}

// RecipePlanned is the payload of recipe.planned. EntryID, Day, and Date
// match the planning module's entry; Event.Week holds the plan's ISO week.
type RecipePlanned struct {
	EntryID string `json:"entryId,omitempty" bson:"entryId,omitempty"`
	// Day is one of PlanDays, or empty for "this week, not scheduled".
	Day string `json:"day,omitempty" bson:"day,omitempty"`
	// Date is Day's YYYY-MM-DD in the household's time zone, or empty.
	Date     string `json:"date,omitempty" bson:"date,omitempty"`
	Servings int    `json:"servings,omitempty" bson:"servings,omitempty"`
	// Origin is one of PlanOrigins. Optional.
	Origin string `json:"origin,omitempty" bson:"origin,omitempty"`
	// ProposalID is the Autopilot proposal an accepted entry came from.
	ProposalID string `json:"proposalId,omitempty" bson:"proposalId,omitempty"`
}

// RecipeUnplanned is the payload of recipe.unplanned (an entry was removed).
type RecipeUnplanned struct {
	EntryID string `json:"entryId,omitempty" bson:"entryId,omitempty"`
	Day     string `json:"day,omitempty" bson:"day,omitempty"`
	Date    string `json:"date,omitempty" bson:"date,omitempty"`
	// Origin is the removed entry's origin, one of PlanOrigins. Optional.
	Origin string `json:"origin,omitempty" bson:"origin,omitempty"`
}

// RecipeCooked is the payload of recipe.cooked. EntryID links it to a plan
// entry when the meal was planned.
type RecipeCooked struct {
	EntryID  string `json:"entryId,omitempty" bson:"entryId,omitempty"`
	Date     string `json:"date,omitempty" bson:"date,omitempty"`
	Servings int    `json:"servings,omitempty" bson:"servings,omitempty"`
}

// RecipeSkipped is the payload of recipe.skipped (planned but not cooked).
type RecipeSkipped struct {
	EntryID string `json:"entryId,omitempty" bson:"entryId,omitempty"`
	Date    string `json:"date,omitempty" bson:"date,omitempty"`
	// Reason is one of SkipReasons. Optional.
	Reason string `json:"reason,omitempty" bson:"reason,omitempty"`
}

// GroceryItemChecked is the payload of grocery.item_checked.
type GroceryItemChecked struct {
	// IngredientID or Name identifies the item; at least one is required.
	IngredientID string `json:"ingredientId,omitempty" bson:"ingredientId,omitempty"`
	Name         string `json:"name,omitempty" bson:"name,omitempty"`
	// Checked is false when an item was unchecked again.
	Checked bool `json:"checked" bson:"checked"`
}

// ImportCompleted is the payload of import.completed.
type ImportCompleted struct {
	Source    string `json:"source" bson:"source"`
	Created   int    `json:"created" bson:"created"`
	Updated   int    `json:"updated" bson:"updated"`
	Unchanged int    `json:"unchanged" bson:"unchanged"`
	Rejected  int    `json:"rejected" bson:"rejected"`
}

// FieldChange summarizes how one preference field changed. List fields report
// the Added and Removed values; scalar fields report From and To as text, with
// "" for unset.
type FieldChange struct {
	Field   string   `json:"field" bson:"field"`
	Added   []string `json:"added,omitempty" bson:"added,omitempty"`
	Removed []string `json:"removed,omitempty" bson:"removed,omitempty"`
	From    string   `json:"from,omitempty" bson:"from,omitempty"`
	To      string   `json:"to,omitempty" bson:"to,omitempty"`
}

// Bounds for preference change summaries.
const (
	MaxFieldChanges = 40
	MaxChangeValues = 30
	maxSections     = 10
)

// AutopilotPreferencesUpdated is the payload of
// autopilot.preferences_updated: which profile sections a member changed,
// and how.
type AutopilotPreferencesUpdated struct {
	Sections []string      `json:"sections" bson:"sections"`
	Changes  []FieldChange `json:"changes,omitempty" bson:"changes,omitempty"`
}

// AutopilotWeekContextUpdated is the payload of
// autopilot.week_context_updated. Event.Week is the week; Cleared means the
// context was removed.
type AutopilotWeekContextUpdated struct {
	Cleared bool          `json:"cleared,omitempty" bson:"cleared,omitempty"`
	Changes []FieldChange `json:"changes,omitempty" bson:"changes,omitempty"`
}

// AutopilotRecipeOverrideUpdated is the payload of
// autopilot.recipe_override_updated: a member said whether a recipe suits a
// cooking method, overriding the heuristic.
type AutopilotRecipeOverrideUpdated struct {
	// Method or Category (a meal category such as pasta) names what changed;
	// exactly one is set.
	Method   string `json:"method,omitempty" bson:"method,omitempty"`
	Category string `json:"category,omitempty" bson:"category,omitempty"`
	// Value and Previous are one of OverrideValues; auto means no override.
	Value    string `json:"value" bson:"value"`
	Previous string `json:"previous,omitempty" bson:"previous,omitempty"`
}

// WeekGenerated is the payload of week.generated. Event.Week is the week.
type WeekGenerated struct {
	ProposalID   string `json:"proposalId" bson:"proposalId"`
	ModelVersion string `json:"modelVersion" bson:"modelVersion"`
	// Attempt counts generations for the week, starting at 1.
	Attempt int `json:"attempt" bson:"attempt"`
	// Requested is how many meals the week called for; Planned how many the
	// proposal holds; Unfilled how many days it couldn't fill.
	Requested int `json:"requested" bson:"requested"`
	Planned   int `json:"planned" bson:"planned"`
	Unfilled  int `json:"unfilled" bson:"unfilled"`
	// Candidates is how many recipes passed the hard constraints.
	Candidates int  `json:"candidates" bson:"candidates"`
	ColdStart  bool `json:"coldStart,omitempty" bson:"coldStart,omitempty"`
	// ReplacedProposalID is the pending proposal this one replaced.
	ReplacedProposalID string `json:"replacedProposalId,omitempty" bson:"replacedProposalId,omitempty"`
}

// WeekAccepted is the payload of week.accepted.
type WeekAccepted struct {
	ProposalID   string `json:"proposalId" bson:"proposalId"`
	ModelVersion string `json:"modelVersion" bson:"modelVersion"`
	// Planned is the proposal's meal count; Added the entries added to the
	// plan; Excluded the meals the member left out; Skipped the meals that
	// no longer fit the plan; Swaps the swaps made before accepting.
	Planned  int `json:"planned" bson:"planned"`
	Added    int `json:"added" bson:"added"`
	Excluded int `json:"excluded" bson:"excluded"`
	Skipped  int `json:"skipped" bson:"skipped"`
	Swaps    int `json:"swaps" bson:"swaps"`
}

// WeekRejected is the payload of week.rejected: a pending proposal was
// dismissed, or replaced by generating again.
type WeekRejected struct {
	ProposalID   string `json:"proposalId" bson:"proposalId"`
	ModelVersion string `json:"modelVersion" bson:"modelVersion"`
	Planned      int    `json:"planned" bson:"planned"`
	Swaps        int    `json:"swaps" bson:"swaps"`
	// Reason is one of WeekRejectReasons.
	Reason string `json:"reason" bson:"reason"`
}

// MealSwapped is the payload of meal.swapped. Event.RecipeID is the recipe
// swapped in; PreviousRecipeID the one swapped out.
type MealSwapped struct {
	ProposalID       string `json:"proposalId" bson:"proposalId"`
	SlotID           string `json:"slotId" bson:"slotId"`
	Day              string `json:"day" bson:"day"`
	Date             string `json:"date,omitempty" bson:"date,omitempty"`
	PreviousRecipeID string `json:"previousRecipeId" bson:"previousRecipeId"`
	ModelVersion     string `json:"modelVersion" bson:"modelVersion"`
	// SwapNumber counts swaps of this slot, starting at 1.
	SwapNumber int `json:"swapNumber" bson:"swapNumber"`
}

// MealRejected is the payload of meal.rejected: a member left a proposed
// meal out when accepting the week. Event.RecipeID is the meal.
type MealRejected struct {
	ProposalID   string `json:"proposalId" bson:"proposalId"`
	SlotID       string `json:"slotId" bson:"slotId"`
	Day          string `json:"day" bson:"day"`
	Date         string `json:"date,omitempty" bson:"date,omitempty"`
	ModelVersion string `json:"modelVersion" bson:"modelVersion"`
}

// MealCustomized is the payload of meal.customized: a member changed how a
// planned meal is cooked. Event.RecipeID is the meal and Event.Week its week.
// Each change is one ingredient line's choice before and after: "original",
// "double", "swap:<proteinId>", or "swap:<proteinId>:double". Autopilot
// doesn't learn from it yet; it's kept as a preference signal ("they always
// swap pork for chicken").
type MealCustomized struct {
	EntryID string                `json:"entryId" bson:"entryId"`
	Changes []CustomizationChange `json:"changes" bson:"changes"`
}

// CustomizationChange is one ingredient line's customization before and
// after a change.
type CustomizationChange struct {
	// IngredientKey is the line's catalog ID or "name:<normalized name>".
	IngredientKey string `json:"ingredientKey" bson:"ingredientKey"`
	From          string `json:"from" bson:"from"`
	To            string `json:"to" bson:"to"`
}

// Bounds for meal customization changes.
const (
	MaxCustomizationChanges = 20
	maxIngredientKey        = 200
)

// ShoppingHandoffCreated is the payload of shopping.handoff_created. It holds
// counts only: product IDs from providers are never copied into events, which
// are forwarded to Autopilot (docs/shopping-providers.md#risks-and-open-questions).
type ShoppingHandoffCreated struct {
	HandoffID string `json:"handoffId" bson:"handoffId"`
	Provider  string `json:"provider" bson:"provider"`
	// Lines are the grocery lines in the cart links; Packages sums their
	// package counts.
	Lines    int `json:"lines" bson:"lines"`
	Packages int `json:"packages" bson:"packages"`
	// CheckAmount counts lines whose package count was flagged.
	CheckAmount int `json:"checkAmount" bson:"checkAmount"`
	// Excluded counts grocery lines left out of the links.
	Excluded int `json:"excluded" bson:"excluded"`
	Links    int `json:"links" bson:"links"`
}

// ShoppingOrderConfirmed is the payload of shopping.order_confirmed: what a
// member newly confirmed as ordered (recorded as pantry purchases) or not
// ordered in one confirmation.
type ShoppingOrderConfirmed struct {
	HandoffID string `json:"handoffId" bson:"handoffId"`
	Provider  string `json:"provider" bson:"provider"`
	Confirmed int    `json:"confirmed" bson:"confirmed"`
	Packages  int    `json:"packages" bson:"packages"`
	Skipped   int    `json:"skipped" bson:"skipped"`
}

// Allowed values for optional enumerated payload fields.
var (
	ViewSurfaces = []string{"detail", "plan", "search", "recommendation"}
	PlanDays     = []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}
	PlanOrigins  = []string{"manual", "autopilot"}
	SkipReasons  = []string{"no-time", "ate-out", "missing-ingredients", "not-in-the-mood", "other"}
	// OverrideValues are a recipe override's states.
	OverrideValues = []string{"yes", "no", "auto"}
	// WeekRejectReasons say why a proposal was rejected.
	WeekRejectReasons = []string{"dismissed", "regenerated"}
)

// EventType implements Payload.
func (RecipeViewed) EventType() Type { return TypeRecipeViewed }

// EventType implements Payload.
func (RecipeRated) EventType() Type { return TypeRecipeRated }

// EventType implements Payload.
func (RecipeUnrated) EventType() Type { return TypeRecipeUnrated }

// EventType implements Payload.
func (RecipePlanned) EventType() Type { return TypeRecipePlanned }

// EventType implements Payload.
func (RecipeUnplanned) EventType() Type { return TypeRecipeUnplanned }

// EventType implements Payload.
func (RecipeCooked) EventType() Type { return TypeRecipeCooked }

// EventType implements Payload.
func (RecipeSkipped) EventType() Type { return TypeRecipeSkipped }

// EventType implements Payload.
func (GroceryItemChecked) EventType() Type { return TypeGroceryItemChecked }

// EventType implements Payload.
func (ImportCompleted) EventType() Type { return TypeImportCompleted }

// EventType implements Payload.
func (AutopilotPreferencesUpdated) EventType() Type { return TypeAutopilotPreferencesUpdated }

// EventType implements Payload.
func (AutopilotWeekContextUpdated) EventType() Type { return TypeAutopilotWeekContextUpdated }

// EventType implements Payload.
func (AutopilotRecipeOverrideUpdated) EventType() Type { return TypeAutopilotRecipeOverrideUpdated }

// EventType implements Payload.
func (WeekGenerated) EventType() Type { return TypeWeekGenerated }

// EventType implements Payload.
func (WeekAccepted) EventType() Type { return TypeWeekAccepted }

// EventType implements Payload.
func (WeekRejected) EventType() Type { return TypeWeekRejected }

// EventType implements Payload.
func (MealSwapped) EventType() Type { return TypeMealSwapped }

// EventType implements Payload.
func (MealRejected) EventType() Type { return TypeMealRejected }

// EventType implements Payload.
func (ShoppingHandoffCreated) EventType() Type { return TypeShoppingHandoffCreated }

// EventType implements Payload.
func (ShoppingOrderConfirmed) EventType() Type { return TypeShoppingOrderConfirmed }

func (p RecipeViewed) validate() error {
	return optionalEnum("surface", p.Surface, ViewSurfaces)
}

func (p RecipeRated) validate() error {
	switch {
	case p.Score < 1 || p.Score > 5:
		return invalid("score must be between 1 and 5")
	case p.PreviousScore < 0 || p.PreviousScore > 5:
		return invalid("previousScore must be between 1 and 5 when present")
	case len(p.Tags) > 20:
		return invalid("at most 20 tags")
	}
	return nil
}

func (p RecipeUnrated) validate() error {
	if p.PreviousScore < 1 || p.PreviousScore > 5 {
		return invalid("previousScore must be between 1 and 5")
	}
	return nil
}

func (p RecipePlanned) validate() error {
	return errors.Join(validEntryID(p.EntryID), optionalEnum("day", p.Day, PlanDays), validDate(p.Date),
		validServings(p.Servings), optionalEnum("origin", p.Origin, PlanOrigins), validID("proposalId", p.ProposalID, false))
}

func (p RecipeUnplanned) validate() error {
	return errors.Join(validEntryID(p.EntryID), optionalEnum("day", p.Day, PlanDays), validDate(p.Date),
		optionalEnum("origin", p.Origin, PlanOrigins))
}

func (p RecipeCooked) validate() error {
	return errors.Join(validEntryID(p.EntryID), validDate(p.Date), validServings(p.Servings))
}

func (p RecipeSkipped) validate() error {
	return errors.Join(validEntryID(p.EntryID), validDate(p.Date), optionalEnum("reason", p.Reason, SkipReasons))
}

func (p GroceryItemChecked) validate() error {
	switch {
	case strings.TrimSpace(p.IngredientID) == "" && strings.TrimSpace(p.Name) == "":
		return invalid("ingredientId or name is required")
	case len(p.IngredientID) > MaxClientEventIDLength:
		return invalid("ingredientId is too long")
	case utf8.RuneCountInString(p.Name) > maxShortText:
		return invalid(fmt.Sprintf("name must be at most %d characters", maxShortText))
	}
	return nil
}

func (p ImportCompleted) validate() error {
	switch {
	case p.Source == "" || len(p.Source) > 32:
		return invalid("source is required")
	case p.Created < 0 || p.Updated < 0 || p.Unchanged < 0 || p.Rejected < 0:
		return invalid("counts must not be negative")
	}
	return nil
}

func (p AutopilotPreferencesUpdated) validate() error {
	if len(p.Sections) == 0 || len(p.Sections) > maxSections {
		return invalid(fmt.Sprintf("sections must list 1 to %d sections", maxSections))
	}
	var errs []error
	for _, section := range p.Sections {
		if section == "" || len(section) > 32 {
			errs = append(errs, invalid("each section must be 1 to 32 characters"))
		}
	}
	return errors.Join(append(errs, validChanges(p.Changes))...)
}

func (p AutopilotWeekContextUpdated) validate() error {
	return validChanges(p.Changes)
}

func (p AutopilotRecipeOverrideUpdated) validate() error {
	switch {
	case (p.Method == "") == (p.Category == ""):
		return invalid("exactly one of method or category is required")
	case len(p.Method) > 32 || len(p.Category) > 32:
		return invalid("method and category must be at most 32 characters")
	}
	if p.Value == "" {
		return invalid("value is required")
	}
	return errors.Join(optionalEnum("value", p.Value, OverrideValues), optionalEnum("previous", p.Previous, OverrideValues))
}

func (p WeekGenerated) validate() error {
	return errors.Join(validProposal(p.ProposalID, p.ModelVersion), validID("replacedProposalId", p.ReplacedProposalID, false),
		nonNegative(p.Attempt, p.Requested, p.Planned, p.Unfilled, p.Candidates))
}

func (p WeekAccepted) validate() error {
	return errors.Join(validProposal(p.ProposalID, p.ModelVersion), nonNegative(p.Planned, p.Added, p.Excluded, p.Skipped, p.Swaps))
}

func (p WeekRejected) validate() error {
	var reason error
	if p.Reason == "" {
		reason = invalid("reason is required")
	}
	return errors.Join(validProposal(p.ProposalID, p.ModelVersion), nonNegative(p.Planned, p.Swaps), reason,
		optionalEnum("reason", p.Reason, WeekRejectReasons))
}

func (p MealSwapped) validate() error {
	return errors.Join(validProposal(p.ProposalID, p.ModelVersion), validID("slotId", p.SlotID, true),
		validID("previousRecipeId", p.PreviousRecipeID, true), requiredDay(p.Day), validDate(p.Date), nonNegative(p.SwapNumber))
}

func (p MealRejected) validate() error {
	return errors.Join(validProposal(p.ProposalID, p.ModelVersion), validID("slotId", p.SlotID, true),
		requiredDay(p.Day), validDate(p.Date))
}

// EventType implements Payload.
func (MealCustomized) EventType() Type { return TypeMealCustomized }

func (p MealCustomized) validate() error {
	errs := []error{validID("entryId", p.EntryID, true)}
	if len(p.Changes) == 0 || len(p.Changes) > MaxCustomizationChanges {
		errs = append(errs, invalid(fmt.Sprintf("changes must list 1 to %d changes", MaxCustomizationChanges)))
	}
	for _, c := range p.Changes {
		switch {
		case c.IngredientKey == "" || utf8.RuneCountInString(c.IngredientKey) > maxIngredientKey:
			errs = append(errs, invalid(fmt.Sprintf("each change needs an ingredientKey of at most %d characters", maxIngredientKey)))
		case c.From == "" || c.To == "" || utf8.RuneCountInString(c.From) > maxShortText || utf8.RuneCountInString(c.To) > maxShortText:
			errs = append(errs, invalid(fmt.Sprintf("each change needs from and to of at most %d characters", maxShortText)))
		}
	}
	return errors.Join(errs...)
}

func validHandoff(handoffID, provider string) error {
	switch {
	case handoffID == "" || len(handoffID) > MaxClientEventIDLength:
		return invalid("handoffId is required")
	case provider == "" || len(provider) > 32:
		return invalid("provider is required")
	}
	return nil
}

func (p ShoppingHandoffCreated) validate() error {
	if p.Lines < 0 || p.Packages < 0 || p.CheckAmount < 0 || p.Excluded < 0 || p.Links < 0 {
		return errors.Join(validHandoff(p.HandoffID, p.Provider), invalid("counts must not be negative"))
	}
	return validHandoff(p.HandoffID, p.Provider)
}

func (p ShoppingOrderConfirmed) validate() error {
	if p.Confirmed < 0 || p.Packages < 0 || p.Skipped < 0 {
		return errors.Join(validHandoff(p.HandoffID, p.Provider), invalid("counts must not be negative"))
	}
	return validHandoff(p.HandoffID, p.Provider)
}

// typeSpec describes the rules for one event type.
type typeSpec struct {
	// recipe events require RecipeID; other events must not carry one.
	recipe bool
	// client events may be sent to the ingestion endpoint.
	client bool
	// decode decodes a payload of this type with unmarshal. Empty data yields
	// the zero payload.
	decode func(data []byte, unmarshal func([]byte, any) error) (Payload, error)
}

var typeSpecs = map[Type]typeSpec{
	TypeRecipeViewed:       {recipe: true, client: true, decode: decoder[RecipeViewed]()},
	TypeRecipeRated:        {recipe: true, decode: decoder[RecipeRated]()},
	TypeRecipeUnrated:      {recipe: true, decode: decoder[RecipeUnrated]()},
	TypeRecipePlanned:      {recipe: true, decode: decoder[RecipePlanned]()},
	TypeRecipeUnplanned:    {recipe: true, decode: decoder[RecipeUnplanned]()},
	TypeRecipeCooked:       {recipe: true, client: true, decode: decoder[RecipeCooked]()},
	TypeRecipeSkipped:      {recipe: true, client: true, decode: decoder[RecipeSkipped]()},
	TypeGroceryItemChecked: {client: true, decode: decoder[GroceryItemChecked]()},
	TypeImportCompleted:    {decode: decoder[ImportCompleted]()},

	TypeAutopilotPreferencesUpdated:    {decode: decoder[AutopilotPreferencesUpdated]()},
	TypeAutopilotWeekContextUpdated:    {decode: decoder[AutopilotWeekContextUpdated]()},
	TypeAutopilotRecipeOverrideUpdated: {recipe: true, decode: decoder[AutopilotRecipeOverrideUpdated]()},
	TypeWeekGenerated:                  {decode: decoder[WeekGenerated]()},
	TypeWeekAccepted:                   {decode: decoder[WeekAccepted]()},
	TypeWeekRejected:                   {decode: decoder[WeekRejected]()},
	TypeMealSwapped:                    {recipe: true, decode: decoder[MealSwapped]()},
	TypeMealRejected:                   {recipe: true, decode: decoder[MealRejected]()},
	TypePairingSuggested:               {recipe: true, decode: decoder[PairingSuggested]()},
	TypePairingAccepted:                {recipe: true, decode: decoder[PairingAccepted]()},
	TypePairingDismissed:               {recipe: true, decode: decoder[PairingDismissed]()},
	TypePairingRuleCreated:             {decode: decoder[PairingRuleCreated]()},
	TypeMealCustomized:                 {recipe: true, decode: decoder[MealCustomized]()},
	TypeShoppingHandoffCreated:         {decode: decoder[ShoppingHandoffCreated]()},
	TypeShoppingOrderConfirmed:         {decode: decoder[ShoppingOrderConfirmed]()},
}

func decoder[P Payload]() func([]byte, func([]byte, any) error) (Payload, error) {
	return func(data []byte, unmarshal func([]byte, any) error) (Payload, error) {
		var p P
		if len(data) > 0 {
			if err := unmarshal(data, &p); err != nil {
				return nil, err
			}
		}
		return p, nil
	}
}

// Types returns every event type, sorted.
func Types() []Type {
	out := make([]Type, 0, len(typeSpecs))
	for t := range typeSpecs {
		out = append(out, t)
	}
	slices.Sort(out)
	return out
}

// ClientTypes returns the event types clients may send, sorted.
func ClientTypes() []Type {
	var out []Type
	for _, t := range Types() {
		if typeSpecs[t].client {
			out = append(out, t)
		}
	}
	return out
}

// Valid reports whether t is a known event type.
func (t Type) Valid() bool {
	_, ok := typeSpecs[t]
	return ok
}

// isoWeek matches "2026-W01" through "2026-W53".
var isoWeek = regexp.MustCompile(`^\d{4}-W(0[1-9]|[1-4]\d|5[0-3])$`)

// normalize fills defaults and validates e. It never checks that referenced
// recipes exist; the ingestion path does that separately.
func normalize(e Event) (Event, error) {
	spec, ok := typeSpecs[e.Type]
	switch {
	case e.HouseholdID == "":
		return Event{}, invalid("householdId is required")
	case !ok:
		return Event{}, invalid(fmt.Sprintf("type %q is not a known event type", e.Type))
	case e.Source != SourceAPI && e.Source != SourceClient:
		return Event{}, invalid("source must be api or client")
	case e.OccurredAt.IsZero():
		return Event{}, invalid("occurredAt is required")
	case spec.recipe && e.RecipeID == "":
		return Event{}, invalid(fmt.Sprintf("recipeId is required for %s", e.Type))
	case !spec.recipe && e.RecipeID != "":
		return Event{}, invalid(fmt.Sprintf("recipeId is not allowed for %s", e.Type))
	case e.Week != "" && !isoWeek.MatchString(e.Week):
		return Event{}, invalid("week must be an ISO week such as 2026-W38")
	case len(e.ClientEventID) > MaxClientEventIDLength:
		return Event{}, invalid(fmt.Sprintf("clientEventId must be at most %d characters", MaxClientEventIDLength))
	}
	if e.Payload == nil {
		p, _ := spec.decode(nil, nil)
		e.Payload = p
	}
	if e.Payload.EventType() != e.Type {
		return Event{}, invalid(fmt.Sprintf("payload for %s has the wrong type", e.Type))
	}
	if err := e.Payload.validate(); err != nil {
		// Payload validators wrap ErrInvalidEvent and may be joined; report
		// their messages once, on one line.
		msg := strings.ReplaceAll(err.Error(), ErrInvalidEvent.Error()+": ", "")
		return Event{}, invalid("payload: " + strings.ReplaceAll(msg, "\n", "; "))
	}
	e.OccurredAt = e.OccurredAt.UTC()
	e.RecordedAt = e.RecordedAt.UTC()
	return e, nil
}

func invalid(msg string) error { return fmt.Errorf("%w: %s", ErrInvalidEvent, msg) }

func optionalEnum(field, value string, allowed []string) error {
	if value == "" || slices.Contains(allowed, value) {
		return nil
	}
	return invalid(fmt.Sprintf("%s must be one of %s", field, strings.Join(allowed, ", ")))
}

func validDate(s string) error {
	if s == "" {
		return nil
	}
	if _, err := time.Parse(time.DateOnly, s); err != nil {
		return invalid("date must be YYYY-MM-DD")
	}
	return nil
}

func validServings(n int) error {
	if n < 0 || n > maxServings {
		return invalid(fmt.Sprintf("servings must be between 1 and %d when present", maxServings))
	}
	return nil
}

// validID checks an identifier field: at most MaxClientEventIDLength
// characters, and present when required.
func validID(field, id string, required bool) error {
	switch {
	case required && id == "":
		return invalid(field + " is required")
	case len(id) > MaxClientEventIDLength:
		return invalid(fmt.Sprintf("%s must be at most %d characters", field, MaxClientEventIDLength))
	}
	return nil
}

func validProposal(proposalID, modelVersion string) error {
	return errors.Join(validID("proposalId", proposalID, true), validID("modelVersion", modelVersion, true))
}

func requiredDay(day string) error {
	if day == "" {
		return invalid("day is required")
	}
	return optionalEnum("day", day, PlanDays)
}

func nonNegative(counts ...int) error {
	for _, n := range counts {
		if n < 0 {
			return invalid("counts must not be negative")
		}
	}
	return nil
}

func validChanges(changes []FieldChange) error {
	if len(changes) > MaxFieldChanges {
		return invalid(fmt.Sprintf("at most %d changes", MaxFieldChanges))
	}
	for _, c := range changes {
		switch {
		case c.Field == "" || utf8.RuneCountInString(c.Field) > maxShortText:
			return invalid(fmt.Sprintf("each change needs a field of at most %d characters", maxShortText))
		case len(c.Added) > MaxChangeValues || len(c.Removed) > MaxChangeValues:
			return invalid(fmt.Sprintf("a change lists at most %d added and %d removed values", MaxChangeValues, MaxChangeValues))
		case utf8.RuneCountInString(c.From) > maxShortText || utf8.RuneCountInString(c.To) > maxShortText:
			return invalid(fmt.Sprintf("from and to must be at most %d characters", maxShortText))
		}
		for _, v := range slices.Concat(c.Added, c.Removed) {
			if utf8.RuneCountInString(v) > maxShortText {
				return invalid(fmt.Sprintf("change values must be at most %d characters", maxShortText))
			}
		}
	}
	return nil
}

func validEntryID(id string) error {
	if len(id) > MaxClientEventIDLength {
		return invalid(fmt.Sprintf("entryId must be at most %d characters", MaxClientEventIDLength))
	}
	return nil
}
