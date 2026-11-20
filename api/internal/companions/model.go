// Package companions owns a household's companions (decisions 715-719):
// things that go with dinner but aren't dinner. Some are store items
// ("Tortilla chips", "a baguette"), some are the household's own add-on
// recipes ("Garlic Bread"). A companion says what it goes with (cuisines,
// specific recipes), how often it is due, and whether Autopilot adds it to
// the week's grocery list, suggests it, or leaves it alone.
//
// Evaluation lives with the pairings (recommendations/pairings_companions.go):
// companions show in the week's suggestions beside pairing rules and learned
// pairings, and accepted ones join the week's grocery list the same way.
//
// Existing add-on recipes become companions lazily: the first time a
// household's companions are read or evaluated, each add-on recipe without a
// companion gets one linked to it (Service.migrate). The add-on recipe itself
// is never changed or deleted.
package companions

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
)

// Errors returned by stores and the service.
var (
	ErrNotFound = errors.New("companions: not found")
	// ErrConflict means a write raced another one.
	ErrConflict = errors.New("companions: conflict")
)

// ValidationError describes invalid input. Its message is safe to show in
// the app.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

func invalid(format string, args ...any) error {
	return &ValidationError{Message: fmt.Sprintf(format, args...)}
}

// Frequencies: how often a companion is due without a meal it goes with.
const (
	EveryWeek     = "every_week"
	Every2Weeks   = "every_2_weeks"
	EveryMonth    = "every_month"
	FrequencyNone = "none"
)

// Modes: what happens when a companion is due.
const (
	// ModeAuto puts it on the week's grocery list (or plans its recipe)
	// without asking.
	ModeAuto = "auto"
	// ModeSuggest offers it in the week's suggestions, with Add and Not This
	// Week.
	ModeSuggest = "suggest"
	// ModeNever turns it off. For a companion linked to an add-on recipe it
	// also hides that add-on's other pairings.
	ModeNever = "never"
)

// Option is a value with the words the app shows for it.
type Option struct {
	Value string
	Label string
}

// FrequencyOptions are the "How often" choices, in the order the app shows
// them.
var FrequencyOptions = []Option{
	{Value: EveryWeek, Label: "Every Week"},
	{Value: Every2Weeks, Label: "Every 2 Weeks"},
	{Value: EveryMonth, Label: "Once a Month"},
	{Value: FrequencyNone, Label: "Only With Meals It Goes With"},
}

// ModeOptions are the "When it's due" choices.
var ModeOptions = []Option{
	{Value: ModeAuto, Label: "Add It for Me"},
	{Value: ModeSuggest, Label: "Just Suggest It"},
	{Value: ModeNever, Label: "Never"},
}

// Limits.
const (
	MaxNameLength      = 60
	MaxStoreItemLength = 60
	MaxURLLength       = 500
	MaxCuisines        = 12
	MaxRecipes         = 20
	MaxMealCategories  = 10
	MaxValueLength     = 40
	MaxQuantity        = 99
	// MaxPerHousehold bounds what one household makes by hand. Companions
	// made from add-on recipes don't count against it.
	MaxPerHousehold = 100
)

// RecipeRef is a recipe a companion names, with its name when it was saved.
type RecipeRef struct {
	ID   string
	Name string
}

// Companion is one thing that goes with dinner.
type Companion struct {
	ID          string
	HouseholdID string
	Name        string
	// StoreItem is what goes on the grocery list; Name when empty.
	StoreItem string
	// Quantity is exact ("1", "1/2") or ""; Unit is an ingredient unit code,
	// set when Quantity is.
	Quantity   string
	Unit       string
	ProductURL string
	// LinkedRecipe is the add-on recipe this companion was made from. Its
	// suggestions plan that recipe rather than a store item.
	LinkedRecipe *RecipeRef
	// Cuisines are canonical cuisine values; Recipes are specific main
	// meals. MealCategories are carried over from a pairing rule when an
	// add-on is migrated.
	Cuisines       []string
	Recipes        []RecipeRef
	MealCategories []string
	Frequency      string
	Mode           string
	// LastListedWeek is the last week it went on a grocery list (or into the
	// plan, for a linked recipe), and LastListedAt when.
	LastListedWeek string
	LastListedAt   *time.Time
	CreatedBy      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	// HiddenAt is set when a companion made from an add-on recipe is
	// deleted: it stays stored, hidden, so migration doesn't bring it back.
	HiddenAt *time.Time
}

