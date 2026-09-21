package recipes

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

// Manual entry limits. They are generous for a real recipe and small enough
// that a paste or a fetched page can never become a large document.
const (
	MaxDraftTextBytes   = 64 << 10
	maxDraftName        = 200
	maxDraftLine        = 500
	maxDraftIngredients = 100
	maxDraftSteps       = 100
	maxDraftLabels      = 20
	maxDraftServings    = 12
)

// ErrInvalidDraft means a draft cannot be saved as a recipe. Its message
// after the prefix is safe to show.
var ErrInvalidDraft = errors.New("recipes: invalid recipe")

// Draft is a recipe as a member reviews it before saving: a flat, forgiving
// shape with one amount per ingredient and plain-text steps.
//
// A draft is never stored. Parsing returns one, the member edits it, and
// CreateFromDraft turns the edited draft into a recipe. Nothing a parser
// produced reaches the library without a person having seen it, which is what
// makes it safe to parse text and pages we do not control.
type Draft struct {
	Name        string
	Headline    string
	Description string
	SourceURL   string
	ImageURL    string
	// Servings is the number of servings the amounts are for. 0 means the
	// source didn't say, and the household's default is used.
	Servings     int
	PrepMinutes  int
	TotalMinutes int
	Cuisines     []string
	Tags         []string
	Ingredients  []DraftIngredient
	Steps        []string
	// Warnings explain what a parser could not read. They are advice for the
	// person reviewing, never a reason to refuse.
	Warnings []string
}

// DraftIngredient is one ingredient line.
type DraftIngredient struct {
	Name string
	// Quantity is the amount as typed ("1", "1 1/2", "0.5"). Empty means the
	// line gave none ("salt to taste").
	Quantity string
	// Unit is a DinnerOS unit code (ingredients.UnitCodes). Empty means
	// counted, or a unit the parser could not map, in which case RawText
	// keeps what the line actually said.
	Unit         string
	RawText      string
	PantryStaple bool
}

// CreateFromDraft saves a reviewed draft as one of the household's recipes
// and returns it.
//
// The recipe is private to the household. Its source is SourceManual for a
// typed recipe and SourceUser for one parsed from a page, and neither is a
// public source, so it never reaches the global catalog unless the household
// later shares it explicitly (Service.Share).
//
// Callers authorize first: the HTTP route requires households.PermRecipesEdit.
func (s *Service) CreateFromDraft(ctx context.Context, householdID string, d Draft, fromURL bool) (Recipe, error) {
	if householdID == "" {
		return Recipe{}, errHouseholdRequired
	}
	source := SourceManual
	if fromURL {
		source = SourceUser
	}
	in, err := draftImportRecipe(d, source)
	if err != nil {
		return Recipe{}, err
	}
	res, err := s.Import(ctx, householdID, ImportFile{
		Version: ImportVersion, Source: source, GeneratedAt: s.now().UTC(), Recipes: []ImportRecipe{in},
	})
	if err != nil {
		return Recipe{}, err
	}
	if len(res.Errors) > 0 {
		return Recipe{}, fmt.Errorf("%w: %s", ErrInvalidDraft, strings.Join(res.Errors[0].Problems, "; "))
	}
	stored, err := s.store.FindRecipesBySourceIDs(ctx, householdID, source, []string{in.SourceRecipeID})
	if err != nil || len(stored) == 0 {
		return Recipe{}, fmt.Errorf("recipes: saved recipe %q is missing after the write: %w", in.SourceRecipeID, err)
	}
	return s.Get(ctx, householdID, stored[0].ID)
}

