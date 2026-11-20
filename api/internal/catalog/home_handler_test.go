package catalog

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func newHomeServer(t *testing.T) (*testServer, *memCollections) {
	t.Helper()
	h := newHarness(t, nil)
	cols := &memCollections{}
	h.service.WithCollections(cols).WithSaves(&fakeSaves{})
	r := chi.NewRouter()
	r.Route("/api/v1", NewHandler(HandlerOptions{
		Service: h.service, Authorizer: testAuthorizer, Tokens: fakeTokens{}, Operators: []string{userAda},
	}).Mount)
	return &testServer{router: r, harness: h}, cols
}

func (s *testServer) send(t *testing.T, method, path, userID, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if userID != "" {
		req.Header.Set("Authorization", "Bearer token-"+userID)
	}
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)
	return rec
}

func TestDiscoverHomeRoute(t *testing.T) {
	s, _ := newHomeServer(t)
	seedHome(t, s.harness)
	if rec := s.do(t, http.MethodGet, "/api/v1/households/"+hhAda+"/discover/home", userBob); rec.Code != http.StatusNotFound {
		t.Fatalf("another household's member got %d", rec.Code)
	}
	rec := s.do(t, http.MethodGet, "/api/v1/households/"+hhAda+"/discover/home", userViewer)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	resp := decode[DiscoverHomeResponse](t, rec)
	if len(resp.Sections) != 4 || resp.Sections[0].ID != "chefs-picks" || resp.Sections[0].Type != TypeEditorial {
		t.Fatalf("sections = %+v", resp.Sections)
	}
	if resp.BrowseTitle != "Browse All" || resp.SeeAllTitle != "See All" || resp.SearchPrompt == "" || resp.NoMatchTitle == "" {
		t.Errorf("words = %+v", resp)
	}
	if len(resp.Filters) != 3 || resp.Filters[0].ID != "cuisine" || resp.Filters[1].Param != "maxMinutes" {
		t.Errorf("filters = %+v", resp.Filters)
	}
	body := rec.Body.String()
	for _, bad := range []string{"Garlic Bread", "sponsorLabel", "householdId", "\u2014"} {
		if strings.Contains(body, bad) {
			t.Errorf("home has %q: %s", bad, body)
		}
	}
}

func TestDiscoverSectionRoute(t *testing.T) {
	s, _ := newHomeServer(t)
	seedHome(t, s.harness)
	rec := s.do(t, http.MethodGet, "/api/v1/households/"+hhAda+"/discover/sections/chefs-picks?limit=2", userAda)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	resp := decode[DiscoverSectionPageResponse](t, rec)
	if resp.ID != "chefs-picks" || len(resp.Items) != 2 || resp.NextCursor == "" || !resp.HasMore || resp.Total != 3 {
		t.Errorf("section = %+v", resp)
	}
	if rec := s.do(t, http.MethodGet, "/api/v1/households/"+hhAda+"/discover/sections/nope", userAda); rec.Code != http.StatusNotFound {
		t.Errorf("unknown section = %d", rec.Code)
	}
}

func TestListFiltersOverHTTP(t *testing.T) {
	s, _ := newHomeServer(t)
	seedHome(t, s.harness)
	base := "/api/v1/households/" + hhAda
	rec := s.do(t, http.MethodGet, base+"/catalog/recipes?maxMinutes=30&cuisine=mexican", userAda)
	resp := decode[CatalogListResponse](t, rec)
	if len(resp.Items) != 1 || resp.Items[0].Name != "Quick Tacos" || len(resp.Filters) != 3 {
		t.Fatalf("filtered search = %s", rec.Body.String())
	}
	rec = s.do(t, http.MethodGet, base+"/discover?minServings=6", userAda)
	if resp := decode[CatalogListResponse](t, rec); len(resp.Items) != 1 || resp.Items[0].Name != "Big Beef Chili" {
		t.Fatalf("filtered discover = %s", rec.Body.String())
	}
	for _, q := range []string{"maxMinutes=abc", "minServings=0", "maxMinutes=-5"} {
		if rec := s.do(t, http.MethodGet, base+"/catalog/recipes?"+q, userAda); rec.Code != http.StatusBadRequest {
			t.Errorf("%s = %d; want 400", q, rec.Code)
		}
	}
}

func TestCollectionAdminRoutesAreForOperators(t *testing.T) {
	s, cols := newHomeServer(t)
	_, _, spring := seedHome(t, s.harness)
	const path = "/api/v1/admin/discover/collections"
	body := `{"id":"pasta-week","title":"Pasta Week","type":"sponsored","recipeIds":["` + spring + `"],"position":1,"sponsor":{"name":"Acme Pasta"}}`

	if rec := s.send(t, http.MethodGet, path, "", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous list = %d", rec.Code)
	}
	for _, m := range []string{http.MethodGet, http.MethodPost} {
		if rec := s.send(t, m, path, userBob, body); rec.Code != http.StatusForbidden {
			t.Errorf("non-operator %s = %d", m, rec.Code)
		}
	}
	if rec := s.send(t, http.MethodDelete, path+"/chefs-picks", userBob, ""); rec.Code != http.StatusForbidden {
		t.Errorf("non-operator delete = %d", rec.Code)
	}
	if len(cols.items) != 0 {
		t.Fatalf("a non-operator wrote %v", cols.items)
	}

	rec := s.send(t, http.MethodPost, path, userAda, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("operator post = %d (%s)", rec.Code, rec.Body.String())
	}
	home := decode[DiscoverHomeResponse](t, s.do(t, http.MethodGet, "/api/v1/households/"+hhAda+"/discover/home", userAda))
	if home.Sections[0].ID != "pasta-week" || home.Sections[0].SponsorLabel != "Sponsored by Acme Pasta" {
		t.Errorf("sponsored row = %+v", home.Sections[0])
	}

	bad := s.send(t, http.MethodPut, path+"/pasta-week", userAda, `{"title":"Pasta Week","type":"sponsored","rule":"best"}`)
	if bad.Code != http.StatusBadRequest || !strings.Contains(bad.Body.String(), "sponsor") {
		t.Errorf("bad put = %d %s", bad.Code, bad.Body.String())
	}
	list := decode[CollectionListResponse](t, s.send(t, http.MethodGet, path, userAda, ""))
	if len(list.Collections) != 7 || list.Collections[0].ID != "pasta-week" || len(list.Rules) == 0 {
		t.Errorf("list = %+v", list)
	}
	if rec := s.send(t, http.MethodDelete, path+"/pasta-week", userAda, ""); rec.Code != http.StatusNoContent {
		t.Errorf("delete = %d", rec.Code)
	}
	if rec := s.send(t, http.MethodDelete, path+"/pasta-week", userAda, ""); rec.Code != http.StatusNotFound {
		t.Errorf("second delete = %d", rec.Code)
	}
}

func TestSectionTitlesShowCurlyApostrophes(t *testing.T) {
	got := sectionResponse(Collection{ID: "x", Title: "Chef's Picks", Subtitle: "Cook's choice"}, nil, false)
	if got.Title != "Chef’s Picks" || got.Subtitle != "Cook’s choice" {
		t.Errorf("section = %q / %q", got.Title, got.Subtitle)
	}
}
