// Package recommendations is DinnerOS's Autopilot module. It owns the
// household's taste profile, week contexts, per-recipe cooking-method
// overrides, and Autopilot week proposals, and it adapts DinnerOS data
// (recipes, ratings, plans, events, pantry) into the generic inputs of an
// autopilot.RecommendationProvider.
//
// The provider never sees DinnerOS types: this package translates recipes
// into catalog items (with explainable attributes such as proteins, diets,
// allergens, time bands, and cooking methods), ratings and history into
// generic signals, and provider results back into proposals.
//
// Proposals are separate from plans (docs/autopilot.md#proposals): generating
// writes a proposal, swapping edits it, and accepting adds its meals to the
// draft plan as entries with origin autopilot, never replacing manual entries.
package recommendations
