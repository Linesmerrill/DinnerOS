package recipes

import (
	"context"
	"slices"
	"strings"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

// A package bought for the week often feeds more than one meal: 8 oz of
// mushrooms, 4 oz for Monday's pasta and 4 oz for Thursday's stir fry. The
// cooking screen says how much of it this meal uses and what to keep for the
// others, so the rest isn't cooked or thrown away (decision 663).

// SaveForLater is one ingredient this meal shares with other meals the
// household still plans to make.
type SaveForLater struct {
	// Keys are the grocery keys the ingredient was bought under: its own and
	// any other name the household linked to it.
	Keys []string
	// PartOf are the keys of recipe lines this ingredient is only one part
	// of, like the cumin in a house-made spice blend. A note found on such a
	// line keeps its own name.
	PartOf []string
	Name   string
	// Use is this meal's amount ("4 oz"), empty when the recipe gave none.
	Use string
	// Text is the whole note: "Use 4 oz. Save 4 oz for Beef Stir Fry on
	// Thursday."
	Text string
	// Short is the step list's reminder: "Save 4 oz for Beef Stir Fry".
	Short string
}

// SaveForLaterSource finds what a meal shares with the rest of its week.
// *planning.Service implements it.
type SaveForLaterSource interface {
	SaveForLater(ctx context.Context, householdID, recipeID string) ([]SaveForLater, error)
}

// SaveForLaterResponse is one ingredient to keep some of for a later meal.
type SaveForLaterResponse struct {
	// IngredientIndex is the recipe ingredient it is, when the recipe lists it
	// under one of the names it was bought under.
	IngredientIndex *int `json:"ingredientIndex,omitempty"`
	// IngredientKey is that ingredient's key, as the checklist rows carry it.
	IngredientKey string `json:"ingredientKey,omitempty"`
	Name          string `json:"name"`
	Text          string `json:"text"`
	Short         string `json:"short"`
}

// saveForLaterResponses matches each note to the recipe's ingredients.
func saveForLaterResponses(notes []SaveForLater, in Instructions) []SaveForLaterResponse {
	out := make([]SaveForLaterResponse, 0, len(notes))
	for _, n := range notes {
		r := SaveForLaterResponse{Name: n.Name, Text: n.Text, Short: n.Short}
		for _, st := range in.Ingredients {
			if slices.ContainsFunc(n.Keys, func(k string) bool { return sameKey(k, st.IngredientKey) }) {
				index := st.Index
				r.IngredientIndex, r.IngredientKey = &index, st.IngredientKey
				// The recipe's own word for it, unless the note reached this
				// line through one part of it: a blend's cumin stays "Cumin".
				if n.Name == "" || !slices.ContainsFunc(n.PartOf, func(k string) bool { return sameKey(k, st.IngredientKey) }) {
					r.Name = st.Name
				}
				break
			}
		}
		// A house-made blend's spices each come as their own note and all
		// read as the blend's line: the cook sees it once.
		if slices.ContainsFunc(out, func(o SaveForLaterResponse) bool {
			return o.Name == r.Name && o.Text == r.Text && sameIndex(o.IngredientIndex, r.IngredientIndex)
		}) {
			continue
		}
		out = append(out, r)
	}
	return out
}

func sameIndex(a, b *int) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

// sameKey reports whether two ingredient keys are one ingredient. A line
// without a catalog ID is keyed by its name, which the grocery list folds
// further ("Garlic Cloves" and "Garlic Clove") than the recipe does.
func sameKey(a, b string) bool {
	if a == b {
		return true
	}
	an, aok := strings.CutPrefix(a, "name:")
	bn, bok := strings.CutPrefix(b, "name:")
	return aok && bok && ingredients.SameIngredientKey(an) == ingredients.SameIngredientKey(bn)
}
