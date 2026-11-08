// Package sameitems owns the names a household says are the same thing to
// buy (decision 661): recipes say "Chipotle Powder" and "Chipotle Chili
// Powder", or "Fine Garlic Powder" and "Garlic Powder", for one jar. A link
// makes the grocery list and the Shop tab treat the variant as the kept item:
// one line, one amount, one product, and the pantry and skips answer for
// both.
//
// A link never rewrites a recipe: the recipe keeps its own words, and the
// list says which other names an item was bought under so the link can be
// undone.
package sameitems

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
	ErrNotFound = errors.New("sameitems: not found")
	// ErrForbidden means the actor's role lacks plan.edit.
	ErrForbidden = errors.New("sameitems: forbidden")

	errHouseholdRequired = errors.New("sameitems: household id is required")
)

// ValidationError describes invalid input. Its message is safe to show in
// the app.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

func invalid(format string, args ...any) error {
	return &ValidationError{Message: fmt.Sprintf(format, args...)}
}

// Limits.
const (
	// MaxNameLength is the longest display name, in characters.
	MaxNameLength = 200
	// MaxKeyLength bounds a stored grocery line key.
	MaxKeyLength = 300
	// MaxPerHousehold bounds how many links a household keeps, so one query
	// returns them all.
	MaxPerHousehold = 200
)

// UnresolvedKeyPrefix prefixes the grocery line key of a recipe line with no
// catalog ingredient ID, as in the planning and skips packages.
const UnresolvedKeyPrefix = "name:"

// Link says the variant is the same thing to buy as the kept item. Links
// never chain: a kept item is never itself a variant (Service.Link keeps it
// that way).
type Link struct {
	ID          string
	HouseholdID string
	// VariantKey is the grocery line key the link was made from: a catalog
	// ingredient ID or UnresolvedKeyPrefix + a normalized name. Unique per
	// household: a name is the same as one other item at most.
	VariantKey  string
	VariantName string
	KeptKey     string
	KeptName    string
	CreatedBy   string
	CreatedAt   time.Time
}

// Input is a request to link a variant to the item it is the same as.
type Input struct {
	VariantKey  string
	VariantName string
	KeptKey     string
	KeptName    string
}

// normalizeKey trims a key and normalizes a free-text one, so a client that
// builds "name:Fine Garlic Powder" matches the key the list gives that line.
func normalizeKey(key string) string {
	key = strings.TrimSpace(key)
	if rest, ok := strings.CutPrefix(key, UnresolvedKeyPrefix); ok {
		if normalized := ingredients.NormalizeName(rest); normalized != "" {
			return UnresolvedKeyPrefix + normalized
		}
	}
	return key
}

// normalize validates in. Its messages reach the app, so they are plain.
func (in Input) normalize() (Input, error) {
	in.VariantKey, in.KeptKey = normalizeKey(in.VariantKey), normalizeKey(in.KeptKey)
	in.VariantName, in.KeptName = strings.TrimSpace(in.VariantName), strings.TrimSpace(in.KeptName)
	for _, key := range []string{in.VariantKey, in.KeptKey} {
		if key == "" || utf8.RuneCountInString(key) > MaxKeyLength {
			return Input{}, invalid("That item can't be linked. Try another one.")
		}
	}
	for _, name := range []string{in.VariantName, in.KeptName} {
		if ingredients.NormalizeName(name) == "" || utf8.RuneCountInString(name) > MaxNameLength {
			return Input{}, invalid("That item can't be linked. Try another one.")
		}
	}
	if in.VariantKey == in.KeptKey {
		return Input{}, invalid("Pick a different item.")
	}
	return in, nil
}
