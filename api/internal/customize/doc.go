// Package customize lets a household change a planned meal's protein: swap it
// for another protein or double the portion ("Customize your meal").
//
// A curated Table decides which ingredient lines are proteins and what each
// swaps with. Choices are computed at read time from the live recipe, the
// serving size, and the household's Autopilot restrictions. Chosen
// customizations are stored on the plan entry (planning.Customization) and
// applied by pure transforms: ApplyGrocery changes an entry's grocery lines
// before aggregation, and ApplyRecipe changes the recipe a cooked meal
// deducts from the pantry. docs/api.md#customize-a-meal describes the API.
package customize
