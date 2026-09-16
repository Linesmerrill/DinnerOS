// Package substitutes owns specialty ingredients: the proprietary blends,
// sauces, pastes, and stock concentrates that meal-kit recipes name but a
// grocery store doesn't sell under that name. Each has curated options, a
// store alternative (regular ingredients per amount) or a house-made batch (a
// small recipe kept in the pantry), plus household custom options and a
// household choice. Grocery lists and cook deductions use the choice through
// small interfaces (GrocerySpecialties, ResolveKeys).
//
// The curated set is versioned in seed/specialty-ingredients.json, embedded
// in the binary, and synced to MongoDB on startup (SyncSeed).
// docs/specialty-ingredients.md explains the model.
package substitutes

import (
	"errors"
	"fmt"
	"time"
)

// Errors returned by stores and the service.
var (
	ErrNotFound  = errors.New("substitutes: not found")
	ErrDuplicate = errors.New("substitutes: duplicate")
	// ErrForbidden means the actor's role lacks pantry.edit.
	ErrForbidden = errors.New("substitutes: forbidden")

	errHouseholdRequired = errors.New("substitutes: household id is required")
)

// ValidationError describes invalid input. Its message is safe to return to
// API clients.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

func invalid(format string, args ...any) error {
	return &ValidationError{Message: fmt.Sprintf(format, args...)}
}

// OptionType is the kind of an option.
type OptionType string

// Option types.
const (
	// TypeStoreAlternative: regular grocery ingredients per amount of the
	// specialty ingredient.
	TypeStoreAlternative OptionType = "store_alternative"
	// TypeHouseMadeBatch: a small recipe that makes a batch kept in the pantry.
	TypeHouseMadeBatch OptionType = "house_made_batch"
)

// Valid reports whether t is a known option type.
func (t OptionType) Valid() bool {
	return t == TypeStoreAlternative || t == TypeHouseMadeBatch
}

// OptionSource says who wrote an option.
type OptionSource string

// Option sources.
const (
	SourceCurated   OptionSource = "curated"
	SourceHousehold OptionSource = "household"
)

// OptionAsIs is the choice that keeps a specialty ingredient on grocery lists
// by its own name (the household buys it somewhere, or doesn't want to be
// asked).
const OptionAsIs = "as_is"

// Limits.
const (
	MaxOptionsPerSpecialty = 10
	MaxNameLength          = 100
	MaxNotesLength         = 500
	MaxNoteLength          = 300
	MaxComponents          = 20
	MaxSteps               = 20
	MaxStepLength          = 500
	MaxShelfLifeDays       = 730
	MaxBatches             = 10
	maxQuantityLength      = 32
)

// Measure is an exact amount: Quantity is "n" or "n/d", Unit a DinnerOS unit
// code.
type Measure struct {
	Quantity string
	Unit     string
}

// UnitSize says one Per (a discrete unit such as count or package) holds
// Quantity Unit: "1 count = 2 tsp".
type UnitSize struct {
	Per      string
	Quantity string
	Unit     string
}

// Component is one regular ingredient of an option. Quantity and Unit are
// empty for "to taste" (batches only). Category, when set, is used for an
// ingredient the catalog doesn't know.
type Component struct {
	Name     string
	Quantity string
	Unit     string
	Category string
}

// Option is a way to replace a specialty ingredient.
type Option struct {
	ID          string
	SpecialtyID string
	Source      OptionSource
	// HouseholdID is set for household options.
	HouseholdID string
	Type        OptionType
	Name        string
	Notes       string
	// Per is the specialty amount the components replace (store alternatives).
	Per *Measure
	// Ingredients are per Per for a store alternative, or for one batch.
	Ingredients []Component
	// Steps, Yield, and ShelfLifeDays describe a batch.
	Steps         []string
	Yield         *Measure
	ShelfLifeDays int
	// BasedOnOptionID is the option a household option was copied from.
	BasedOnOptionID string
	CreatedBy       string
	UpdatedBy       string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Specialty is a curated specialty ingredient. The curated set is global.
type Specialty struct {
	// ID is a stable slug ("tex-mex-paste").
	ID string
	// Key is ingredients.NormalizeName(Name); AliasKeys are the aliases'.
	// Recipe ingredients whose catalog key or normalized name is either are
	// this specialty.
	Key       string
	Name      string
	Aliases   []string
	AliasKeys []string
	Category  string
	// Note is a short shopping note the app can show, for a specialty
	// ingredient whose store route needs a caveat ("sold in the international
	// aisle") or that has no honest store equivalent at all. Usually empty.
	Note      string
	UnitSizes []UnitSize
	// DefaultOptionID is the curated option suggested first.
	DefaultOptionID string
	// Options are the curated options, with Source curated.
	Options []Option
	// SeedVersion and ContentHash identify the seed content last synced.
	SeedVersion int
	ContentHash string
	// Retired is true when the seed no longer has it. It stays so existing
	// choices still resolve, but isn't listed or matched.
	Retired   bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// option returns the curated option with id.
func (s Specialty) option(id string) (Option, bool) {
	for _, o := range s.Options {
		if o.ID == id {
			return o, true
		}
	}
	return Option{}, false
}

// Choice is a household's chosen option for a specialty ingredient.
type Choice struct {
	HouseholdID string
	SpecialtyID string
	// OptionID is a curated or household option ID, or OptionAsIs.
	OptionID string
	ChosenBy string
	ChosenAt time.Time
}
