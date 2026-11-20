package companions

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

const (
	testHousehold  = "66e5a1f2c3b4a5d6e7f80a01"
	otherHousehold = "66e5a1f2c3b4a5d6e7f80a02"
	testUser       = "66e5a1f2c3b4a5d6e7f83001"
	rTacos         = "66e5a1f2c3b4a5d6e7f81001"
	rBread         = "66e5a1f2c3b4a5d6e7f81008"
	rSlaw          = "66e5a1f2c3b4a5d6e7f81009"
)

var testNow = time.Date(2026, 11, 16, 18, 0, 0, 0, time.UTC)

// --- fakes ------------------------------------------------------------------------

type memoryStore struct {
	mu   sync.Mutex
	next int
	all  []Companion
}

func (m *memoryStore) List(_ context.Context, householdID string, hidden bool) ([]Companion, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Companion
	for _, c := range m.all {
		if c.HouseholdID == householdID && (hidden || c.HiddenAt == nil) {
			out = append(out, c)
		}
	}
	return out, nil
}

func (m *memoryStore) find(householdID, id string) int {
	return slices.IndexFunc(m.all, func(c Companion) bool { return c.HouseholdID == householdID && c.ID == id && c.HiddenAt == nil })
}

func (m *memoryStore) Get(_ context.Context, householdID, id string) (Companion, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if i := m.find(householdID, id); i >= 0 {
		return m.all[i], nil
	}
	return Companion{}, ErrNotFound
}

func (m *memoryStore) Insert(_ context.Context, c Companion) (Companion, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.next++
	c.ID = strings.Repeat("0", 22) + string(rune('a'+m.next/10)) + string(rune('0'+m.next%10))
	m.all = append(m.all, c)
	return c, nil
}

func (m *memoryStore) EnsureLinked(ctx context.Context, c Companion) (bool, error) {
	m.mu.Lock()
	for _, e := range m.all {
		if e.HouseholdID == c.HouseholdID && e.LinkedRecipe != nil && e.LinkedRecipe.ID == c.LinkedRecipe.ID {
			m.mu.Unlock()
			return false, nil
		}
	}
	m.mu.Unlock()
	_, err := m.Insert(ctx, c)
	return err == nil, err
}

func (m *memoryStore) Update(_ context.Context, c Companion) (Companion, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	i := m.find(c.HouseholdID, c.ID)
	if i < 0 {
		return Companion{}, ErrNotFound
	}
	cur := m.all[i]
	c.LinkedRecipe, c.LastListedWeek, c.LastListedAt, c.CreatedBy, c.CreatedAt = cur.LinkedRecipe, cur.LastListedWeek, cur.LastListedAt, cur.CreatedBy, cur.CreatedAt
	m.all[i] = c
	return c, nil
}

func (m *memoryStore) Hide(_ context.Context, householdID, id string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	i := m.find(householdID, id)
	if i < 0 {
		return ErrNotFound
	}
	m.all[i].HiddenAt = &at
	return nil
}

func (m *memoryStore) Delete(_ context.Context, householdID, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	i := m.find(householdID, id)
	if i < 0 {
		return ErrNotFound
	}
	m.all = slices.Delete(m.all, i, i+1)
	return nil
}

func (m *memoryStore) MarkListed(_ context.Context, householdID, id, week string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	i := m.find(householdID, id)
	if i < 0 {
		return ErrNotFound
	}
	if m.all[i].LastListedWeek <= week {
		m.all[i].LastListedWeek, m.all[i].LastListedAt = week, &at
	}
	return nil
}

type fakeRecipes struct{ list []recipes.Recipe }

func (f *fakeRecipes) Catalog(context.Context, string) ([]recipes.Recipe, error) {
	return slices.Clone(f.list), nil
}

func (f *fakeRecipes) Get(_ context.Context, _, id string) (recipes.Recipe, error) {
	for _, r := range f.list {
		if r.ID == id {
			return r, nil
		}
	}
	return recipes.Recipe{}, recipes.ErrNotFound
}

