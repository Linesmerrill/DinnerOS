package sameitems

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// Synthetic IDs, valid ObjectID hex so the fixtures work against MongoDB.
const (
	testHousehold  = "66e5a1f2c3b4a5d6e7f80a01"
	otherHousehold = "66e5a1f2c3b4a5d6e7f80b01"
	testUser       = "66e5a1f2c3b4a5d6e7f80c01"
)

var testNow = time.Date(2026, 11, 8, 18, 30, 0, 0, time.UTC)

// memoryStore is an in-memory Store following MongoStore's contract.
type memoryStore struct {
	mu    sync.Mutex
	next  int
	links []Link
}

func (m *memoryStore) ListLinks(_ context.Context, householdID string) ([]Link, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Link
	for _, l := range m.links {
		if l.HouseholdID == householdID {
			out = append(out, l)
		}
	}
	return out, nil
}

func (m *memoryStore) PutLink(_ context.Context, l Link) (Link, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, have := range m.links {
		if have.HouseholdID == l.HouseholdID && have.VariantKey == l.VariantKey {
			m.links[i].VariantName, m.links[i].KeptKey, m.links[i].KeptName = l.VariantName, l.KeptKey, l.KeptName
			return m.links[i], false, nil
		}
	}
	m.next++
	l.ID = fmt.Sprintf("66e5a1f2c3b4a5d6e7f8%04d", m.next)
	m.links = append(m.links, l)
	return l, true, nil
}

func (m *memoryStore) RepointKept(_ context.Context, householdID, fromKey, keptKey, keptName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, l := range m.links {
		if l.HouseholdID == householdID && l.KeptKey == fromKey {
			m.links[i].KeptKey, m.links[i].KeptName = keptKey, keptName
		}
	}
	return nil
}

func (m *memoryStore) DeleteLink(_ context.Context, householdID, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, l := range m.links {
		if l.HouseholdID == householdID && l.ID == id {
			m.links = slices.Delete(m.links, i, i+1)
			return nil
		}
	}
	return ErrNotFound
}

type fakeCatalog map[string]string // normalized name → catalog ID

func (f fakeCatalog) IngredientsByKey(_ context.Context, keys []string) ([]recipes.Ingredient, error) {
	var out []recipes.Ingredient
	for _, k := range keys {
		if id, ok := f[k]; ok {
			out = append(out, recipes.Ingredient{ID: id, Key: k})
		}
	}
	return out, nil
}

var editor = households.Membership{HouseholdID: testHousehold, UserID: testUser, Role: households.RoleMember}

func newTestService() (*Service, *memoryStore) {
	store := &memoryStore{}
	svc := NewService(store, fakeCatalog{"chipotle powder": "ing-chipotle-powder"})
	svc.now = func() time.Time { return testNow }
	return svc, store
}

func link(t *testing.T, svc *Service, variant, variantName, kept, keptName string) Link {
	t.Helper()
	l, _, err := svc.Link(context.Background(), editor, Input{
		VariantKey: variant, VariantName: variantName, KeptKey: kept, KeptName: keptName,
	})
	if err != nil {
		t.Fatalf("Link(%s → %s) error = %v", variant, kept, err)
	}
	return l
}

func TestLinkMakesTheVariantTheKeptItem(t *testing.T) {
	svc, _ := newTestService()
	l := link(t, svc, "name:Chipotle Powder", "Chipotle Powder", "ing-chipotle-chili-powder", "Chipotle Chili Powder")
	if l.VariantKey != "name:chipotle powder" || l.KeptKey != "ing-chipotle-chili-powder" || l.CreatedBy != testUser {
		t.Fatalf("Link() = %+v; want the free-text key normalized and the kept item stored", l)
	}
	same, err := svc.GrocerySameItems(context.Background(), testHousehold)
	if err != nil || len(same) != 1 {
		t.Fatalf("GrocerySameItems() = %+v, %v", same, err)
	}
	// Reached as free text or through the catalog, it's the same variant.
	want := []string{"name:chipotle powder", "ing-chipotle-powder"}
	if !slices.Equal(same[0].VariantKeys, want) || same[0].KeptName != "Chipotle Chili Powder" || same[0].LinkID != l.ID {
		t.Errorf("GrocerySameItems()[0] = %+v; want keys %v", same[0], want)
	}
}

