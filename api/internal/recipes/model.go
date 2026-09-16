package recipes

import (
	"errors"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

// Errors returned by stores and the service.
var (
	ErrNotFound  = errors.New("recipes: not found")
	ErrDuplicate = errors.New("recipes: duplicate")
	// ErrInvalidImport means the import file as a whole is unusable (wrong
	// version or source). Problems with individual recipes are reported in
	// ImportResult.Errors instead.
	ErrInvalidImport = errors.New("recipes: invalid import file")
	// ErrInvalidQuery means list parameters, including the cursor, are invalid.
	ErrInvalidQuery = errors.New("recipes: invalid query")

	errHouseholdRequired = errors.New("recipes: household id is required")
)

// Recipe sources accepted by the import contract (docs/import-format.md).
const (
	SourceHelloFresh = "hellofresh"
	SourceManual     = "manual"
	SourceImport     = "import"
	SourceUser       = "user"
	SourceProvider   = "provider"
	SourcePartner    = "partner"
)

func knownSource(s string) bool {
	switch s {
	case SourceHelloFresh, SourceManual, SourceImport, SourceUser, SourceProvider, SourcePartner:
		return true
	}
	return false
}

// Recipe is a household's recipe. (HouseholdID, Source, SourceRecipeID) is
// unique; SourceAliases holds other source IDs known to be the same recipe.
type Recipe struct {
	ID             string
	HouseholdID    string
	Source         string
	SourceRecipeID string
	SourceAliases  []string
	SourceURL      string
	Name           string
	Headline       string
	Description    string
	ImageURL       string
	IsAddon        bool
	Servings       []int
	PrepMinutes    int
	TotalMinutes   int
	Difficulty     int
	Cuisines       []string
	Tags           []string
	Utensils       []string
	Allergens      []string
	Nutrition      []Nutrient
	Ingredients    []RecipeIngredient
	Steps          []Step
	// OrderWeeks are the ISO weeks ("2026-W30") the household received this
	// recipe, sorted. TimesOrdered and LastOrderedWeek are derived from it.
	OrderWeeks      []string
	TimesOrdered    int
	LastOrderedWeek string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// CookMinutes is the recipe's effective cook time (see CookMinutes).
func (r Recipe) CookMinutes() int { return CookMinutes(r.PrepMinutes, r.TotalMinutes) }

// CookMinutes returns a source-agnostic effective cook time: the larger of
// prep and total minutes, ignoring missing (zero or negative) values. It is 0
// when neither is usable, which callers treat as unknown.
//
// Sources don't agree on what the two fields mean. Some report a total that is
// smaller than the prep time, or only a prep time, so neither field can be
// used on its own: a total below prep can't be a total. The larger value is
// the best available estimate of how long dinner takes.
func CookMinutes(prepMinutes, totalMinutes int) int {
	return max(prepMinutes, totalMinutes, 0)
}

// Nutrient is a per-serving nutrition value.
type Nutrient struct {
	Name   string
	Amount float64
	Unit   string
}

// Step is one ordered instruction.
type Step struct {
	Index    int
	Text     string
	ImageURL string
}

// RecipeIngredient is an ingredient line that references the catalog.
type RecipeIngredient struct {
	IngredientID string
	Name         string
	// Category comes from the ingredient catalog. Service.Get fills it in; it
	// is not stored on the recipe.
	Category string
	// ImageURL is the catalog ingredient's image. Like Category, Service.Get
	// fills it in and it is not stored on the recipe.
	ImageURL     string
	PantryStaple bool
	// Amounts has one entry per serving size, ascending.
	Amounts []Amount
}

// Amount is an ingredient quantity for one serving size.
type Amount struct {
	Servings int
	// Quantity is the exact amount as "n" or "n/d". It is empty when the
	// source gave no amount ("to taste").
	Quantity   string
	Unit       string
	SourceUnit string
	RawText    string
}

// ExactQuantity returns the parsed quantity. ok is false when there is none.
func (a Amount) ExactQuantity() (q ingredients.Quantity, ok bool) {
	if a.Quantity == "" {
		return ingredients.Quantity{}, false
	}
	q, err := ingredients.ParseQuantity(a.Quantity)
	return q, err == nil
}

// RecipeSummary is the list view of a recipe.
type RecipeSummary struct {
	ID              string
	Name            string
	Headline        string
	ImageURL        string
	PrepMinutes     int
	TotalMinutes    int
	TimesOrdered    int
	LastOrderedWeek string
	IsAddon         bool
	Tags            []string
	// Nutrition is the per-serving list, read for Facts.
	Nutrition []Nutrient
}

// SummaryOf returns the list view of a recipe.
func SummaryOf(r Recipe) RecipeSummary {
	return RecipeSummary{
		ID: r.ID, Name: r.Name, Headline: r.Headline, ImageURL: r.ImageURL, PrepMinutes: r.PrepMinutes, TotalMinutes: r.TotalMinutes,
		TimesOrdered: r.TimesOrdered, LastOrderedWeek: r.LastOrderedWeek, IsAddon: r.IsAddon, Tags: r.Tags, Nutrition: r.Nutrition,
	}
}

// CookMinutes is the summary's effective cook time (see CookMinutes).
func (s RecipeSummary) CookMinutes() int { return CookMinutes(s.PrepMinutes, s.TotalMinutes) }

// Ingredient is a canonical catalog ingredient. The catalog is global, not
// household-scoped. Key is ingredients.NormalizeName(Name) and is unique.
type Ingredient struct {
	ID   string
	Key  string
	Name string
	// Category is assigned by ingredients.Categorize. When CategoryConfident is
	// false no rule matched: the category is "other" and a person should
	// review it.
	Category          string
	CategoryConfident bool
	SourceRefs        []SourceRef
	ImageURL          string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// SourceRef is a source's own identifier for an ingredient.
type SourceRef struct {
	Source             string
	SourceIngredientID string
}

// ReviewItem is something an importer could not map confidently, kept so a
// person can resolve it later.
type ReviewItem struct {
	Source         string
	SourceRecipeID string
	RecipeName     string
	Field          string
	Value          string
	Reason         string
}

// Review item statuses. An item is Open until a person resolves it; nothing
// closes one automatically, because only a person can say what the box held.
const (
	ReviewStatusOpen = "open"
)

// ReviewRecord is a stored ReviewItem with the bookkeeping the store adds.
// RecipeID is the household recipe the item is about, empty when the import
// that recorded it no longer matches a stored recipe.
type ReviewRecord struct {
	ReviewItem
	RecipeID  string
	Status    string
	CreatedAt time.Time
}
