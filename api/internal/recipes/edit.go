package recipes

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

// A household can change a recipe it has: swap the ramen for lo mein, fix an
// amount, rewrite a step (decision 667). The change is the household's own:
// it never reaches the global catalog, and a later import of the same meal
// kit recipe keeps the household's ingredients and steps (mergeStored).

// ErrInvalidEdit means an edit can't be saved. Its message after the prefix
// is safe to show.
var ErrInvalidEdit = errors.New("recipes: invalid edit")

// invalidEdit is a sentence for the member: capitalized, with a period.
func invalidEdit(msg string) error {
	first, size := utf8.DecodeRuneInString(msg)
	return editError(string(unicode.ToUpper(first)) + msg[size:] + ".")
}

// editError is an ErrInvalidEdit with its sentence.
type editError string

func (e editError) Error() string { return ErrInvalidEdit.Error() + ": " + string(e) }
func (e editError) Unwrap() error { return ErrInvalidEdit }

// EditInput is a recipe's ingredients and steps as the member left them.
type EditInput struct {
	Ingredients []EditIngredient
	Steps       []string
}

// EditIngredient is one ingredient line. IngredientID, when it is the
// line's own and the name is unchanged, keeps the catalog ingredient; a new
// or renamed line is matched to the catalog by name.
type EditIngredient struct {
	IngredientID string
	Name         string
	PantryStaple bool
	// Amounts has one entry per serving size the recipe offers.
	Amounts []EditAmount
}

// EditAmount is an ingredient's amount for one serving size. An empty
// Quantity means none ("to taste").
type EditAmount struct {
	Servings int
	Quantity string
	Unit     string
}

const maxEditIngredients = 100

// Edit saves a household's changes to one of its recipes and returns it.
// Callers authorize first: the HTTP route requires households.PermRecipesEdit.
func (s *Service) Edit(ctx context.Context, householdID, recipeID string, in EditInput) (Recipe, error) {
	r, err := s.store.GetRecipe(ctx, householdID, recipeID)
	if err != nil {
		return Recipe{}, err
	}
	lines, err := s.editedIngredients(ctx, r, in.Ingredients)
	if err != nil {
		return Recipe{}, err
	}
	steps, err := editedSteps(r.Steps, in.Steps)
	if err != nil {
		return Recipe{}, err
	}
	now := s.now().UTC()
	r.Ingredients, r.Steps = lines, steps
	r.EditedAt, r.UpdatedAt = now, now
	if err := s.store.SaveRecipes(ctx, householdID, []Recipe{r}); err != nil {
		return Recipe{}, fmt.Errorf("save edited recipe: %w", err)
	}
	return s.Get(ctx, householdID, recipeID)
}

func (s *Service) editedIngredients(ctx context.Context, r Recipe, in []EditIngredient) ([]RecipeIngredient, error) {
	switch {
	case len(in) == 0:
		return nil, invalidEdit("a recipe needs at least one ingredient")
	case len(in) > maxEditIngredients:
		return nil, invalidEdit("a recipe may have at most 100 ingredients")
	}
	own := map[string]RecipeIngredient{}
	for _, line := range r.Ingredients {
		own[line.IngredientID] = line
	}
	out := make([]RecipeIngredient, 0, len(in))
	var resolve []int
	for i, e := range in {
		name := strings.Join(strings.Fields(e.Name), " ")
		if name == "" || utf8.RuneCountInString(name) > maxSwapName {
			return nil, invalidEdit(fmt.Sprintf("ingredient %d needs a name of at most 200 characters", i+1))
		}
		line := RecipeIngredient{Name: name, PantryStaple: e.PantryStaple}
		amounts, err := editedAmounts(name, r.Servings, e.Amounts)
		if err != nil {
			return nil, err
		}
		line.Amounts = amounts
		if prev, ok := own[e.IngredientID]; ok && e.IngredientID != "" &&
			ingredients.NormalizeName(prev.Name) == ingredients.NormalizeName(name) {
			line.IngredientID = prev.IngredientID
			keepSourceText(&line, prev)
		} else {
			resolve = append(resolve, i)
		}
		out = append(out, line)
	}
	if len(resolve) > 0 {
		// The import's own lookup: a name the catalog knows is that
		// ingredient, and a new one is added, exactly as for a typed recipe.
		var cands []importCandidate
		for _, i := range resolve {
			cands = append(cands, importCandidate{in: ImportRecipe{Source: SourceManual, Ingredients: []ImportIngredient{{Name: out[i].Name}}}})
		}
		ids, _, err := s.resolveIngredients(ctx, cands, s.now().UTC())
		if err != nil {
			return nil, err
		}
		for _, i := range resolve {
			out[i].IngredientID = ids[lineKeyOf(SourceManual, ImportIngredient{Name: out[i].Name})]
		}
	}
	return out, nil
}

