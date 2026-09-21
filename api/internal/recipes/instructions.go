package recipes

import (
	"math/big"
	"sort"
	"strings"
	"unicode"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

// This file renders a recipe's steps the way a member reads them while
// cooking: every ingredient the recipe lists is marked where the step names
// it, with the amount for the servings being cooked, and a specialty
// ingredient reads as whatever the household actually buys or makes.
//
// Nothing here is stored. Instructions are built on every read from the
// recipe as imported plus the household's current choices, so changing a
// choice changes the next read of every recipe that uses it, with no
// migration and nothing to keep in sync (decision 512).

// SegmentKind says what a piece of a rendered step is.
type SegmentKind string

// Segment kinds.
const (
	// SegmentText is plain instruction text.
	SegmentText SegmentKind = "text"
	// SegmentIngredient is an ingredient the recipe lists, with its amount.
	SegmentIngredient SegmentKind = "ingredient"
)

// Measure is an exact amount in a DinnerOS unit code.
type Measure struct {
	Quantity ingredients.Quantity
	Unit     string
}

// Text renders the measure for people: "1 ½ cups", "2 cloves", "3".
func (m Measure) Text() string {
	text := m.Quantity.Format()
	if u, err := ingredients.LookupUnit(m.Unit); err == nil {
		if label := u.Label(m.Quantity); label != "" {
			text += " " + label
		}
	}
	return text
}

// Segment is one run of a rendered step. Joining every Text in order gives
// the step's text exactly, so a client that only wants a string needs no
// offsets and no encoding rules.
type Segment struct {
	Kind SegmentKind
	Text string
	// IngredientID, Name, Amount, and the flags are set on SegmentIngredient.
	// IngredientID is empty for an ingredient the catalog doesn't know.
	IngredientID string
	// Name is what the segment stands for: the recipe's ingredient, or the
	// substitute when one replaced it.
	Name string
	// Amount is nil when the recipe gave no amount, when a later mention in
	// the same step already carried it, or when a substitution's amount
	// doesn't convert.
	Amount *Measure
	// Spicy is the catalog's heat signal (ingredients.Spicy).
	Spicy bool
	// Substituted is true when the household's specialty choice changed this
	// mention's name or amount.
	Substituted bool
	// SpecialtyID is set when the mention is a specialty ingredient, whether
	// or not the household has chosen for it.
	SpecialtyID string
	// SpecialtyName is the specialty ingredient this mention stands in for,
	// set with SpecialtyID.
	SpecialtyName string
}

// StepNoteKind says why a step carries a note.
type StepNoteKind string

// Step note kinds.
const (
	// NoteSubstitution: the household's choice changes what goes in, or how
	// much, in a way a swapped word can't say on its own.
	NoteSubstitution StepNoteKind = "substitution"
)

// StepNote is a sentence shown under a step.
type StepNote struct {
	Kind        StepNoteKind
	SpecialtyID string
	Text        string
}

// InstructionStep is one step ready to read.
type InstructionStep struct {
	Index int
	// Text is the rendered step: the recipe's own text with substituted names
	// and amounts in place.
	Text string
	// Original is the recipe's own text, set only when Text differs from it.
	Original string
	ImageURL string
	Segments []Segment
	Notes    []StepNote
}

// SpecialtyRef names a specialty ingredient a recipe uses.
type SpecialtyRef struct {
	ID   string
	Key  string
	Name string
}

// Substitution is one specialty ingredient the instructions read differently
// because of the household's choice.
type Substitution struct {
	SpecialtyRef
	OptionID   string
	OptionName string
	// Type is grocery.ChoiceStoreAlternative or grocery.ChoiceHouseMadeBatch.
	Type grocery.ChoiceType
	// Source is "household" when a member chose the option and "strategy"
	// when the household's standing strategy picked it
	// (docs/specialty-ingredients.md).
	Source string
	// Text says what replaced what: "Tex-Mex Paste → tomato paste, chili
	// powder, cumin, oil".
	Text string
}

// Choice sources.
const (
	ChoiceSourceHousehold = "household"
	ChoiceSourceStrategy  = "strategy"
)

// Instructions is a recipe's steps rendered for one serving size.
type Instructions struct {
	RecipeID   string
	RecipeName string
	// Servings is the serving size the amounts are for; ServingOptions are the
	// sizes the recipe was authored at.
	Servings       int
	ServingOptions []int
	// SpecialtiesApplied is true when the household's specialty choices were
	// consulted. It is false when no source was configured, so a client can
	// tell "no substitutions" from "substitutions weren't looked up".
	SpecialtiesApplied bool
	Steps              []InstructionStep
	// Substitutions are the specialty ingredients that read differently, and
	// Unchosen the ones the household hasn't decided about yet — those read as
	// the card wrote them.
	Substitutions []Substitution
	Unchosen      []SpecialtyRef
}

// GroceryLines returns the recipe's ingredient lines for servings in the form
// the grocery engine and the specialty resolver expect. Lines with no
// authored amount at that size still appear, without a quantity.
func GroceryLines(r Recipe, servings int) []grocery.Line {
	lines := make([]grocery.Line, 0, len(r.Ingredients))
	for _, ing := range r.Ingredients {
		line := grocery.Line{
			IngredientKey: ingredientKey(ing), Name: ing.Name,
			Category: ing.Category, PantryStaple: ing.PantryStaple,
		}
		if m, ok := amountAt(ing, servings); ok {
			q := m.Quantity
			line.Quantity, line.UnitCode = &q, m.Unit
		}
		lines = append(lines, line)
	}
	return lines
}

// ingredientKey is the key a line is registered under: the catalog ID, or a
// stable key from the normalized name when the catalog doesn't know it. It
// matches what planning builds, so grocery.Specialties resolves the same way
// for a recipe read as for a week's list.
func ingredientKey(ing RecipeIngredient) string {
	if ing.IngredientID != "" {
		return ing.IngredientID
	}
	return "name:" + ingredients.NormalizeName(ing.Name)
}

// amountAt returns the line's authored amount for servings. Amounts are never
// scaled from another serving size (the grocery engine's rule), so a size the
// recipe wasn't authored at has no amount.
func amountAt(ing RecipeIngredient, servings int) (Measure, bool) {
	for _, a := range ing.Amounts {
		if a.Servings != servings {
			continue
		}
		q, ok := a.ExactQuantity()
		if !ok || a.Unit == "" && q.IsZero() {
			return Measure{}, false
		}
		return Measure{Quantity: q, Unit: a.Unit}, true
	}
	return Measure{}, false
}

// mention is one ingredient of the recipe, ready to be found in step text.
type mention struct {
	ingredientID string
	// name is the recipe's own name; display is what the step should read
	// (the substitute's name when one replaced it).
	name    string
	display string
	amount  *Measure
	spicy   bool

	substituted   bool
	specialtyID   string
	specialtyName string
	// note is the sentence a step gets the first time it mentions this
	// ingredient, empty when the swap speaks for itself.
	note string
	// forms are the spellings to look for, longest first.
	forms []string
}

// Annotate renders r's steps for servings, with the household's specialty
// choices in specs applied. specs may be nil, in which case the steps read as
// the recipe wrote them; applied says whether they were looked up at all.
func Annotate(r Recipe, servings int, specs grocery.Specialties, applied bool) Instructions {
	out := Instructions{
		RecipeID: r.ID, RecipeName: r.Name, Servings: servings,
		ServingOptions: append([]int(nil), r.Servings...), SpecialtiesApplied: applied,
		Steps: make([]InstructionStep, 0, len(r.Steps)),
	}
	mentions := make([]mention, 0, len(r.Ingredients))
	seenSpecialty := map[string]bool{}
	for _, ing := range r.Ingredients {
		m := mention{ingredientID: ing.IngredientID, name: ing.Name, display: ing.Name, spicy: ingredients.Spicy(ing.Name)}
		if a, ok := amountAt(ing, servings); ok {
			m.amount = &a
		}
		spec := specs[ingredientKey(ing)]
		if spec != nil {
			m.specialtyID, m.specialtyName = spec.ID, spec.Name
			ref := SpecialtyRef{ID: spec.ID, Key: spec.Key, Name: spec.Name}
			switch {
			case spec.Choice == nil:
				if !seenSpecialty[spec.ID] {
					out.Unchosen = append(out.Unchosen, ref)
				}
			case spec.Choice.Type == grocery.ChoiceAsIs:
				// The household buys it under its own name: nothing changes.
			default:
				sub := applySubstitute(&m, spec)
				if sub != nil && !seenSpecialty[spec.ID] {
					sub.SpecialtyRef = ref
					out.Substitutions = append(out.Substitutions, *sub)
				}
			}
			seenSpecialty[spec.ID] = true
		}
		m.forms = nameForms(m.name, spec)
		mentions = append(mentions, m)
	}
	for _, step := range r.Steps {
		out.Steps = append(out.Steps, renderStep(step, mentions))
	}
	sort.SliceStable(out.Substitutions, func(i, j int) bool { return out.Substitutions[i].Name < out.Substitutions[j].Name })
	sort.SliceStable(out.Unchosen, func(i, j int) bool { return out.Unchosen[i].Name < out.Unchosen[j].Name })
	return out
}

// applySubstitute rewrites m to read as the household's chosen option, and
// returns what changed for the recipe-level summary.
func applySubstitute(m *mention, spec *grocery.Specialty) *Substitution {
	choice := spec.Choice
	source := ChoiceSourceHousehold
	if choice.Strategy != "" {
		source = ChoiceSourceStrategy
	}
	sub := &Substitution{OptionID: choice.OptionID, OptionName: choice.OptionName, Type: choice.Type, Source: source}
	switch choice.Type {
	case grocery.ChoiceStoreAlternative:
		ratio := storeRatio(m.amount, spec, choice)
		parts := componentTexts(choice.Components, ratio)
		sub.Text = spec.Name + " → " + joinList(parts)
		m.substituted = true
		if len(choice.Components) == 1 && ratio != nil {
			// One ingredient at a known amount replaces the packet outright,
			// so the step can simply name it.
			c := choice.Components[0]
			m.display = c.Name
			m.spicy = ingredients.Spicy(c.Name)
			m.amount = scaledComponent(c, ratio)
			return sub
		}
		// More than one ingredient, or an amount that doesn't convert: the
		// method changes, so the step keeps the original name and says what
		// to use instead rather than swapping a word silently.
		m.note = "Instead of " + amountAndName(m.amount, spec.Name) + ", use " + joinList(parts) + "."
		m.spicy = m.spicy || anySpicy(choice.Components)
	case grocery.ChoiceHouseMadeBatch:
		sub.Text = spec.Name + " → your house-made batch (" + choice.OptionName + ")"
		m.substituted = true
		converted, changed := batchAmount(m.amount, spec, choice)
		if changed {
			m.note = amountAndName(m.amount, spec.Name) + " is " + converted.Text() + " of your house-made " + spec.Name + "."
			m.amount = converted
		}
	default:
		return nil
	}
	return sub
}

// storeRatio is how many times the option's "per" amount this line needs, or
// nil when the line has no amount or the units don't convert exactly.
func storeRatio(amount *Measure, spec *grocery.Specialty, choice *grocery.Choice) *big.Rat {
	if amount == nil || amount.Quantity.IsZero() || choice.Per.Quantity.IsZero() {
		return nil
	}
	inPer, ok := grocery.ConvertMeasure(amount.Quantity.Rat(), amount.Unit, choice.Per.Unit, spec.UnitSizes)
	if !ok {
		return nil
	}
	return inPer.Quo(inPer, choice.Per.Quantity.Rat())
}

// batchAmount converts the line's amount into the batch's yield unit. changed
// is false when there is nothing to convert or no exact conversion, in which
// case the step keeps the recipe's own amount.
func batchAmount(amount *Measure, spec *grocery.Specialty, choice *grocery.Choice) (*Measure, bool) {
	if amount == nil || amount.Quantity.IsZero() || choice.Yield.Unit == "" || choice.Yield.Unit == amount.Unit {
		return amount, false
	}
	inYield, ok := grocery.ConvertMeasure(amount.Quantity.Rat(), amount.Unit, choice.Yield.Unit, spec.UnitSizes)
	if !ok {
		return amount, false
	}
	return &Measure{Quantity: quantityOf(inYield), Unit: choice.Yield.Unit}, true
}

func scaledComponent(c grocery.Component, ratio *big.Rat) *Measure {
	if c.Quantity == nil || c.Quantity.IsZero() || c.Unit == "" {
		return nil
	}
	q := c.Quantity.MulRat(ratio)
	return &Measure{Quantity: q, Unit: c.Unit}
}

func quantityOf(r *big.Rat) ingredients.Quantity {
	return ingredients.NewQuantity(1, 1).MulRat(r)
}

// componentTexts renders an option's ingredients with their amounts, scaled by
// ratio when it is known.
func componentTexts(components []grocery.Component, ratio *big.Rat) []string {
	out := make([]string, 0, len(components))
	for _, c := range components {
		if ratio != nil {
			if m := scaledComponent(c, ratio); m != nil {
				out = append(out, m.Text()+" "+c.Name)
				continue
			}
		}
		out = append(out, c.Name)
	}
	return out
}

func anySpicy(components []grocery.Component) bool {
	for _, c := range components {
		if ingredients.Spicy(c.Name) {
			return true
		}
	}
	return false
}

// amountAndName is "1 tbsp Tex-Mex Paste", or just the name when there is no
// amount.
func amountAndName(m *Measure, name string) string {
	if m == nil {
		return name
	}
	return m.Text() + " " + name
}

// joinList joins parts as "A", "A and B", or "A, B, and C".
func joinList(parts []string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	case 2:
		return parts[0] + " and " + parts[1]
	}
	return strings.Join(parts[:len(parts)-1], ", ") + ", and " + parts[len(parts)-1]
}

