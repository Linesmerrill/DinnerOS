package ratings

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

// fakeTokens accepts bearer tokens of the form "token-<userID>".
type fakeTokens struct{}

func (fakeTokens) ValidateAccessToken(token string) (string, error) {
	if id, ok := strings.CutPrefix(token, "token-"); ok && id != "" {
		return id, nil
	}
	return "", errors.New("invalid token")
}

// fakeAuthorizer grants the listed permissions per (household, user); unknown
// pairs are ErrNotFound, like households.Service.Authorize.
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

type ratingTestServer struct {
	router *chi.Mux
	env    *testEnv
}

func newRatingTestServer(t *testing.T) *ratingTestServer {
	t.Helper()
	env := newTestEnv(t)
	// Members hold only household.view, the least a member can have.
	view := []households.Permission{households.PermHouseholdView}
	authz := fakeAuthorizer{hhA + "/" + userAda: view, hhA + "/" + userAlan: view, hhB + "/" + userBob: view}
	r := chi.NewRouter()
	r.Use(httpx.LimitBody(1 << 20))
	r.Route("/api/v1", NewHandler(HandlerOptions{Service: env.svc, Authorizer: authz, Tokens: fakeTokens{}, Logger: slog.New(slog.DiscardHandler)}).Mount)
	return &ratingTestServer{router: r, env: env}
}

func (s *ratingTestServer) do(t *testing.T, method, path, body, userID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(body))
	if userID != "" {
		req.Header.Set("Authorization", "Bearer token-"+userID)
	}
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)
	return rec
}

func decodeBody[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return v
}

func ratingPath(householdID, recipeID string) string {
	return "/households/" + householdID + "/recipes/" + recipeID + "/rating"
}

func ratingsPath(householdID, recipeID string) string {
	return "/households/" + householdID + "/recipes/" + recipeID + "/ratings"
}

func TestRatingHandlers(t *testing.T) {
	srv := newRatingTestServer(t)

	rec := srv.do(t, http.MethodPut, ratingPath(hhA, tacos), `{"score":5,"comment":"Crowd pleaser","tags":["kid-favorite","make-again"]}`, userAda)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body %s", rec.Code, rec.Body.String())
	}
	mine := decodeBody[RatingResponse](t, rec)
	if mine.RecipeID != tacos || mine.UserID != userAda || mine.Score != 5 || mine.Comment != "Crowd pleaser" ||
		!slices.Equal(mine.Tags, []string{"make-again", "kid-favorite"}) || mine.CreatedAt.IsZero() {
		t.Errorf("rating = %+v", mine)
	}

	rec = srv.do(t, http.MethodPut, ratingPath(hhA, tacos), `{"score":2}`, userAlan)
	if !strings.Contains(rec.Body.String(), `"tags":[]`) || !strings.Contains(rec.Body.String(), `"comment":""`) {
		t.Errorf("rating without tags or comment = %s; want empty values, not omitted", rec.Body.String())
	}

	rec = srv.do(t, http.MethodGet, ratingsPath(hhA, tacos), "", userAda)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body %s", rec.Code, rec.Body.String())
	}
	list := decodeBody[RatingListResponse](t, rec)
	if list.HouseholdRating.Count != 2 || list.HouseholdRating.Average == nil || *list.HouseholdRating.Average != 3.5 || len(list.Items) != 2 {
		t.Fatalf("list = %s", rec.Body.String())
	}
	names := []string{list.Items[0].DisplayName, list.Items[1].DisplayName}
	if !slices.Contains(names, "Ada") || !slices.Contains(names, "Alan") {
		t.Errorf("display names = %v", names)
	}

	for range 2 { // idempotent
		if rec := srv.do(t, http.MethodDelete, ratingPath(hhA, tacos), "", userAda); rec.Code != http.StatusNoContent {
			t.Fatalf("DELETE status = %d, body %s", rec.Code, rec.Body.String())
		}
	}
	srv.do(t, http.MethodDelete, ratingPath(hhA, tacos), "", userAlan)
	if body := srv.do(t, http.MethodGet, ratingsPath(hhA, tacos), "", userAlan).Body.String(); strings.TrimSpace(body) != `{"householdRating":{"average":null,"count":0},"items":[]}` {
		t.Errorf("empty list = %s", body)
	}
}

func TestRatingHandlerRecorderFailure(t *testing.T) {
	srv := newRatingTestServer(t)
	srv.env.recorder.err = errors.New("events down")
	if rec := srv.do(t, http.MethodPut, ratingPath(hhA, tacos), `{"score":4}`, userAda); rec.Code != http.StatusOK {
		t.Errorf("PUT with failing recorder = %d, want 200", rec.Code)
	}
}

func TestRatingHandlerErrors(t *testing.T) {
	srv := newRatingTestServer(t)
	valid := `{"score":4}`
	put, del, get := http.MethodPut, http.MethodDelete, http.MethodGet

	tests := []struct {
		name         string
		method, path string
		body, userID string
		status       int
		code         string
	}{
		{"put unauthenticated", put, ratingPath(hhA, tacos), valid, "", 401, "unauthenticated"},
		{"delete unauthenticated", del, ratingPath(hhA, tacos), "", "", 401, "unauthenticated"},
		{"list unauthenticated", get, ratingsPath(hhA, tacos), "", "", 401, "unauthenticated"},

		{"put by non-member", put, ratingPath(hhA, tacos), valid, userBob, 404, "not_found"},
		{"delete by non-member", del, ratingPath(hhA, tacos), "", userBob, 404, "not_found"},
		{"list by non-member", get, ratingsPath(hhA, tacos), "", userBob, 404, "not_found"},
		{"malformed household", put, ratingPath("nope", tacos), valid, userAda, 404, "not_found"},

		{"other household's recipe", put, ratingPath(hhA, bobsStew), valid, userAda, 404, "not_found"},
		{"list other household's recipe", get, ratingsPath(hhB, tacos), "", userBob, 404, "not_found"},
		{"delete unknown recipe", del, ratingPath(hhA, "ffffffffffffffffffffffff"), "", userAda, 404, "not_found"},
		{"malformed recipe", put, ratingPath(hhA, "not-an-id"), valid, userAda, 404, "not_found"},

		{"score out of range", put, ratingPath(hhA, tacos), `{"score":9}`, userAda, 400, "validation_failed"},
		{"missing score", put, ratingPath(hhA, tacos), `{"comment":"hi"}`, userAda, 400, "validation_failed"},
		{"unknown tag", put, ratingPath(hhA, tacos), `{"score":3,"tags":["meh"]}`, userAda, 400, "validation_failed"},
		{"long comment", put, ratingPath(hhA, tacos), `{"score":3,"comment":"` + strings.Repeat("a", 501) + `"}`, userAda, 400, "validation_failed"},
		{"empty body", put, ratingPath(hhA, tacos), ``, userAda, 400, "invalid_request"},
		{"unknown field", put, ratingPath(hhA, tacos), `{"score":3,"userId":"` + userAlan + `"}`, userAda, 400, "invalid_request"},
		{"fractional score", put, ratingPath(hhA, tacos), `{"score":3.5}`, userAda, 400, "invalid_request"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := srv.do(t, tt.method, tt.path, tt.body, tt.userID)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tt.status, rec.Body.String())
			}
			if got := decodeBody[httpx.ErrorResponse](t, rec).Error.Code; got != tt.code {
				t.Fatalf("code = %q, want %q", got, tt.code)
			}
		})
	}
	if srv.env.store.count() != 0 {
		t.Error("a failed request stored a rating")
	}
}
