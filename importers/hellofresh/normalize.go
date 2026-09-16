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
	"unicode"
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
	"thumb": "thumb", "thumbs": "thumb",
	"teaspoon": "tsp", "teaspoons": "tsp", "tsp": "tsp", "teaspoon (tsp)": "tsp",
	"tablespoon": "tbsp", "tablespoons": "tbsp", "tbsp": "tbsp", "tablespoon (tbsp)": "tbsp",
	"cup": "cup", "cups": "cup",
	"fluid ounce": "floz", "fluid ounces": "floz", "fl oz": "floz",
	"ounce": "oz", "ounces": "oz", "oz": "oz",
	"pound": "lb", "pounds": "lb", "lb": "lb",
	"gram": "g", "grams": "g", "g": "g",
	"kilogram": "kg", "kilograms": "kg", "kg": "kg",
	"milliliter": "ml", "milliliters": "ml", "millilitre": "ml", "millilitres": "ml", "ml": "ml",
	"liter": "l", "liters": "l", "l": "l",
	"pinch": "pinch",
	// A "pick" is one item picked into the box: HelloFresh uses it for counted
	// produce such as scallions and limes.
	"pick": "count", "picks": "count",
}

// hfRecipe is the subset of the HelloFresh recipe object the normalizer reads.
type hfRecipe struct {
	RecipeID      string         `json:"recipeId"`
	ID            string         `json:"id"`
	Slug          string         `json:"slug"`
	Name          string         `json:"name"`
	Headline      string         `json:"headline"`
	Description   string         `json:"description"`
	ImagePath     string         `json:"imagePath"`
	WebsiteURL    string         `json:"websiteUrl"`
	CanonicalLink string         `json:"canonicalLink"`
	CardLink      string         `json:"cardLink"`
	PrepTime      string         `json:"prepTime"`
	TotalTime     string         `json:"totalTime"`
	Difficulty    int            `json:"difficulty"`
	IsAddon       bool           `json:"isAddon"`
	UpdatedAt     string         `json:"updatedAt"`
	Cuisines      []named        `json:"cuisines"`
	Tags          []named        `json:"tags"`
	Utensils      []named        `json:"utensils"`
	Allergens     []named        `json:"allergens"`
	Nutrition     []hfNutrient   `json:"nutrition"`
	Ingredients   []hfIngredient `json:"ingredients"`
	Yields        []hfYield      `json:"yields"`
	Steps         []hfStep       `json:"steps"`
}

type hfNutrient struct {
	Name   string  `json:"name"`
	Amount float64 `json:"amount"`
	Unit   string  `json:"unit"`
}

type hfIngredient struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	ImagePath string `json:"imagePath"`
	Shipped   bool   `json:"shipped"`
}

type hfYield struct {
	Yields      int                 `json:"yields"`
	Ingredients []hfYieldIngredient `json:"ingredients"`
}

type hfYieldIngredient struct {
	ID     string   `json:"id"`
	Amount *float64 `json:"amount"`
	Unit   string   `json:"unit"`
}

type hfStep struct {
	Index        int    `json:"index"`
	Instructions string `json:"instructions"`
	Images       []struct {
		Path string `json:"path"`
	} `json:"images"`
}

type parsedRaw struct {
	raw    RawRecipe
	recipe hfRecipe
}