type fakeHints []Hint

func (f fakeHints) CompanionHints(context.Context, string) ([]Hint, error) { return f, nil }

func testRecipes() *fakeRecipes {
	return &fakeRecipes{list: []recipes.Recipe{
		{ID: rTacos, HouseholdID: testHousehold, Name: "Beef Tacos", Cuisines: []string{"Mexican"}},
		{ID: rBread, HouseholdID: testHousehold, Name: "Garlic Bread", IsAddon: true, Servings: []int{2, 4}},
		{ID: rSlaw, HouseholdID: testHousehold, Name: "Lime Slaw", IsAddon: true, Servings: []int{4}},
	}}
}

func newTestService(store Store, rs *fakeRecipes, hints HintSource) *Service {
	return NewService(Options{
		Store: store, Recipes: rs, Hints: hints, Now: func() time.Time { return testNow },
		CanonicalCuisine: func(s string) string {
			if s == "mexico" {
				return "mexican"
			}
			return s
		},
		CuisineLabel: func(s string) string {
			if s == "mexican" {
				return "Mexican"
			}
			return ""
		},
		MealCategory: func(s string) bool { return s == "pasta" || s == "soup" },
	})
}

// --- model ------------------------------------------------------------------------

func TestNormalize(t *testing.T) {
	canon := func(s string) string {
		return map[string]string{"mexico": "mexican"}[s] + strings.TrimPrefix(s, "mexico")
	}
	for _, tt := range []struct {
		name    string
		in      Input
		want    Input
		wantErr string
	}{
		{name: "just a name", in: Input{Name: "  Paper   towels "},
			want: Input{Name: "Paper towels", Frequency: EveryWeek, Mode: ModeSuggest}},
		{name: "paired defaults to only with its meals", in: Input{Name: "Chips", Cuisines: []string{"Mexico", "mexican"}},
			want: Input{Name: "Chips", Cuisines: []string{"mexican"}, Frequency: FrequencyNone, Mode: ModeSuggest}},
		{name: "amount gets a unit", in: Input{Name: "Chips", StoreItem: "Tortilla chips", Quantity: "2", Mode: "AUTO", Frequency: "every_month"},
			want: Input{Name: "Chips", StoreItem: "Tortilla chips", Quantity: "2", Unit: "count", Frequency: EveryMonth, Mode: ModeAuto}},
		{name: "store item same as the name", in: Input{Name: "Salsa", StoreItem: "salsa"},
			want: Input{Name: "Salsa", Frequency: EveryWeek, Mode: ModeSuggest}},
		{name: "link", in: Input{Name: "Salsa", ProductURL: " https://example.com/salsa "},
			want: Input{Name: "Salsa", ProductURL: "https://example.com/salsa", Frequency: EveryWeek, Mode: ModeSuggest}},
		{name: "no name", in: Input{Name: "  "}, wantErr: "Give it a name."},
		{name: "long name", in: Input{Name: strings.Repeat("a", MaxNameLength+1)}, wantErr: "Keep the name under 60 letters."},
		{name: "not a web link", in: Input{Name: "Salsa", ProductURL: "javascript:alert(1)"}, wantErr: "That link doesn’t look right."},
		{name: "ftp link", in: Input{Name: "Salsa", ProductURL: "ftp://example.com"}, wantErr: "That link doesn’t look right."},
		{name: "zero amount", in: Input{Name: "Salsa", Quantity: "0"}, wantErr: "Pick an amount from 1 to 99."},
		{name: "huge amount", in: Input{Name: "Salsa", Quantity: "500"}, wantErr: "Pick an amount from 1 to 99."},
		{name: "odd unit", in: Input{Name: "Salsa", Quantity: "1", Unit: "bushel-ish"}, wantErr: "Pick an amount from the list."},
		{name: "bad frequency", in: Input{Name: "Salsa", Frequency: "hourly"}, wantErr: "Pick how often you get it."},
		{name: "bad mode", in: Input{Name: "Salsa", Mode: "maybe"}, wantErr: "Pick what happens when it’s due."},
		{name: "unknown meal category", in: Input{Name: "Salsa", MealCategories: []string{"brunch"}}, wantErr: "That kind of meal isn’t one we know."},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.in.normalize(canon, func(s string) bool { return s == "pasta" })
			if tt.wantErr != "" {
				var v *ValidationError
				if !errors.As(err, &v) || !strings.HasPrefix(v.Message, tt.wantErr) {
					t.Fatalf("err = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Name != tt.want.Name || got.StoreItem != tt.want.StoreItem || got.Quantity != tt.want.Quantity || got.Unit != tt.want.Unit ||
				got.ProductURL != tt.want.ProductURL || !slices.Equal(got.Cuisines, tt.want.Cuisines) || got.Frequency != tt.want.Frequency ||
				got.Mode != tt.want.Mode {
				t.Errorf("normalize = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestDueByFrequency(t *testing.T) {
	week := func(s string) planning.Week {
		w, err := planning.ParseWeek(s)
		if err != nil {
			t.Fatal(err)
		}
		return w
	}
	for _, tt := range []struct {
		frequency, last, week string
		want                  bool
	}{
		{FrequencyNone, "", "2026-W47", false},
		{EveryWeek, "2026-W47", "2026-W48", true},
		{Every2Weeks, "", "2026-W47", true},
		{Every2Weeks, "2026-W47", "2026-W47", true},
		{Every2Weeks, "2026-W47", "2026-W48", false},
		{Every2Weeks, "2026-W47", "2026-W49", true},
		{Every2Weeks, "2026-W47", "2026-W46", false},
		{EveryMonth, "2026-W47", "2026-W50", false},
		{EveryMonth, "2026-W47", "2026-W51", true},
		{EveryMonth, "2026-W52", "2027-W03", true},
	} {
		c := Companion{Frequency: tt.frequency, LastListedWeek: tt.last}
		if got := c.DueByFrequency(week(tt.week)); got != tt.want {
			t.Errorf("%s last %q, due in %s = %v, want %v", tt.frequency, tt.last, tt.week, got, tt.want)
		}
	}
}

func TestSummary(t *testing.T) {
	label := func(s string) string { return map[string]string{"mexican": "Mexican"}[s] }
	for _, tt := range []struct {
		c    Companion
		want string
	}{
		{Companion{Cuisines: []string{"mexican"}, Frequency: FrequencyNone, Mode: ModeSuggest}, "Goes with Mexican · Suggested"},
		{Companion{Cuisines: []string{"mexican", "tex mex"}, Recipes: []RecipeRef{{ID: rTacos, Name: "Beef Tacos"}}, Frequency: EveryWeek, Mode: ModeAuto},
			"Goes with Mexican, Tex Mex, and Beef Tacos · Every week · Added for you"},
		{Companion{MealCategories: []string{"pasta"}, Frequency: EveryMonth, Mode: ModeNever}, "Goes with pasta meals · Once a month · Turned off"},
		{Companion{Frequency: Every2Weeks, Mode: ModeSuggest}, "Every 2 weeks · Suggested"},
		{Companion{Frequency: FrequencyNone, Mode: ModeSuggest}, "Not paired with anything yet · Suggested"},
	} {
		if got := tt.c.Summary(label); got != tt.want {
			t.Errorf("Summary = %q, want %q", got, tt.want)
		}
	}
}

// --- service ----------------------------------------------------------------------

func TestMigrationMakesOneCompanionPerAddon(t *testing.T) {
	ctx := context.Background()
	store := &memoryStore{}
	rs := testRecipes()
	before := slices.Clone(rs.list)
	svc := newTestService(store, rs, fakeHints{{RecipeID: rBread, MealCategories: []string{"pasta"}, Cuisines: []string{"italian"}}})

	list, err := svc.List(ctx, testHousehold)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("companions = %+v", list)
	}
	bread := list[slices.IndexFunc(list, func(c Companion) bool { return c.Name == "Garlic Bread" })]
	if bread.LinkedRecipe == nil || bread.LinkedRecipe.ID != rBread || bread.Mode != ModeSuggest || bread.Frequency != FrequencyNone ||
		!slices.Equal(bread.MealCategories, []string{"pasta"}) || !slices.Equal(bread.Cuisines, []string{"italian"}) {
		t.Errorf("bread = %+v", bread)
	}
	// Again: nothing new.
	if again, _ := svc.List(ctx, testHousehold); len(again) != 2 {
		t.Errorf("second read = %d companions", len(again))
	}
	// The add-on recipes themselves are exactly as they were.
	for i := range before {
		if before[i].Name != rs.list[i].Name || before[i].IsAddon != rs.list[i].IsAddon {
			t.Errorf("recipe changed: %+v", rs.list[i])
		}
	}
	// Deleting a migrated one hides it, and migration doesn't bring it back.
	if err := svc.Delete(ctx, testHousehold, bread.ID); err != nil {
		t.Fatal(err)
	}
	if after, _ := svc.List(ctx, testHousehold); len(after) != 1 || after[0].Name != "Lime Slaw" {
		t.Errorf("after delete = %+v", after)
	}
	// A new add-on later gets one too.
	rs.list = append(rs.list, recipes.Recipe{ID: "66e5a1f2c3b4a5d6e7f8100f", Name: "Cornbread", IsAddon: true})
	if after, _ := svc.List(ctx, testHousehold); len(after) != 2 {
		t.Errorf("after a new add-on = %+v", after)
	}
	// Recipe names are read fresh.
	rs.list[2].Name = "Lime Cabbage Slaw"
	after, _ := svc.List(ctx, testHousehold)
	if after[0].LinkedRecipe.Name != "Lime Cabbage Slaw" {
		t.Errorf("linked name = %q", after[0].LinkedRecipe.Name)
	}
}

func TestCreateUpdateDelete(t *testing.T) {
	ctx := context.Background()
	store := &memoryStore{}
	svc := newTestService(store, testRecipes(), nil)

	if _, err := svc.Create(ctx, testHousehold, testUser, Input{Name: "Chips", RecipeIDs: []string{"66e5a1f2c3b4a5d6e7f8ffff"}}); err == nil ||
		!strings.Contains(err.Error(), "isn’t in your recipes") {
		t.Errorf("unknown recipe = %v", err)
	}
	c, err := svc.Create(ctx, testHousehold, testUser, Input{Name: "Chips", Cuisines: []string{"mexico"}, RecipeIDs: []string{rTacos}, Mode: ModeAuto})
	if err != nil {
		t.Fatal(err)
	}
	if c.CreatedBy != testUser || !slices.Equal(c.Cuisines, []string{"mexican"}) || len(c.Recipes) != 1 || c.Recipes[0].Name != "Beef Tacos" {
		t.Errorf("created = %+v", c)
	}
	if err := svc.MarkListed(ctx, testHousehold, c.ID, "2026-W47", testNow); err != nil {
		t.Fatal(err)
	}
	u, err := svc.Update(ctx, testHousehold, c.ID, Input{Name: "Chips and salsa", Frequency: Every2Weeks})
	if err != nil {
		t.Fatal(err)
	}
	if u.Name != "Chips and salsa" || u.Frequency != Every2Weeks || u.Mode != ModeSuggest || u.LastListedWeek != "2026-W47" || len(u.Recipes) != 0 {
		t.Errorf("updated = %+v", u)
	}
	if _, err := svc.Update(ctx, otherHousehold, c.ID, Input{Name: "x"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("update from another household = %v", err)
	}
	if err := svc.Delete(ctx, testHousehold, c.ID); err != nil {
		t.Fatal(err)
	}
	if len(store.all) != 0 {
		t.Errorf("a hand-made companion is deleted, not hidden: %+v", store.all)
	}
}

func TestCreateIsLimited(t *testing.T) {
	ctx := context.Background()
	store := &memoryStore{}
	svc := newTestService(store, &fakeRecipes{}, nil)
	for i := range MaxPerHousehold {
		store.all = append(store.all, Companion{ID: string(rune(i)), HouseholdID: testHousehold, Name: "x"})
	}
	if _, err := svc.Create(ctx, testHousehold, testUser, Input{Name: "One more"}); err == nil {
		t.Error("created past the limit")
	}
}

// --- handler ----------------------------------------------------------------------

type fakeTokens struct{}

func (fakeTokens) ValidateAccessToken(token string) (string, error) {
	if id, ok := strings.CutPrefix(token, "token-"); ok && id != "" {
		return id, nil
	}
	return "", errors.New("invalid token")
}

type fakeAuthorizer map[string][]households.Permission

func (f fakeAuthorizer) Authorize(_ context.Context, householdID, userID string, perm households.Permission) (households.Membership, error) {
	perms, ok := f[householdID+"/"+userID]
	if !ok {
		return households.Membership{}, households.ErrNotFound
	}
	m := households.Membership{HouseholdID: householdID, UserID: userID, Role: households.RoleMember}
	if !slices.Contains(perms, perm) {
		return m, households.ErrForbidden
	}
	return m, nil
}

func TestHandler(t *testing.T) {
	svc := newTestService(&memoryStore{}, testRecipes(), nil)
	r := chi.NewRouter()
	r.Use(httpx.LimitBody(1 << 20))
	r.Route("/api/v1", NewHandler(HandlerOptions{
		Service: svc, Tokens: fakeTokens{},
		Authorizer: fakeAuthorizer{
			testHousehold + "/" + testUser: {households.PermHouseholdView, households.PermRecipesEdit},
			testHousehold + "/viewer":      {households.PermHouseholdView},
		},
	}).Mount)
	do := func(method, path, body, user string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/v1/households/"+testHousehold+"/companions"+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer token-"+user)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
	body := `{"name":"Chips","storeItem":"Tortilla chips","quantity":1,"productUrl":"https://example.com/chips","cuisines":["Mexican"],"recipeIds":["` + rTacos + `"],"frequency":"none","mode":"suggest"}`
	if rec := do(http.MethodPost, "", body, "viewer"); rec.Code != http.StatusForbidden {
		t.Errorf("viewer POST = %d; want 403", rec.Code)
	}
	rec := do(http.MethodPost, "", body, testUser)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST = %d %s", rec.Code, rec.Body)
	}
	var created CompanionJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.StoreItem == nil || *created.StoreItem != "Tortilla chips" || created.Quantity == nil || *created.Quantity != 1 ||
		created.Unit == nil || *created.Unit != "count" || len(created.Recipes) != 1 || created.Recipes[0].Name != "Beef Tacos" ||
		created.Summary != "Goes with Mexican and Beef Tacos · Suggested" || created.LinkedRecipe != nil {
		t.Errorf("created = %s", rec.Body)
	}
	rec = do(http.MethodPost, "", `{"name":""}`, testUser)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Give it a name.") {
		t.Errorf("no name = %d %s", rec.Code, rec.Body)
	}
	rec = do(http.MethodGet, "", "", "viewer")
	var list ListResponse
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &list) != nil {
		t.Fatalf("GET = %d %s", rec.Code, rec.Body)
	}
	// The hand-made one first, then the two add-ons, made on first read.
	if len(list.Items) != 3 || list.Items[0].ID != created.ID || list.Items[1].LinkedRecipe == nil || len(list.Frequencies) != 4 || list.Modes[1].Label != "Just Suggest It" ||
		list.Frequencies[3].Label != "Only With Meals It Goes With" {
		t.Errorf("list = %s", rec.Body)
	}
	if rec := do(http.MethodGet, "/"+created.ID, "", "viewer"); rec.Code != http.StatusOK {
		t.Errorf("GET one = %d", rec.Code)
	}
	rec = do(http.MethodPut, "/"+created.ID, `{"name":"Chips and salsa","mode":"auto","frequency":"every_week"}`, testUser)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"storeItem":null`) || !strings.Contains(rec.Body.String(), `"mode":"auto"`) {
		t.Errorf("PUT = %d %s", rec.Code, rec.Body)
	}
	if rec := do(http.MethodDelete, "/"+created.ID, "", "viewer"); rec.Code != http.StatusForbidden {
		t.Errorf("viewer DELETE = %d", rec.Code)
	}
	if rec := do(http.MethodDelete, "/"+created.ID, "", testUser); rec.Code != http.StatusNoContent {
		t.Errorf("DELETE = %d", rec.Code)
	}
	rec = do(http.MethodDelete, "/"+created.ID, "", testUser)
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "That companion isn’t here anymore.") {
		t.Errorf("DELETE again = %d %s", rec.Code, rec.Body)
	}
}

// --- MongoDB ----------------------------------------------------------------------

func TestIntegrationMongoStore(t *testing.T) {
	client := mongotest.Client(t)
	ctx := context.Background()
	for range 2 {
		if err := client.EnsureIndexes(ctx, Indexes()...); err != nil {
			t.Fatalf("EnsureIndexes() error = %v", err)
		}
	}
	store := NewMongoStore(client.Database())
	svc := newTestService(store, testRecipes(), fakeHints{{RecipeID: rBread, Cuisines: []string{"italian"}}})

	// Migration from two requests at once makes one companion per add-on.
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			if _, err := svc.List(ctx, testHousehold); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	list, err := store.List(ctx, testHousehold, true)
	if err != nil || len(list) != 2 {
		t.Fatalf("migrated = %+v, %v", list, err)
	}
	bread := list[slices.IndexFunc(list, func(c Companion) bool { return c.LinkedRecipe.ID == rBread })]
	if bread.Name != "Garlic Bread" || bread.LinkedRecipe.Name != "Garlic Bread" || !slices.Equal(bread.Cuisines, []string{"italian"}) || bread.Mode != ModeSuggest {
		t.Errorf("bread = %+v", bread)
	}

	c, err := svc.Create(ctx, testHousehold, testUser, Input{Name: "Chips", StoreItem: "Tortilla chips", Quantity: "1", Cuisines: []string{"mexican"}, RecipeIDs: []string{rTacos}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(ctx, testHousehold, c.ID)
	if err != nil || got.StoreItem != "Tortilla chips" || got.Unit != "count" || got.CreatedBy != testUser || len(got.Recipes) != 1 {
		t.Fatalf("Get = %+v, %v", got, err)
	}
	if _, err := store.Get(ctx, otherHousehold, c.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get from another household = %v", err)
	}
	if err := store.MarkListed(ctx, testHousehold, c.ID, "2026-W48", testNow); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkListed(ctx, testHousehold, c.ID, "2026-W47", testNow); err != nil {
		t.Fatal(err)
	}
	u, err := svc.Update(ctx, testHousehold, c.ID, Input{Name: "Chips", Mode: ModeAuto})
	if err != nil || u.LastListedWeek != "2026-W48" || u.StoreItem != "" || u.Quantity != "" || len(u.Recipes) != 0 || u.Mode != ModeAuto {
		t.Fatalf("Update = %+v, %v", u, err)
	}
	if err := svc.Delete(ctx, testHousehold, bread.ID); err != nil {
		t.Fatal(err)
	}
	if visible, _ := svc.List(ctx, testHousehold); len(visible) != 2 {
		t.Errorf("after hiding the bread = %+v", visible)
	}
	if err := svc.Delete(ctx, testHousehold, c.ID); err != nil {
		t.Fatal(err)
	}
	if all, _ := store.List(ctx, testHousehold, true); len(all) != 2 {
		t.Errorf("stored = %d, want the hidden bread and the slaw", len(all))
	}
	if err := store.PurgeHousehold(ctx, testHousehold); err != nil {
		t.Fatal(err)
	}
	if left, _ := store.List(ctx, testHousehold, true); len(left) != 0 {
		t.Errorf("PurgeHousehold left %d", len(left))
	}
}
