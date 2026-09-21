package catalog

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// memoryStore is an in-memory Store for unit tests. Its ordering mirrors
// MongoStore: folded name, then ID.
type memoryStore struct {
	mu      sync.Mutex
	next    int
	entries []Recipe

	upsertCalls int
	// scanLimit records the limit the last Scan was given.
	scanLimit int
}

func newMemoryStore() *memoryStore { return &memoryStore{} }

func (m *memoryStore) id() string {
	m.next++
	return fmt.Sprintf("%024x", m.next)
}

func (m *memoryStore) Upsert(_ context.Context, entries []Recipe, now time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.upsertCalls++
	written := 0
	for _, e := range entries {
		if e.CatalogKey == "" {
			continue
		}
		e.Content = Content(e.Content)
		i := slices.IndexFunc(m.entries, func(o Recipe) bool { return o.CatalogKey == e.CatalogKey })
		if i < 0 {
			e.ID = m.id()
			e.Content.ID = e.ID
			e.FirstPublishedAt, e.UpdatedAt = now, now
			m.entries = append(m.entries, e)
			written++
			continue
		}
		prev := m.entries[i]
		// Content clears the entry's own ID, as newEntryDoc does before it
		// hashes, so an unchanged re-publish hashes the same.
		if contentHash(Content(prev.Content)) == contentHash(e.Content) {
			continue
		}
		e.ID, e.Content.ID, e.FirstPublishedAt, e.UpdatedAt = prev.ID, prev.ID, prev.FirstPublishedAt, now
		m.entries[i] = e
		written++
	}
	return written, nil
}

func (m *memoryStore) Get(_ context.Context, id string) (Recipe, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.entries {
		if e.ID == id {
			return e, nil
		}
	}
	return Recipe{}, ErrNotFound
}

func (m *memoryStore) Search(_ context.Context, f Filter) ([]Recipe, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var hits []Recipe
	for _, e := range m.entries {
		if matches(e, f) {
			hits = append(hits, e)
		}
	}
	slices.SortStableFunc(hits, func(a, b Recipe) int {
		if c := cmp.Compare(fold(a.Content.Name), fold(b.Content.Name)); c != 0 {
			return c
		}
		return cmp.Compare(a.ID, b.ID)
	})
	total := len(hits)
	if f.Offset >= total {
		return nil, total, nil
	}
	return hits[f.Offset:min(f.Offset+f.Limit, total)], total, nil
}

func (m *memoryStore) Scan(_ context.Context, limit int) ([]Recipe, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.scanLimit = limit
	out := slices.Clone(m.entries)
	slices.SortFunc(out, func(a, b Recipe) int { return cmp.Compare(a.ID, b.ID) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// fakeLibrary is a household's recipes as the catalog sees them.
type fakeLibrary struct {
	// have maps householdID -> catalogKey -> recipeID.
	have map[string]map[string]string
	// added records every AddFromCatalog call as "householdID/name".
	added []string
	err   error
}

func newFakeLibrary() *fakeLibrary { return &fakeLibrary{have: map[string]map[string]string{}} }

func (f *fakeLibrary) InLibrary(_ context.Context, householdID string, keys []string) (map[string]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := map[string]string{}
	for _, k := range keys {
		if id, ok := f.have[householdID][k]; ok {
			out[k] = id
		}
	}
	return out, nil
}

func (f *fakeLibrary) AddFromCatalog(_ context.Context, householdID string, content recipes.Recipe) (recipes.Recipe, bool, error) {
	if f.err != nil {
		return recipes.Recipe{}, false, f.err
	}
	key := recipes.CatalogKey(content)
	if f.have[householdID] == nil {
		f.have[householdID] = map[string]string{}
	}
	f.added = append(f.added, householdID+"/"+content.Name)
	if id, ok := f.have[householdID][key]; ok {
		content.ID = id
		return content, false, nil
	}
	id := fmt.Sprintf("%024x", len(f.added)+900)
	f.have[householdID][key] = id
	content.ID, content.HouseholdID = id, householdID
	return content, true, nil
}
