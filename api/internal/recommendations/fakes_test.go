package recommendations

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot/baseline"
	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/ratings"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// Fixtures are made up; IDs are hex so they also work with MongoDB.
const (
	hhA      = "66e5a1f2c3b4a5d6e7f80a01"
	hhB      = "66e5a1f2c3b4a5d6e7f80b01"
	userAda  = "66e5a1f2c3b4a5d6e7f83001"
	userAlan = "66e5a1f2c3b4a5d6e7f83002"
	userView = "66e5a1f2c3b4a5d6e7f83003"
	testWeek = "2026-W38"

	rTacos      = "66e5a1f2c3b4a5d6e7f81001"
	rTenderloin = "66e5a1f2c3b4a5d6e7f81002"
	rThighs     = "66e5a1f2c3b4a5d6e7f81003"
	rCurry      = "66e5a1f2c3b4a5d6e7f81004"
	rSalmon     = "66e5a1f2c3b4a5d6e7f81005"
	rRigatoni   = "66e5a1f2c3b4a5d6e7f81006"
	rStirFry    = "66e5a1f2c3b4a5d6e7f81007"
	rBread      = "66e5a1f2c3b4a5d6e7f81008"
	rBurger     = "66e5a1f2c3b4a5d6e7f81009"
	rSoup       = "66e5a1f2c3b4a5d6e7f8100a"
	rOmelet     = "66e5a1f2c3b4a5d6e7f8100b"
	rBobs       = "66e5a1f2c3b4a5d6e7f8100c"
)

var testNow = time.Date(2026, 9, 15, 18, 0, 0, 0, time.UTC)

func line(names ...string) []recipes.RecipeIngredient {
	var out []recipes.RecipeIngredient
	for i, n := range names {
		out = append(out, recipes.RecipeIngredient{IngredientID: fmt.Sprintf("66e5a1f2c3b4a5d6e7f8%04x", 0x2000+i), Name: n})
	}
	return out
}

func testRecipes() []recipes.Recipe {
	r := func(id, name string, prep, total int, cuisines []string, ingredients ...string) recipes.Recipe {
		return recipes.Recipe{
			ID: id, HouseholdID: hhA, Name: name, ImageURL: "https://img.example.com/" + id + ".jpg",
			Servings: []int{2, 4}, PrepMinutes: prep, TotalMinutes: total, Cuisines: cuisines, Ingredients: line(ingredients...),
		}
	}
	list := []recipes.Recipe{
		r(rTacos, "Beef Tacos", 10, 25, []string{"Mexican"}, "Ground Beef", "Flour Tortillas", "Lime"),
		r(rTenderloin, "Smoky Pork Tenderloin", 15, 90, []string{"American"}, "Pork Tenderloin", "BBQ Rub"),
		r(rThighs, "Low and Slow Chicken", 20, 75, []string{"Southern"}, "Bone-In Chicken Thighs", "Brown Sugar"),
		r(rCurry, "Chickpea Curry", 10, 30, []string{"Indian"}, "Chickpeas", "Coconut Milk", "Curry Paste"),
		r(rSalmon, "Teriyaki Salmon", 5, 20, []string{"Japanese"}, "Salmon Fillet", "Soy Sauce", "Rice"),
		// The source's total is smaller than its prep time.
		r(rRigatoni, "Rigatoni", 20, 5, []string{"Italian"}, "Rigatoni", "Parmesan Cheese", "Tomato"),
		r(rStirFry, "Spicy Tofu Stir Fry", 15, 0, []string{"Chinese"}, "Tofu", "Sriracha", "Broccoli"),
		r(rBread, "Garlic Bread", 15, 0, nil, "Baguette", "Garlic", "Butter"),
		r(rBurger, "Smash Burgers", 10, 30, []string{"American"}, "Ground Beef", "Buns", "Cheddar Cheese"),
		r(rSoup, "Onion Soup", 10, 40, []string{"French"}, "Onion", "Butter", "Beef Stock"),
		r(rOmelet, "Garden Omelet", 5, 15, []string{"French"}, "Eggs", "Spinach"),
	}
	list[2].Tags = []string{"Family Friendly"}
	list[6].Tags = []string{"Spicy", "Quick"}
	list[7].IsAddon = true
	list[0].OrderWeeks = []string{"2026-W20", "2026-W30"}
	bobs := r(rBobs, "Bob's Chili", 10, 60, []string{"Tex-Mex"}, "Ground Beef")
	bobs.HouseholdID = hhB
	return append(list, bobs)
}