func TestLinkRefusesWhatCantBeLinked(t *testing.T) {
	svc, _ := newTestService()
	cases := []Input{
		{VariantKey: "ing-a", VariantName: "Garlic Powder", KeptKey: "ing-a", KeptName: "Garlic Powder"},
		{VariantKey: "name:Garlic", VariantName: "Garlic", KeptKey: "name:garlic", KeptName: "Garlic"},
		{VariantKey: "", VariantName: "Garlic", KeptKey: "ing-b", KeptName: "Garlic Powder"},
		{VariantKey: "ing-a", VariantName: "  ", KeptKey: "ing-b", KeptName: "Garlic Powder"},
		{VariantKey: "ing-a", VariantName: "Garlic", KeptKey: "ing-b", KeptName: strings.Repeat("g", MaxNameLength+1)},
	}
	for _, in := range cases {
		_, _, err := svc.Link(context.Background(), editor, in)
		var v *ValidationError
		if !errors.As(err, &v) {
			t.Errorf("Link(%+v) error = %v; want a validation error", in, err)
		}
	}
	viewer := editor
	viewer.Role = households.Role("guest") // not a role, so it may do nothing
	if _, _, err := svc.Link(context.Background(), viewer, Input{
		VariantKey: "ing-a", VariantName: "A", KeptKey: "ing-b", KeptName: "B",
	}); !errors.Is(err, ErrForbidden) {
		t.Errorf("a viewer linking error = %v; want ErrForbidden", err)
	}
}

func TestLinksNeverChain(t *testing.T) {
	svc, store := newTestService()
	// Fine Garlic Powder → Garlic Powder, then Garlic Powder → Granulated Garlic:
	// both now buy Granulated Garlic.
	link(t, svc, "ing-fine", "Fine Garlic Powder", "ing-powder", "Garlic Powder")
	link(t, svc, "ing-powder", "Garlic Powder", "ing-granulated", "Granulated Garlic")
	for _, l := range store.links {
		if l.KeptKey != "ing-granulated" {
			t.Errorf("%s is kept as %s; want every link to end at Granulated Garlic", l.VariantName, l.KeptName)
		}
	}
	// Linking to a variant links to its kept item instead.
	l := link(t, svc, "ing-minced", "Garlic, Minced Dried", "ing-fine", "Fine Garlic Powder")
	if l.KeptKey != "ing-granulated" || l.KeptName != "Granulated Garlic" {
		t.Errorf("Link to a variant = %+v; want it kept as Granulated Garlic", l)
	}
	// Two names that are already one item can't be linked again the other way.
	_, _, err := svc.Link(context.Background(), editor, Input{
		VariantKey: "ing-granulated", VariantName: "Granulated Garlic", KeptKey: "ing-fine", KeptName: "Fine Garlic Powder",
	})
	var v *ValidationError
	if !errors.As(err, &v) || v.Message != "These are already the same item." {
		t.Errorf("linking a kept item to its own variant error = %v", err)
	}
}

func TestLinkingANameAgainReplacesItsLink(t *testing.T) {
	svc, store := newTestService()
	first := link(t, svc, "ing-fine", "Fine Garlic Powder", "ing-powder", "Garlic Powder")
	_, created, err := svc.Link(context.Background(), editor, Input{
		VariantKey: "ing-fine", VariantName: "Fine Garlic Powder", KeptKey: "ing-granulated", KeptName: "Granulated Garlic",
	})
	if err != nil || created || len(store.links) != 1 || store.links[0].ID != first.ID ||
		store.links[0].KeptKey != "ing-granulated" {
		t.Fatalf("relinking = created %v, %v, links %+v; want the one link moved", created, err, store.links)
	}
}