// LoadRawRecipes reads every raw recipe file in dir/recipes (public pages),
// dir/delivered (account captures), and dir/cards (parsed recipe cards).
func LoadRawRecipes(dir string) ([]RawRecipe, error) {
	var paths []string
	for _, sub := range []string{"recipes", "delivered", "cards"} {
		matches, err := filepath.Glob(filepath.Join(dir, sub, "*.json"))
		if err != nil {
			return nil, err
		}
		for _, m := range matches {
			if !strings.HasSuffix(m, ".missing.json") {
				paths = append(paths, m)
			}
		}
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
//
// Each delivered recipe is read from the most exact source available: an
// account capture, then its printed card, then the public page (which can
// redirect a weekly clone to a different canonical variant). Clones of one dish
// merge into one recipe; a delivered variant that is a different dish (pork
// instead of beef chili) becomes its own recipe under its delivered ID.
func Normalize(raws []RawRecipe, history History, now time.Time) (ImportFile, error) {
	weeksByDelivered := map[string][]string{}
	namesByDelivered := map[string]string{}
	urlsByDelivered := map[string]string{}
	for _, r := range history.UniqueRecipes() {
		weeksByDelivered[r.DeliveredID] = r.Weeks
		namesByDelivered[r.DeliveredID] = r.Name
		urlsByDelivered[r.DeliveredID] = r.URL
	}

	parsed, captured, err := parseRaws(raws)
	if err != nil {
		return ImportFile{}, err
	}
	cards := map[string]parsedRaw{}
	for _, p := range parsed {
		if p.raw.Origin == OriginCard {
			cards[p.raw.DeliveredID] = p
		}
	}

	// Public pages group by canonical recipe ID. An account capture replaces the
	// public page for its delivered ID. So does a card whose recipe is a
	// different dish from the page; a card of the same dish confirms the page.
	groups := map[string][]parsedRaw{}
	keyByVariant := map[string]string{}
	verified := map[string]bool{} // delivered IDs whose page is confirmed by their card
	var exact []parsedRaw
	for _, p := range parsed {
		switch {
		case p.raw.Origin == OriginAccount:
			exact = append(exact, p)
			continue
		case p.raw.Origin != "" || captured[p.raw.DeliveredID]:
			continue
		}
		delivered := namesByDelivered[p.raw.DeliveredID]
		if card, ok := cards[p.raw.DeliveredID]; ok && delivered != "" && !sameVariant(delivered, p.recipe) {
			if !sameDish(card.recipe, p.recipe) {
				exact = append(exact, withPageDetails(card, p, urlsByDelivered[p.raw.DeliveredID]))
				continue
			}
			verified[p.raw.DeliveredID] = true
		}
		key := p.recipe.RecipeID
		if key == "" {
			key = strings.TrimSuffix(p.recipe.ID, "-en-US")
		}
		if key == "" {
			key = p.raw.DeliveredID
		}
		groups[key] = append(groups[key], p)
		keyByVariant[variantKey(firstNonEmpty(p.recipe.Slug, p.recipe.Name))] = key
	}

	// Exact recipes carry no canonical ID. They join the public recipe of the
	// same name, or group by name under their newest delivered ID.
	exactGroups := map[string][]parsedRaw{}
	for _, p := range exact {
		vk := variantKey(firstNonEmpty(p.recipe.Slug, p.recipe.Name))
		if key, ok := keyByVariant[vk]; ok {
			groups[key] = append(groups[key], p)
			continue
		}
		exactGroups[vk] = append(exactGroups[vk], p)
	}
	for _, group := range exactGroups {
		key := group[0].raw.DeliveredID
		for _, g := range group[1:] {
			key = max(key, g.raw.DeliveredID) // Object IDs sort by creation time.
		}
		groups[key] = append(groups[key], group...)
	}

	// HelloFresh republishes the same dish under new recipe IDs over the years.
	// To the household they are one recipe, so groups with the same name merge
	// under the newest ID (Object IDs sort by creation time).
	keysByName := map[string][]string{}
	for key, group := range groups {
		sortByPreference(group)
		name := variantKey(group[0].recipe.Name)
		if name == "" {
			continue
		}
		keysByName[name] = append(keysByName[name], key)
	}
	for _, same := range keysByName {
		if len(same) < 2 {
			continue
		}
		sort.Strings(same)
		target := same[len(same)-1]
		for _, k := range same[:len(same)-1] {
			groups[target] = append(groups[target], groups[k]...)
			delete(groups, k)
		}
	}

	file := ImportFile{Version: ImportVersion, Source: "hellofresh", GeneratedAt: now.UTC(), Recipes: []ImportRecipe{}, Review: []ReviewItem{}}
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		group := groups[key]
		sortByPreference(group)
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
			if id := g.recipe.RecipeID; id != "" && id != key {
				aliasSet[id] = true
			}
		}

		// A recipe without steps takes them from a card of the same dish.
		if len(primary.Steps) == 0 {
			for _, g := range group {
				if card, ok := cards[g.raw.DeliveredID]; ok && len(card.recipe.Steps) > 0 && (sameVariant(card.recipe.Name, primary) || sameDish(card.recipe, primary)) {
					primary.Steps = card.recipe.Steps
					break
				}
			}
		}

		rec, review := normalizeRecipe(key, primary, group[0].raw)
		rec.OrderWeeks = sortedKeys(weekSet)
		rec.SourceAliases = sortedKeys(aliasSet)

		// Recipe pages redirect weekly menu clones to a canonical recipe, which is
		// occasionally a different variant (e.g. pork delivered, chicken page).
		// Without a capture or card, flag it so a person knows the stored details
		// may not match the box.
		variants := map[string]bool{}
		for _, g := range group {
			delivered := namesByDelivered[g.raw.DeliveredID]
			if g.raw.Origin != "" || verified[g.raw.DeliveredID] || delivered == "" || sameVariant(delivered, primary) || variants[variantKey(delivered)] {
				continue
			}
			variants[variantKey(delivered)] = true
			review = append(review, ReviewItem{
				SourceRecipeID: key,
				RecipeName:     rec.Name,
				Field:          "variant",
				Value:          delivered,
				Reason:         "delivered menu variant differs from the canonical recipe page; stored details reflect the canonical recipe",
			})
		}

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

// sortByPreference orders a group's copies best first: full recipe data (a
// page or capture) before a card, then the most recently updated.
func sortByPreference(group []parsedRaw) {
	rank := func(p parsedRaw) int {
		if p.raw.Origin == OriginCard {
			return 1
		}
		return 0
	}
	sort.SliceStable(group, func(i, j int) bool {
		if ri, rj := rank(group[i]), rank(group[j]); ri != rj {
			return ri < rj
		}
		return group[i].recipe.UpdatedAt > group[j].recipe.UpdatedAt
	})
}

// withPageDetails fills what a card doesn't print from the public page its
// delivered ID resolves to: cuisines, tags, and difficulty, which don't change
// with the protein, and the page's IDs and images for ingredients of the same
// name. The page's photo, description, and nutrition describe the other
// variant and are not copied.
func withPageDetails(card, page parsedRaw, deliveredURL string) parsedRaw {
	out := card
	r := card.recipe
	r.Cuisines, r.Tags, r.Difficulty = page.recipe.Cuisines, page.recipe.Tags, page.recipe.Difficulty
	pageIngredients := map[string]hfIngredient{}
	for _, ing := range page.recipe.Ingredients {
		pageIngredients[variantKey(ing.Name)] = ing
	}
	ids := map[string]string{}
	r.Ingredients = append([]hfIngredient(nil), card.recipe.Ingredients...)
	for i, ing := range r.Ingredients {
		if p, ok := pageIngredients[variantKey(ing.Name)]; ok && p.ID != "" {
			ids[ing.ID] = p.ID
			r.Ingredients[i].ID, r.Ingredients[i].Slug, r.Ingredients[i].ImagePath = p.ID, p.Slug, p.ImagePath
		}
	}
	r.Yields = make([]hfYield, len(card.recipe.Yields))
	for i, y := range card.recipe.Yields {
		r.Yields[i] = hfYield{Yields: y.Yields, Ingredients: append([]hfYieldIngredient(nil), y.Ingredients...)}
		for j, yi := range r.Yields[i].Ingredients {
			if id, ok := ids[yi.ID]; ok {
				r.Yields[i].Ingredients[j].ID = id
			}
		}
	}
	out.recipe = r
	if out.raw.FinalURL == "" {
		out.raw.FinalURL = deliveredURL
	}
	return out
}

// sameDish reports whether two recipes ship the same ingredients, by name: a
// renamed clone rather than a different dinner.
func sameDish(a, b hfRecipe) bool {
	set := func(r hfRecipe) map[string]bool {
		out := map[string]bool{}
		for _, ing := range r.Ingredients {
			if ing.Shipped {
				out[variantKey(strings.TrimRight(ing.Name, "*"))] = true
			}
		}
		return out
	}
	sa, sb := set(a), set(b)
	if len(sa) == 0 || len(sa) != len(sb) {
		return false
	}
	for k := range sa {
		if !sb[k] {
			return false
		}
	}
	return true
}

// parseRaws decodes every raw recipe and reports which delivered IDs have an
// account capture.
func parseRaws(raws []RawRecipe) ([]parsedRaw, map[string]bool, error) {
	parsed := make([]parsedRaw, 0, len(raws))
	captured := map[string]bool{}
	for _, raw := range raws {
		var rec hfRecipe
		if err := json.Unmarshal(raw.Recipe, &rec); err != nil {
			return nil, nil, fmt.Errorf("parse recipe %s: %w", raw.DeliveredID, err)
		}
		parsed = append(parsed, parsedRaw{raw: raw, recipe: rec})
		if raw.Origin == OriginAccount {
			captured[raw.DeliveredID] = true
		}
	}
	return parsed, captured, nil
}

// PendingVariant is a delivered recipe whose public page resolved to a different
// variant and that has no account capture or parsed card yet.
type PendingVariant struct {
	DeliveredID string `json:"deliveredId"`
	Name        string `json:"name"`
	LastWeek    string `json:"lastWeek"`
}

// PendingVariants lists delivered recipes that still need an account capture,
// newest delivery first.
func PendingVariants(raws []RawRecipe, history History) ([]PendingVariant, error) {
	parsed, captured, err := parseRaws(raws)
	if err != nil {
		return nil, err
	}
	carded := map[string]bool{}
	for _, p := range parsed {
		if p.raw.Origin == OriginCard {
			carded[p.raw.DeliveredID] = true
		}
	}
	ordered := map[string]OrderedRecipe{}
	for _, r := range history.UniqueRecipes() {
		ordered[r.DeliveredID] = r
	}

	out := []PendingVariant{}
	for _, p := range parsed {
		o, ok := ordered[p.raw.DeliveredID]
		if p.raw.Origin != "" || captured[p.raw.DeliveredID] || carded[p.raw.DeliveredID] || !ok || o.Name == "" || sameVariant(o.Name, p.recipe) {
			continue
		}
		pv := PendingVariant{DeliveredID: o.DeliveredID, Name: o.Name}
		for _, w := range o.Weeks {
			pv.LastWeek = max(pv.LastWeek, w)
		}
		out = append(out, pv)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].LastWeek != out[j].LastWeek {
			return out[i].LastWeek > out[j].LastWeek
		}
		return out[i].DeliveredID < out[j].DeliveredID
	})
	return out, nil
}

