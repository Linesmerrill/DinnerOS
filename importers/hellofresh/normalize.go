package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ImportVersion is the version of the DinnerOS recipe import contract
// (docs/import-format.md).
const ImportVersion = 1

const imageBaseURL = "https://img.hellofresh.com/f_auto,fl_lossy,q_auto,w_1200/hellofresh_s3"

// ImportFile is the normalized output consumed by the DinnerOS API.
type ImportFile struct {
	Version     int            `json:"version"`
	Source      string         `json:"source"`
	GeneratedAt time.Time      `json:"generatedAt"`
	Recipes     []ImportRecipe `json:"recipes"`
	Review      []ReviewItem   `json:"review"`
}

// ImportRecipe is one source-neutral recipe.
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
	Nutrition      []Nutrient         `json:"nutritionPerServing,omitempty"`
	Ingredients    []ImportIngredient `json:"ingredients"`
	Steps          []ImportStep       `json:"steps"`
	OrderWeeks     []string           `json:"orderWeeks"`
}

// Nutrient is a per-serving nutrition value.
type Nutrient struct {
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

// ImportAmount is a quantity for a serving size. A nil Quantity means the source
// gave no amount (e.g. "salt to taste"); nothing is invented.
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

// ReviewItem flags something the normalizer could not map confidently.
type ReviewItem struct {
	SourceRecipeID string `json:"sourceRecipeId"`
	RecipeName     string `json:"recipeName"`
	Field          string `json:"field"`
	Value          string `json:"value"`
	Reason         string `json:"reason"`
}

// sourceUnits maps HelloFresh unit strings to DinnerOS unit codes
// (docs/grocery-engine.md). Unknown units are kept verbatim and flagged.
var sourceUnits = map[string]string{
	"unit": "count", "units": "count", "piece": "count", "pieces": "count",
	"clove": "clove", "cloves": "clove",
	"can": "can", "cans": "can",
	"package": "package", "packages": "package", "packet": "package", "packets": "package",
	"slice": "slice", "slices": "slice",
	"bunch": "bunch", "bunches": "bunch",
	"teaspoon": "tsp", "teaspoons": "tsp", "tsp": "tsp",
	"tablespoon": "tbsp", "tablespoons": "tbsp", "tbsp": "tbsp",
	"cup": "cup", "cups": "cup",
	"fluid ounce": "floz", "fluid ounces": "floz", "fl oz": "floz",
	"ounce": "oz", "ounces": "oz", "oz": "oz",
	"pound": "lb", "pounds": "lb", "lb": "lb",
	"gram": "g", "grams": "g", "g": "g",
	"kilogram": "kg", "kilograms": "kg", "kg": "kg",
	"milliliter": "ml", "milliliters": "ml", "ml": "ml",
	"liter": "l", "liters": "l", "l": "l",
	"pinch": "pinch",
}

// hfRecipe is the subset of the HelloFresh recipe object the normalizer reads.
type hfRecipe struct {
	RecipeID      string  `json:"recipeId"`
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Headline      string  `json:"headline"`
	Description   string  `json:"description"`
	ImagePath     string  `json:"imagePath"`
	WebsiteURL    string  `json:"websiteUrl"`
	CanonicalLink string  `json:"canonicalLink"`
	PrepTime      string  `json:"prepTime"`
	TotalTime     string  `json:"totalTime"`
	Difficulty    int     `json:"difficulty"`
	IsAddon       bool    `json:"isAddon"`
	UpdatedAt     string  `json:"updatedAt"`
	Cuisines      []named `json:"cuisines"`
	Tags          []named `json:"tags"`
	Utensils      []named `json:"utensils"`
	Allergens     []named `json:"allergens"`
	Nutrition     []struct {
		Name   string  `json:"name"`
		Amount float64 `json:"amount"`
		Unit   string  `json:"unit"`
	} `json:"nutrition"`
	Ingredients []struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Slug      string `json:"slug"`
		ImagePath string `json:"imagePath"`
		Shipped   bool   `json:"shipped"`
	} `json:"ingredients"`
	Yields []struct {
		Yields      int `json:"yields"`
		Ingredients []struct {
			ID     string   `json:"id"`
			Amount *float64 `json:"amount"`
			Unit   string   `json:"unit"`
		} `json:"ingredients"`
	} `json:"yields"`
	Steps []struct {
		Index        int    `json:"index"`
		Instructions string `json:"instructions"`
		Images       []struct {
			Path string `json:"path"`
		} `json:"images"`
	} `json:"steps"`
}

type parsedRaw struct {
	raw    RawRecipe
	recipe hfRecipe
}

// LoadRawRecipes reads every raw recipe file in dir/recipes.
func LoadRawRecipes(dir string) ([]RawRecipe, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "recipes", "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	out := make([]RawRecipe, 0, len(paths))
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		var r RawRecipe
		if err := json.Unmarshal(data, &r); err != nil {
			return nil, fmt.Errorf("parse %s: %w", p, err)
		}
		out = append(out, r)
	}
	return out, nil
}