func TestLinksAreLimited(t *testing.T) {
	svc, store := newTestService()
	for i := range MaxPerHousehold {
		store.links = append(store.links, Link{
			ID: fmt.Sprint(i), HouseholdID: testHousehold, VariantKey: fmt.Sprintf("ing-%d", i), KeptKey: "ing-kept",
		})
	}
	_, _, err := svc.Link(context.Background(), editor, Input{
		VariantKey: "ing-new", VariantName: "New", KeptKey: "ing-kept", KeptName: "Kept",
	})
	var v *ValidationError
	if !errors.As(err, &v) || v.Message != "You can link up to 200 items." {
		t.Errorf("Link() over the limit error = %v", err)
	}
	// An existing link stays changeable at the cap.
	if _, _, err := svc.Link(context.Background(), editor, Input{
		VariantKey: "ing-3", VariantName: "Three", KeptKey: "ing-other", KeptName: "Other",
	}); err != nil {
		t.Errorf("relinking at the cap error = %v", err)
	}
}

func TestUnlinkMakesTheNameItsOwnItemAgain(t *testing.T) {
	svc, store := newTestService()
	l := link(t, svc, "ing-fine", "Fine Garlic Powder", "ing-powder", "Garlic Powder")
	if err := svc.Unlink(context.Background(), editor, l.ID); err != nil || len(store.links) != 0 {
		t.Fatalf("Unlink() = %v, links %+v", err, store.links)
	}
	if err := svc.Unlink(context.Background(), editor, l.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Unlink() twice error = %v; want ErrNotFound", err)
	}
	other := editor
	other.HouseholdID = otherHousehold
	l = link(t, svc, "ing-fine", "Fine Garlic Powder", "ing-powder", "Garlic Powder")
	if err := svc.Unlink(context.Background(), other, l.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("another household's Unlink() error = %v; want ErrNotFound", err)
	}
}

// --- HTTP ---------------------------------------------------------------------

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

func TestHandlerLinksListsAndUnlinks(t *testing.T) {
	svc, _ := newTestService()
	r := chi.NewRouter()
	r.Use(httpx.LimitBody(1 << 20))
	r.Route("/api/v1", NewHandler(HandlerOptions{
		Service: svc, Tokens: fakeTokens{},
		Authorizer: fakeAuthorizer{
			testHousehold + "/" + testUser: {households.PermHouseholdView, households.PermPlanEdit},
			testHousehold + "/viewer":      {households.PermHouseholdView},
		},
	}).Mount)
	do := func(method, path, body, user string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/v1/households/"+testHousehold+"/same-items"+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer token-"+user)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
	body := `{"variantKey":"name:chipotle powder","variantName":"Chipotle Powder","keptKey":"ing-ccp","keptName":"Chipotle Chili Powder"}`
	if rec := do(http.MethodPost, "", body, "viewer"); rec.Code != http.StatusForbidden {
		t.Errorf("viewer POST = %d; want 403", rec.Code)
	}
	rec := do(http.MethodPost, "", body, testUser)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST = %d %s; want 201", rec.Code, rec.Body)
	}
	var created LinkResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || created.KeptName != "Chipotle Chili Powder" {
		t.Fatalf("POST body = %s, %v", rec.Body, err)
	}
	if rec := do(http.MethodPost, "", body, testUser); rec.Code != http.StatusOK {
		t.Errorf("POST again = %d; want 200", rec.Code)
	}
	rec = do(http.MethodPost, "", `{"variantKey":"ing-a","variantName":"A","keptKey":"ing-a","keptName":"A"}`, testUser)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Pick a different item.") {
		t.Errorf("self link = %d %s; want 400 in plain words", rec.Code, rec.Body)
	}
	rec = do(http.MethodGet, "", "", "viewer")
	var list LinkListResponse
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &list) != nil || len(list.Items) != 1 {
		t.Fatalf("GET = %d %s", rec.Code, rec.Body)
	}
	if rec := do(http.MethodDelete, "/"+created.ID, "", testUser); rec.Code != http.StatusNoContent {
		t.Errorf("DELETE = %d; want 204", rec.Code)
	}
	if rec := do(http.MethodDelete, "/"+created.ID, "", testUser); rec.Code != http.StatusNotFound {
		t.Errorf("DELETE again = %d; want 404", rec.Code)
	}
}

