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
	// Upsert writes each entry by its entry key (its Origin when it has one,
	// else its CatalogKey; decision 697). An entry with an origin and no
	// entry yet adopts the one published under its LegacyCatalogKey before
	// decision 697, if no other origin has. It writes by inserting it with
	// FirstPublishedAt and UpdatedAt set to now, or refreshing the content
	// and UpdatedAt of an entry that exists. It returns how many documents it
	// wrote (inserted plus modified). It is idempotent: re-publishing
	// unchanged content writes nothing.
	Upsert(ctx context.Context, entries []Recipe, now time.Time) (int, error)
	// Get returns one entry by ID, with GroupKeys.
	Get(ctx context.Context, id string) (Recipe, error)
	// Search returns one representative per group of entries (decision 689)
	// for the groups with an entry matching f and none in f.HiddenGroups,
	// ordered by relevance when f.Text is set and by name otherwise, and the
	// total number of such groups. Paging counts groups, not entries.
	// Results carry full content and GroupKeys.
	Search(ctx context.Context, f Filter) (entries []Recipe, total int, err error)
	// Scan returns up to limit group representatives for ranking, ordered by
	// ID, with GroupKeys. Entries carry no steps and no description:
	// discovery ranks and lists, it does not render a recipe.
	Scan(ctx context.Context, limit int) ([]Recipe, error)
	// Groups maps each of ids to its entry's group (groupKey); an unknown ID
	// maps to its own group. Reports are kept per entry and apply to the
	// whole group.
	Groups(ctx context.Context, ids []string) (map[string]string, error)
}