// draftImportRecipe converts a reviewed draft into the import contract, which
// is where every recipe in the system is validated and ingredient lines are
// resolved to the global ingredient catalog. Manual entry gets the same
// normalization, de-duplication, and unit checking as a file import rather
// than a second path into the same collection.
func draftImportRecipe(d Draft, source string) (ImportRecipe, error) {
	name := clip(d.Name, maxDraftName)
	if strings.TrimSpace(name) == "" {
		return ImportRecipe{}, fmt.Errorf("%w: name is required", ErrInvalidDraft)
	}
	servings := d.Servings
	if servings < 0 || servings > maxDraftServings {
		return ImportRecipe{}, fmt.Errorf("%w: servings must be between 1 and %d", ErrInvalidDraft, maxDraftServings)
	}
	if servings == 0 {
		servings = 2
	}
	if d.PrepMinutes < 0 || d.TotalMinutes < 0 {
		return ImportRecipe{}, fmt.Errorf("%w: times must not be negative", ErrInvalidDraft)
	}
	if len(d.Ingredients) > maxDraftIngredients || len(d.Steps) > maxDraftSteps {
		return ImportRecipe{}, fmt.Errorf("%w: a recipe may have at most %d ingredients and %d steps", ErrInvalidDraft, maxDraftIngredients, maxDraftSteps)
	}

	out := ImportRecipe{
		Source: source,
		// A typed recipe has no source identifier, so it gets one: an
		// ObjectID hex, unique per draft. It only has to be stable for this
		// recipe, and the catalog identity of a private recipe is its
		// name-and-ingredients fingerprint anyway (identity.go).
		SourceRecipeID: bson.NewObjectID().Hex(),
		SourceURL:      safeHTTPURL(d.SourceURL),
		Name:           name,
		Headline:       clip(d.Headline, maxDraftLine),
		Description:    clip(d.Description, MaxDraftTextBytes),
		ImageURL:       safeHTTPURL(d.ImageURL),
		Servings:       []int{servings},
		PrepMinutes:    d.PrepMinutes,
		TotalMinutes:   d.TotalMinutes,
		Cuisines:       labels(d.Cuisines),
		Tags:           labels(d.Tags),
	}
	for i, line := range d.Ingredients {
		lineName := clip(line.Name, maxDraftLine)
		if ingredients.NormalizeName(lineName) == "" {
			return ImportRecipe{}, fmt.Errorf("%w: ingredient %d has no name", ErrInvalidDraft, i+1)
		}
		amount := ImportAmount{Servings: servings, RawText: clip(line.RawText, maxDraftLine)}
		if unit := strings.TrimSpace(line.Unit); unit != "" {
			if _, err := ingredients.LookupUnit(unit); err != nil {
				return ImportRecipe{}, fmt.Errorf("%w: ingredient %d has unit %q, which is not a DinnerOS unit", ErrInvalidDraft, i+1, unit)
			}
			amount.Unit = unit
		}
		if q := strings.TrimSpace(line.Quantity); q != "" {
			value, ok := parseQuantity(q)
			if !ok {
				return ImportRecipe{}, fmt.Errorf("%w: ingredient %d has quantity %q, which is not a number", ErrInvalidDraft, i+1, q)
			}
			amount.Quantity = &value
		}
		out.Ingredients = append(out.Ingredients, ImportIngredient{
			Name: lineName, PantryStaple: line.PantryStaple, Amounts: []ImportAmount{amount},
		})
	}
	for i, text := range d.Steps {
		text = clip(text, MaxDraftTextBytes)
		if strings.TrimSpace(text) == "" {
			continue
		}
		out.Steps = append(out.Steps, ImportStep{Index: i + 1, Text: text})
	}
	return out, nil
}

// --- Text parsing -------------------------------------------------------------

var (
	headingRe   = regexp.MustCompile(`(?i)^\s*(ingredients?|steps?|instructions?|directions?|method|preparation)\b\s*:?\s*$`)
	servesRe    = regexp.MustCompile(`(?i)\b(?:serves|servings?|yield)\b\D{0,12}(\d{1,2})|\b(\d{1,2})\s+servings?\b`)
	prepTimeRe  = regexp.MustCompile(`(?i)\bprep(?:aration)?\s*time\D{0,10}(\d{1,3})\s*(?:min|minutes?)\b`)
	totalTimeRe = regexp.MustCompile(`(?i)\b(?:total|cook(?:ing)?|ready in)\s*(?:time)?\D{0,10}(\d{1,3})\s*(?:min|minutes?)\b`)
	bulletRe    = regexp.MustCompile(`^\s*(?:[-*\x{2022}\x{00b7}\x{25e6}]+|\d{1,2}[.)])\s+`)
	// The mixed number ("1 1/2") and the bare fraction ("1/2") come first:
	// Go's alternation is leftmost-first, so a plain integer listed earlier
	// would match the "1" of "1/2" and leave "/2" in the name.
	leadAmountRe  = regexp.MustCompile(`^\s*([0-9]+\s+[0-9]+/[0-9]+|[0-9]+/[0-9]+|[0-9]+(?:\.[0-9]+)?)\s*(.*)$`)
	whitespaceRe  = regexp.MustCompile(`[ \t\x{00a0}]+`)
	controlCharRe = regexp.MustCompile(`[\x00-\x08\x0b\x0c\x0e-\x1f\x7f]`)
)

