package planning

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// IDs are hex ObjectIDs so the same fixtures work with MongoStore.
const (
	hhAda = "66e5a1f2c3b4a5d6e7f80a01"
	hhBob = "66e5a1f2c3b4a5d6e7f80b01"

	userAda    = "66e5a1f2c3b4a5d6e7f83001" // plans in hhAda
	userViewer = "66e5a1f2c3b4a5d6e7f83002" // may view hhAda but not plan
	userBob    = "66e5a1f2c3b4a5d6e7f83003" // plans in hhBob
	userCat    = "66e5a1f2c3b4a5d6e7f83004" // no household

	recipeTacos = "66e5a1f2c3b4a5d6e7f81001"
	recipeSoup  = "66e5a1f2c3b4a5d6e7f81002"
	recipeSalad = "66e5a1f2c3b4a5d6e7f81003"
	recipeBobs  = "66e5a1f2c3b4a5d6e7f81004"

	ingOnion      = "66e5a1f2c3b4a5d6e7f82001"
	ingGarlic     = "66e5a1f2c3b4a5d6e7f82002"
	ingSourCream  = "66e5a1f2c3b4a5d6e7f82003"
	ingSalt       = "66e5a1f2c3b4a5d6e7f82004"
	ingOliveOil   = "66e5a1f2c3b4a5d6e7f82005"
	ingChicken    = "66e5a1f2c3b4a5d6e7f82006"
	testWeek      = "2026-W38"
	testWeekStart = "2026-09-14"
)

var testNow = time.Date(2026, 9, 15, 18, 30, 0, 0, time.UTC)

// --- Synthetic recipes ----------------------------------------------------------

func amt(servings int, quantity, unit string) recipes.Amount {
	return recipes.Amount{Servings: servings, Quantity: quantity, Unit: unit}
}

func ingredientLine(id, name, category string, staple bool, amounts ...recipes.Amount) recipes.RecipeIngredient {
	return recipes.RecipeIngredient{IngredientID: id, Name: name, Category: category, PantryStaple: staple, Amounts: amounts}
}

func tacosRecipe() recipes.Recipe {
	return recipes.Recipe{
		ID: recipeTacos, HouseholdID: hhAda, Name: "Beef Tacos", ImageURL: "https://img.example.com/tacos.jpg", Servings: []int{2, 4},
		Ingredients: []recipes.RecipeIngredient{
			ingredientLine(ingOnion, "Yellow Onion", "produce", false, amt(2, "1/2", "count"), amt(4, "1", "count")),
			ingredientLine(ingGarlic, "Garlic", "produce", false, amt(2, "2", "clove"), amt(4, "4", "clove")),
			ingredientLine(ingSourCream, "Sour Cream", "dairy-eggs", false, amt(2, "2", "tbsp"), amt(4, "4", "tbsp")),
			ingredientLine(ingSalt, "Salt", "spices", true, amt(2, "", ""), amt(4, "", "")),
		},
	}
}

// soupRecipe measures onion by weight, which can't combine with a count.
func soupRecipe() recipes.Recipe {
	return recipes.Recipe{
		ID: recipeSoup, HouseholdID: hhAda, Name: "Onion Soup", Servings: []int{2, 4},
		Ingredients: []recipes.RecipeIngredient{
			ingredientLine(ingOnion, "Yellow Onion", "produce", false, amt(2, "8", "oz"), amt(4, "16", "oz")),
			ingredientLine(ingGarlic, "Garlic", "produce", false, amt(2, "3", "clove"), amt(4, "6", "clove")),
			ingredientLine(ingSourCream, "Sour Cream", "dairy-eggs", false, amt(2, "1/4", "cup"), amt(4, "1/2", "cup")),
			ingredientLine(ingSalt, "Salt", "spices", true, amt(2, "", ""), amt(4, "", "")),
			ingredientLine(ingOliveOil, "Olive Oil", "pantry", true, amt(2, "1", "tbsp"), amt(4, "2", "tbsp")),
		},
	}
}