// GroceryName is what goes on the list.
func (c Companion) GroceryName() string {
	if c.StoreItem != "" {
		return c.StoreItem
	}
	return c.Name
}

// Migrated reports whether the companion was made from an add-on recipe.
func (c Companion) Migrated() bool { return c.LinkedRecipe != nil }

// HasRecipe reports whether the companion goes with a specific recipe.
func (c Companion) HasRecipe(id string) bool {
	return slices.ContainsFunc(c.Recipes, func(r RecipeRef) bool { return r.ID == id })
}

// frequencyWeeks is how many weeks apart a frequency is due, or 0 for none.
func frequencyWeeks(f string) int {
	switch f {
	case EveryWeek:
		return 1
	case Every2Weeks:
		return 2
	case EveryMonth:
		return 4
	}
	return 0
}

// DueByFrequency reports whether the companion is due in w because of how
// often it is bought, whatever is planned: never listed, listed in w itself
// (it stays due, so the week shows it as on the list), or listed at least its
// frequency's weeks away from w, before or after.
func (c Companion) DueByFrequency(w planning.Week) bool {
	n := frequencyWeeks(c.Frequency)
	if n == 0 {
		return false
	}
	if c.LastListedWeek == "" || n == 1 {
		return true
	}
	last, err := planning.ParseWeek(c.LastListedWeek)
	if err != nil {
		return true
	}
	gap := last.WeeksUntil(w)
	if gap < 0 {
		gap = -gap
	}
	return gap == 0 || gap >= n
}

// Input is a companion as a member writes it.
type Input struct {
	Name       string
	StoreItem  string
	Quantity   string
	Unit       string
	ProductURL string
	Cuisines   []string
	RecipeIDs  []string
	// MealCategories round-trip what migration carried over; the app
	// doesn't offer them.
	MealCategories []string
	Frequency      string
	Mode           string
}

func optionValue(options []Option, v string) bool {
	return slices.ContainsFunc(options, func(o Option) bool { return o.Value == v })
}

func label(options []Option, v string) string {
	for _, o := range options {
		if o.Value == v {
			return o.Label
		}
	}
	return v
}

func clean(s string) string { return strings.Join(strings.Fields(s), " ") }