// vulgarFractions maps the single-character fractions recipe sites use.
var vulgarFractions = map[rune]string{
	'¼': "1/4", '½': "1/2", '¾': "3/4", '⅐': "1/7", '⅑': "1/9", '⅒': "1/10",
	'⅓': "1/3", '⅔': "2/3", '⅕': "1/5", '⅖': "2/5", '⅗': "3/5", '⅘': "4/5",
	'⅙': "1/6", '⅚': "5/6", '⅛': "1/8", '⅜': "3/8", '⅝': "5/8", '⅞': "7/8",
}

// manualUnitWords maps what people write to a DinnerOS unit code. Only unambiguous
// words are here: anything else stays in the line's text, where the person
// reviewing can see it and decide.
var manualUnitWords = map[string]string{
	"tsp": "tsp", "tsps": "tsp", "teaspoon": "tsp", "teaspoons": "tsp",
	"tbsp": "tbsp", "tbsps": "tbsp", "tablespoon": "tbsp", "tablespoons": "tbsp",
	"cup": "cup", "cups": "cup",
	"ml": "ml", "milliliter": "ml", "milliliters": "ml", "millilitre": "ml", "millilitres": "ml",
	"l": "l", "liter": "l", "liters": "l", "litre": "l", "litres": "l",
	"g": "g", "gram": "g", "grams": "g",
	"kg": "kg", "kilogram": "kg", "kilograms": "kg",
	"oz": "oz", "ounce": "oz", "ounces": "oz",
	"lb": "lb", "lbs": "lb", "pound": "lb", "pounds": "lb",
	"clove": "clove", "cloves": "clove",
	"can": "can", "cans": "can",
	"package": "package", "packages": "package", "pkg": "package",
	"slice": "slice", "slices": "slice",
	"bunch": "bunch", "bunches": "bunch",
	"pinch": "pinch", "pinches": "pinch",
}

// ParseText reads a pasted recipe into a Draft.
//
// The text is data, never instruction: nothing in it selects a code path,
// names a household, or reaches a store. The parser only recognizes shapes —
// a heading, a bulleted line, a leading amount — and everything it cannot
// place becomes a warning for the person reviewing.
//
// It is deliberately forgiving. A draft that is half right and clearly marked
// is more useful than a refusal, because the next step is a person fixing it.
func ParseText(text string) Draft {
	text = sanitizeText(text)
	var d Draft
	lines := strings.Split(text, "\n")

	mode := "head"
	var intro []string
	for _, raw := range lines {
		line := strings.TrimSpace(whitespaceRe.ReplaceAllString(raw, " "))
		if line == "" {
			continue
		}
		if m := headingRe.FindStringSubmatch(line); m != nil {
			if strings.HasPrefix(strings.ToLower(m[1]), "ingredient") {
				mode = "ingredients"
			} else {
				mode = "steps"
			}
			continue
		}
		switch mode {
		case "head":
			if d.Name == "" {
				d.Name = clip(line, maxDraftName)
				continue
			}
			intro = append(intro, line)
		case "ingredients":
			if len(d.Ingredients) >= maxDraftIngredients {
				continue
			}
			d.Ingredients = append(d.Ingredients, parseIngredientLine(line))
		case "steps":
			if len(d.Steps) >= maxDraftSteps {
				continue
			}
			d.Steps = append(d.Steps, clip(bulletRe.ReplaceAllString(line, ""), MaxDraftTextBytes))
		}
	}
	d.Description = clip(strings.Join(intro, "\n"), MaxDraftTextBytes)

	if m := servesRe.FindStringSubmatch(text); m != nil {
		d.Servings = atoiClamped(cmpFirst(m[1], m[2]), maxDraftServings)
	}
	if m := prepTimeRe.FindStringSubmatch(text); m != nil {
		d.PrepMinutes = atoiClamped(m[1], 24*60)
	}
	if m := totalTimeRe.FindStringSubmatch(text); m != nil {
		d.TotalMinutes = atoiClamped(m[1], 24*60)
	}

	switch {
	case d.Name == "":
		d.Warnings = append(d.Warnings, "Couldn't find a title — the first line is used as the name.")
	case mode == "head":
		d.Warnings = append(d.Warnings,
			"Couldn't find an \"Ingredients\" or \"Steps\" heading, so nothing was split up. Add the headings and paste again, or fill this in by hand.")
	}
	if len(d.Ingredients) == 0 {
		d.Warnings = append(d.Warnings, "No ingredients were found.")
	}
	if len(d.Steps) == 0 {
		d.Warnings = append(d.Warnings, "No steps were found.")
	}
	for _, line := range d.Ingredients {
		if line.Quantity == "" && line.Unit == "" {
			d.Warnings = append(d.Warnings, fmt.Sprintf("No amount for %q — add one so it can be scaled and shopped for.", line.Name))
		}
	}
	return d
}

