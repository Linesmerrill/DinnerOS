// Package pantry owns a household's pantry: what it has at home, what is
// running low or out, and the always-have staples (salt, oil). The grocery
// engine reads it through Service.GroceryPantry.
//
// One item exists per ingredient per household: (householdId, key) is unique,
// where key is ingredients.NormalizeName of the ingredient (the catalog Key
// when the item references the catalog). Adding an ingredient the pantry
// already has updates that item instead of creating a second one.
package pantry

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

// Errors returned by stores and the service.
var (
	ErrNotFound  = errors.New("pantry: not found")
	ErrDuplicate = errors.New("pantry: duplicate")
	// ErrForbidden means the actor's role lacks pantry.edit.
	ErrForbidden = errors.New("pantry: forbidden")
	// ErrConflict means a concurrent change kept winning; the caller may retry.
	ErrConflict = errors.New("pantry: concurrent change")

	errHouseholdRequired = errors.New("pantry: household id is required")
)

// ValidationError describes invalid input. Its message is safe to return to
// API clients.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

func invalid(format string, args ...any) error {
	return &ValidationError{Message: fmt.Sprintf(format, args...)}
}

// Status is how much of an item the household has.
type Status string

// Item statuses.
const (
	StatusInStock Status = "in_stock"
	StatusLow     Status = "low"
	StatusOut     Status = "out"
)

// Valid reports whether s is a known status.
func (s Status) Valid() bool {
	switch s {
	case StatusInStock, StatusLow, StatusOut:
		return true
	}
	return false
}

// Storage is where in the house an item is kept.
type Storage string

// Storages. The empty value means StoragePantry: every item written before
// the freezer existed is on a shelf.
const (
	StoragePantry  Storage = "pantry"
	StorageFreezer Storage = "freezer"
)

// Valid reports whether s is a known storage.
func (s Storage) Valid() bool {
	switch s {
	case StoragePantry, StorageFreezer:
		return true
	}
	return false
}

// Or returns s, or StoragePantry when it is empty.
func (s Storage) Or() Storage {
	if s == "" {
		return StoragePantry
	}
	return s
}

// Limits.
const (
	// MaxItems bounds a household's pantry so lists can stay unpaginated.
	MaxItems          = 1000
	MaxBulkUpdates    = 200
	MaxNameLength     = 100
	MaxNoteLength     = 500
	maxQuantityLength = 32
	maxSearchLength   = 100
	// DateLayout is the format of ExpiresOn.
	DateLayout = "2006-01-02"
)

// Item is one ingredient in a household's pantry.
type Item struct {
	ID          string
	HouseholdID string
	// IngredientID references the global ingredient catalog. It is empty for
	// free-text items that matched no catalog ingredient when added.
	IngredientID string
	// Key is ingredients.NormalizeName of the ingredient and is unique per
	// household.
	Key         string
	DisplayName string
	Category    string
	// Quantity is the exact amount as "n" or "n/d". It is empty when the
	// household didn't say how much ("have some"); Unit is then empty too.
	Quantity string
	// Unit is a DinnerOS unit code (ingredients.LookupUnit).
	Unit   string
	Status Status
	// IsStaple marks always-have items such as salt and oil.
	IsStaple bool
	// ExpiresOn is a calendar date (DateLayout) or empty.
	ExpiresOn string
	Note      string

	// Storage is where the item is kept; empty means StoragePantry. A
	// freezer item covers a grocery line without being bought again
	// (freezer.go, docs/pantry-usage.md#the-freezer).
	Storage Storage
	// FrozenOn is the calendar date (DateLayout) the item went in the
	// freezer, or empty.
	FrozenOn string
	// Portions is how many sealed portions the frozen amount was split
	// into, or 0 when nobody said. It decides the thaw estimate's weight:
	// one portion is thawed, not the whole bag.
	Portions int
	// FrozenFrom is the handoff line whose bulk pack was sealed, as
	// "<handoffId>:<lineId>", or empty. Freezing the same line twice is a
	// no-op, so a retried request never doubles the freezer.
	FrozenFrom string

	// StatusSource says who set Status: a person (the default for items
	// written before usage tracking) or the usage estimate.
	StatusSource StatusSource
	// StatusSetAt is when Status last changed. Zero for older items.
	StatusSetAt time.Time
	// Tracking is the current usage cycle, or nil when the household hasn't
	// recorded how much it has (docs/pantry-usage.md).
	Tracking *Tracking
	// UnitSize says how much one discrete unit holds ("1 package = 8 oz"),
	// so purchases counted in that unit are tracked in a measurable one.
	UnitSize *UnitSize
	// History holds the most recent closed usage segments, oldest first, at
	// most MaxHistorySegments.
	History []Segment
	// Rate is the learned non-recipe use, or nil until there's enough
	// history.
	Rate *Rate
	// LowThresholdPercent overrides the household's threshold for this item;
	// 0 means use the household's.
	LowThresholdPercent int
	// LowAlertCycleID is the cycle the estimate last marked low and alerted
	// about, so each cycle alerts at most once.
	LowAlertCycleID string

	// Version increases with every write; stores use it to reject updates
	// based on a stale read.
	Version   int64
	CreatedAt time.Time
	UpdatedBy string
	UpdatedAt time.Time
}

