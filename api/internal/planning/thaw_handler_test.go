package planning

import (
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

func TestThawEndpoint(t *testing.T) {
	svc, _ := thawService(t, ptr(6), frozenChicken())
	mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeChicken, Day: "tue", Servings: 2})
	r := chi.NewRouter()
	r.Use(httpx.LimitBody(1 << 20))
	r.Route("/api/v1", NewHandler(HandlerOptions{Service: svc, Authorizer: testAuthorizer, Tokens: fakeTokens{}}).Mount)

	path := "/api/v1/households/" + hhAda + "/thaw"
	rec := do(t, r, http.MethodGet, path, "", userAda)
	wantStatus(t, rec, http.StatusOK)
	resp := decodeBody[ThawDueResponse](t, rec)
	switch {
	case resp.Date != thawDate:
		t.Errorf("date = %q, want %q", resp.Date, thawDate)
	case resp.ReminderHour != 6:
		t.Errorf("reminderHour = %d, want 6", resp.ReminderHour)
	case len(resp.Items) != 1:
		t.Fatalf("%d items, want the chicken", len(resp.Items))
	case resp.Items[0].Hours != 5:
		t.Errorf("hours = %d, want 5", resp.Items[0].Hours)
	case resp.Items[0].MoveBy != "13:00":
		t.Errorf("moveBy = %q, want 13:00", resp.Items[0].MoveBy)
	case len(resp.Items[0].Recipes) != 1:
		t.Errorf("recipes = %v, want the meal that needs it", resp.Items[0].Recipes)
	}

	// A viewer may read it; someone from another household may not.
	wantStatus(t, do(t, r, http.MethodGet, path, "", userViewer), http.StatusOK)
	wantError(t, do(t, r, http.MethodGet, path, "", userBob), http.StatusNotFound, "not_found")
}

// Without a freezer wired in, the endpoint answers "nothing to thaw" rather
// than failing: the rest of planning works with no pantry at all.
func TestThawEndpointWithoutAFreezer(t *testing.T) {
	svc, _ := newTestService(t, newMemoryStore())
	r := chi.NewRouter()
	r.Route("/api/v1", NewHandler(HandlerOptions{Service: svc, Authorizer: testAuthorizer, Tokens: fakeTokens{}}).Mount)
	rec := do(t, r, http.MethodGet, "/api/v1/households/"+hhAda+"/thaw", "", userAda)
	wantStatus(t, rec, http.StatusOK)
	if items := decodeBody[ThawDueResponse](t, rec).Items; len(items) != 0 {
		t.Errorf("items = %v, want none", items)
	}
}
