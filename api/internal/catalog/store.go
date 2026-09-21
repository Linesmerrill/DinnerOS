package catalog

import (
	"context"
	"time"
)

// Store persists the global recipe catalog.
//
// Nothing here is household-scoped: the catalog is the same for every
// household, and no method takes a household ID. Implementations return
// ErrNotFound for a missing entry, and nil rather than empty slices.
type Store interface {
	// Upsert writes each entry by its CatalogKey: inserting it with
	// FirstPublishedAt and UpdatedAt set to now, or refreshing the content
	// and UpdatedAt of an entry that exists. It returns how many documents it
	// wrote (inserted plus modified). It is idempotent: re-publishing
	// unchanged content writes nothing.
	Upsert(ctx context.Context, entries []Recipe, now time.Time) (int, error)
	// Get returns one entry by ID.
	Get(ctx context.Context, id string) (Recipe, error)
	// Search returns entries matching f, ordered by relevance when f.Text is
	// set and by name otherwise, and the total number of matches. Results
	// carry full content.
	Search(ctx context.Context, f Filter) (entries []Recipe, total int, err error)
	// Scan returns up to limit entries for ranking, ordered by ID. Entries
	// carry no steps and no description: discovery ranks and lists, it does
	// not render a recipe.
	Scan(ctx context.Context, limit int) ([]Recipe, error)
}