// variantStopwords are ignored when comparing recipe names, so "Beef & Zucchini
// Ragu" and "beef-zucchini-ragu" are the same variant.
var variantStopwords = map[string]bool{"and": true, "with": true}

// variantKey reduces a recipe name or slug to what a respelling can't change:
// case, accents, spacing, punctuation, and "&" versus "and". Any other
// difference in wording — "molten", "pork" for "beef" — is kept, so a real
// variant never compares equal. It mirrors the iOS app's
// ImportVariantMatch.isSpellingOnly, plus the stopwords above.
func variantKey(s string) string {
	s = foldAccents(strings.ToLower(strings.ReplaceAll(s, "&", " and ")))
	var b strings.Builder
	for _, word := range strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if !variantStopwords[word] {
			b.WriteString(word)
		}
	}
	return b.String()
}

var accentFolds = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ä", "a", "ã", "a", "å", "a",
	"é", "e", "è", "e", "ê", "e", "ë", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i",
	"ó", "o", "ò", "o", "ô", "o", "ö", "o", "õ", "o",
	"ú", "u", "ù", "u", "û", "u", "ü", "u",
	"ñ", "n", "ç", "c", "ý", "y", "ÿ", "y",
)

func foldAccents(s string) string { return accentFolds.Replace(s) }

// sameVariant reports whether a delivered name matches a recipe's slug or name
// once spelling is set aside (see variantKey).
func sameVariant(delivered string, r hfRecipe) bool {
	k := variantKey(delivered)
	return k != "" && (k == variantKey(r.Slug) || k == variantKey(r.Name))
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
	// Effective cook time is max(prep, total) at read time (architecture.md
	// #127), so a recipe only has no cook time when neither field is usable.
	// HelloFresh routinely omits totalTime (every add-on, and some mains), and
	// that alone is fine. Flag the both-missing case rather than invent a
	// number: consumers treat an unknown time as unknown, never as quick.
	if out.PrepMinutes <= 0 && out.TotalMinutes <= 0 {
		flag("cookTime", "", "recipe has neither a prep nor a total time; its cook time is unknown")
	}

	for _, n := range r.Nutrition {
		out.Nutrition = append(out.Nutrition, Nutrient(n))
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
		sourceID := ing.ID
		if strings.HasPrefix(sourceID, cardIngredientIDPrefix) {
			sourceID = "" // made up by the card conversion; the card has no IDs
		}
		line := ImportIngredient{
			SourceIngredientID: sourceID,
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

	steps := append([]hfStep(nil), r.Steps...)
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
