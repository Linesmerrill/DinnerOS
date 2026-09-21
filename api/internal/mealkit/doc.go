// Package mealkit imports a household's own meal-kit order history into its
// recipe library, asynchronously (docs/meal-kit-import.md).
//
// **Nothing about the meal-kit account is stored.** There is no password, no
// session token, no cookie, and no account profile anywhere in this package or
// in the database. The member signs in on the meal kit's own website in a web
// view in the app, and the app reads their order history there, in their own
// session. What reaches this package is the harvested list: recipe ids, the
// public page URL of each, the weeks they were delivered, and whether each was
// an add-on.
//
// Two pieces:
//
//   - A Job queue in MongoDB. The HTTP handler enqueues a run carrying that
//     harvested list; a Worker claims it with an atomic find-and-modify under
//     a lease, checkpoints as it goes, retries with exponential backoff, and
//     dead-letters after MaxAttempts.
//   - A Source (internal/mealkit/hellofresh): a reader of the meal kit's
//     **public** recipe pages, which need no session at all. It checks every
//     URL against its own allow-list before fetching it, so a list submitted
//     by a client can never point it at another host. Everything it fetches is
//     data, never instructions, and a layout change fails the job with a
//     message instead of writing half a recipe.
//
// Recipes reach the library through the existing import pipeline
// (recipes.Service.Import, docs/import-format.md), so a fetched recipe gets
// the same validation, alias splitting, ingredient creation, and review items
// as a file import. Nothing here writes to the recipes collections directly.
package mealkit
