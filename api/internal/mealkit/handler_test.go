package mealkit

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

type handlerFixture struct {
	router  chi.Router
	store   *memoryStore
	source  *fakeSource
	service *Service
}

func newHandlerFixture(t *testing.T, enabled bool) *handlerFixture {
	t.Helper()
	f := &handlerFixture{
		store:  &memoryStore{},
		source: &fakeSource{},
	}
	f.service = NewService(ServiceOptions{
		Store: f.store, Enabled: enabled, Sources: map[string]Source{SourceHelloFresh: f.source},
	})
	r := chi.NewRouter()
	r.Route("/api/v1", NewHandler(HandlerOptions{
		Service: f.service, Authorizer: testAuthorizer, Tokens: fakeTokens{},
	}).Mount)
	f.router = r
	return f
}

func (f *handlerFixture) do(method, path, body, userID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if userID != "" {
		req.Header.Set("Authorization", "Bearer token-"+userID)
	}
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return rec
}

const statusPath = "/api/v1/households/" + hhAda + "/meal-kit/hellofresh"
const importsPath = statusPath + "/imports"

// harvest is a body shaped like the one the app sends after reading the
// member's order history in their own browser session.
const harvest = `{"recipes":[
	{"sourceRecipeId":"recipe-1","name":"Sheet Pan Chicken","url":"https://example.test/recipes/recipe-1","weeks":["2026-W38"]},
	{"sourceRecipeId":"recipe-2","name":"Garlic Bread","isAddon":true}
]}`

func TestMealKitRoutesRequireTheImportPermission(t *testing.T) {
	f := newHandlerFixture(t, true)
	if rec := f.do(http.MethodGet, statusPath, "", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated = %d", rec.Code)
	}
	// Bob may view his own household but not import anywhere.
	if rec := f.do(http.MethodGet, statusPath, "", userBob); rec.Code != http.StatusForbidden && rec.Code != http.StatusNotFound {
		t.Errorf("another household's member = %d %s", rec.Code, rec.Body)
	}
	bobPath := "/api/v1/households/" + hhBob + "/meal-kit/hellofresh"
	if rec := f.do(http.MethodGet, bobPath, "", userBob); rec.Code != http.StatusForbidden {
		t.Errorf("member without recipes.import = %d %s", rec.Code, rec.Body)
	}
}

func TestStartingAnImportQueuesTheHarvestedHistoryAndStatusReportsIt(t *testing.T) {
	f := newHandlerFixture(t, true)

	rec := f.do(http.MethodPost, importsPath, harvest, userAda)
	var job JobResponse
	if rec.Code != http.StatusAccepted || json.Unmarshal(rec.Body.Bytes(), &job) != nil {
		t.Fatalf("start = %d %s", rec.Code, rec.Body)
	}
	if job.Status != string(JobQueued) || job.RecipesFound != 2 || job.Phase != string(PhaseRecipes) {
		t.Fatalf("job = %+v", job)
	}
	if job.Failures == nil {
		t.Error("failures should be an empty array, not null")
	}

	rec = f.do(http.MethodGet, statusPath, "", userAda)
	var status StatusResponse
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &status) != nil {
		t.Fatalf("status = %d %s", rec.Code, rec.Body)
	}
	if !status.Enabled || status.LatestJob == nil || status.LatestJob.ID != job.ID {
		t.Fatalf("status = %+v", status)
	}

	// Starting again returns the run that is already queued rather than a
	// second one, so a member tapping twice never doubles the work.
	rec = f.do(http.MethodPost, importsPath, harvest, userAda)
	var again JobResponse
	if rec.Code != http.StatusAccepted || json.Unmarshal(rec.Body.Bytes(), &again) != nil || again.ID != job.ID {
		t.Fatalf("second start = %d %s", rec.Code, rec.Body)
	}
	if len(f.store.jobs) != 1 {
		t.Errorf("jobs = %d, want the one", len(f.store.jobs))
	}

	rec = f.do(http.MethodGet, importsPath, "", userAda)
	var list JobListResponse
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &list) != nil || len(list.Items) != 1 {
		t.Fatalf("list = %d %s", rec.Code, rec.Body)
	}
}

// There is no link route and no credential field anywhere on this API. A body
// carrying one is refused rather than quietly ignored, so an app that tries to
// send a token finds out immediately.
func TestTheApiHasNowhereToPutACredential(t *testing.T) {
	f := newHandlerFixture(t, true)
	linkPath := statusPath + "/link"
	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		if rec := f.do(method, linkPath, `{"accessToken":"a"}`, userAda); rec.Code != http.StatusNotFound &&
			rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s = %d %s; the link route must be gone", method, linkPath, rec.Code, rec.Body)
		}
	}
	body := `{"recipes":[{"sourceRecipeId":"recipe-1"}],"accessToken":"a-token"}`
	rec := f.do(http.MethodPost, importsPath, body, userAda)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("a body carrying a token = %d %s", rec.Code, rec.Body)
	}
}

func TestStoppingImportsCancelsEveryRunAndIsRepeatable(t *testing.T) {
	f := newHandlerFixture(t, true)
	if rec := f.do(http.MethodPost, importsPath, harvest, userAda); rec.Code != http.StatusAccepted {
		t.Fatalf("start = %d %s", rec.Code, rec.Body)
	}
	for range 2 {
		if rec := f.do(http.MethodDelete, importsPath, "", userAda); rec.Code != http.StatusNoContent {
			t.Fatalf("stop = %d %s", rec.Code, rec.Body)
		}
	}
	if f.store.jobs[0].Status != JobCanceled {
		t.Errorf("job after stopping = %q", f.store.jobs[0].Status)
	}
}

func TestMealKitRoutesRefuseBadInputAndUnknownServices(t *testing.T) {
	f := newHandlerFixture(t, true)
	for name, body := range map[string]string{
		"no recipes":     `{"recipes":[]}`,
		"no body at all": `{}`,
		"nothing usable": `{"recipes":[{"name":"no id at all"}]}`,
	} {
		if rec := f.do(http.MethodPost, importsPath, body, userAda); rec.Code != http.StatusBadRequest {
			t.Errorf("%s = %d %s", name, rec.Code, rec.Body)
		}
	}
	unknown := "/api/v1/households/" + hhAda + "/meal-kit/blueapron"
	if rec := f.do(http.MethodGet, unknown, "", userAda); rec.Code != http.StatusNotFound {
		t.Errorf("unknown service = %d %s", rec.Code, rec.Body)
	}
}

func TestMealKitRoutesSayTheFeatureIsOffWhenItIs(t *testing.T) {
	f := newHandlerFixture(t, false)
	rec := f.do(http.MethodGet, statusPath, "", userAda)
	var status StatusResponse
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &status) != nil || status.Enabled {
		t.Fatalf("status = %d %s", rec.Code, rec.Body)
	}
	rec = f.do(http.MethodPost, importsPath, harvest, userAda)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "import_disabled") {
		t.Errorf("import with the feature off = %d %s", rec.Code, rec.Body)
	}
}
