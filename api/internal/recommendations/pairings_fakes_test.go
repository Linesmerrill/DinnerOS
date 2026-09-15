package recommendations

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// Fixtures for pairings. IDs are hex so they also work with MongoDB.
const (
	rRolls    = "66e5a1f2c3b4a5d6e7f8100d"
	rCrackers = "66e5a1f2c3b4a5d6e7f8100e"
)

// memoryPairings is an in-memory PairingStore with the store's optimistic
// concurrency.
type memoryPairings struct {
	mu    sync.Mutex
	weeks map[string]WeekPairings
	saves int
}

var _ PairingStore = (*memoryPairings)(nil)

func newMemoryPairings() *memoryPairings { return &memoryPairings{weeks: map[string]WeekPairings{}} }

func (m *memoryPairings) GetWeekPairings(_ context.Context, householdID, week string) (WeekPairings, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	wp, ok := m.weeks[householdID+"/"+week]
	if !ok {
		return WeekPairings{}, ErrNotFound
	}
	wp.Decisions, wp.GroceryItems = slices.Clone(wp.Decisions), slices.Clone(wp.GroceryItems)
	return wp, nil
}

func (m *memoryPairings) SaveWeekPairings(_ context.Context, wp WeekPairings) (WeekPairings, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.saves++
	next := wp
	next.Version++
	next.Decisions, next.GroceryItems = slices.Clone(wp.Decisions), slices.Clone(wp.GroceryItems)
	if err := saveVersioned(m.weeks, wp.HouseholdID+"/"+wp.Week, wp.Version, next, func(v WeekPairings) int64 { return v.Version }); err != nil {
		return WeekPairings{}, err
	}
	return next, nil
}

// pairingEnv is a testEnv with pairings switched on.
type pairingEnv struct {
	*testEnv
	pairings *memoryPairings
}

// newPairingEnv adds the pairing store and two more add-ons to the catalog:
// dinner rolls (learned) and crackers (a recipe that uses club crackers as an
// ingredient is added by tests that need one).
func newPairingEnv(t *testing.T) *pairingEnv {
	t.Helper()
	env := newTestEnv(t)
	store := newMemoryPairings()
	env.svc.pairings = store
	rolls := recipes.Recipe{
		ID: rRolls, HouseholdID: hhA, Name: "Buttery Dinner Rolls", IsAddon: true, Servings: []int{2, 4},
		PrepMinutes: 10, Ingredients: line("Dinner Rolls"),
	}
	env.recipes.list = append(env.recipes.list, rolls)
	return &pairingEnv{testEnv: env, pairings: store}
}

// recipe returns a fixture recipe by ID.
func (e *pairingEnv) recipe(t *testing.T, id string) *recipes.Recipe {
	t.Helper()
	for i := range e.recipes.list {
		if e.recipes.list[i].ID == id {
			return &e.recipes.list[i]
		}
	}
	t.Fatalf("no fixture recipe %s", id)
	return nil
}

// learnRolls gives the household a history where dinner rolls come with
// pasta: 4 of 5 pasta weeks, and never in the 5 taco weeks.
func (e *pairingEnv) learnRolls(t *testing.T) {
	t.Helper()
	pasta, tacos, rolls := e.recipe(t, rRigatoni), e.recipe(t, rTacos), e.recipe(t, rRolls)
	pasta.OrderWeeks, tacos.OrderWeeks, rolls.OrderWeeks = nil, nil, nil
	for i := 1; i <= 5; i++ {
		pasta.OrderWeeks = append(pasta.OrderWeeks, week(i))
		if i <= 4 {
			rolls.OrderWeeks = append(rolls.OrderWeeks, week(i))
		}
	}
	for i := 6; i <= 10; i++ {
		tacos.OrderWeeks = append(tacos.OrderWeeks, week(i))
	}
}

// planMeal adds a main meal to the week and returns its entry.
func (e *pairingEnv) planMeal(t *testing.T, recipeID, day string, servings int) planning.Entry {
	t.Helper()
	_, added, err := e.planner.AddEntries(context.Background(), hhA, userAda, testWeek, []planning.NewEntry{
		{RecipeID: recipeID, Day: day, Servings: servings},
	})
	if err != nil {
		t.Fatal(err)
	}
	return added[0]
}

// tacosBreadRule pairs an add-on with tacos, which the fixtures' week
// proposal plans (the pasta dish doesn't make the week).
func tacosBreadRule() PairingRule {
	return PairingRule{When: PairingWhen{MealCategories: []string{"tacos"}}, Add: PairingTarget{RecipeID: rBread}, Frequency: PairingAlways}
}

// tacosChipsRule pairs a grocery item with tacos.
func tacosChipsRule() PairingRule {
	return PairingRule{
		When:      PairingWhen{MealCategories: []string{"tacos"}},
		Add:       PairingTarget{GroceryItem: &GroceryItem{Name: "Tortilla chips", Quantity: "1", Unit: "package"}},
		Frequency: PairingAlways,
	}
}

// mustJSON encodes a value for a request body.
func mustJSON(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// saveRules saves the household's pairing rules.
func (e *pairingEnv) saveRules(t *testing.T, rules ...PairingRule) Profile {
	t.Helper()
	p, err := e.svc.UpdateProfile(context.Background(), hhA, userAda, ProfileUpdate{Pairings: &rules}, false)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
