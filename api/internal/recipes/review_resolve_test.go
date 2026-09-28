package recipes

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"
)

// reviewFixture is one stored recipe with two flagged deliveries (a protein
// swap and a respelling), plus a recipe with no steps. Synthetic names only.
func reviewFixture() ImportFile {
	bowls := testRecipe("canon-bowls", "Turkey Fajita Bowls", "2026-W10", "2026-W12")
	bowls.SourceAliases = []string{"clone-bowls"}
	bowls.ImageURL = "https://img.example.com/bowls.jpg"
	cake := testRecipe("r-cake", "Lava Cake")
	file := testFile(bowls, cake)
	file.Review = []ImportReviewItem{
		{SourceRecipeID: "clone-bowls", RecipeName: "Turkey Fajita Bowls", Field: ReviewFieldVariant, Value: "pork fajita bowls", Reason: "delivered menu variant differs"},
		{SourceRecipeID: "canon-bowls", RecipeName: "Turkey Fajita Bowls", Field: ReviewFieldVariant, Value: "turkey fajita bowls", Reason: "delivered menu variant differs"},
		{SourceRecipeID: "r-cake", RecipeName: "Lava Cake", Field: "steps", Reason: "recipe has no steps"},
	}
	return file
}

// openByField lists the household's open items keyed by field and value.
func openByField(t *testing.T, svc *Service, householdID string) map[string]ReviewRecord {
	t.Helper()
	items, err := svc.ImportReviews(context.Background(), householdID, ReviewStatusOpen, 0)
	if err != nil {
		t.Fatalf("ImportReviews() error = %v", err)
	}
	out := map[string]ReviewRecord{}
	for _, it := range items {
		out[it.Field+"|"+it.Value] = it
	}
	return out
}

// runResolveContract is shared by the memory and Mongo stores.
func runResolveContract(t *testing.T, svc *Service, householdID, otherHousehold string) {
	t.Helper()
	ctx := context.Background()
	mustImport(t, svc, householdID, reviewFixture())
	mustImport(t, svc, otherHousehold, reviewFixture())

	open := openByField(t, svc, householdID)
	if len(open) != 3 {
		t.Fatalf("open items = %d, want 3", len(open))
	}
	swap, spelling, steps := open["variant|pork fajita bowls"], open["variant|turkey fajita bowls"], open["steps|"]
	for _, it := range []ReviewRecord{swap, spelling, steps} {
		if it.ID == "" {
			t.Fatalf("item %+v has no id", it.ReviewItem)
		}
	}
	if swap.RecipeImageURL != "https://img.example.com/bowls.jpg" {
		t.Errorf("recipeImageUrl = %q, want the stored recipe's photo", swap.RecipeImageURL)
	}

	// Same recipe does not apply to a steps item: it stays open, uncounted.
	n, err := svc.ResolveImportReviews(ctx, householdID, "user-1", ResolveReviews{IDs: []string{swap.ID, steps.ID}, Resolution: ResolutionSameRecipe})
	if err != nil || n != 1 {
		t.Fatalf("resolve same_recipe = %d, %v, want 1", n, err)
	}
	open = openByField(t, svc, householdID)
	if _, still := open["variant|pork fajita bowls"]; still {
		t.Error("the resolved item is still open")
	}
	if _, ok := open["steps|"]; !ok {
		t.Error("same_recipe closed a steps item")
	}
	if len(open) != 2 {
		t.Errorf("open items = %d, want 2", len(open))
	}

	// Resolving again changes nothing and is not an error.
	if n, err := svc.ResolveImportReviews(ctx, householdID, "user-1", ResolveReviews{IDs: []string{swap.ID}, Resolution: ResolutionDismissed}); err != nil || n != 0 {
		t.Errorf("resolve twice = %d, %v, want 0, nil", n, err)
	}

	// Dismiss works on any item.
	if n, err := svc.ResolveImportReviews(ctx, householdID, "user-1", ResolveReviews{IDs: []string{steps.ID}, Resolution: ResolutionDismissed}); err != nil || n != 1 {
		t.Errorf("dismiss = %d, %v, want 1", n, err)
	}

	// The resolved list carries the decision.
	resolved, err := svc.ImportReviews(ctx, householdID, ReviewStatusResolved, 0)
	if err != nil || len(resolved) != 2 {
		t.Fatalf("resolved items = %d, %v, want 2", len(resolved), err)
	}
	for _, it := range resolved {
		if it.Status != ReviewStatusResolved || it.Resolved == nil || it.Resolved.ResolvedBy != "user-1" || it.Resolved.ResolvedAt.IsZero() {
			t.Errorf("resolved item = %+v, %+v", it.ReviewItem, it.Resolved)
			continue
		}
		want := ResolutionDismissed
		if it.Field == ReviewFieldVariant {
			want = ResolutionSameRecipe
		}
		if it.Resolved.Resolution != want {
			t.Errorf("%s resolution = %q, want %q", it.Field, it.Resolved.Resolution, want)
		}
	}

	// A different recipe may link the recipe the member added for it.
	cakeID := ""
	for _, r := range mustList(t, svc, householdID) {
		if r.Name == "Lava Cake" {
			cakeID = r.ID
		}
	}
	if n, err := svc.ResolveImportReviews(ctx, householdID, "user-1", ResolveReviews{IDs: []string{spelling.ID}, Resolution: ResolutionDifferentRecipe, LinkedRecipeID: cakeID}); err != nil || n != 1 {
		t.Fatalf("different_recipe = %d, %v, want 1", n, err)
	}
	if len(openByField(t, svc, householdID)) != 0 {
		t.Error("items still open after every one was resolved")
	}

	// A re-import that flags the same things never reopens them.
	mustImport(t, svc, householdID, reviewFixture())
	if open := openByField(t, svc, householdID); len(open) != 0 {
		t.Errorf("re-import reopened %d items", len(open))
	}

	// Another household's IDs are not touched from here, and its items are
	// still open.
	if other := openByField(t, svc, otherHousehold); len(other) != 3 {
		t.Errorf("other household open = %d, want 3", len(other))
	}
}

