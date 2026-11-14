package recipes

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

// fakePlans records what a recipe delete and restore did to the plan.
type fakePlans struct {
	planned  map[string][]RemovedPlanEntry // by recipe ID
	restored []RemovedPlanEntry
	users    []string
}

func (p *fakePlans) RemoveRecipeFromUpcoming(_ context.Context, _, userID, recipeID string) ([]RemovedPlanEntry, error) {
	p.users = append(p.users, userID)
	removed := p.planned[recipeID]
	delete(p.planned, recipeID)
	return removed, nil
}

func (p *fakePlans) RestoreRecipeEntries(_ context.Context, _, _, _ string, entries []RemovedPlanEntry) error {
	p.restored = append(p.restored, entries...)
	return nil
}

func newDeleteService(t *testing.T) (*Service, *memoryStore, *fakePlans, *time.Time) {
	t.Helper()
	store := newMemoryStore()
	now := time.Date(2026, 11, 16, 18, 0, 0, 0, time.UTC)
	plans := &fakePlans{planned: map[string][]RemovedPlanEntry{}}
	svc := NewService(store).WithPlans(plans)
	svc.now = func() time.Time { return now }
	return svc, store, plans, &now
}

func TestDeleteHidesTheRecipeButKeepsItReadable(t *testing.T) {
	ctx := context.Background()
	svc, _, plans, _ := newDeleteService(t)
	mustImport(t, svc, hhAda, testFile(testRecipe("hf-1", "Garlic Noodles"), testRecipe("hf-2", "Tacos")))
	noodles := recipeByName(t, svc, hhAda, "Garlic Noodles")
	plans.planned[noodles.ID] = []RemovedPlanEntry{
		{Week: "2026-W48", Day: "tue", Servings: 2}, {Week: "2026-W47", Day: "wed", Servings: 2}, {Week: "2026-W48", Servings: 4},
	}

	res, err := svc.Delete(ctx, hhAda, userAda, noodles.ID)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"2026-W47", "2026-W48"}; !slices.Equal(res.RemovedFromWeeks, want) {
		t.Errorf("removed from %v; want %v", res.RemovedFromWeeks, want)
	}
	if want := time.Date(2026, 12, 16, 18, 0, 0, 0, time.UTC); !res.UndoUntil.Equal(want) {
		t.Errorf("undo until %v; want %v", res.UndoUntil, want)
	}
	if !slices.Equal(plans.users, []string{userAda}) {
		t.Errorf("plan changed by %v", plans.users)
	}

	page, err := svc.List(ctx, hhAda, ListQuery{})
	if err != nil || len(page.Items) != 1 || page.Items[0].Name != "Tacos" {
		t.Fatalf("list after delete = %+v, %v", page.Items, err)
	}
	if found, _ := svc.List(ctx, hhAda, ListQuery{Search: "garlic"}); len(found.Items) != 0 {
		t.Errorf("search found the deleted recipe: %+v", found.Items)
	}
	for name, list := range map[string]func(context.Context, string) ([]Recipe, error){
		"catalog": svc.Catalog, "menu catalog": svc.MenuCatalog,
	} {
		got, err := list(ctx, hhAda)
		if err != nil || len(got) != 1 || got[0].Name != "Tacos" {
			t.Errorf("%s after delete = %v, %v", name, names(got), err)
		}
	}
	// Past weeks and cooked history still read it by ID.
	kept, err := svc.Get(ctx, hhAda, noodles.ID)
	if err != nil || kept.DeletedAt.IsZero() {
		t.Fatalf("get deleted = %+v, %v", kept, err)
	}
	many, err := svc.GetMany(ctx, hhAda, []string{noodles.ID})
	if err != nil || len(many) != 1 {
		t.Errorf("get many deleted = %v, %v", names(many), err)
	}

	// Deleting again changes nothing.
	again, err := svc.Delete(ctx, hhAda, userAda, noodles.ID)
	if err != nil || !again.UndoUntil.Equal(res.UndoUntil) || len(plans.users) != 1 {
		t.Errorf("second delete = %+v, %v (plan calls %d)", again, err, len(plans.users))
	}
}

