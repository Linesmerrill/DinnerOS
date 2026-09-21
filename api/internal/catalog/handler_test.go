package catalog

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
)

const (
	userAda    = "ada"
	userViewer = "viewer"
	userBob    = "bob"
)

// fakeTokens accepts "token-<userID>".
type fakeTokens struct{}

func (fakeTokens) ValidateAccessToken(token string) (string, error) {
	id, ok := strings.CutPrefix(token, "token-")
	if !ok || id == "" {
		return "", households.ErrNotFound
	}
	return id, nil
}

// fakeAuthorizer maps householdID + "/" + userID to the permissions that
// member has, mirroring households.Service.Authorize.
type fakeAuthorizer map[string][]households.Permission

func (f fakeAuthorizer) Authorize(_ context.Context, householdID, userID string, perm households.Permission) (households.Membership, error) {
	perms, ok := f[householdID+"/"+userID]
	if !ok {
		return households.Membership{}, households.ErrNotFound
	}
	for _, p := range perms {
		if p == perm {
			return households.Membership{HouseholdID: householdID, UserID: userID}, nil
		}
	}
	return households.Membership{}, households.ErrForbidden
}

var testAuthorizer = fakeAuthorizer{
	hhAda + "/" + userAda:    {households.PermHouseholdView, households.PermRecipesEdit},
	hhAda + "/" + userViewer: {households.PermHouseholdView},
	hhBob + "/" + userBob:    {households.PermHouseholdView, households.PermRecipesEdit},
}

type testServer struct {
	router  *chi.Mux
	harness *harness
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()
	h := newHarness(t, nil)
	r := chi.NewRouter()
	r.Route("/api/v1", NewHandler(HandlerOptions{
		Service: h.service, Authorizer: testAuthorizer, Tokens: fakeTokens{},
	}).Mount)
	return &testServer{router: r, harness: h}
}

func (s *testServer) do(t *testing.T, method, path, userID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if userID != "" {
		req.Header.Set("Authorization", "Bearer token-"+userID)
	}
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
	return out
}

func TestDiscoverRequiresMembership(t *testing.T) {
	s := newTestServer(t)
	for _, tc := range []struct {
		name, user string
		want       int
	}{
		{"anonymous", "", http.StatusUnauthorized},
		{"another household's member", userBob, http.StatusNotFound},
		{"a viewer", userViewer, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := s.do(t, http.MethodGet, "/api/v1/households/"+hhAda+"/discover", tc.user)
			if rec.Code != tc.want {
				t.Fatalf("status = %d; want %d (%s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestDiscoverReturnsUnownedRecipes(t *testing.T) {
	s := newTestServer(t)
	publish(t, s.harness, "hf-1", "Alpha Bowl")
	publish(t, s.harness, "hf-2", "Beta Bowl")
	s.harness.library.have[hhAda] = map[string]string{"hellofresh:hf-2": "own-2"}

	rec := s.do(t, http.MethodGet, "/api/v1/households/"+hhAda+"/discover", userAda)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	resp := decode[CatalogListResponse](t, rec)
	if len(resp.Items) != 1 || resp.Items[0].Name != "Alpha Bowl" {
		t.Fatalf("items = %+v; want only Alpha Bowl", resp.Items)
	}
	if resp.Items[0].InLibrary {
		t.Error("a discover result is marked as already in the library")
	}
}

func TestCatalogSearchMarksTheLibrary(t *testing.T) {
	s := newTestServer(t)
	publish(t, s.harness, "hf-1", "Tikka Masala")
	s.harness.library.have[hhAda] = map[string]string{"hellofresh:hf-1": "own-1"}

	rec := s.do(t, http.MethodGet, "/api/v1/households/"+hhAda+"/catalog/recipes?q=tikka", userAda)
	resp := decode[CatalogListResponse](t, rec)
	if len(resp.Items) != 1 {
		t.Fatalf("items = %+v; want one hit", resp.Items)
	}
	if !resp.Items[0].InLibrary || resp.Items[0].LibraryRecipeID != "own-1" {
		t.Errorf("hit = %+v; want inLibrary with libraryRecipeId own-1", resp.Items[0])
	}
}

func TestCatalogSearchRejectsABadLimit(t *testing.T) {
	s := newTestServer(t)
	rec := s.do(t, http.MethodGet, "/api/v1/households/"+hhAda+"/catalog/recipes?limit=0", userAda)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d; want 400 (%s)", rec.Code, rec.Body.String())
	}
}

func TestCatalogDetailNeverCarriesHouseholdFields(t *testing.T) {
	s := newTestServer(t)
	id := publish(t, s.harness, "hf-1", "Shakshuka")

	rec := s.do(t, http.MethodGet, "/api/v1/households/"+hhAda+"/catalog/recipes/"+id, userAda)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, forbidden := range []string{"householdId", "timesOrdered", "lastOrderedWeek", "orderWeeks", "householdRating", "myRating"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("catalog detail exposes %q: %s", forbidden, body)
		}
	}
}

func TestCatalogDetailIsNotFoundForAnUnknownID(t *testing.T) {
	s := newTestServer(t)
	rec := s.do(t, http.MethodGet, "/api/v1/households/"+hhAda+"/catalog/recipes/66e5a1f2c3b4a5d6e7f8dead", userAda)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d; want 404 (%s)", rec.Code, rec.Body.String())
	}
}

func TestAddRequiresRecipesEdit(t *testing.T) {
	s := newTestServer(t)
	id := publish(t, s.harness, "hf-1", "Shakshuka")
	path := "/api/v1/households/" + hhAda + "/catalog/recipes/" + id + "/add"

	if rec := s.do(t, http.MethodPost, path, userViewer); rec.Code != http.StatusForbidden {
		t.Fatalf("a viewer got %d; want 403 (%s)", rec.Code, rec.Body.String())
	}
	rec := s.do(t, http.MethodPost, path, userAda)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d; want 201 (%s)", rec.Code, rec.Body.String())
	}
	resp := decode[AddToLibraryResponse](t, rec)
	if !resp.Created || resp.RecipeID == "" {
		t.Fatalf("response = %+v; want a created recipe", resp)
	}
	again := s.do(t, http.MethodPost, path, userAda)
	if again.Code != http.StatusOK {
		t.Fatalf("second add status = %d; want 200 (%s)", again.Code, again.Body.String())
	}
	if decode[AddToLibraryResponse](t, again).Created {
		t.Error("the second add reported created")
	}
}
