package mealkit

import (
	"context"

	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// RecipePublisher puts fetched recipes into a household's library.
//
// This is the single seam between the import pipeline and everything
// downstream of it, and it is deliberately the *existing* import contract:
// whatever a worker fetches goes through the same validation, alias
// splitting, ingredient creation, and review items as a file import
// (docs/import-format.md). Nothing in this package writes to the recipe
// collections itself, so there is never a second path into the database.
//
// *recipes.Service satisfies it as it stands. When the global recipe catalog
// grows its own "publish an imported recipe" interface, the adapter that
// implements this one is the one place that has to change — the worker, the
// queue, and the sources do not know which side of the seam they are on.
type RecipePublisher interface {
	Import(ctx context.Context, householdID string, file recipes.ImportFile) (recipes.ImportResult, error)
}

// ServicePublisher adapts *recipes.Service to RecipePublisher. It exists so
// the adapter has a name to swap out rather than a type assertion buried in
// cmd wiring.
type ServicePublisher struct{ Service *recipes.Service }

var _ RecipePublisher = ServicePublisher{}

// Import implements RecipePublisher.
func (p ServicePublisher) Import(ctx context.Context, householdID string, file recipes.ImportFile) (recipes.ImportResult, error) {
	return p.Service.Import(ctx, householdID, file)
}
