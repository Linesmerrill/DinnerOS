package planning

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

type fakeOutcomes struct {
	list []events.Event
	err  error
	got  events.Query
}

func (f *fakeOutcomes) List(_ context.Context, q events.Query) ([]events.Event, error) {
	f.got = q
	return f.list, f.err
}

// One member answers "did you make it?" and every member's plan says so: the
// newest answer per entry, from the household's events.
func TestPlanCarriesEachMealsOutcome(t *testing.T) {
	outcomes := &fakeOutcomes{}
	svc, _ := newTestService(t, newMemoryStore())
	router := chi.NewRouter()
	router.Use(httpx.LimitBody(1 << 20))
	router.Route("/api/v1", NewHandler(HandlerOptions{
		Service: svc, Authorizer: testAuthorizer, Tokens: fakeTokens{}, Outcomes: outcomes,
	}).Mount)
	plan := plansPath(hhAda) + "/" + testWeek
	tacos := decodeBody[AddEntryResponse](t, do(t, router, http.MethodPost, plan+"/entries", `{"recipeId":"`+recipeTacos+`","day":"tue","servings":2}`, userAda)).Entry
	soup := decodeBody[AddEntryResponse](t, do(t, router, http.MethodPost, plan+"/entries", `{"recipeId":"`+recipeSoup+`","day":"wed","servings":2}`, userAda)).Entry
	salad := decodeBody[AddEntryResponse](t, do(t, router, http.MethodPost, plan+"/entries", `{"recipeId":"`+recipeSalad+`","day":"thu","servings":2}`, userAda)).Entry

	at := time.Date(2026, 9, 15, 19, 0, 0, 0, time.UTC)
	// Newest first, as asked: the later skip of the soup wins over an
	// earlier cook, and an event for another plan's entry is ignored.
	outcomes.list = []events.Event{
		{UserID: userAda, Type: events.TypeRecipeSkipped, OccurredAt: at.Add(2 * time.Hour), Payload: events.RecipeSkipped{EntryID: soup.ID, Reason: "ate-out"}},
		{UserID: userAda, Type: events.TypeRecipeCooked, OccurredAt: at.Add(time.Hour), Payload: events.RecipeCooked{EntryID: tacos.ID}},
		{UserID: userAda, Type: events.TypeRecipeCooked, OccurredAt: at, Payload: events.RecipeCooked{EntryID: soup.ID}},
		{UserID: userAda, Type: events.TypeRecipeCooked, OccurredAt: at, Payload: events.RecipeCooked{EntryID: "elsewhere"}},
	}
	got := decodeBody[PlanResponse](t, do(t, router, http.MethodGet, plan, "", userViewer))
	byID := map[string]*EntryOutcomeResponse{}
	for _, e := range got.Entries {
		byID[e.ID] = e.Outcome
	}
	if o := byID[tacos.ID]; o == nil || o.Kind != OutcomeCooked || o.By != userAda || !o.At.Equal(at.Add(time.Hour)) {
		t.Errorf("tacos outcome = %+v", o)
	}
	if o := byID[soup.ID]; o == nil || o.Kind != OutcomeSkipped || o.Reason != "ate-out" {
		t.Errorf("soup outcome = %+v", o)
	}
	if byID[salad.ID] != nil {
		t.Errorf("salad outcome = %+v, want none", byID[salad.ID])
	}
	if outcomes.got.HouseholdID != hhAda || !outcomes.got.Newest || len(outcomes.got.Types) != 2 || outcomes.got.Since.IsZero() {
		t.Errorf("query = %+v", outcomes.got)
	}

	// A failed lookup still returns the plan, without answers.
	outcomes.err = errors.New("down")
	rec := do(t, router, http.MethodGet, plan, "", userViewer)
	wantStatus(t, rec, http.StatusOK)
	for _, e := range decodeBody[PlanResponse](t, rec).Entries {
		if e.Outcome != nil {
			t.Errorf("outcome after a failed lookup = %+v", e.Outcome)
		}
	}
}
