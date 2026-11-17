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
		e.Fingerprint, e.GroupKeys = Fingerprint(e.Content), nil
		// By entry key, as MongoStore does; an entry with an origin and no
		// entry yet adopts an origin-less one under its legacy key.
		i := slices.IndexFunc(m.entries, func(o Recipe) bool { return o.entryKey() == e.entryKey() })
		if legacy := recipes.LegacyCatalogKey(e.Content); i < 0 && e.Origin != "" && legacy != "" {
			i = slices.IndexFunc(m.entries, func(o Recipe) bool { return o.Origin == "" && o.CatalogKey == legacy })
		}
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
		if contentHash(Content(prev.Content)) == contentHash(e.Content) && prev.entryKey() == e.entryKey() && prev.CatalogKey == e.CatalogKey {
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
		if e.ID != id {
			continue
		}
		e.GroupKeys = []string{e.CatalogKey}
		for _, o := range m.entries {
			if o.ID != e.ID && e.Fingerprint != "" && o.Fingerprint == e.Fingerprint {
				e.GroupKeys = append(e.GroupKeys, o.CatalogKey)
			}
		}
		return e, nil
	}
	return Recipe{}, ErrNotFound
}

// Search mirrors MongoStore.Search: groups with a matching, unhidden entry,
// ordered by their lowest folded matching name, then group, each shown as its
// best copy from the whole group.
func (m *memoryStore) Search(_ context.Context, f Filter) ([]Recipe, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	hidden := map[string]bool{}
	for _, g := range f.HiddenGroups {
		hidden[g] = true
	}
	nameKey := map[string]string{}
	for _, e := range m.entries {
		if !matches(e, f) || isHidden(e, hidden) {
			continue
		}
		g := groupKey(e)
		if k, ok := nameKey[g]; !ok || fold(e.Content.Name) < k {
			nameKey[g] = fold(e.Content.Name)
		}
	}
	var hits []Recipe
	for _, rep := range Dedupe(m.entries) {
		if _, ok := nameKey[groupKey(rep)]; ok {
			hits = append(hits, rep)
		}
	}
	slices.SortStableFunc(hits, func(a, b Recipe) int {
		if c := cmp.Compare(nameKey[groupKey(a)], nameKey[groupKey(b)]); c != 0 {
			return c
		}
		return cmp.Compare(groupKey(a), groupKey(b))
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
	out := Dedupe(m.entries)
	slices.SortFunc(out, func(a, b Recipe) int { return cmp.Compare(a.ID, b.ID) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *memoryStore) Cuisines(context.Context) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for _, e := range m.entries {
		if !servable(e) {
			continue
		}
		for _, c := range e.Content.Cuisines {
			if !slices.Contains(out, c) {
				out = append(out, c)
			}
		}
	}
	return out, nil
}

func (m *memoryStore) Groups(_ context.Context, ids []string) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]string{}
	for _, id := range ids {
		out[id] = idGroup(id)
		for _, e := range m.entries {
			if e.ID == id {
				out[id] = groupKey(e)
			}
		}
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