// normalize validates in and puts it in canonical form. canonicalCuisine and
// mealCategory may be nil. Recipe IDs are checked by the service. Messages
// reach the app, so they are plain.
func (in Input) normalize(canonicalCuisine func(string) string, mealCategory func(string) bool) (Input, error) {
	out := Input{
		Name: clean(in.Name), StoreItem: clean(in.StoreItem), ProductURL: strings.TrimSpace(in.ProductURL),
		Frequency: strings.ToLower(strings.TrimSpace(in.Frequency)), Mode: strings.ToLower(strings.TrimSpace(in.Mode)),
		Unit: strings.TrimSpace(in.Unit),
	}
	switch n := utf8.RuneCountInString(out.Name); {
	case n == 0:
		return Input{}, invalid("Give it a name.")
	case n > MaxNameLength:
		return Input{}, invalid("Keep the name under %d letters.", MaxNameLength)
	}
	if utf8.RuneCountInString(out.StoreItem) > MaxStoreItemLength {
		return Input{}, invalid("Keep the store item under %d letters.", MaxStoreItemLength)
	}
	if strings.EqualFold(out.StoreItem, out.Name) {
		out.StoreItem = ""
	}
	raw := strings.TrimSpace(in.Quantity)
	switch {
	case raw == "":
		out.Unit = ""
	default:
		q, err := ingredients.ParseQuantity(raw)
		if err != nil || q.IsZero() || q.Cmp(ingredients.NewQuantity(MaxQuantity, 1)) > 0 {
			return Input{}, invalid("Pick an amount from 1 to %d.", MaxQuantity)
		}
		out.Quantity = q.String()
		if out.Unit == "" {
			out.Unit = "count"
		}
		if _, err := ingredients.LookupUnit(out.Unit); err != nil {
			return Input{}, invalid("Pick an amount from the list.")
		}
	}
	if out.ProductURL != "" {
		u, err := url.Parse(out.ProductURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || len(out.ProductURL) > MaxURLLength {
			return Input{}, invalid("That link doesn’t look right. Paste a web address that starts with http or https.")
		}
	}
	for _, c := range in.Cuisines {
		v := strings.ToLower(clean(c))
		if canonicalCuisine != nil {
			v = canonicalCuisine(v)
		}
		if v == "" || utf8.RuneCountInString(v) > MaxValueLength {
			return Input{}, invalid("Pick a cuisine from the list.")
		}
		if !slices.Contains(out.Cuisines, v) {
			out.Cuisines = append(out.Cuisines, v)
		}
	}
	if len(out.Cuisines) > MaxCuisines {
		return Input{}, invalid("Pick up to %d cuisines.", MaxCuisines)
	}
	for _, id := range in.RecipeIDs {
		id = strings.ToLower(strings.TrimSpace(id))
		if id != "" && !slices.Contains(out.RecipeIDs, id) {
			out.RecipeIDs = append(out.RecipeIDs, id)
		}
	}
	if len(out.RecipeIDs) > MaxRecipes {
		return Input{}, invalid("Pick up to %d recipes.", MaxRecipes)
	}
	for _, c := range in.MealCategories {
		v := strings.ToLower(clean(c))
		if v == "" || (mealCategory != nil && !mealCategory(v)) {
			return Input{}, invalid("That kind of meal isn’t one we know.")
		}
		if !slices.Contains(out.MealCategories, v) {
			out.MealCategories = append(out.MealCategories, v)
		}
	}
	if len(out.MealCategories) > MaxMealCategories {
		return Input{}, invalid("Pick up to %d kinds of meals.", MaxMealCategories)
	}
	if out.Frequency == "" {
		out.Frequency = FrequencyNone
		if len(out.Cuisines)+len(out.RecipeIDs)+len(out.MealCategories) == 0 {
			out.Frequency = EveryWeek
		}
	}
	if !optionValue(FrequencyOptions, out.Frequency) {
		return Input{}, invalid("Pick how often you get it.")
	}
	if out.Mode == "" {
		out.Mode = ModeSuggest
	}
	if !optionValue(ModeOptions, out.Mode) {
		return Input{}, invalid("Pick what happens when it’s due.")
	}
	return out, nil
}

// Summary is the one line under a companion in the list: "Goes with Mexican
// · Every week · Just suggest it". cuisineLabel may be nil.
func (c Companion) Summary(cuisineLabel func(string) string) string {
	var with []string
	for _, v := range c.Cuisines {
		l := ""
		if cuisineLabel != nil {
			l = cuisineLabel(v)
		}
		if l == "" {
			l = titleCase(v)
		}
		with = append(with, l)
	}
	for _, r := range c.Recipes {
		if r.Name != "" {
			with = append(with, r.Name)
		}
	}
	for _, m := range c.MealCategories {
		with = append(with, mealCategoryWords(m))
	}
	var parts []string
	switch {
	case len(with) > 0:
		parts = append(parts, "Goes with "+joinAnd(with))
	case c.Frequency == FrequencyNone:
		parts = append(parts, "Not paired with anything yet")
	}
	if c.Frequency != FrequencyNone {
		parts = append(parts, sentenceCase(label(FrequencyOptions, c.Frequency)))
	}
	parts = append(parts, modeWords[c.Mode])
	return strings.Join(parts, " · ")
}

// modeWords say what a mode does, in a list row.
var modeWords = map[string]string{ModeAuto: "Added for you", ModeSuggest: "Suggested", ModeNever: "Turned off"}

// FrequencyPhrase finishes "You get it ...": "every week".
func FrequencyPhrase(f string) string {
	switch f {
	case EveryWeek:
		return "every week"
	case Every2Weeks:
		return "every 2 weeks"
	case EveryMonth:
		return "once a month"
	}
	return ""
}

// mealCategoryWords names a meal category in a sentence: "pasta meals".
func mealCategoryWords(category string) string {
	return strings.ReplaceAll(category, "_", " ") + " meals"
}

func titleCase(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

func sentenceCase(s string) string {
	if s == "" {
		return s
	}
	return s[:1] + strings.ToLower(s[1:])
}

func joinAnd(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	case 2:
		return items[0] + " and " + items[1]
	}
	return strings.Join(items[:len(items)-1], ", ") + ", and " + items[len(items)-1]
}