// keepSourceText keeps what the source wrote for an amount the member
// didn't change.
func keepSourceText(line *RecipeIngredient, prev RecipeIngredient) {
	for i, a := range line.Amounts {
		for _, p := range prev.Amounts {
			if p.Servings == a.Servings && p.Unit == a.Unit && sameQuantity(p.Quantity, a.Quantity) {
				line.Amounts[i].SourceUnit, line.Amounts[i].RawText = p.SourceUnit, p.RawText
			}
		}
	}
}

func sameQuantity(a, b string) bool {
	if a == "" || b == "" {
		return a == b
	}
	qa, errA := ingredients.ParseQuantity(a)
	qb, errB := ingredients.ParseQuantity(b)
	return errA == nil && errB == nil && qa.String() == qb.String()
}

// editedAmounts checks one line's amounts against the recipe's serving
// sizes: one per size at most, each a quantity and unit the import accepts.
func editedAmounts(name string, servings []int, in []EditAmount) ([]Amount, error) {
	var out []Amount
	for _, a := range in {
		if !slices.Contains(servings, a.Servings) {
			return nil, invalidEdit(fmt.Sprintf("%s has an amount for %d servings, which the recipe doesn't make", name, a.Servings))
		}
		if slices.ContainsFunc(out, func(o Amount) bool { return o.Servings == a.Servings }) {
			return nil, invalidEdit(fmt.Sprintf("%s has two amounts for %d servings", name, a.Servings))
		}
		q, unit := strings.TrimSpace(a.Quantity), strings.TrimSpace(a.Unit)
		amount := Amount{Servings: a.Servings, Unit: unit}
		if q != "" {
			parsed, err := ingredients.ParseQuantity(q)
			if err != nil || parsed.IsZero() {
				return nil, invalidEdit(fmt.Sprintf("the amount for %s isn't a number", name))
			}
			amount.Quantity = parsed.String()
		}
		if unit != "" {
			if _, err := ingredients.LookupUnit(unit); err != nil {
				return nil, invalidEdit(fmt.Sprintf("%s has a unit we don't know", name))
			}
		}
		out = append(out, amount)
	}
	slices.SortFunc(out, func(a, b Amount) int { return a.Servings - b.Servings })
	return out, nil
}

// editedSteps numbers the steps from 1 and keeps each one's photo where a
// step stays in its place.
func editedSteps(old []Step, in []string) ([]Step, error) {
	if len(in) > maxSwapSteps {
		return nil, invalidEdit("a recipe may have at most 100 steps")
	}
	var out []Step
	for _, text := range in {
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		if len(text) > maxStepText {
			return nil, invalidEdit("each step must be at most 4000 characters")
		}
		step := Step{Index: len(out) + 1, Text: text}
		if i := len(out); i < len(old) {
			step.ImageURL = old[i].ImageURL
		}
		out = append(out, step)
	}
	if len(out) == 0 {
		return nil, invalidEdit("a recipe needs at least one step")
	}
	return out, nil
}
