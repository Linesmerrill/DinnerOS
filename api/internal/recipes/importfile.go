package recipes

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

// ImportVersion is the supported version of the recipe import contract.
const ImportVersion = 1

// ImportFile is the source-neutral import contract (docs/import-format.md).
// The JSON shape mirrors importers/hellofresh/normalize.go.
type ImportFile struct {
	Version     int                `json:"version"`
	Source      string             `json:"source"`
	GeneratedAt time.Time          `json:"generatedAt"`
	Recipes     []ImportRecipe     `json:"recipes"`
	Review      []ImportReviewItem `json:"review"`
}

// ImportRecipe is one recipe in an import file.
type ImportRecipe struct {
	Source         string             `json:"source"`
	SourceRecipeID string             `json:"sourceRecipeId"`
	SourceAliases  []string           `json:"sourceAliases,omitempty"`
	SourceURL      string             `json:"sourceUrl"`
	Name           string             `json:"name"`
	Headline       string             `json:"headline,omitempty"`
	Description    string             `json:"description,omitempty"`
	ImageURL       string             `json:"imageUrl,omitempty"`
	IsAddon        bool               `json:"isAddon"`
	Servings       []int              `json:"servings"`
	PrepMinutes    int                `json:"prepMinutes,omitempty"`
	TotalMinutes   int                `json:"totalMinutes,omitempty"`
	Difficulty     int                `json:"difficulty,omitempty"`
	Cuisines       []string           `json:"cuisines,omitempty"`
	Tags           []string           `json:"tags,omitempty"`
	Utensils       []string           `json:"utensils,omitempty"`
	Allergens      []string           `json:"allergens,omitempty"`
	Nutrition      []ImportNutrient   `json:"nutritionPerServing,omitempty"`
	Ingredients    []ImportIngredient `json:"ingredients"`
	Steps          []ImportStep       `json:"steps"`
	OrderWeeks     []string           `json:"orderWeeks"`
}

// ImportNutrient is a per-serving nutrition value.
type ImportNutrient struct {
	Name   string  `json:"name"`
	Amount float64 `json:"amount"`
	Unit   string  `json:"unit"`
}

// ImportIngredient is an ingredient line with amounts for each serving size.
type ImportIngredient struct {
	SourceIngredientID string         `json:"sourceIngredientId"`
	Name               string         `json:"name"`
	Slug               string         `json:"slug,omitempty"`
	ImageURL           string         `json:"imageUrl,omitempty"`
	PantryStaple       bool           `json:"pantryStaple"`
	Amounts            []ImportAmount `json:"amounts"`
}

// ImportAmount is a quantity for one serving size. A nil Quantity means the
// source gave no amount.
type ImportAmount struct {
	Servings   int      `json:"servings"`
	Quantity   *float64 `json:"quantity"`
	Unit       string   `json:"unit"`
	SourceUnit string   `json:"sourceUnit"`
	RawText    string   `json:"rawText"`
}

// ImportStep is one ordered instruction.
type ImportStep struct {
	Index    int    `json:"index"`
	Text     string `json:"text"`
	ImageURL string `json:"imageUrl,omitempty"`
}

// ImportReviewItem flags something the importer could not map confidently.
type ImportReviewItem struct {
	SourceRecipeID string `json:"sourceRecipeId"`
	RecipeName     string `json:"recipeName"`
	Field          string `json:"field"`
	Value          string `json:"value"`
	Reason         string `json:"reason"`
}

func validateFile(f ImportFile) error {
	if f.Version != ImportVersion {
		return fmt.Errorf("%w: version must be %d, got %d", ErrInvalidImport, ImportVersion, f.Version)
	}
	if !knownSource(f.Source) {
		return fmt.Errorf("%w: source %q is not supported", ErrInvalidImport, f.Source)
	}
	return nil
}

// isoWeekRe matches ISO weeks. Zero-padded weeks sort chronologically as strings.
var isoWeekRe = regexp.MustCompile(`^\d{4}-W(0[1-9]|[1-4]\d|5[0-3])$`)

// cleanImportRecipe trims identifiers so validation and matching agree.
func cleanImportRecipe(r ImportRecipe) ImportRecipe {
	r.Source = strings.TrimSpace(r.Source)
	r.SourceRecipeID = strings.TrimSpace(r.SourceRecipeID)
	r.SourceAliases = uniqueStrings(r.SourceAliases, r.SourceRecipeID)
	return r
}

