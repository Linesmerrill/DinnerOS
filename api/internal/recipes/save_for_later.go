package recipes

import (
	"cmp"
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
	// Save is the note's second sentence, what to keep for the other meals:
	// "Save 4 oz for Beef Stir Fry on Thursday."
	Save string
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

// saveForLaterResponses matches each note to the recipe's ingredients. An
// ingredient the household leaves out of this meal (just this once, for this
// dish, or for every dish) isn't used here, so there's nothing to save from
// it: its note is dropped (build 123, Merrill's iPad review).
func saveForLaterResponses(notes []SaveForLater, in Instructions) []SaveForLaterResponse {
	out := make([]SaveForLaterResponse, 0, len(notes))
	for _, n := range notes {
		if leftOutOfMeal(n, in) {
			continue
		}
		r := SaveForLaterResponse{Name: n.Name, Text: n.Text, Short: n.Short}
		for _, st := range in.Ingredients {
			if slices.ContainsFunc(n.Keys, func(k string) bool { return sameKey(k, st.IngredientKey) }) {
				index := st.Index
				r.IngredientIndex, r.IngredientKey = &index, st.IngredientKey
				// The recipe's own word for it, unless the note reached this
				// line through one part of it: a blend's cumin stays "Cumin".
				partOf := slices.ContainsFunc(n.PartOf, func(k string) bool { return sameKey(k, st.IngredientKey) })
				if n.Name == "" || !partOf {
					r.Name = st.Name
				}
				// The steps use less than the whole amount and Have Ready says
				// what's left: the card's "Use" is what the steps use, so the
				// two agree ("Use ½ Lime" beside "½ left over"; build 123,
				// Hark's review).
				if use := usedText(st); use != "" && n.Save != "" && !partOf {
					r.Text = "Use " + use + ". " + n.Save
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

// leftOutOfMeal reports whether note is for an ingredient this meal leaves
// out: one of its keys, or its name, is a left-out ingredient of the recipe.
// The name counts too, so a note bought under a catalog name the recipe's
// line doesn't carry still can't outlive the line being crossed out.
func leftOutOfMeal(n SaveForLater, in Instructions) bool {
	name := "name:" + ingredients.NormalizeName(n.Name)
	for _, st := range in.Ingredients {
		if st.LeftOut == nil {
			continue
		}
		if slices.ContainsFunc(n.Keys, func(k string) bool { return sameKey(k, st.IngredientKey) }) {
			return true
		}
		if n.Name != "" && (sameKey(name, st.IngredientKey) || sameKey(name, "name:"+ingredients.NormalizeName(st.Name))) {
			return true
		}
	}
	return false
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

// usedText is what the steps use of an ingredient they don't use all of, as
// the card says it: "½ Lime", "2 Lime wedges", "¼ tsp", "half the Cilantro".
// Empty when the steps use all of it.
func usedText(st IngredientState) string {
	used := st.Used
	if st.LeftOver == nil || used == nil || st.LeftOut != nil || st.Amount == nil {
		return ""
	}
	name := cmp.Or(st.SwapName, st.Name)
	one := ingredients.NewQuantity(1, 1)
	switch {
	case used.Unit == "wedge":
		noun := " wedge"
		if used.Quantity.Cmp(one) > 0 {
			noun = " wedges"
		}
		return used.Quantity.Format() + " " + singularize(name) + noun
	case used.Unit == "count" && st.Amount.Unit != "count":
		// A light herb isn't weighed: "half the Cilantro".
		if used.Quantity.Equal(ingredients.NewQuantity(1, 2)) {
			return "half the " + name
		}
		return used.Quantity.Format() + " of the " + name
	case used.Unit == "count":
		if used.Quantity.Cmp(one) > 0 {
			return used.Quantity.Format() + " " + countPlural(singularize(name))
		}
		return used.Quantity.Format() + " " + singularize(name)
	}
	return used.Text()
}
