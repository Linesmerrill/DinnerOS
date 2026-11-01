package shopping

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
)

func groceryItemNamed(t *testing.T, list planning.GroceryList, key string) (grocery.Item, bool) {
	t.Helper()
	for _, c := range list.Categories {
		for _, it := range c.Items {
			if it.IngredientKey == key {
				return it, true
			}
		}
	}
	return grocery.Item{}, false
}

func TestAddedItemsJoinTheListAndTheCart(t *testing.T) {
	f := newFixture(t)
	f.plans.WithExtras(f.svc)

	added, err := f.svc.AddItems(f.ctx, f.actor, testWeek, []AddItemInput{{Name: "  milk "}, {Name: "Paper Towels", Count: 2}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 2 || added[0].IngredientKey != f.keys["Milk"] || added[0].Name != "Milk" || added[0].Count != 1 {
		t.Fatalf("added = %+v: milk resolves to the recipe's ingredient", added)
	}
	if added[1].IngredientKey != "name:paper towels" || added[1].Name != "Paper Towels" || added[1].Count != 2 {
		t.Fatalf("added = %+v", added[1])
	}

	list, err := f.plans.GroceryList(f.ctx, testHousehold, testWeek)
	if err != nil {
		t.Fatal(err)
	}
	towels, ok := groceryItemNamed(t, list, "name:paper towels")
	if !ok || len(towels.Sources) != 0 || len(towels.Extras) != 1 || towels.Extras[0].Text != addedText ||
		towels.Extras[0].ID != added[1].ID || towels.Status != grocery.StatusToBuy {
		t.Fatalf("paper towels = %+v", towels)
	}
	milk, _ := groceryItemNamed(t, list, f.keys["Milk"])
	if len(milk.Sources) != 1 || len(milk.Extras) != 1 {
		t.Errorf("milk keeps its recipe and gains the added line: %+v", milk)
	}

	// Adding it again sets the count; the line keeps its ID.
	again, err := f.svc.AddItems(f.ctx, f.actor, testWeek, []AddItemInput{{Name: "paper towels", Count: 3}}, false)
	if err != nil || again[0].ID != added[1].ID || again[0].Count != 3 || again[0].Name != "Paper Towels" {
		t.Fatalf("again = %+v, %v", again, err)
	}

	_, _, err = f.svc.PutPreference(f.ctx, f.actor, "walmart", "name:paper towels", PreferenceInput{
		ProductURL: "https://www.walmart.com/ip/100000090", DisplayName: "Test Towels", PackageSize: &PackageSize{Quantity: "6", Unit: "count"},
	})
	if err != nil {
		t.Fatal(err)
	}
	m, err := f.svc.Match(f.ctx, testHousehold, testWeek, "walmart", MatchInput{})
	if err != nil {
		t.Fatal(err)
	}
	line := lineFor(t, m, "Paper Towels")
	for _, e := range m.Excluded {
		if e.IngredientKey == f.keys["Milk"] && e.Added {
			t.Errorf("milk is for a meal too, so it isn't only added: %+v", e)
		}
	}
	if !line.Added || line.Packages != 3 || line.ComputedPackages != 3 || line.Reason != "" || len(line.Shares) != 0 {
		t.Fatalf("line = %+v: an added item sends its count as packages", line)
	}
	h, _, err := f.svc.CreateHandoff(f.ctx, f.actor, testWeek, "walmart", MatchInput{Lines: []LineSelection{{IngredientKey: "name:paper towels"}}})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := f.store.GetHandoff(f.ctx, testHousehold, h.ID)
	if err != nil {
		t.Fatal(err)
	}
	if l := lineFor(t, stored.Proposal, "Paper Towels"); !l.Added || l.PackageCount().Packages != 3 {
		t.Errorf("stored line = %+v", l)
	}

	if err := f.svc.RemoveAddedItem(f.ctx, viewer(), added[1].ID); !errors.Is(err, ErrForbidden) {
		t.Errorf("a viewer can't remove items: %v", err)
	}
	if err := f.svc.RemoveAddedItem(f.ctx, f.actor, added[1].ID); err != nil {
		t.Fatal(err)
	}
	list, _ = f.plans.GroceryList(f.ctx, testHousehold, testWeek)
	if _, ok := groceryItemNamed(t, list, "name:paper towels"); ok {
		t.Error("a removed item leaves the list")
	}
	if err := f.svc.RemoveAddedItem(f.ctx, f.actor, added[1].ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("removing twice = %v", err)
	}
}

func TestAddItemsValidates(t *testing.T) {
	f := newFixture(t)
	cases := map[string][]AddItemInput{
		"no items":   {},
		"blank name": {{Name: "  "}},
		"too many":   {{Name: "Milk", Count: MaxAddedItemCount + 1}},
		"negative":   {{Name: "Milk", Count: -1}},
	}
	for name, in := range cases {
		var validation *ValidationError
		if _, err := f.svc.AddItems(f.ctx, f.actor, testWeek, in, false); !errors.As(err, &validation) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if _, err := f.svc.AddItems(f.ctx, viewer(), testWeek, []AddItemInput{{Name: "Milk"}}, false); !errors.Is(err, ErrForbidden) {
		t.Errorf("viewer: %v", err)
	}
	if _, err := f.svc.AddItems(f.ctx, f.actor, "2026-38", []AddItemInput{{Name: "Milk"}}, false); !errors.Is(err, planning.ErrInvalidWeek) {
		t.Errorf("bad week: %v", err)
	}
}

func TestUsualsComeFromTheHouseholdsOwnHistory(t *testing.T) {
	f := newFixture(t)
	f.plans.WithExtras(f.svc)
	for _, week := range []string{"2026-W35", "2026-W36", "2026-W37"} {
		if _, err := f.svc.AddItems(f.ctx, f.actor, week, []AddItemInput{{Name: "Eggs", Count: 2}, {Name: "Milk"}}, false); err != nil {
			t.Fatal(err)
		}
	}
	// Milk is already on this week's list (the tacos need it); eggs aren't.
	v, err := f.svc.Usuals(f.ctx, testHousehold, testWeek)
	if err != nil {
		t.Fatal(err)
	}
	if v.ShoppingWeeks != 3 || len(v.Items) != 2 || !v.Prompt {
		t.Fatalf("view = %+v", v)
	}
	offered := v.Offered()
	if len(offered) != 1 || offered[0].IngredientKey != "name:eggs" || offered[0].Count != 2 || offered[0].Weeks != 3 {
		t.Fatalf("offered = %+v", offered)
	}

	// Not Now hides the prompt for this week only.
	if err := f.svc.DismissUsuals(f.ctx, f.actor, testWeek); err != nil {
		t.Fatal(err)
	}
	if v, _ := f.svc.Usuals(f.ctx, testHousehold, testWeek); v.Prompt || len(v.Offered()) != 1 {
		t.Errorf("after Not Now: %+v", v)
	}
	if v, _ := f.svc.Usuals(f.ctx, testHousehold, "2026-W39"); !v.Prompt {
		t.Errorf("next week asks again: %+v", v)
	}

	// Adding them from the prompt puts them on the list and answers it.
	if _, err := f.svc.AddItems(f.ctx, f.actor, "2026-W39", []AddItemInput{{Name: "Eggs", Count: 2}}, true); err != nil {
		t.Fatal(err)
	}
	v, _ = f.svc.Usuals(f.ctx, testHousehold, "2026-W39")
	if v.Prompt || len(v.Offered()) != 1 || v.Offered()[0].IngredientKey == "name:eggs" {
		t.Errorf("after adding: %+v", v)
	}
	list, _ := f.plans.GroceryList(f.ctx, testHousehold, "2026-W39")
	if eggs, ok := groceryItemNamed(t, list, "name:eggs"); !ok || eggs.Extras[0].Text != fromUsualsText {
		t.Errorf("eggs = %+v", eggs)
	}
}

func TestPinningAndNotAUsual(t *testing.T) {
	f := newFixture(t)
	for _, week := range []string{"2026-W35", "2026-W36", "2026-W37"} {
		if _, err := f.svc.AddItems(f.ctx, f.actor, week, []AddItemInput{{Name: "Eggs"}}, false); err != nil {
			t.Fatal(err)
		}
	}
	pinned, err := f.svc.SetUsual(f.ctx, f.actor, UsualInput{Name: "Coffee", Count: 2, Pinned: true})
	if err != nil || pinned.IngredientKey != "name:coffee" || pinned.Count != 2 {
		t.Fatalf("pinned = %+v, %v", pinned, err)
	}
	if _, err := f.svc.SetUsual(f.ctx, f.actor, UsualInput{IngredientKey: "name:eggs", Name: "Eggs", NotUsual: true}); err != nil {
		t.Fatal(err)
	}
	v, err := f.svc.Usuals(f.ctx, testHousehold, testWeek)
	if err != nil {
		t.Fatal(err)
	}
	keys := []string{}
	for _, u := range v.Items {
		keys = append(keys, u.IngredientKey)
	}
	if !slices.Equal(keys, []string{"name:coffee"}) || len(v.NotUsual) != 1 || v.NotUsual[0].IngredientKey != "name:eggs" {
		t.Fatalf("view = %+v", v)
	}

	// Clearing both puts eggs back to being counted.
	if _, err := f.svc.SetUsual(f.ctx, f.actor, UsualInput{IngredientKey: "name:eggs", Name: "Eggs"}); err != nil {
		t.Fatal(err)
	}
	if v, _ := f.svc.Usuals(f.ctx, testHousehold, testWeek); len(v.Items) != 2 || len(v.NotUsual) != 0 {
		t.Errorf("view = %+v", v)
	}

	var validation *ValidationError
	if _, err := f.svc.SetUsual(f.ctx, f.actor, UsualInput{Name: "Eggs", Pinned: true, NotUsual: true}); !errors.As(err, &validation) {
		t.Errorf("pinned and not usual: %v", err)
	}
	if _, err := f.svc.SetUsual(f.ctx, viewer(), UsualInput{Name: "Eggs", Pinned: true}); !errors.Is(err, ErrForbidden) {
		t.Errorf("viewer: %v", err)
	}
}

func TestPurgingAHouseholdRemovesItsUsuals(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.AddItems(f.ctx, f.actor, testWeek, []AddItemInput{{Name: "Eggs"}}, true); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.SetUsual(f.ctx, f.actor, UsualInput{Name: "Coffee", Pinned: true}); err != nil {
		t.Fatal(err)
	}
	if err := f.store.PurgeHousehold(f.ctx, testHousehold); err != nil {
		t.Fatal(err)
	}
	items, _ := f.store.ListAddedItems(f.ctx, testHousehold, "")
	prefs, _ := f.store.ListUsualPrefs(f.ctx, testHousehold)
	_, err := f.store.GetUsualsPrompt(f.ctx, testHousehold, testWeek)
	if len(items) != 0 || len(prefs) != 0 || !errors.Is(err, ErrNotFound) {
		t.Errorf("left behind: %+v, %+v, %v", items, prefs, err)
	}
}

func TestUsualsHTTP(t *testing.T) {
	f := newFixture(t)
	f.plans.WithExtras(f.svc)
	r := chi.NewRouter()
	r.Route("/api/v1", NewHandler(HandlerOptions{
		Service: f.svc, Pantry: f.pantry, Tokens: fakeTokens{},
		Authorizer: fakeAuthorizer{testHousehold + "/" + testUser: households.RoleMember, testHousehold + "/" + viewerUser: households.Role("viewer")},
		Households: fakeHouseholds{testUser: {testHousehold}},
	}).Mount)
	do := func(method, path, body, userID string) (*httptest.ResponseRecorder, map[string]any) {
		t.Helper()
		req := httptest.NewRequest(method, "/api/v1/households/"+testHousehold+"/shopping"+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer token-"+userID)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec, out
	}

	for _, week := range []string{"2026-W35", "2026-W36", "2026-W37"} {
		if rec, body := do(http.MethodPost, "/weeks/"+week+"/items", `{"items":[{"name":"Eggs","count":2}]}`, testUser); rec.Code != http.StatusOK {
			t.Fatalf("add = %d %v", rec.Code, body)
		}
	}
	rec, body := do(http.MethodGet, "/weeks/"+testWeek+"/usuals", "", testUser)
	items, _ := body["items"].([]any)
	if rec.Code != http.StatusOK || body["prompt"] != true || body["window"] != 6.0 || body["minWeeks"] != 3.0 || len(items) != 1 {
		t.Fatalf("usuals = %d %v", rec.Code, body)
	}
	if eggs := items[0].(map[string]any); eggs["ingredientKey"] != "name:eggs" || eggs["count"] != 2.0 || eggs["onList"] != false || eggs["weeks"] != 3.0 {
		t.Errorf("eggs = %v", eggs)
	}

	rec, body = do(http.MethodPost, "/weeks/"+testWeek+"/items", `{"items":[{"name":"Eggs","count":2}],"fromUsuals":true}`, testUser)
	added, _ := body["items"].([]any)
	if rec.Code != http.StatusOK || len(added) != 1 || added[0].(map[string]any)["fromUsuals"] != true {
		t.Fatalf("add usuals = %d %v", rec.Code, body)
	}
	id, _ := added[0].(map[string]any)["id"].(string)
	if _, body = do(http.MethodGet, "/weeks/"+testWeek+"/usuals", "", testUser); body["prompt"] != false {
		t.Errorf("answered = %v", body)
	}

	if rec, body = do(http.MethodPut, "/usuals", `{"name":"Coffee","pinned":true}`, testUser); rec.Code != http.StatusOK ||
		body["usual"].(map[string]any)["ingredientKey"] != "name:coffee" {
		t.Errorf("pin = %d %v", rec.Code, body)
	}
	if rec, _ = do(http.MethodPost, "/weeks/2026-W39/usuals/dismiss", "", testUser); rec.Code != http.StatusNoContent {
		t.Errorf("dismiss = %d", rec.Code)
	}
	if rec, _ = do(http.MethodPost, "/weeks/"+testWeek+"/items", `{"items":[{"name":"Milk"}]}`, viewerUser); rec.Code != http.StatusForbidden {
		t.Errorf("viewer add = %d", rec.Code)
	}
	if rec, body = do(http.MethodPost, "/weeks/"+testWeek+"/items", `{"items":[{"name":""}]}`, testUser); rec.Code != http.StatusBadRequest {
		t.Errorf("blank = %d %v", rec.Code, body)
	}
	if rec, _ = do(http.MethodPost, "/weeks/"+testWeek+"/items", `{"items":[{"name":"Paper Towels"}]}`, testUser); rec.Code != http.StatusOK {
		t.Fatalf("add towels = %d", rec.Code)
	}
	if rec, _ = do(http.MethodDelete, "/weeks/"+testWeek+"/items?ingredientKey=name%3Apaper%20towels", "", testUser); rec.Code != http.StatusNoContent {
		t.Errorf("remove by key = %d", rec.Code)
	}
	if rec, _ = do(http.MethodDelete, "/weeks/"+testWeek+"/items?ingredientKey=name%3Apaper%20towels", "", testUser); rec.Code != http.StatusNotFound {
		t.Errorf("remove by key again = %d", rec.Code)
	}
	if rec, _ = do(http.MethodDelete, "/weeks/"+testWeek+"/items/"+id, "", testUser); rec.Code != http.StatusNoContent {
		t.Errorf("remove = %d", rec.Code)
	}
	if rec, _ = do(http.MethodDelete, "/weeks/"+testWeek+"/items/"+id, "", testUser); rec.Code != http.StatusNotFound {
		t.Errorf("remove again = %d", rec.Code)
	}
}
