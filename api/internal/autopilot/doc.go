// Package autopilot is the recommendation contract DinnerOS depends on: the
// RecommendationProvider interface and its request and response types.
//
// The package is deliberately independent of DinnerOS. It imports only the
// standard library and sees opaque households, catalog items with generic
// attributes, member ratings, and interactions, never recipes, plans, or
// recipe sources. The DinnerOS adapter (internal/recommendations) translates
// its own data into these types.
//
// Three layers have strict precedence (docs/autopilot.md):
//
//   - Hard constraints (allergens, diets, exclusions, never-again, time caps)
//     remove candidates before scoring. Nothing reintroduces them.
//   - Soft preferences (likes, weekday rules, cook-time mix, history) are
//     weighted scoring signals.
//   - Business objectives are a bounded boost applied after constraints.
//
// autopilot/baseline is the local, deterministic, explainable implementation.
// Tuned or proprietary models are expected to live behind the same interface
// elsewhere.
package autopilot