// saladRecipe only comes in one size, and doesn't mark salt as a staple.
func saladRecipe() recipes.Recipe {
	return recipes.Recipe{
		ID: recipeSalad, HouseholdID: hhAda, Name: "Chicken Salad", Servings: []int{2},
		Ingredients: []recipes.RecipeIngredient{
			ingredientLine(ingOnion, "Yellow Onion", "produce", false, amt(2, "1/2", "count")),
			ingredientLine(ingChicken, "Chicken Breast", "meat-seafood", false, amt(2, "10", "oz")),
			ingredientLine(ingSalt, "Salt", "spices", false, amt(2, "1/4", "tsp")),
		},
	}
}

func bobsRecipe() recipes.Recipe {
	return recipes.Recipe{
		ID: recipeBobs, HouseholdID: hhBob, Name: "Bob's Chili", Servings: []int{2},
		Ingredients: []recipes.RecipeIngredient{
			ingredientLine(ingOnion, "Yellow Onion", "produce", false, amt(2, "1", "count")),
		},
	}
}

// --- fakeRecipes ------------------------------------------------------------------

// fakeRecipes is a RecipeReader over fixed recipes.
type fakeRecipes struct {
	mu           sync.Mutex
	recipes      map[string]recipes.Recipe
	getManyCalls [][]string
}

func newFakeRecipes(list ...recipes.Recipe) *fakeRecipes {
	f := &fakeRecipes{recipes: map[string]recipes.Recipe{}}
	for _, r := range list {
		f.put(r)
	}
	return f
}

func (f *fakeRecipes) put(r recipes.Recipe) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recipes[r.ID] = r
}

func (f *fakeRecipes) remove(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.recipes, id)
}

func (f *fakeRecipes) Get(_ context.Context, householdID, id string) (recipes.Recipe, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.recipes[id]
	if !ok || r.HouseholdID != householdID {
		return recipes.Recipe{}, recipes.ErrNotFound
	}
	return r, nil
}

func (f *fakeRecipes) GetMany(_ context.Context, householdID string, ids []string) ([]recipes.Recipe, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getManyCalls = append(f.getManyCalls, slices.Clone(ids))
	var out []recipes.Recipe
	for _, id := range ids {
		if r, ok := f.recipes[id]; ok && r.HouseholdID == householdID {
			out = append(out, r)
		}
	}
	slices.SortFunc(out, func(a, b recipes.Recipe) int { return cmp.Compare(a.ID, b.ID) })
	return out, nil
}

// --- memoryStore ------------------------------------------------------------------

// memoryStore is an in-memory Store for unit tests. A mutex makes each method
// atomic, like MongoStore's single-document updates, and it reports missing
// entries before finalized plans, as MongoStore does.
type memoryStore struct {
	mu    sync.Mutex
	next  int
	plans map[string]Plan
}

var _ Store = (*memoryStore)(nil)

func newMemoryStore() *memoryStore { return &memoryStore{plans: map[string]Plan{}} }

func planKey(householdID string, w Week) string { return householdID + "/" + w.String() }

func clonePlan(p Plan) Plan {
	p.Entries = slices.Clone(p.Entries)
	if len(p.Entries) == 0 {
		p.Entries = nil
	}
	return p
}

func (m *memoryStore) GetPlan(_ context.Context, householdID string, w Week) (Plan, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.plans[planKey(householdID, w)]
	if !ok {
		return Plan{}, ErrNotFound
	}
	return clonePlan(p), nil
}

func (m *memoryStore) ListSummaries(_ context.Context, householdID string, from, to Week) ([]Summary, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Summary
	for _, p := range m.plans {
		if p.HouseholdID != householdID || from.WeeksUntil(p.Week) < 0 || p.Week.WeeksUntil(to) < 0 {
			continue
		}
		out = append(out, Summary{Week: p.Week, Status: p.Status, EntryCount: len(p.Entries), UpdatedAt: p.UpdatedAt})
	}
	slices.SortFunc(out, func(a, b Summary) int { return cmp.Compare(a.Week.String(), b.Week.String()) })
	return out, nil
}

func (m *memoryStore) AddEntry(ctx context.Context, householdID string, w Week, e Entry, maxEntries int, now time.Time) (Plan, string, error) {
	p, ids, err := m.AddEntries(ctx, householdID, w, []Entry{e}, maxEntries, now)
	if err != nil {
		return Plan{}, "", err
	}
	return p, ids[0], nil
}