// Normalize converts raw source recipes into the DinnerOS import contract.
// Delivered menu clones of the same canonical recipe are merged into one recipe.
func Normalize(raws []RawRecipe, history History, now time.Time) (ImportFile, error) {
	weeksByDelivered := map[string][]string{}
	for _, r := range history.UniqueRecipes() {
		weeksByDelivered[r.DeliveredID] = r.Weeks
	}

	groups := map[string][]parsedRaw{}
	for _, raw := range raws {
		var rec hfRecipe
		if err := json.Unmarshal(raw.Recipe, &rec); err != nil {
			return ImportFile{}, fmt.Errorf("parse recipe %s: %w", raw.DeliveredID, err)
		}
		key := rec.RecipeID
		if key == "" {
			key = strings.TrimSuffix(rec.ID, "-en-US")
		}
		if key == "" {
			key = raw.DeliveredID
		}
		groups[key] = append(groups[key], parsedRaw{raw: raw, recipe: rec})
	}

	file := ImportFile{Version: ImportVersion, Source: "hellofresh", GeneratedAt: now.UTC()}
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		group := groups[key]
		// Prefer the most recently updated copy as the primary source.
		sort.SliceStable(group, func(i, j int) bool { return group[i].recipe.UpdatedAt > group[j].recipe.UpdatedAt })
		primary := group[0].recipe

		weekSet := map[string]bool{}
		aliasSet := map[string]bool{}
		for _, g := range group {
			for _, w := range weeksByDelivered[g.raw.DeliveredID] {
				weekSet[w] = true
			}
			if g.raw.DeliveredID != key {
				aliasSet[g.raw.DeliveredID] = true
			}
		}

		rec, review := normalizeRecipe(key, primary, group[0].raw)
		rec.OrderWeeks = sortedKeys(weekSet)
		rec.SourceAliases = sortedKeys(aliasSet)
		file.Recipes = append(file.Recipes, rec)
		file.Review = append(file.Review, review...)
	}

	sort.SliceStable(file.Recipes, func(i, j int) bool {
		if file.Recipes[i].Name != file.Recipes[j].Name {
			return file.Recipes[i].Name < file.Recipes[j].Name
		}
		return file.Recipes[i].SourceRecipeID < file.Recipes[j].SourceRecipeID
	})
	return file, nil
}

func normalizeRecipe(id string, r hfRecipe, raw RawRecipe) (ImportRecipe, []ReviewItem) {
	var review []ReviewItem
	flag := func(field, value, reason string) {
		review = append(review, ReviewItem{SourceRecipeID: id, RecipeName: r.Name, Field: field, Value: value, Reason: reason})
	}

	out := ImportRecipe{
		Source:         "hellofresh",
		SourceRecipeID: id,
		SourceURL:      firstNonEmpty(r.CanonicalLink, r.WebsiteURL, raw.FinalURL),
		Name:           strings.TrimSpace(r.Name),
		Headline:       strings.TrimSpace(r.Headline),
		Description:    strings.TrimSpace(r.Description),
		ImageURL:       imageURL(r.ImagePath),
		IsAddon:        r.IsAddon,
		Difficulty:     r.Difficulty,
		Cuisines:       names(r.Cuisines),
		Tags:           names(r.Tags),
		Utensils:       names(r.Utensils),
		Allergens:      names(r.Allergens),
	}

	if m, ok := ParseISODurationMinutes(r.PrepTime); ok {
		out.PrepMinutes = m
	} else if r.PrepTime != "" {
		flag("prepTime", r.PrepTime, "unparseable duration")
	}
	if m, ok := ParseISODurationMinutes(r.TotalTime); ok {
		out.TotalMinutes = m
	} else if r.TotalTime != "" {
		flag("totalTime", r.TotalTime, "unparseable duration")
	}

	for _, n := range r.Nutrition {
		out.Nutrition = append(out.Nutrition, Nutrient{Name: n.Name, Amount: n.Amount, Unit: n.Unit})
	}

	servingSet := map[int]bool{}
	type amountRef struct {
		servings int
		amount   *float64
		unit     string
	}
	amounts := map[string][]amountRef{}
	for _, y := range r.Yields {
		if y.Yields <= 0 {
			continue
		}
		servingSet[y.Yields] = true
		for _, ya := range y.Ingredients {
			amounts[ya.ID] = append(amounts[ya.ID], amountRef{y.Yields, ya.Amount, ya.Unit})
		}
	}
	for s := range servingSet {
		out.Servings = append(out.Servings, s)
	}
	sort.Ints(out.Servings)
	if len(out.Servings) == 0 {
		flag("yields", "", "recipe has no serving sizes")
	}

	for _, ing := range r.Ingredients {
		line := ImportIngredient{
			SourceIngredientID: ing.ID,
			Name:               strings.TrimSpace(ing.Name),
			Slug:               ing.Slug,
			ImageURL:           imageURL(ing.ImagePath),
			PantryStaple:       !ing.Shipped,
		}
		refs := amounts[ing.ID]
		sort.Slice(refs, func(i, j int) bool { return refs[i].servings < refs[j].servings })
		for _, ref := range refs {
			sourceUnit := strings.TrimSpace(strings.ToLower(ref.unit))
			a := ImportAmount{Servings: ref.servings, SourceUnit: ref.unit}
			if ref.amount != nil && *ref.amount > 0 {
				q := *ref.amount
				a.Quantity = &q
			}
			if code, ok := sourceUnits[sourceUnit]; ok {
				a.Unit = code
			} else if sourceUnit != "" {
				flag("ingredients."+line.Name+".unit", ref.unit, "unknown unit")
			}
			a.RawText = rawText(a.Quantity, ref.unit, line.Name)
			line.Amounts = append(line.Amounts, a)
		}
		if len(refs) == 0 {
			flag("ingredients."+line.Name, "", "ingredient has no amounts in any yield")
		}
		out.Ingredients = append(out.Ingredients, line)
	}

	steps := append([]struct {
		Index        int    `json:"index"`
		Instructions string `json:"instructions"`
		Images       []struct {
			Path string `json:"path"`
		} `json:"images"`
	}(nil), r.Steps...)
	sort.SliceStable(steps, func(i, j int) bool { return steps[i].Index < steps[j].Index })
	for i, s := range steps {
		st := ImportStep{Index: i + 1, Text: cleanInstructions(s.Instructions)}
		if len(s.Images) > 0 {
			st.ImageURL = imageURL(s.Images[0].Path)
		}
		out.Steps = append(out.Steps, st)
	}
	if len(out.Steps) == 0 {
		flag("steps", "", "recipe has no steps")
	}

	return out, review
}