// parseIngredientLine reads "1 1/2 cups all-purpose flour" into its parts and
// keeps the original line in RawText, so nothing the parser misreads is lost.
func parseIngredientLine(line string) DraftIngredient {
	raw := clip(line, maxDraftLine)
	rest := bulletRe.ReplaceAllString(raw, "")
	out := DraftIngredient{RawText: raw}

	rest = expandFractions(rest)
	if m := leadAmountRe.FindStringSubmatch(rest); m != nil {
		if _, ok := parseQuantity(m[1]); ok {
			out.Quantity = strings.TrimSpace(m[1])
			rest = m[2]
		}
	}
	fields := strings.Fields(rest)
	if len(fields) > 1 {
		word := strings.ToLower(strings.Trim(fields[0], ".,"))
		if code, ok := manualUnitWords[word]; ok {
			out.Unit = code
			fields = fields[1:]
		}
	}
	// "1 cup flour, sifted" — the preparation after the comma is not part of
	// the ingredient's name, and keeping it would make a second catalog entry.
	name := strings.Join(fields, " ")
	if before, _, ok := strings.Cut(name, ","); ok {
		name = before
	}
	out.Name = clip(strings.TrimSpace(name), maxDraftLine)
	if out.Name == "" {
		out.Name = clip(strings.TrimSpace(rest), maxDraftLine)
	}
	return out
}

// --- Helpers ------------------------------------------------------------------

// sanitizeText makes untrusted text safe to store and show: control
// characters removed, line endings normalized, and the whole thing bounded.
func sanitizeText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = controlCharRe.ReplaceAllString(s, " ")
	s = strings.ToValidUTF8(s, "")
	if len(s) > MaxDraftTextBytes {
		s = s[:MaxDraftTextBytes]
	}
	return s
}

// clip trims and bounds one field of untrusted text.
func clip(s string, maxBytes int) string {
	s = strings.TrimSpace(controlCharRe.ReplaceAllString(strings.ToValidUTF8(s, ""), " "))
	if len(s) > maxBytes {
		s = strings.ToValidUTF8(s[:maxBytes], "")
	}
	return strings.TrimSpace(s)
}

// labels bounds and de-duplicates a cuisine or tag list.
func labels(values []string) []string {
	var out []string
	for _, v := range values {
		v = clip(v, 60)
		if v == "" || slices.ContainsFunc(out, func(o string) bool { return strings.EqualFold(o, v) }) {
			continue
		}
		if out = append(out, v); len(out) == maxDraftLabels {
			break
		}
	}
	return out
}

// expandFractions rewrites "1½" and "½" as "1 1/2" and "1/2".
func expandFractions(s string) string {
	var b strings.Builder
	for i, r := range s {
		frac, ok := vulgarFractions[r]
		if !ok {
			b.WriteRune(r)
			continue
		}
		if i > 0 && unicode.IsDigit(rune(s[i-1])) {
			b.WriteByte(' ')
		}
		b.WriteString(frac)
	}
	return b.String()
}

// parseQuantity reads "2", "1.5", "3/4", or "1 1/2" as a number.
func parseQuantity(s string) (float64, bool) {
	s = strings.TrimSpace(expandFractions(s))
	total := 0.0
	for _, part := range strings.Fields(s) {
		whole, frac, isFrac := strings.Cut(part, "/")
		if isFrac {
			num, err1 := strconv.ParseFloat(whole, 64)
			den, err2 := strconv.ParseFloat(frac, 64)
			if err1 != nil || err2 != nil || den == 0 {
				return 0, false
			}
			total += num / den
			continue
		}
		v, err := strconv.ParseFloat(part, 64)
		if err != nil {
			return 0, false
		}
		total += v
	}
	if total <= 0 {
		return 0, false
	}
	// The import contract stores exact fractions, so a quantity it cannot
	// represent is not a quantity we can keep.
	if _, err := ingredients.QuantityFromFloat(total); err != nil {
		return 0, false
	}
	return total, true
}

func atoiClamped(s string, maxValue int) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n <= 0 {
		return 0
	}
	return min(n, maxValue)
}

func cmpFirst(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