// --- memory store -----------------------------------------------------------------

type memoryStore struct {
	mu        sync.Mutex
	profiles  map[string]Profile
	contexts  map[string]WeekContext
	proposals map[string]Proposal
	overrides map[string]RecipeOverride
	saveErr   error
}

var _ Store = (*memoryStore)(nil)

func newMemoryStore() *memoryStore {
	return &memoryStore{
		profiles: map[string]Profile{}, contexts: map[string]WeekContext{},
		proposals: map[string]Proposal{}, overrides: map[string]RecipeOverride{},
	}
}

func saveVersioned[T any](m map[string]T, key string, version int64, value T, stored func(T) int64) error {
	current, ok := m[key]
	switch {
	case version == 0 && ok, version != 0 && (!ok || stored(current) != version):
		return ErrConflict
	}
	m[key] = value
	return nil
}

func (m *memoryStore) GetProfile(_ context.Context, householdID string) (Profile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.profiles[householdID]
	if !ok {
		return Profile{}, ErrNotFound
	}
	p.Sections = maps.Clone(p.Sections)
	return p, nil
}

func (m *memoryStore) SaveProfile(_ context.Context, p Profile) (Profile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.saveErr != nil {
		return Profile{}, m.saveErr
	}
	next := p
	next.Version++
	next.Sections = maps.Clone(p.Sections)
	if err := saveVersioned(m.profiles, p.HouseholdID, p.Version, next, func(v Profile) int64 { return v.Version }); err != nil {
		return Profile{}, err
	}
	return next, nil
}

func (m *memoryStore) GetWeekContext(_ context.Context, householdID, week string) (WeekContext, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.contexts[householdID+"/"+week]
	if !ok {
		return WeekContext{}, ErrNotFound
	}
	return c, nil
}

func (m *memoryStore) SaveWeekContext(_ context.Context, c WeekContext) (WeekContext, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	next := c
	next.Version++
	if err := saveVersioned(m.contexts, c.HouseholdID+"/"+c.Week, c.Version, next, func(v WeekContext) int64 { return v.Version }); err != nil {
		return WeekContext{}, err
	}
	return next, nil
}

func (m *memoryStore) DeleteWeekContext(_ context.Context, householdID, week string) (WeekContext, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := householdID + "/" + week
	c, ok := m.contexts[key]
	if !ok {
		return WeekContext{}, ErrNotFound
	}
	delete(m.contexts, key)
	return c, nil
}

func (m *memoryStore) BusyWeeks(_ context.Context, householdID, from, to string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var weeks []string
	for _, c := range m.contexts {
		if c.HouseholdID == householdID && c.Busy && c.Week >= from && c.Week <= to {
			weeks = append(weeks, c.Week)
		}
	}
	slices.Sort(weeks)
	return weeks, nil
}

func (m *memoryStore) GetProposal(_ context.Context, householdID, week string) (Proposal, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.proposals[householdID+"/"+week]
	if !ok {
		return Proposal{}, ErrNotFound
	}
	p.Slots = slices.Clone(p.Slots)
	return p, nil
}

func (m *memoryStore) SaveProposal(_ context.Context, p Proposal) (Proposal, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	next := p
	next.Version++
	next.Slots = slices.Clone(p.Slots)
	if err := saveVersioned(m.proposals, p.HouseholdID+"/"+p.Week, p.Version, next, func(v Proposal) int64 { return v.Version }); err != nil {
		return Proposal{}, err
	}
	return next, nil
}