func mustList(t *testing.T, svc *Service, householdID string) []RecipeSummary {
	t.Helper()
	page, err := svc.List(context.Background(), householdID, ListQuery{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	return page.Items
}

func TestResolveImportReviews(t *testing.T) {
	svc := NewService(newMemoryStore())
	svc.now = func() time.Time { return time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC) }
	runResolveContract(t, svc, hhAda, hhBob)
}

func TestResolveImportReviewsRejectsUnusableRequests(t *testing.T) {
	svc := NewService(newMemoryStore())
	mustImport(t, svc, hhAda, reviewFixture())
	ctx := context.Background()
	tooMany := make([]string, MaxReviewLimit+1)
	for i := range tooMany {
		tooMany[i] = "id-" + strconv.Itoa(i)
	}
	for name, in := range map[string]ResolveReviews{
		"no ids":             {Resolution: ResolutionDismissed},
		"blank ids":          {IDs: []string{" ", ""}, Resolution: ResolutionDismissed},
		"unknown resolution": {IDs: []string{"x"}, Resolution: "fixed"},
		"too many":           {IDs: tooMany, Resolution: ResolutionDismissed},
		"link without split": {IDs: []string{"x"}, Resolution: ResolutionSameRecipe, LinkedRecipeID: "abc"},
	} {
		if _, err := svc.ResolveImportReviews(ctx, hhAda, "u", in); !errors.Is(err, ErrInvalidResolution) {
			t.Errorf("%s: err = %v, want ErrInvalidResolution", name, err)
		}
	}
	// A linked recipe must be the household's.
	if _, err := svc.ResolveImportReviews(ctx, hhAda, "u", ResolveReviews{IDs: []string{"x"}, Resolution: ResolutionDifferentRecipe, LinkedRecipeID: "ffffffffffffffffffffffff"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown linked recipe: err = %v, want ErrNotFound", err)
	}
}

func TestResolveImportReviewsEndpoint(t *testing.T) {
	srv := newRecipeTestServer(t, nil)
	base := recipesPath(hhAda) + "/import-reviews"
	if rec := srv.do(t, http.MethodPost, recipesPath(hhAda)+"/import", mustJSON(t, reviewFixture()), userAda); rec.Code != http.StatusOK {
		t.Fatalf("import status = %d (%s)", rec.Code, rec.Body.String())
	}
	list := decodeBody[ImportReviewListResponse](t, srv.do(t, http.MethodGet, base, "", userAda))
	if len(list.Items) != 3 {
		t.Fatalf("items = %d, want 3", len(list.Items))
	}
	var variantIDs []string
	for _, it := range list.Items {
		if it.ID == "" {
			t.Fatalf("item without id: %+v", it)
		}
		if it.Field == ReviewFieldVariant {
			variantIDs = append(variantIDs, it.ID)
			if it.RecipeImageURL == "" {
				t.Errorf("variant item has no recipeImageUrl")
			}
		}
	}

	// Permission: viewing the household is not enough.
	body := mustJSON(t, ResolveImportReviewsRequest{IDs: variantIDs, Resolution: string(ResolutionSameRecipe)})
	if rec := srv.do(t, http.MethodPost, base+"/resolve", body, userViewer); rec.Code != http.StatusForbidden {
		t.Errorf("viewer status = %d, want 403", rec.Code)
	}
	// Another household's member resolves nothing here.
	if rec := srv.do(t, http.MethodPost, recipesPath(hhBob)+"/import-reviews/resolve", body, userBob); rec.Code != http.StatusOK ||
		decodeBody[ResolveImportReviewsResponse](t, rec).Resolved != 0 {
		t.Errorf("bob resolved hhAda items: %d %s", rec.Code, rec.Body.String())
	}

	// Bulk same recipe: both variant items, in one call.
	rec := srv.do(t, http.MethodPost, base+"/resolve", body, userAda)
	if rec.Code != http.StatusOK {
		t.Fatalf("resolve status = %d (%s)", rec.Code, rec.Body.String())
	}
	if got := decodeBody[ResolveImportReviewsResponse](t, rec).Resolved; got != 2 {
		t.Errorf("resolved = %d, want 2", got)
	}

	// The open list now holds exactly what is left, and its count is the
	// count a badge shows.
	open := decodeBody[ImportReviewListResponse](t, srv.do(t, http.MethodGet, base, "", userAda))
	if len(open.Items) != 1 || open.Items[0].Field != "steps" {
		t.Fatalf("open after resolve = %+v", open.Items)
	}
	resolved := decodeBody[ImportReviewListResponse](t, srv.do(t, http.MethodGet, base+"?status=resolved", "", userAda))
	if len(resolved.Items) != 2 {
		t.Fatalf("resolved list = %d, want 2", len(resolved.Items))
	}
	for _, it := range resolved.Items {
		if it.Status != ReviewStatusResolved || it.Resolution != string(ResolutionSameRecipe) || it.ResolvedAt == nil {
			t.Errorf("resolved item = %+v", it)
		}
	}
	if all := decodeBody[ImportReviewListResponse](t, srv.do(t, http.MethodGet, base+"?status=all", "", userAda)); len(all.Items) != 3 {
		t.Errorf("status=all = %d, want 3", len(all.Items))
	}

	// Bad requests.
	wantError(t, srv.do(t, http.MethodPost, base+"/resolve", `{"ids":[],"resolution":"dismissed"}`, userAda), http.StatusBadRequest, "validation_failed")
	wantError(t, srv.do(t, http.MethodPost, base+"/resolve", `{"ids":["x"],"resolution":"maybe"}`, userAda), http.StatusBadRequest, "validation_failed")
	wantError(t, srv.do(t, http.MethodPost, base+"/resolve",
		`{"ids":["x"],"resolution":"different_recipe","recipeId":"ffffffffffffffffffffffff"}`, userAda), http.StatusNotFound, "not_found")
}