func TestRestoreBringsTheRecipeAndItsMealsBack(t *testing.T) {
	ctx := context.Background()
	svc, _, plans, now := newDeleteService(t)
	mustImport(t, svc, hhAda, testFile(testRecipe("hf-1", "Garlic Noodles")))
	r := onlyRecipe(t, svc, hhAda)
	meals := []RemovedPlanEntry{{Week: "2026-W47", Day: "wed", Servings: 2, Note: "double garlic", Origin: "autopilot"}}
	plans.planned[r.ID] = meals
	if _, err := svc.Delete(ctx, hhAda, userAda, r.ID); err != nil {
		t.Fatal(err)
	}
	back, err := svc.Restore(ctx, hhAda, userAda, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !back.DeletedAt.IsZero() || len(back.RemovedPlanEntries) != 0 {
		t.Errorf("restored = %+v", back)
	}
	if !slices.Equal(plans.restored, meals) {
		t.Errorf("restored meals = %+v; want %+v", plans.restored, meals)
	}
	if onlyRecipe(t, svc, hhAda).ID != r.ID {
		t.Error("restored recipe isn't listed")
	}
	// Restoring a recipe that isn't deleted changes nothing.
	if _, err := svc.Restore(ctx, hhAda, userAda, r.ID); err != nil || len(plans.restored) != 1 {
		t.Errorf("second restore: %v, %d meals", err, len(plans.restored))
	}

	// Past the window it stays deleted.
	if _, err := svc.Delete(ctx, hhAda, userAda, r.ID); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(RestoreWindow + time.Minute)
	if _, err := svc.Restore(ctx, hhAda, userAda, r.ID); err != ErrRestoreExpired {
		t.Errorf("late restore error = %v; want ErrRestoreExpired", err)
	}
}

func TestDeleteIsScopedToTheHousehold(t *testing.T) {
	ctx := context.Background()
	svc, _, _, _ := newDeleteService(t)
	mustImport(t, svc, hhAda, testFile(testRecipe("hf-1", "Garlic Noodles")))
	r := onlyRecipe(t, svc, hhAda)
	if _, err := svc.Delete(ctx, hhBob, userBob, r.ID); err != ErrNotFound {
		t.Errorf("other household's delete = %v; want ErrNotFound", err)
	}
	if _, err := svc.Restore(ctx, hhBob, userBob, r.ID); err != ErrNotFound {
		t.Errorf("other household's restore = %v; want ErrNotFound", err)
	}
}

func TestReimportKeepsADeletedRecipeDeleted(t *testing.T) {
	ctx := context.Background()
	svc, _, _, _ := newDeleteService(t)
	mustImport(t, svc, hhAda, testFile(testRecipe("hf-1", "Garlic Noodles", "2026-W40")))
	r := onlyRecipe(t, svc, hhAda)
	if _, err := svc.Delete(ctx, hhAda, userAda, r.ID); err != nil {
		t.Fatal(err)
	}
	mustImport(t, svc, hhAda, testFile(testRecipe("hf-1", "Garlic Noodles", "2026-W40", "2026-W46")))
	if page, _ := svc.List(ctx, hhAda, ListQuery{}); len(page.Items) != 0 {
		t.Fatalf("re-import brought it back: %+v", page.Items)
	}
	kept, err := svc.Get(ctx, hhAda, r.ID)
	if err != nil || kept.DeletedAt.IsZero() || kept.TimesOrdered != 2 {
		t.Errorf("after re-import = deleted %v, ordered %d, %v", kept.DeletedAt, kept.TimesOrdered, err)
	}
}

func TestDeletedRecipesAreNotSharedOrMarkedInLibrary(t *testing.T) {
	ctx := context.Background()
	store, publisher := newMemoryStore(), &fakePublisher{}
	svc := NewService(store).WithCatalog(publisher)
	mustImport(t, svc, hhAda, testFile(testRecipe("hf-1", "Garlic Noodles")))
	r := onlyRecipe(t, svc, hhAda)
	key := CatalogKey(r)
	if _, err := svc.Share(ctx, hhAda, r.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Delete(ctx, hhAda, userAda, r.ID); err != nil {
		t.Fatal(err)
	}
	deleted, _ := svc.Get(ctx, hhAda, r.ID)
	if deleted.SharedToCatalog || svc.InCatalog(ctx, deleted) {
		t.Error("a deleted recipe is still shared")
	}
	before := len(publisher.published)
	if _, err := svc.PublishToCatalog(ctx, []Recipe{deleted}); err != nil || len(publisher.published) != before {
		t.Errorf("published a deleted recipe: %v", err)
	}
	if res, err := svc.Backfill(ctx, hhAda, true); err != nil || res.Read != 0 {
		t.Errorf("backfill read %d deleted recipes: %v", res.Read, err)
	}
	marks, err := svc.InLibrary(ctx, hhAda, []string{key})
	if err != nil || len(marks) != 0 {
		t.Errorf("in library = %v, %v", marks, err)
	}
	if _, err := svc.FindIngredientUse(ctx, hhAda, []string{r.Ingredients[0].IngredientID}); err != nil {
		t.Fatal(err)
	} else if uses, _ := svc.FindIngredientUse(ctx, hhAda, []string{r.Ingredients[0].IngredientID}); len(uses) != 0 {
		t.Errorf("ingredient use lists a deleted recipe: %+v", uses)
	}

	// Adding it again from Shared Recipes brings the household's own back.
	back, created, err := svc.AddFromCatalog(ctx, hhAda, catalogContent(deleted))
	if err != nil || !created || back.ID != r.ID || !back.DeletedAt.IsZero() {
		t.Errorf("add again = %s created %v deleted %v, %v", back.ID, created, back.DeletedAt, err)
	}
}

func TestStarterCopyLeavesOutDeletedRecipes(t *testing.T) {
	ctx := context.Background()
	svc, _, _, _ := newDeleteService(t)
	mustImport(t, svc, hhAda, testFile(testRecipe("hf-1", "Garlic Noodles"), testRecipe("hf-2", "Tacos")))
	if _, err := svc.Delete(ctx, hhAda, userAda, recipeByName(t, svc, hhAda, "Tacos").ID); err != nil {
		t.Fatal(err)
	}
	res, err := svc.CopyStarterRecipes(ctx, hhAda, hhBob, true)
	if err != nil {
		t.Fatal(err)
	}
	page, _ := svc.List(ctx, hhBob, ListQuery{})
	if len(page.Items) != 1 || page.Items[0].Name != "Garlic Noodles" {
		t.Errorf("starter copy = %+v (%+v)", page.Items, res)
	}
}

func TestDeleteAndRestoreRoutes(t *testing.T) {
	plans := &fakePlans{planned: map[string][]RemovedPlanEntry{}}
	srv := newRecipeTestServer(t, func(o *HandlerOptions) { o.Service.WithPlans(plans) })
	created := decodeBody[RecipeResponse](t, srv.do(t, http.MethodPost, recipesPath(hhAda), mustJSON(t, DraftResponse{
		Name: "No Bake Cookies", Ingredients: []DraftIngredientWire{{Name: "oats", Quantity: "3", Unit: "cup"}}, Steps: []string{"Stir."},
	}), userAda))
	path := recipesPath(hhAda) + "/" + created.ID
	plans.planned[created.ID] = []RemovedPlanEntry{{Week: "2026-W47", Day: "fri", Servings: 2}}

	if rec := srv.do(t, http.MethodDelete, path, "", userViewer); rec.Code != http.StatusForbidden {
		t.Errorf("viewer delete = %d", rec.Code)
	}
	if rec := srv.do(t, http.MethodDelete, path, "", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("signed-out delete = %d", rec.Code)
	}
	rec := srv.do(t, http.MethodDelete, path, "", userAda)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete = %d %s", rec.Code, rec.Body.String())
	}
	body := decodeBody[DeleteRecipeResponse](t, rec)
	if !slices.Equal(body.RemovedFromWeeks, []string{"2026-W47"}) || body.UndoUntil.IsZero() {
		t.Errorf("delete body = %s", rec.Body.String())
	}

	list := decodeBody[struct {
		Items []struct{ ID string } `json:"items"`
	}](t, srv.do(t, http.MethodGet, recipesPath(hhAda), "", userAda))
	if len(list.Items) != 0 {
		t.Errorf("list after delete = %+v", list.Items)
	}
	got := decodeBody[RecipeResponse](t, srv.do(t, http.MethodGet, path, "", userAda))
	if !got.Deleted || got.InCatalog {
		t.Errorf("get deleted = deleted %v, inCatalog %v", got.Deleted, got.InCatalog)
	}

	rec = srv.do(t, http.MethodPost, path+"/restore", "{}", userAda)
	if rec.Code != http.StatusOK {
		t.Fatalf("restore = %d %s", rec.Code, rec.Body.String())
	}
	if back := decodeBody[RecipeResponse](t, rec); back.Deleted || back.ID != created.ID || back.Name != "No Bake Cookies" {
		t.Errorf("restore body = %s", rec.Body.String())
	}
	if len(plans.restored) != 1 {
		t.Errorf("restored meals = %+v", plans.restored)
	}

	// Nothing planned answers an empty list, not null.
	rec = srv.do(t, http.MethodDelete, path, "", userAda)
	if !strings.Contains(rec.Body.String(), `"removedFromWeeks":[]`) {
		t.Errorf("unplanned delete body = %s", rec.Body.String())
	}

	other := recipesPath(hhBob) + "/" + created.ID
	rec = srv.do(t, http.MethodDelete, other, "", userBob)
	wantError(t, rec, http.StatusNotFound, "not_found")
	if !strings.Contains(rec.Body.String(), "That recipe isn't in your recipes.") {
		t.Errorf("other household body = %s", rec.Body.String())
	}
}

func TestRestoreRouteAfterTheWindow(t *testing.T) {
	srv := newRecipeTestServer(t, nil)
	created := decodeBody[RecipeResponse](t, srv.do(t, http.MethodPost, recipesPath(hhAda), mustJSON(t, DraftResponse{
		Name: "Chili", Ingredients: []DraftIngredientWire{{Name: "beans", Quantity: "2", Unit: "can"}}, Steps: []string{"Simmer."},
	}), userAda))
	path := recipesPath(hhAda) + "/" + created.ID
	if rec := srv.do(t, http.MethodDelete, path, "", userAda); rec.Code != http.StatusOK {
		t.Fatalf("delete = %d", rec.Code)
	}
	if _, err := srv.store.SetRecipeDeleted(context.Background(), hhAda, created.ID, time.Now().Add(-RestoreWindow-time.Hour), nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	rec := srv.do(t, http.MethodPost, path+"/restore", "{}", userAda)
	wantError(t, rec, http.StatusGone, "restore_expired")
	if !strings.Contains(rec.Body.String(), "It's too late to bring that recipe back.") {
		t.Errorf("body = %s", rec.Body.String())
	}
}

func recipeByName(t *testing.T, svc *Service, householdID, name string) Recipe {
	t.Helper()
	page, err := svc.List(context.Background(), householdID, ListQuery{})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range page.Items {
		if s.Name == name {
			r, err := svc.Get(context.Background(), householdID, s.ID)
			if err != nil {
				t.Fatal(err)
			}
			return r
		}
	}
	t.Fatalf("no recipe %q", name)
	return Recipe{}
}

// catalogContent is a recipe as the catalog holds it (catalog.Content).
func catalogContent(r Recipe) Recipe {
	r.ID, r.HouseholdID, r.CatalogKey = "", "", ""
	r.OrderWeeks, r.TimesOrdered, r.LastOrderedWeek, r.SharedToCatalog = nil, 0, "", false
	r.DeletedAt, r.RemovedPlanEntries = time.Time{}, nil
	return r
}

func names(rs []Recipe) []string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.Name)
	}
	return out
}