func (m *memoryStore) ListOverrides(_ context.Context, householdID string) ([]RecipeOverride, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []RecipeOverride
	for _, o := range m.overrides {
		if o.HouseholdID == householdID {
			o.Methods = maps.Clone(o.Methods)
			out = append(out, o)
		}
	}
	slices.SortFunc(out, func(a, b RecipeOverride) int { return cmp.Compare(a.RecipeID, b.RecipeID) })
	return out, nil
}

func (m *memoryStore) GetOverride(_ context.Context, householdID, recipeID string) (RecipeOverride, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.overrides[householdID+"/"+recipeID]
	if !ok {
		return RecipeOverride{}, ErrNotFound
	}
	o.Methods = maps.Clone(o.Methods)
	return o, nil
}

func (m *memoryStore) SaveOverride(_ context.Context, o RecipeOverride) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := o.HouseholdID + "/" + o.RecipeID
	if len(o.Methods) == 0 && len(o.MealCategories) == 0 {
		delete(m.overrides, key)
		return nil
	}
	o.Methods, o.MealCategories = maps.Clone(o.Methods), maps.Clone(o.MealCategories)
	m.overrides[key] = o
	return nil
}

// --- collaborators ----------------------------------------------------------------

type fakeHouseholds map[string]households.Household

func (f fakeHouseholds) GetHousehold(_ context.Context, id string) (households.Household, error) {
	h, ok := f[id]
	if !ok {
		return households.Household{}, households.ErrNotFound
	}
	return h, nil
}

type fakeRecipes struct{ list []recipes.Recipe }

func (f *fakeRecipes) Catalog(_ context.Context, householdID string) ([]recipes.Recipe, error) {
	var out []recipes.Recipe
	for _, r := range f.list {
		if r.HouseholdID == householdID {
			out = append(out, r)
		}
	}
	slices.SortFunc(out, func(a, b recipes.Recipe) int { return cmp.Compare(a.ID, b.ID) })
	return out, nil
}

func (f *fakeRecipes) Get(_ context.Context, householdID, id string) (recipes.Recipe, error) {
	for _, r := range f.list {
		if r.ID == id && r.HouseholdID == householdID {
			return r, nil
		}
	}
	return recipes.Recipe{}, recipes.ErrNotFound
}

type fakeRatings struct{ list []ratings.Rating }

func (f *fakeRatings) HouseholdRatings(_ context.Context, householdID string) ([]ratings.Rating, error) {
	var out []ratings.Rating
	for _, r := range f.list {
		if r.HouseholdID == householdID {
			out = append(out, r)
		}
	}
	return out, nil
}

// eventStore is an events.Store behind the real events service, so every
// recorded payload is validated.
type eventStore struct {
	mu   sync.Mutex
	list []events.Event
}

func (s *eventStore) Insert(_ context.Context, list []events.Event) (int, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range list {
		e.ID = fmt.Sprintf("%024x", len(s.list)+1)
		s.list = append(s.list, e)
	}
	return len(list), 0, nil
}

func (s *eventStore) List(_ context.Context, q events.Query) ([]events.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []events.Event
	for _, e := range s.list {
		if e.HouseholdID == q.HouseholdID && (len(q.Types) == 0 || slices.Contains(q.Types, e.Type)) && !e.OccurredAt.Before(q.Since) {
			out = append(out, e)
		}
	}
	if q.Newest {
		slices.Reverse(out)
	}
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, nil
}

// add records an event directly, as another part of the app would have.
func (s *eventStore) add(e events.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e.ID = fmt.Sprintf("%024x", len(s.list)+1)
	s.list = append(s.list, e)
}

func (s *eventStore) ofType(t events.Type) []events.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []events.Event
	for _, e := range s.list {
		if e.Type == t {
			out = append(out, e)
		}
	}
	return out
}

// fakePlanner keeps plans in memory and adds entries like planning.Service.
type fakePlanner struct {
	mu    sync.Mutex
	plans map[string]planning.Plan
	next  int
}

func newFakePlanner() *fakePlanner { return &fakePlanner{plans: map[string]planning.Plan{}} }