var isoDurationRe = regexp.MustCompile(`^P(?:(\d+)D)?(?:T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?)?$`)

// ParseISODurationMinutes parses durations like "PT1H15M" into whole minutes.
func ParseISODurationMinutes(s string) (int, bool) {
	m := isoDurationRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil || s == "P" || s == "PT" {
		return 0, false
	}
	num := func(v string) int {
		if v == "" {
			return 0
		}
		n, _ := strconv.Atoi(v)
		return n
	}
	seconds := num(m[1])*86400 + num(m[2])*3600 + num(m[3])*60 + num(m[4])
	return int(math.Round(float64(seconds) / 60)), true
}

var fractions = []struct {
	value float64
	glyph string
}{
	{0.125, "⅛"}, {0.25, "¼"}, {1.0 / 3, "⅓"}, {0.5, "½"}, {2.0 / 3, "⅔"}, {0.75, "¾"},
}

// FormatQuantity renders a quantity the way recipe cards do ("1 ½", "¼").
func FormatQuantity(q float64) string {
	whole := math.Floor(q + 1e-9)
	frac := q - whole
	glyph := ""
	for _, f := range fractions {
		if math.Abs(frac-f.value) < 0.01 {
			glyph = f.glyph
			frac = 0
			break
		}
	}
	switch {
	case frac > 0.01:
		return strconv.FormatFloat(q, 'f', -1, 64)
	case whole == 0 && glyph != "":
		return glyph
	case glyph != "":
		return fmt.Sprintf("%d %s", int(whole), glyph)
	default:
		return strconv.Itoa(int(whole))
	}
}

func rawText(q *float64, unit, name string) string {
	parts := []string{}
	if q != nil {
		parts = append(parts, FormatQuantity(*q))
	}
	if u := strings.TrimSpace(unit); u != "" {
		parts = append(parts, u)
	}
	parts = append(parts, name)
	return strings.Join(parts, " ")
}

var bulletRe = regexp.MustCompile(`(?m)^\s*•\s*`)

func cleanInstructions(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	var out []string
	var current strings.Builder
	flush := func() {
		if t := strings.TrimSpace(current.String()); t != "" {
			out = append(out, t)
		}
		current.Reset()
	}
	for _, line := range lines {
		if bulletRe.MatchString(line) {
			flush()
			current.WriteString(bulletRe.ReplaceAllString(line, ""))
			continue
		}
		// Source text hard-wraps sentences; rejoin wrapped lines.
		if current.Len() > 0 {
			current.WriteString(" ")
		}
		current.WriteString(strings.TrimSpace(line))
	}
	flush()
	return strings.Join(out, "\n")
}

func imageURL(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "http") {
		return path
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return imageBaseURL + path
}

// named is any source object identified by a display name (cuisine, tag, ...).
type named struct {
	Name string `json:"name"`
}

// names returns trimmed, de-duplicated names in source order.
func names(items []named) []string {
	var out []string
	seen := map[string]bool{}
	for _, it := range items {
		n := strings.TrimSpace(it.Name)
		if n != "" && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
