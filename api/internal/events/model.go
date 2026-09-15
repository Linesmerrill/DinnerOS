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
}

// RecipeUnplanned is the payload of recipe.unplanned (an entry was removed).
type RecipeUnplanned struct {
	EntryID string `json:"entryId,omitempty" bson:"entryId,omitempty"`
	Day     string `json:"day,omitempty" bson:"day,omitempty"`
	Date    string `json:"date,omitempty" bson:"date,omitempty"`
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

// Allowed values for optional enumerated payload fields.
var (
	ViewSurfaces = []string{"detail", "plan", "search", "recommendation"}
	PlanDays     = []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}
	PlanOrigins  = []string{"manual", "autopilot"}
	SkipReasons  = []string{"no-time", "ate-out", "missing-ingredients", "not-in-the-mood", "other"}
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
		validServings(p.Servings), optionalEnum("origin", p.Origin, PlanOrigins))
}

func (p RecipeUnplanned) validate() error {
	return errors.Join(validEntryID(p.EntryID), optionalEnum("day", p.Day, PlanDays), validDate(p.Date))
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

func validEntryID(id string) error {
	if len(id) > MaxClientEventIDLength {
		return invalid(fmt.Sprintf("entryId must be at most %d characters", MaxClientEventIDLength))
	}
	return nil
}