func (f *fakePlanner) Get(_ context.Context, householdID, week string) (planning.Plan, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w, err := planning.ParseWeek(week)
	if err != nil {
		return planning.Plan{}, err
	}
	p, ok := f.plans[householdID+"/"+week]
	if !ok {
		return planning.Plan{HouseholdID: householdID, Week: w, Status: planning.StatusDraft}, nil
	}
	p.Entries = slices.Clone(p.Entries)
	return p, nil
}

func (f *fakePlanner) ListPlans(_ context.Context, householdID, from, to string) ([]planning.Plan, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []planning.Plan
	for _, p := range f.plans {
		if p.HouseholdID == householdID && p.Week.String() >= from && p.Week.String() <= to {
			out = append(out, p)
		}
	}
	slices.SortFunc(out, func(a, b planning.Plan) int { return cmp.Compare(a.Week.String(), b.Week.String()) })
	return out, nil
}

func (f *fakePlanner) AddEntries(_ context.Context, householdID, userID, week string, in []planning.NewEntry) (planning.Plan, []planning.Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w, err := planning.ParseWeek(week)
	if err != nil {
		return planning.Plan{}, nil, err
	}
	key := householdID + "/" + week
	p, ok := f.plans[key]
	if !ok {
		p = planning.Plan{HouseholdID: householdID, Week: w, Status: planning.StatusDraft}
	}
	if p.Status == planning.StatusFinalized {
		return planning.Plan{}, nil, planning.ErrFinalized
	}
	var added []planning.Entry
	for _, ne := range in {
		f.next++
		origin := ne.Origin
		if origin == "" {
			origin = planning.OriginManual
		}
		e := planning.Entry{
			ID: fmt.Sprintf("%024x", f.next), RecipeID: ne.RecipeID, Day: planning.Day(ne.Day), Servings: ne.Servings,
			AddedBy: userID, Origin: origin, ProposalID: ne.ProposalID,
		}
		p.Entries = append(p.Entries, e)
		added = append(added, e)
	}
	f.plans[key] = p
	return p, added, nil
}

func (f *fakePlanner) ReplaceEntryRecipe(_ context.Context, householdID, week, entryID, recipeID string, origin planning.Origin) (planning.Plan, planning.Entry, planning.Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := householdID + "/" + week
	p, ok := f.plans[key]
	if !ok {
		return planning.Plan{}, planning.Entry{}, planning.Entry{}, planning.ErrNotFound
	}
	if p.Status == planning.StatusFinalized {
		return planning.Plan{}, planning.Entry{}, planning.Entry{}, planning.ErrFinalized
	}
	p.Entries = slices.Clone(p.Entries)
	for i, e := range p.Entries {
		if e.ID != entryID {
			continue
		}
		next := e
		next.RecipeID, next.Origin, next.ProposalID, next.Customizations = recipeID, origin, "", nil
		p.Entries[i] = next
		f.plans[key] = p
		return p, e, next, nil
	}
	return planning.Plan{}, planning.Entry{}, planning.Entry{}, planning.ErrNotFound
}

func (f *fakePlanner) set(p planning.Plan) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.plans[p.HouseholdID+"/"+p.Week.String()] = p
}

// --- environment ------------------------------------------------------------------

type testEnv struct {
	svc     *Service
	store   *memoryStore
	events  *eventStore
	planner *fakePlanner
	ratings *fakeRatings
	recipes *fakeRecipes
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	env := &testEnv{
		store: newMemoryStore(), events: &eventStore{}, planner: newFakePlanner(),
		ratings: &fakeRatings{}, recipes: &fakeRecipes{list: testRecipes()},
	}
	clock := testNow
	env.svc = NewService(ServiceOptions{
		Store:    env.store,
		Provider: baseline.New(baseline.Options{}),
		Households: fakeHouseholds{
			hhA: {ID: hhA, DefaultServings: 2, TimeZone: "America/Denver"},
			hhB: {ID: hhB, DefaultServings: 4, TimeZone: "UTC"},
		},
		Recipes: env.recipes,
		Ratings: env.ratings,
		Events:  events.NewService(events.ServiceOptions{Store: env.events, Now: func() time.Time { return clock }}),
		Plans:   env.planner,
		Now:     func() time.Time { return clock },
	})
	return env
}

func ptr[T any](v T) *T { return &v }
