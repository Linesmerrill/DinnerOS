package ratings

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
)

// memoryStore is an in-memory Store for unit tests, mirroring MongoStore's
// upsert and ordering semantics.
type memoryStore struct {
	mu        sync.Mutex
	next      int
	ratings   []Rating
	upsertErr error
}

func cloneRating(r Rating) Rating {
	r.Tags = nilIfEmpty(slices.Clone(r.Tags))
	return r
}

func (m *memoryStore) find(householdID, recipeID, userID string) int {
	return slices.IndexFunc(m.ratings, func(r Rating) bool {
		return r.HouseholdID == householdID && r.RecipeID == recipeID && r.UserID == userID
	})
}

func (m *memoryStore) Upsert(_ context.Context, r Rating) (Rating, *Rating, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.upsertErr != nil {
		return Rating{}, nil, m.upsertErr
	}
	r = cloneRating(r)
	i := m.find(r.HouseholdID, r.RecipeID, r.UserID)
	if i < 0 {
		m.next++
		r.ID = fmt.Sprintf("%024x", m.next)
		m.ratings = append(m.ratings, r)
		return cloneRating(r), nil, nil
	}
	previous := cloneRating(m.ratings[i])
	r.ID, r.CreatedAt = previous.ID, previous.CreatedAt
	m.ratings[i] = r
	return cloneRating(r), &previous, nil
}

func (m *memoryStore) Delete(_ context.Context, householdID, recipeID, userID string) (Rating, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	i := m.find(householdID, recipeID, userID)
	if i < 0 {
		return Rating{}, ErrNotFound
	}
	deleted := m.ratings[i]
	m.ratings = slices.Delete(m.ratings, i, i+1)
	return deleted, nil
}

func (m *memoryStore) ListForRecipe(_ context.Context, householdID, recipeID string) ([]Rating, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Rating
	for _, r := range m.ratings {
		if r.HouseholdID == householdID && r.RecipeID == recipeID {
			out = append(out, cloneRating(r))
		}
	}
	slices.SortFunc(out, func(a, b Rating) int {
		if c := b.UpdatedAt.Compare(a.UpdatedAt); c != 0 {
			return c
		}
		return cmp.Compare(b.ID, a.ID)
	})
	return out, nil
}

func (m *memoryStore) ListForHousehold(_ context.Context, householdID string, limit int) ([]Rating, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Rating
	for _, r := range m.ratings {
		if r.HouseholdID == householdID {
			r = cloneRating(r)
			r.Comment = ""
			out = append(out, r)
		}
	}
	slices.SortFunc(out, func(a, b Rating) int {
		if c := cmp.Compare(a.RecipeID, b.RecipeID); c != 0 {
			return c
		}
		return cmp.Compare(a.UserID, b.UserID)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *memoryStore) Summaries(_ context.Context, householdID, userID string, recipeIDs []string) (map[string]Summary, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]Summary{}
	for _, r := range m.ratings {
		if r.HouseholdID != householdID || !slices.Contains(recipeIDs, r.RecipeID) {
			continue
		}
		s := out[r.RecipeID]
		s.Count++
		s.Sum += r.Score
		if r.UserID == userID {
			mine := cloneRating(r)
			s.Mine = &mine
		}
		out[r.RecipeID] = s
	}
	return out, nil
}

func (m *memoryStore) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.ratings)
}

var errStoreDown = errors.New("store down")