func (m *memoryStore) AddEntries(_ context.Context, householdID string, w Week, entries []Entry, maxEntries int, now time.Time) (Plan, []string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := planKey(householdID, w)
	p, ok := m.plans[k]
	if !ok {
		p = Plan{HouseholdID: householdID, Week: w, Status: StatusDraft, CreatedAt: now}
	}
	switch {
	case p.Status == StatusFinalized:
		return Plan{}, nil, ErrFinalized
	case len(p.Entries)+len(entries) > maxEntries:
		return Plan{}, nil, ErrPlanFull
	}
	p.Entries = slices.Clone(p.Entries)
	var ids []string
	for _, e := range entries {
		m.next++
		e.ID = fmt.Sprintf("%024x", m.next)
		if e.Origin == "" {
			e.Origin = OriginManual
		}
		p.Entries = append(p.Entries, e)
		ids = append(ids, e.ID)
	}
	p.UpdatedAt = now
	m.plans[k] = p
	return clonePlan(p), ids, nil
}

func (m *memoryStore) ListPlans(_ context.Context, householdID string, from, to Week) ([]Plan, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Plan
	for _, p := range m.plans {
		if p.HouseholdID == householdID && from.WeeksUntil(p.Week) >= 0 && p.Week.WeeksUntil(to) >= 0 {
			out = append(out, clonePlan(p))
		}
	}
	slices.SortFunc(out, func(a, b Plan) int { return cmp.Compare(a.Week.String(), b.Week.String()) })
	return out, nil
}

func (m *memoryStore) UpdateEntry(_ context.Context, householdID string, w Week, entryID string, c EntryChanges, now time.Time) (Plan, error) {
	return m.modifyEntry(householdID, w, entryID, now, func(p *Plan, i int) {
		if c.Day != nil {
			p.Entries[i].Day = *c.Day
		}
		if c.Servings != nil {
			p.Entries[i].Servings = *c.Servings
		}
		if c.Note != nil {
			p.Entries[i].Note = *c.Note
		}
	})
}

func (m *memoryStore) DeleteEntry(_ context.Context, householdID string, w Week, entryID string, now time.Time) (Plan, error) {
	return m.modifyEntry(householdID, w, entryID, now, func(p *Plan, i int) {
		p.Entries = slices.Delete(p.Entries, i, i+1)
	})
}

func (m *memoryStore) modifyEntry(householdID string, w Week, entryID string, now time.Time, apply func(p *Plan, i int)) (Plan, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := planKey(householdID, w)
	p, ok := m.plans[k]
	if !ok {
		return Plan{}, ErrNotFound
	}
	i := slices.IndexFunc(p.Entries, func(e Entry) bool { return e.ID == entryID })
	if i < 0 {
		return Plan{}, ErrNotFound
	}
	if p.Status == StatusFinalized {
		return Plan{}, ErrFinalized
	}
	p.Entries = slices.Clone(p.Entries)
	apply(&p, i)
	p.UpdatedAt = now
	m.plans[k] = p
	return clonePlan(p), nil
}

func (m *memoryStore) SetStatus(_ context.Context, householdID string, w Week, status Status, now time.Time) (Plan, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := planKey(householdID, w)
	p, ok := m.plans[k]
	if !ok {
		p = Plan{HouseholdID: householdID, Week: w, CreatedAt: now}
	}
	p.Status, p.UpdatedAt = status, now
	m.plans[k] = p
	return clonePlan(p), nil
}

// --- Helpers ----------------------------------------------------------------------

func newTestService(t *testing.T, store Store) (*Service, *fakeRecipes) {
	t.Helper()
	reader := newFakeRecipes(tacosRecipe(), soupRecipe(), saladRecipe(), bobsRecipe())
	svc := NewService(store, reader)
	svc.now = func() time.Time { return testNow }
	return svc, reader
}

func mustAdd(t *testing.T, svc *Service, householdID, userID, week string, in NewEntry) (Plan, Entry) {
	t.Helper()
	p, e, err := svc.AddEntry(context.Background(), householdID, userID, week, in)
	if err != nil {
		t.Fatalf("AddEntry(%s, %+v) error = %v", week, in, err)
	}
	return p, e
}

func mustWeek(t *testing.T, s string) Week {
	t.Helper()
	w, err := ParseWeek(s)
	if err != nil {
		t.Fatalf("ParseWeek(%q) error = %v", s, err)
	}
	return w
}

func ptr[T any](v T) *T { return &v }