// --- MongoDB ------------------------------------------------------------------

func TestIntegrationMongoStore(t *testing.T) {
	client := mongotest.Client(t)
	ctx := context.Background()
	for range 2 { // applying indexes twice is a no-op
		if err := client.EnsureIndexes(ctx, Indexes()...); err != nil {
			t.Fatalf("EnsureIndexes() error = %v", err)
		}
	}
	store := NewMongoStore(client.Database())
	l := Link{
		HouseholdID: testHousehold, VariantKey: "ing-fine", VariantName: "Fine Garlic Powder",
		KeptKey: "ing-powder", KeptName: "Garlic Powder", CreatedBy: testUser, CreatedAt: testNow,
	}
	first, created, err := store.PutLink(ctx, l)
	if err != nil || !created || first.ID == "" {
		t.Fatalf("PutLink() = %+v, %v, %v", first, created, err)
	}
	l.KeptKey, l.KeptName = "ing-granulated", "Granulated Garlic"
	again, created, err := store.PutLink(ctx, l)
	if err != nil || created || again.ID != first.ID || again.KeptKey != "ing-granulated" {
		t.Fatalf("PutLink() again = %+v, %v, %v; want the same link replaced", again, created, err)
	}
	if err := store.RepointKept(ctx, testHousehold, "ing-granulated", "ing-g2", "Garlic"); err != nil {
		t.Fatal(err)
	}
	links, err := store.ListLinks(ctx, testHousehold)
	if err != nil || len(links) != 1 || links[0].KeptKey != "ing-g2" {
		t.Fatalf("ListLinks() = %+v, %v", links, err)
	}
	if other, _ := store.ListLinks(ctx, otherHousehold); len(other) != 0 {
		t.Errorf("another household sees %d links", len(other))
	}
	if err := store.DeleteLink(ctx, otherHousehold, first.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteLink() from another household = %v", err)
	}
	if err := store.DeleteLink(ctx, testHousehold, first.ID); err != nil {
		t.Errorf("DeleteLink() = %v", err)
	}
	if _, _, err := store.PutLink(ctx, l); err != nil {
		t.Fatal(err)
	}
	if err := store.PurgeHousehold(ctx, testHousehold); err != nil {
		t.Fatal(err)
	}
	if left, _ := store.ListLinks(ctx, testHousehold); len(left) != 0 {
		t.Errorf("PurgeHousehold() left %d links", len(left))
	}
}

type fakeLeftOut grocery.LeftOutSet

func (f fakeLeftOut) RecipeLeftOut(context.Context, string, string) (grocery.LeftOutSet, error) {
	return maps.Clone(grocery.LeftOutSet(f)), nil
}

// Cooking views read skips through the household's links: a skip saved under
// the kept name crosses out the recipe's other name.
func TestLeftOutReadsThroughLinks(t *testing.T) {
	svc, _ := newTestService()
	link(t, svc, "name:chipotle powder", "Chipotle Powder", "ing-ccp", "Chipotle Chili Powder")
	skip := grocery.LeftOut{SkipID: "s-1", Scope: grocery.SkipRecipe}
	src := LeftOut{Skips: fakeLeftOut{"ing-ccp": skip}, Same: svc}
	got, err := src.RecipeLeftOut(context.Background(), editor.HouseholdID, "r-tacos")
	if err != nil {
		t.Fatal(err)
	}
	if got["name:chipotle powder"] != skip {
		t.Errorf("variant = %+v, want the kept name's skip", got["name:chipotle powder"])
	}

	// Without links, or with no service, it's the skips as they are.
	bare := LeftOut{Skips: fakeLeftOut{"ing-ccp": skip}}
	if got, _ := bare.RecipeLeftOut(context.Background(), editor.HouseholdID, "r-tacos"); len(got) != 1 {
		t.Errorf("no service = %+v", got)
	}
}