// validateRecipe returns every problem that prevents importing r.
func validateRecipe(r ImportRecipe) []string {
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }

	if !knownSource(r.Source) {
		add("source %q is not supported", r.Source)
	}
	if r.SourceRecipeID == "" {
		add("sourceRecipeId is required")
	}
	if strings.TrimSpace(r.Name) == "" {
		add("name is required")
	}
	for _, s := range r.Servings {
		if s <= 0 {
			add("servings must be positive, got %d", s)
		}
	}
	if r.PrepMinutes < 0 || r.TotalMinutes < 0 {
		add("prepMinutes and totalMinutes must not be negative")
	}
	for _, w := range r.OrderWeeks {
		if !isoWeekRe.MatchString(w) {
			add("orderWeeks value %q is not an ISO week like 2026-W30", w)
		}
	}
	for i, ing := range r.Ingredients {
		if ingredients.NormalizeName(ing.Name) == "" {
			add("ingredients[%d]: name is required", i)
		}
		for _, a := range ing.Amounts {
			if a.Servings <= 0 {
				add("ingredients[%d] (%s): amount servings must be positive, got %d", i, ing.Name, a.Servings)
			}
			if a.Unit != "" {
				if _, err := ingredients.LookupUnit(a.Unit); err != nil {
					add("ingredients[%d] (%s): unit %q is not a DinnerOS unit code", i, ing.Name, a.Unit)
				}
			}
			if a.Quantity != nil {
				if _, err := ingredients.QuantityFromFloat(*a.Quantity); err != nil {
					add("ingredients[%d] (%s): quantity %v is invalid", i, ing.Name, *a.Quantity)
				}
			}
		}
	}
	return problems
}

// buildRecipe converts a validated import recipe. ingredientID returns the
// catalog ID for each ingredient line.
func buildRecipe(r ImportRecipe, ingredientID func(ImportIngredient) string) Recipe {
	out := Recipe{
		Source:         r.Source,
		SourceRecipeID: r.SourceRecipeID,
		SourceAliases:  sortedSet(r.SourceAliases, r.SourceRecipeID),
		SourceURL:      strings.TrimSpace(r.SourceURL),
		Name:           strings.TrimSpace(r.Name),
		Headline:       strings.TrimSpace(r.Headline),
		Description:    strings.TrimSpace(r.Description),
		ImageURL:       strings.TrimSpace(r.ImageURL),
		IsAddon:        r.IsAddon,
		PrepMinutes:    r.PrepMinutes,
		TotalMinutes:   r.TotalMinutes,
		Difficulty:     r.Difficulty,
		Cuisines:       uniqueStrings(r.Cuisines, ""),
		Tags:           uniqueStrings(r.Tags, ""),
		Utensils:       uniqueStrings(r.Utensils, ""),
		Allergens:      uniqueStrings(r.Allergens, ""),
	}

	seen := map[int]bool{}
	for _, s := range r.Servings {
		if !seen[s] {
			seen[s] = true
			out.Servings = append(out.Servings, s)
		}
	}
	slices.Sort(out.Servings)

	for _, n := range r.Nutrition {
		out.Nutrition = append(out.Nutrition, Nutrient(n))
	}
	for _, ing := range r.Ingredients {
		line := RecipeIngredient{
			IngredientID: ingredientID(ing),
			Name:         strings.TrimSpace(ing.Name),
			PantryStaple: ing.PantryStaple,
		}
		for _, a := range ing.Amounts {
			amount := Amount{Servings: a.Servings, Unit: a.Unit, SourceUnit: a.SourceUnit, RawText: a.RawText}
			if a.Quantity != nil {
				q, _ := ingredients.QuantityFromFloat(*a.Quantity) // validated
				amount.Quantity = q.String()
			}
			line.Amounts = append(line.Amounts, amount)
		}
		out.Ingredients = append(out.Ingredients, line)
	}
	for _, s := range r.Steps {
		out.Steps = append(out.Steps, Step(s))
	}
	out.OrderWeeks = sortedSet(r.OrderWeeks, "")
	out.TimesOrdered, out.LastOrderedWeek = orderStats(out.OrderWeeks)
	return out
}

func orderStats(sortedWeeks []string) (times int, last string) {
	if len(sortedWeeks) == 0 {
		return 0, ""
	}
	return len(sortedWeeks), sortedWeeks[len(sortedWeeks)-1]
}
