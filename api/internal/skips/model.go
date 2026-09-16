// Package skips owns the ingredients a household leaves off its grocery list
// on purpose: the ones it would buy, throw away, and resent buying again
// (cilantro, for the household that tastes soap in it).
//
// A skip is about an INGREDIENT for a HOUSEHOLD, not about a product or a
// single recipe, and it is a third state alongside the two that already exist:
//
//   - the pantry says the household HAS it (grocery.StatusInPantry),
//   - check-off says a member BOUGHT it (client-side, per week),
//   - a skip says the household never WANTS it.
//
// Skipping never rewrites a recipe. The recipe still lists the ingredient; the
// grocery list just doesn't ask anyone to buy it, and says so
// (grocery.List.SkippedItems), so the two never disagree about reality.
package skips

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

// Errors returned by stores and the service.
var (
	ErrNotFound  = errors.New("skips: not found")
	ErrDuplicate = errors.New("skips: duplicate")
	// ErrForbidden means the actor's role lacks plan.edit.
	ErrForbidden = errors.New("skips: forbidden")

	errHouseholdRequired = errors.New("skips: household id is required")
)

// ValidationError describes invalid input. Its message is safe to return to
// API clients.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

func invalid(format string, args ...any) error {
	return &ValidationError{Message: fmt.Sprintf(format, args...)}
}

// Limits.
const (
	// MaxNameLength is the longest display name, in characters.
	MaxNameLength = 200
	// MaxIngredientKeyLength bounds the stored line key.
	MaxIngredientKeyLength = 300
	// MaxPerHousehold bounds how many ingredients a household may skip, so the
	// list stays readable and one query returns it whole.
	MaxPerHousehold = 500
)

// UnresolvedKeyPrefix prefixes the grocery line key of a recipe line with no
// catalog ingredient ID, matching pantry.UnresolvedKeyPrefix.
const UnresolvedKeyPrefix = "name:"

// Scope is how long a skip lasts. The household asked for exactly two
// lifetimes: "skip once" and "skip forever".
type Scope string

// Scopes.
const (
	// ScopeWeek leaves the ingredient off one week's list. Next week it is
	// back, with no action needed.
	ScopeWeek Scope = "week"
	// ScopeAlways leaves it off every list until someone resumes it.
	ScopeAlways Scope = "always"
)

// Valid reports whether s is a known scope.
func (s Scope) Valid() bool { return s == ScopeWeek || s == ScopeAlways }

// Skip is one ingredient a household leaves off its grocery list.
type Skip struct {
	ID          string
	HouseholdID string
	// IngredientKey is the grocery line key the skip was made from: a catalog
	// ingredient ID, or UnresolvedKeyPrefix + the normalized name. It is the
	// unique key, so one ingredient has at most one skip per household.
	IngredientKey string
	// Key is ingredients.NormalizeName of Name. A recipe can reach the same
	// ingredient with a catalog ID or as free text, so the skip is matched
	// under both spellings (see Keys).
	Key string
	// Name is what the list called the ingredient, for the review screen.
	Name  string
	Scope Scope
	// Week is the ISO week a ScopeWeek skip applies to ("2026-W38"), and is
	// empty for ScopeAlways. A week-scoped skip for a past week simply stops
	// matching; nothing is deleted behind the household's back.
	Week      string
	CreatedBy string
	CreatedAt time.Time
	UpdatedBy string
	UpdatedAt time.Time
}

// Keys returns the grocery line keys this skip matches: the key it was made
// from, and UnresolvedKeyPrefix + Key so a recipe that names the ingredient as
// free text matches a skip made from a catalog line, and the other way round.
// The catalog ID for a free-text skip is resolved at read time
// (Service.GrocerySkips), as the pantry does.
func (s Skip) Keys() []string {
	keys := make([]string, 0, 2)
	if s.IngredientKey != "" {
		keys = append(keys, s.IngredientKey)
	}
	if s.Key != "" {
		named := UnresolvedKeyPrefix + s.Key
		if named != s.IngredientKey {
			keys = append(keys, named)
		}
	}
	return keys
}

// AppliesTo reports whether the skip leaves the ingredient off week's list.
// An always-skip applies to every week; a week-skip only to its own.
func (s Skip) AppliesTo(week string) bool {
	if s.Scope == ScopeAlways {
		return true
	}
	return s.Week != "" && s.Week == week
}

// Input is a request to skip an ingredient. An input for an ingredient the
// household already skips replaces that skip, so "skip once" can be changed to
// "skip forever" without removing anything first.
type Input struct {
	IngredientKey string
	// Name is the ingredient's display name. It is required for a skip made
	// from a key the catalog doesn't resolve, since nothing else can name it.
	Name  string
	Scope Scope
	// Week is required for ScopeWeek and must be empty for ScopeAlways.
	Week string
}

// normalize validates in and fills in what the store needs.
func (in Input) normalize() (Input, string, error) {
	in.IngredientKey = strings.TrimSpace(in.IngredientKey)
	in.Name = strings.TrimSpace(in.Name)
	in.Week = strings.TrimSpace(in.Week)
	switch {
	case in.IngredientKey == "":
		return Input{}, "", invalid("ingredientKey is required")
	case utf8.RuneCountInString(in.IngredientKey) > MaxIngredientKeyLength:
		return Input{}, "", invalid("ingredientKey must be at most %d characters", MaxIngredientKeyLength)
	case !in.Scope.Valid():
		return Input{}, "", invalid("scope must be week or always")
	case utf8.RuneCountInString(in.Name) > MaxNameLength:
		return Input{}, "", invalid("name must be at most %d characters", MaxNameLength)
	case in.Scope == ScopeWeek && in.Week == "":
		return Input{}, "", invalid("week is required when scope is week")
	case in.Scope == ScopeAlways && in.Week != "":
		return Input{}, "", invalid("week must be empty when scope is always")
	}
	// The name is what the review screen shows, so a skip without one is only
	// useful when the key itself carries the name.
	name := in.Name
	if name == "" {
		name = strings.TrimPrefix(in.IngredientKey, UnresolvedKeyPrefix)
		if name == in.IngredientKey {
			return Input{}, "", invalid("name is required")
		}
		in.Name = name
	}
	key := ingredients.NormalizeName(in.Name)
	if key == "" {
		return Input{}, "", invalid("name must contain letters or numbers")
	}
	return in, key, nil
}