// nameForms returns the spellings of an ingredient to look for in step text,
// longest first: the name as written, without a parenthesis or a trailing
// clause, in singular and plural, plus the specialty ingredient's aliases.
// Nothing is guessed from the step text itself — a name the recipe doesn't
// list is never matched.
func nameForms(name string, spec *grocery.Specialty) []string {
	seeds := []string{name}
	if spec != nil {
		seeds = append(seeds, spec.Name)
		seeds = append(seeds, spec.Aliases...)
	}
	seen := map[string]bool{}
	var forms []string
	add := func(s string) {
		s = strings.Join(strings.Fields(s), " ")
		if len(s) < 3 || seen[strings.ToLower(s)] {
			return
		}
		seen[strings.ToLower(s)] = true
		forms = append(forms, s)
	}
	for _, seed := range seeds {
		for _, base := range []string{seed, trimQualifiers(seed)} {
			add(base)
			add(pluralize(base))
			add(singularize(base))
		}
	}
	sort.SliceStable(forms, func(i, j int) bool { return len(forms[i]) > len(forms[j]) })
	return forms
}

// trimQualifiers drops a parenthesis or a trailing clause: "Chili Flakes
// (optional)" and "Scallions, thinly sliced" both become their head.
func trimQualifiers(name string) string {
	if i := strings.IndexByte(name, '('); i > 0 {
		name = name[:i]
	}
	if i := strings.IndexByte(name, ','); i > 0 {
		name = name[:i]
	}
	return strings.TrimSpace(name)
}