// AddInput is the input for Service.Add. Either IngredientID or Name is
// required. Empty optional fields are left unset (or, when merging into an
// existing item, unchanged).
type AddInput struct {
	IngredientID string
	// Name is the display name. With IngredientID it defaults to the catalog
	// name.
	Name string
	// Category defaults to the catalog category, or ingredients.Categorize.
	Category string
	Quantity string
	Unit     string
	// Status defaults to in_stock: adding something means you have it.
	Status    Status
	IsStaple  *bool
	ExpiresOn string
	Note      string
}

// UpdateInput is a partial update; nil fields are unchanged. For Quantity,
// ExpiresOn, and Note an empty string clears the value (clearing Quantity also
// clears Unit). LowThresholdPercent 0 clears the item's override.
type UpdateInput struct {
	DisplayName         *string
	Category            *string
	Quantity            *string
	Unit                *string
	Status              *Status
	IsStaple            *bool
	ExpiresOn           *string
	Note                *string
	LowThresholdPercent *int
}

// StatusUpdate sets one item's status.
type StatusUpdate struct {
	ItemID string
	Status Status
}

// BulkResult is returned by Service.SetStatuses. Items are in request order;
// Missing lists requested IDs that are not in the household's pantry.
type BulkResult struct {
	Items   []Item
	Missing []string
}

// StaplesResult is returned by Service.AddDefaultStaples.
type StaplesResult struct {
	Added []Item
	// Skipped counts defaults the pantry already had.
	Skipped int
}

// ListQuery filters a pantry list. Empty fields don't filter.
type ListQuery struct {
	Status   string
	Category string
	// Search matches display names containing the text (case-insensitive) or
	// keys containing its normalized form. Literal text, not a pattern.
	Search string
	Staple *bool
	// Storage, when set, keeps only items kept there. "pantry" also matches
	// items stored before the freezer existed, whose storage is empty.
	Storage string
}

// ListFilter is a validated ListQuery as stores receive it.
type ListFilter struct {
	Status   Status
	Category string
	Staple   *bool
	Storage  Storage
	// NamePattern, when set, is a regular expression (metacharacters escaped)
	// matched case-insensitively against DisplayName. KeyPattern, when set, is
	// matched case-sensitively against Key. An item matches if either does.
	NamePattern string
	KeyPattern  string
}

func (q ListQuery) filter() (ListFilter, error) {
	f := ListFilter{Staple: q.Staple}
	if s := strings.TrimSpace(q.Status); s != "" {
		f.Status = Status(s)
		if !f.Status.Valid() {
			return ListFilter{}, invalid("status must be in_stock, low, or out")
		}
	}
	if c := strings.TrimSpace(q.Category); c != "" {
		category, err := normalizeCategory(c)
		if err != nil {
			return ListFilter{}, err
		}
		f.Category = category
	}
	if st := strings.TrimSpace(q.Storage); st != "" {
		f.Storage = Storage(st)
		if !f.Storage.Valid() {
			return ListFilter{}, invalid("storage must be pantry or freezer")
		}
	}
	search := strings.TrimSpace(q.Search)
	if utf8.RuneCountInString(search) > maxSearchLength {
		return ListFilter{}, invalid("q must be at most %d characters", maxSearchLength)
	}
	if search != "" {
		f.NamePattern = regexp.QuoteMeta(search)
		if key := ingredients.NormalizeName(search); key != "" {
			f.KeyPattern = regexp.QuoteMeta(key)
		}
	}
	return f, nil
}

// --- Validation ---------------------------------------------------------------

func normalizeDisplayName(name string) (string, error) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return "", invalid("name is required")
	case utf8.RuneCountInString(name) > MaxNameLength:
		return "", invalid("name must be at most %d characters", MaxNameLength)
	}
	return name, nil
}

// normalizeCategory accepts the grocery categories (grocery.CategoryOrder).
func normalizeCategory(c string) (string, error) {
	c = strings.ToLower(strings.TrimSpace(c))
	if !slices.Contains(grocery.CategoryOrder, c) {
		return "", invalid("category must be one of %s", strings.Join(grocery.CategoryOrder, ", "))
	}
	return c, nil
}

// catalogCategory maps a stored catalog category onto a known one.
func catalogCategory(c string) string {
	if category, err := normalizeCategory(c); err == nil {
		return category
	}
	return ingredients.CategoryOther
}

// normalizeAmount validates an optional amount. A quantity without a unit is a
// count; a unit without a quantity is invalid. The quantity is returned in
// exact reduced form ("1 1/2" → "3/2").
func normalizeAmount(quantity, unit string) (string, string, error) {
	quantity, unit = strings.TrimSpace(quantity), strings.TrimSpace(unit)
	if quantity == "" {
		if unit != "" {
			return "", "", invalid("unit requires a quantity")
		}
		return "", "", nil
	}
	q, err := ingredients.ParseQuantity(quantity)
	if len(quantity) > maxQuantityLength || err != nil || q.IsZero() {
		return "", "", invalid("quantity must be a positive amount such as 2, 0.5, 1/2, or 1 1/2")
	}
	if unit == "" {
		unit = "count"
	}
	if _, err := ingredients.LookupUnit(unit); err != nil {
		return "", "", invalid("unit %q is not a DinnerOS unit code", unit)
	}
	return q.String(), unit, nil
}

func normalizeDate(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	if _, err := time.Parse(DateLayout, s); err != nil {
		return "", invalid("expiresOn must be a date in YYYY-MM-DD form")
	}
	return s, nil
}

func normalizeNote(s string) (string, error) {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > MaxNoteLength {
		return "", invalid("note must be at most %d characters", MaxNoteLength)
	}
	return s, nil
}
