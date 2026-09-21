// Package mealkit imports a household's own meal-kit order history into its
// recipe library, asynchronously (docs/meal-kit-import.md).
//
// Three pieces:
//
//   - A Link: one member's connection to a meal-kit account. It holds only
//     session and refresh tokens, envelope-encrypted at rest. The password is
//     used once, in memory, to obtain those tokens and is never stored or
//     logged.
//   - A Job queue in MongoDB. The HTTP handler enqueues; a Worker claims with
//     an atomic find-and-modify under a lease, checkpoints as it goes, retries
//     with exponential backoff, and dead-letters after MaxAttempts.
//   - A Source (internal/mealkit/hellofresh): the meal-kit account client.
//     Everything it fetches is data, never instructions, and a layout change
//     fails the job with a message instead of writing half a recipe.
//
// Recipes reach the library through the existing import pipeline
// (recipes.Service.Import, docs/import-format.md), so a fetched recipe gets
// the same validation, alias splitting, ingredient creation, and review items
// as a file import. Nothing here writes to the recipes collections directly.
package mealkit