// pluralize and singularize change the last word only, with the rules that
// hold for ingredient names ("Scallion" ⇄ "Scallions", "Chili" ⇄ "Chilies",
// "Squash" ⇄ "Squashes").
func pluralize(name string) string {
	head, last := splitLastWord(name)
	lower := strings.ToLower(last)
	switch {
	case lower == "":
		return name
	case strings.HasSuffix(lower, "s"), strings.HasSuffix(lower, "x"), strings.HasSuffix(lower, "z"),
		strings.HasSuffix(lower, "ch"), strings.HasSuffix(lower, "sh"):
		return head + last + "es"
	case strings.HasSuffix(lower, "y") && len(lower) > 1 && !isVowel(rune(lower[len(lower)-2])):
		return head + last[:len(last)-1] + "ies"
	case strings.HasSuffix(lower, "o") && len(lower) > 1 && !isVowel(rune(lower[len(lower)-2])):
		return head + last + "es"
	}
	return head + last + "s"
}

func singularize(name string) string {
	head, last := splitLastWord(name)
	lower := strings.ToLower(last)
	switch {
	case strings.HasSuffix(lower, "ies") && len(lower) > 4:
		return head + last[:len(last)-3] + "y"
	case strings.HasSuffix(lower, "oes") && len(lower) > 4:
		return head + last[:len(last)-2]
	case strings.HasSuffix(lower, "ses"), strings.HasSuffix(lower, "xes"), strings.HasSuffix(lower, "zes"),
		strings.HasSuffix(lower, "ches"), strings.HasSuffix(lower, "shes"):
		return head + last[:len(last)-2]
	case strings.HasSuffix(lower, "s") && !strings.HasSuffix(lower, "ss") && len(lower) > 3:
		return head + last[:len(last)-1]
	}
	return name
}

func splitLastWord(name string) (head, last string) {
	if i := strings.LastIndexByte(name, ' '); i >= 0 {
		return name[:i+1], name[i+1:]
	}
	return "", name
}

func isVowel(r rune) bool { return strings.ContainsRune("aeiou", unicode.ToLower(r)) }
