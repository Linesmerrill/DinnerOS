package substitutes

import (
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

// Slug returns the stable ID form of a name: its normalized form with words
// joined by hyphens and apostrophes dropped ("Tex-Mex Paste" → "tex-mex-paste").
func Slug(name string) string {
	key := strings.ReplaceAll(ingredients.NormalizeName(name), "'", "")
	return strings.Join(strings.Fields(key), "-")
}

func cleanText(field, s string, maxLen int, required bool) (string, error) {
	s = strings.TrimSpace(s)
	switch {
	case required && s == "":
		return "", invalid("%s is required", field)
	case utf8.RuneCountInString(s) > maxLen:
		return "", invalid("%s must be at most %d characters", field, maxLen)
	}
	return s, nil
}

// normalizeAmount validates an optional exact amount. A quantity without a
// unit is a count; a unit without a quantity is invalid. The quantity is
// returned reduced ("1 1/2" → "3/2").
func normalizeAmount(field, quantity, unit string) (string, string, error) {
	quantity, unit = strings.TrimSpace(quantity), strings.TrimSpace(unit)
	if quantity == "" {
		if unit != "" {
			return "", "", invalid("%s.unit requires a quantity", field)
		}
		return "", "", nil
	}
	q, err := ingredients.ParseQuantity(quantity)
	if len(quantity) > maxQuantityLength || err != nil || q.IsZero() {
		return "", "", invalid("%s.quantity must be a positive amount such as 2, 0.5, 1/2, or 1 1/2", field)
	}
	if unit == "" {
		unit = "count"
	}
	if _, err := ingredients.LookupUnit(unit); err != nil {
		return "", "", invalid("%s.unit %q is not a DinnerOS unit code", field, unit)
	}
	return q.String(), unit, nil
}

func normalizeMeasure(field string, m *Measure) (*Measure, error) {
	if m == nil {
		return nil, invalid("%s is required", field)
	}
	q, unit, err := normalizeAmount(field, m.Quantity, m.Unit)
	if err != nil {
		return nil, err
	}
	if q == "" {
		return nil, invalid("%s needs a positive quantity", field)
	}
	return &Measure{Quantity: q, Unit: unit}, nil
}

func validCategory(c string) bool { return slices.Contains(grocery.CategoryOrder, c) }

// normalizeOption validates an option's content (not its IDs or authorship)
// and returns it cleaned.
func normalizeOption(o Option) (Option, error) {
	var err error
	if !o.Type.Valid() {
		return Option{}, invalid("type must be store_alternative or house_made_batch")
	}
	if o.Name, err = cleanText("name", o.Name, MaxNameLength, true); err != nil {
		return Option{}, err
	}
	if o.Notes, err = cleanText("notes", o.Notes, MaxNotesLength, false); err != nil {
		return Option{}, err
	}
	switch n := len(o.Ingredients); {
	case n == 0:
		return Option{}, invalid("ingredients must not be empty")
	case n > MaxComponents:
		return Option{}, invalid("ingredients must have at most %d entries", MaxComponents)
	}
	components := make([]Component, 0, len(o.Ingredients))
	for i, c := range o.Ingredients {
		field := "ingredients[" + itoa(i) + "]"
		if c.Name, err = cleanText(field+".name", c.Name, MaxNameLength, true); err != nil {
			return Option{}, err
		}
		if ingredients.NormalizeName(c.Name) == "" {
			return Option{}, invalid("%s.name must contain letters or digits", field)
		}
		if c.Quantity, c.Unit, err = normalizeAmount(field, c.Quantity, c.Unit); err != nil {
			return Option{}, err
		}
		if c.Quantity == "" && o.Type == TypeStoreAlternative {
			return Option{}, invalid("%s needs a quantity: a store alternative scales each ingredient", field)
		}
		c.Category = strings.ToLower(strings.TrimSpace(c.Category))
		if c.Category != "" && !validCategory(c.Category) {
			return Option{}, invalid("%s.category must be one of %s", field, strings.Join(grocery.CategoryOrder, ", "))
		}
		components = append(components, c)
	}
	o.Ingredients = components

	switch o.Type {
	case TypeStoreAlternative:
		if o.Per, err = normalizeMeasure("per", o.Per); err != nil {
			return Option{}, err
		}
		if o.Yield != nil || o.ShelfLifeDays != 0 || len(o.Steps) > 0 {
			return Option{}, invalid("yield, shelfLifeDays, and steps apply only to house_made_batch options")
		}
	case TypeHouseMadeBatch:
		if o.Per != nil {
			return Option{}, invalid("per applies only to store_alternative options")
		}
		if o.Yield, err = normalizeMeasure("yield", o.Yield); err != nil {
			return Option{}, err
		}
		if o.ShelfLifeDays < 1 || o.ShelfLifeDays > MaxShelfLifeDays {
			return Option{}, invalid("shelfLifeDays must be between 1 and %d", MaxShelfLifeDays)
		}
		if len(o.Steps) > MaxSteps {
			return Option{}, invalid("steps must have at most %d entries", MaxSteps)
		}
		steps := make([]string, 0, len(o.Steps))
		for i, step := range o.Steps {
			s, err := cleanText("steps["+itoa(i)+"]", step, MaxStepLength, true)
			if err != nil {
				return Option{}, err
			}
			steps = append(steps, s)
		}
		o.Steps = steps
	}
	if len(o.Steps) == 0 {
		o.Steps = nil
	}
	return o, nil
}

func itoa(i int) string { return strconv.Itoa(i) }
