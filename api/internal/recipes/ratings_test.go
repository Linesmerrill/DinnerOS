package recipes

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
	"github.com/Linesmerrill/DinnerOS/api/internal/ratings"
)

// fakeRatingReader returns fixed summaries and remembers what it was asked.
type fakeRatingReader struct {
	summaries map[string]ratings.Summary
	err       error

	householdID, userID string
	recipeIDs           []string
}

func (f *fakeRatingReader) Summaries(_ context.Context, householdID, userID string, recipeIDs []string) (map[string]ratings.Summary, error) {
	f.householdID, f.userID, f.recipeIDs = householdID, userID, slices.Clone(recipeIDs)
	return f.summaries, f.err
}

type fakeRecorder struct {
	mu     sync.Mutex
	events []events.Event
	err    error
}

func (f *fakeRecorder) Record(_ context.Context, e events.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.events = append(f.events, e)
	return nil
}

func importTwo(t *testing.T, srv *recipeTestServer) (tacosID, cakeID string) {
	t.Helper()
	rec := srv.do(t, http.MethodPost, recipesPath(hhAda)+"/import", mustJSON(t, handlerFixture()), userAda)
	if rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, body %s", rec.Code, rec.Body.String())
	}
	list := decodeBody[RecipeListResponse](t, srv.do(t, http.MethodGet, recipesPath(hhAda), "", userAda))
	return list.Items[0].ID, list.Items[1].ID // Beef Tacos, Lava Cake
}

func TestRecipeResponsesIncludeRatings(t *testing.T) {
	reader := &fakeRatingReader{}
	srv := newRecipeTestServer(t, func(o *HandlerOptions) { o.Ratings = reader })
	tacosID, cakeID := importTwo(t, srv)
	updated := time.Date(2026, 9, 15, 18, 0, 0, 0, time.UTC)
	reader.summaries = map[string]ratings.Summary{
		tacosID: {Count: 2, Sum: 9, Mine: &ratings.Rating{RecipeID: tacosID, UserID: userViewer, Score: 5, Tags: []ratings.Tag{ratings.TagKidFavorite}, CreatedAt: updated, UpdatedAt: updated}},
	}

	rec := srv.do(t, http.MethodGet, recipesPath(hhAda), "", userViewer)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, body %s", rec.Code, rec.Body.String())
	}
	if reader.householdID != hhAda || reader.userID != userViewer || !slices.Equal(reader.recipeIDs, []string{tacosID, cakeID}) {
		t.Errorf("reader asked for household %q user %q recipes %v; want the page for the caller", reader.householdID, reader.userID, reader.recipeIDs)
	}
	list := decodeBody[RecipeListResponse](t, rec)
	tacos, cake := list.Items[0], list.Items[1]
	if tacos.HouseholdRating.Count != 2 || tacos.HouseholdRating.Average == nil || *tacos.HouseholdRating.Average != 4.5 ||
		tacos.MyRating == nil || tacos.MyRating.Score != 5 || !slices.Equal(tacos.MyRating.Tags, []string{"kid-favorite"}) {
		t.Errorf("tacos = %+v", tacos)
	}
	if cake.HouseholdRating.Count != 0 || cake.HouseholdRating.Average != nil || cake.MyRating != nil {
		t.Errorf("unrated cake = %+v", cake)
	}
	if !strings.Contains(rec.Body.String(), `"householdRating":{"average":null,"count":0},"myRating":null`) {
		t.Errorf("unrated recipe must serialize null average and myRating: %s", rec.Body.String())
	}

	rec = srv.do(t, http.MethodGet, recipesPath(hhAda)+"/"+tacosID, "", userViewer)
	got := decodeBody[RecipeResponse](t, rec)
	if got.HouseholdRating.Count != 2 || got.MyRating == nil || got.MyRating.UserID != userViewer || !slices.Equal(reader.recipeIDs, []string{tacosID}) {
		t.Errorf("detail = %+v (reader ids %v)", got, reader.recipeIDs)
	}

	reader.err = errors.New("ratings down")
	wantError(t, srv.do(t, http.MethodGet, recipesPath(hhAda), "", userViewer), 500, "internal")
	wantError(t, srv.do(t, http.MethodGet, recipesPath(hhAda)+"/"+tacosID, "", userViewer), 500, "internal")
}

func TestRecipeResponsesWithoutRatingReader(t *testing.T) {
	srv := newRecipeTestServer(t, nil)
	tacosID, _ := importTwo(t, srv)
	body := srv.do(t, http.MethodGet, recipesPath(hhAda)+"/"+tacosID, "", userAda).Body.String()
	if !strings.Contains(body, `"householdRating":{"average":null,"count":0},"myRating":null`) {
		t.Errorf("detail without ratings = %s", body)
	}
}

func TestImportRecordsEvent(t *testing.T) {
	recorder := &fakeRecorder{}
	srv := newRecipeTestServer(t, func(o *HandlerOptions) { o.Events = recorder })
	importTwo(t, srv)
	srv.do(t, http.MethodPost, recipesPath(hhAda)+"/import", mustJSON(t, handlerFixture()), userAda)

	if len(recorder.events) != 2 {
		t.Fatalf("recorded %d events, want one per import", len(recorder.events))
	}
	e := recorder.events[1]
	if e.Type != events.TypeImportCompleted || e.HouseholdID != hhAda || e.UserID != userAda || e.RecipeID != "" ||
		e.Payload != (events.ImportCompleted{Source: SourceHelloFresh, Unchanged: 2}) {
		t.Errorf("event = %+v", e)
	}

	// Invalid imports record nothing; recorder failures don't fail imports.
	srv.do(t, http.MethodPost, recipesPath(hhAda)+"/import", `{"version":2,"source":"hellofresh","recipes":[]}`, userAda)
	recorder.err = errors.New("events down")
	if rec := srv.do(t, http.MethodPost, recipesPath(hhAda)+"/import", mustJSON(t, handlerFixture()), userAda); rec.Code != http.StatusOK {
		t.Errorf("import with failing recorder = %d, want 200", rec.Code)
	}
	if len(recorder.events) != 2 {
		t.Errorf("recorded %d events, want 2", len(recorder.events))
	}
}

// TestRatingRoutesCoexistWithRecipeRoutes mounts both handlers on one /api/v1
// router, as cmd/server does, so conflicting chi patterns would panic here.
func TestRatingRoutesCoexistWithRecipeRoutes(t *testing.T) {
	store := newMemoryStore()
	r := chi.NewRouter()
	r.Use(httpx.LimitBody(testGlobalBodyLimit))
	r.Route("/api/v1", func(r chi.Router) {
		NewHandler(HandlerOptions{Service: NewService(store), Authorizer: testAuthorizer, Tokens: fakeTokens{}}).Mount(r)
		ratings.NewHandler(ratings.HandlerOptions{Authorizer: testAuthorizer, Tokens: fakeTokens{}}).Mount(r)
	})
	srv := &recipeTestServer{router: r, store: store}

	tacosID, _ := importTwo(t, srv)
	wantError(t, srv.do(t, http.MethodPut, recipesPath(hhAda)+"/"+tacosID+"/rating", `{"score":4}`, ""), 401, "unauthenticated")
	wantError(t, srv.do(t, http.MethodGet, recipesPath(hhAda)+"/"+tacosID+"/ratings", "", userBob), 404, "not_found")
	if rec := srv.do(t, http.MethodGet, recipesPath(hhAda)+"/"+tacosID, "", userAda); rec.Code != http.StatusOK {
		t.Errorf("recipe detail with ratings mounted = %d, body %s", rec.Code, rec.Body.String())
	}
}
