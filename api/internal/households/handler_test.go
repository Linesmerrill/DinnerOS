package households

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/auth"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

func contextWithUser(r *http.Request, userID string) context.Context {
	return auth.ContextWithUserID(r.Context(), userID)
}

// fakeTokens accepts bearer tokens of the form "token-<userID>".
type fakeTokens struct{}

func (fakeTokens) ValidateAccessToken(token string) (string, error) {
	if id, ok := strings.CutPrefix(token, "token-"); ok && id != "" {
		return id, nil
	}
	return "", errors.New("invalid token")
}

type householdTestServer struct {
	router *chi.Mux
	svc    *Service
}

func newHouseholdTestServer(t *testing.T) *householdTestServer {
	t.Helper()
	svc, _ := newTestService(t)
	r := chi.NewRouter()
	r.Route("/api/v1", NewHandler(HandlerOptions{Service: svc, Tokens: fakeTokens{}}).Mount)
	return &householdTestServer{router: r, svc: svc}
}

func (s *householdTestServer) do(t *testing.T, method, path, body, userID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
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

func wantError(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, status, rec.Body.String())
	}
	if got := decodeBody[httpx.ErrorResponse](t, rec).Error.Code; got != code {
		t.Fatalf("error code = %q, want %q", got, code)
	}
}

// createHousehold creates a household as userAda and adds Bob as a member.
func (s *householdTestServer) createHousehold(t *testing.T) string {
	t.Helper()
	rec := s.do(t, http.MethodPost, "/api/v1/households", `{"name":"Home","timeZone":"America/Denver"}`, userAda)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body %s", rec.Code, rec.Body.String())
	}
	id := decodeBody[CreateHouseholdResponse](t, rec).Household.ID
	if _, _, err := s.svc.AddMember(t.Context(), id, userBob, RoleMember); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestCreateHouseholdHandler(t *testing.T) {
	srv := newHouseholdTestServer(t)
	rec := srv.do(t, http.MethodPost, "/api/v1/households", `{"name":"Home","timeZone":"America/Denver","defaultServings":4}`, userAda)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	body := decodeBody[CreateHouseholdResponse](t, rec)
	if body.Household.ID == "" || body.Household.Name != "Home" || body.Household.DefaultServings != 4 ||
		body.Household.TimeZone != "America/Denver" || body.Household.CreatedBy != userAda {
		t.Errorf("household = %+v", body.Household)
	}
	if body.Membership.Role != RoleAdmin || body.Membership.UserID != userAda || body.Membership.HouseholdID != body.Household.ID ||
		len(body.Membership.Permissions) != len(AllPermissions()) {
		t.Errorf("membership = %+v", body.Membership)
	}
	if strings.Contains(rec.Body.String(), "adminCount") {
		t.Errorf("response leaks adminCount: %s", rec.Body.String())
	}
}

func TestListHouseholdsHandler(t *testing.T) {
	srv := newHouseholdTestServer(t)
	id := srv.createHousehold(t)

	body := decodeBody[HouseholdListResponse](t, srv.do(t, http.MethodGet, "/api/v1/households", "", userBob))
	if len(body.Items) != 1 || body.Items[0].Household.ID != id || body.Items[0].Role != RoleMember ||
		slices.Contains(body.Items[0].Permissions, PermMembersInvite) {
		t.Errorf("bob's list = %+v", body)
	}
	rec := srv.do(t, http.MethodGet, "/api/v1/households", "", userEve)
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"items":[]}` {
		t.Errorf("empty list = %d %s", rec.Code, rec.Body.String())
	}
}

func TestGetHouseholdHandler(t *testing.T) {
	srv := newHouseholdTestServer(t)
	id := srv.createHousehold(t)

	rec := srv.do(t, http.MethodGet, "/api/v1/households/"+id, "", userBob)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	body := decodeBody[HouseholdDetailResponse](t, rec)
	if body.Household.ID != id || body.Role != RoleMember || len(body.Members) != 2 ||
		body.Members[0].UserID != userAda || body.Members[0].DisplayName != "Ada" || body.Members[0].Role != RoleAdmin ||
		body.Members[1].UserID != userBob || body.Members[1].JoinedAt.IsZero() {
		t.Errorf("detail = %+v", body)
	}
}

func TestUpdateHouseholdHandler(t *testing.T) {
	srv := newHouseholdTestServer(t)
	id := srv.createHousehold(t)

	rec := srv.do(t, http.MethodPatch, "/api/v1/households/"+id, `{"name":"Casa","timeZone":"Europe/Paris"}`, userAda)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	if body := decodeBody[HouseholdResponse](t, rec); body.Name != "Casa" || body.TimeZone != "Europe/Paris" || body.DefaultServings != 2 || body.MealKit != nil {
		t.Errorf("updated = %+v", body)
	}

	// The meal kit baseline: set, left alone by other changes, cleared with null.
	rec = srv.do(t, http.MethodPatch, "/api/v1/households/"+id, `{"mealKit":{"weeklyCents":13000,"meals":5}}`, userAda)
	if !strings.Contains(rec.Body.String(), `"mealKit":{"weeklyCents":13000,"meals":5}`) {
		t.Fatalf("meal kit = %d %s", rec.Code, rec.Body.String())
	}
	rec = srv.do(t, http.MethodPatch, "/api/v1/households/"+id, `{"name":"Casa Two"}`, userAda)
	if !strings.Contains(rec.Body.String(), `"mealKit":{"weeklyCents":13000,"meals":5}`) {
		t.Errorf("meal kit after rename = %s", rec.Body.String())
	}
	rec = srv.do(t, http.MethodPatch, "/api/v1/households/"+id, `{"mealKit":null}`, userAda)
	if !strings.Contains(rec.Body.String(), `"mealKit":null`) {
		t.Errorf("cleared meal kit = %s", rec.Body.String())
	}
	wantError(t, srv.do(t, http.MethodPatch, "/api/v1/households/"+id, `{"mealKit":{"weeklyCents":13000,"meals":0}}`, userAda), 400, "validation_failed")
}

func TestMemberHandlers(t *testing.T) {
	srv := newHouseholdTestServer(t)
	id := srv.createHousehold(t)
	members := "/api/v1/households/" + id + "/members/"

	rec := srv.do(t, http.MethodPatch, members+userBob, `{"role":"admin"}`, userAda)
	if rec.Code != http.StatusOK {
		t.Fatalf("promote status = %d, body %s", rec.Code, rec.Body.String())
	}
	if body := decodeBody[MemberResponse](t, rec); body.UserID != userBob || body.Role != RoleAdmin || body.DisplayName != "Bob" {
		t.Errorf("promoted = %+v", body)
	}

	// Ada steps down now that Bob is an admin, then Bob removes her.
	if rec := srv.do(t, http.MethodPatch, members+userAda, `{"role":"member"}`, userAda); rec.Code != http.StatusOK {
		t.Fatalf("self-demote status = %d, body %s", rec.Code, rec.Body.String())
	}
	wantError(t, srv.do(t, http.MethodPatch, members+userBob, `{"role":"member"}`, userBob), 409, "last_admin")
	if rec := srv.do(t, http.MethodDelete, members+userAda, "", userBob); rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("remove status = %d, body %q", rec.Code, rec.Body.String())
	}
	wantError(t, srv.do(t, http.MethodGet, "/api/v1/households/"+id, "", userAda), 404, "not_found")
	wantError(t, srv.do(t, http.MethodDelete, members+userBob, "", userBob), 409, "last_admin")
}

func TestHouseholdHandlerErrors(t *testing.T) {
	srv := newHouseholdTestServer(t)
	id := srv.createHousehold(t)
	base := "/api/v1/households/" + id

	tests := []struct {
		name         string
		method, path string
		body         string
		userID       string
		status       int
		code         string
	}{
		{"create unauthenticated", "POST", "/api/v1/households", `{"name":"x","timeZone":"UTC"}`, "", 401, "unauthenticated"},
		{"list unauthenticated", "GET", "/api/v1/households", "", "", 401, "unauthenticated"},
		{"get unauthenticated", "GET", base, "", "", 401, "unauthenticated"},
		{"update unauthenticated", "PATCH", base, `{"name":"x"}`, "", 401, "unauthenticated"},
		{"role unauthenticated", "PATCH", base + "/members/" + userBob, `{"role":"admin"}`, "", 401, "unauthenticated"},
		{"remove unauthenticated", "DELETE", base + "/members/" + userBob, "", "", 401, "unauthenticated"},

		{"create empty body", "POST", "/api/v1/households", ``, userAda, 400, "invalid_request"},
		{"create unknown field", "POST", "/api/v1/households", `{"name":"x","timeZone":"UTC","owner":"me"}`, userAda, 400, "invalid_request"},
		{"create missing name", "POST", "/api/v1/households", `{"timeZone":"UTC"}`, userAda, 400, "validation_failed"},
		{"create bad time zone", "POST", "/api/v1/households", `{"name":"x","timeZone":"Moon/Base"}`, userAda, 400, "validation_failed"},
		{"create bad servings", "POST", "/api/v1/households", `{"name":"x","timeZone":"UTC","defaultServings":0}`, userAda, 400, "validation_failed"},
		{"update empty patch", "PATCH", base, `{}`, userAda, 400, "validation_failed"},
		{"update bad servings type", "PATCH", base, `{"defaultServings":"two"}`, userAda, 400, "invalid_request"},
		{"role missing", "PATCH", base + "/members/" + userBob, `{}`, userAda, 400, "validation_failed"},
		{"role invalid", "PATCH", base + "/members/" + userBob, `{"role":"owner"}`, userAda, 400, "validation_failed"},

		{"get non-member", "GET", base, "", userEve, 404, "not_found"},
		{"update non-member", "PATCH", base, `{"name":"x"}`, userEve, 404, "not_found"},
		{"role non-member", "PATCH", base + "/members/" + userBob, `{"role":"admin"}`, userEve, 404, "not_found"},
		{"remove non-member", "DELETE", base + "/members/" + userBob, "", userEve, 404, "not_found"},
		{"get unknown household", "GET", "/api/v1/households/" + missingID, "", userAda, 404, "not_found"},
		{"get malformed id", "GET", "/api/v1/households/nope", "", userAda, 404, "not_found"},
		{"role unknown target", "PATCH", base + "/members/" + userEve, `{"role":"admin"}`, userAda, 404, "not_found"},
		{"remove unknown target", "DELETE", base + "/members/" + userEve, "", userAda, 404, "not_found"},

		{"member update", "PATCH", base, `{"name":"x"}`, userBob, 403, "forbidden"},
		{"member promote self", "PATCH", base + "/members/" + userBob, `{"role":"admin"}`, userBob, 403, "forbidden"},
		{"member remove admin", "DELETE", base + "/members/" + userAda, "", userBob, 403, "forbidden"},

		{"last admin demote", "PATCH", base + "/members/" + userAda, `{"role":"member"}`, userAda, 409, "last_admin"},
		{"last admin leave", "DELETE", base + "/members/" + userAda, "", userAda, 409, "last_admin"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantError(t, srv.do(t, tt.method, tt.path, tt.body, tt.userID), tt.status, tt.code)
		})
	}

	// Nothing above changed the household.
	body := decodeBody[HouseholdDetailResponse](t, srv.do(t, http.MethodGet, base, "", userAda))
	if body.Household.Name != "Home" || len(body.Members) != 2 || body.Members[0].Role != RoleAdmin || body.Members[1].Role != RoleMember {
		t.Errorf("household changed: %+v", body)
	}
}

func TestRequirePermissionMiddleware(t *testing.T) {
	srv := newHouseholdTestServer(t)
	id := srv.createHousehold(t)

	r := chi.NewRouter()
	var seen Membership
	r.With(RequirePermission(srv.svc, PermMembersInvite, nil)).Get("/h/{householdId}", func(w http.ResponseWriter, r *http.Request) {
		seen, _ = MembershipFromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	})
	serve := func(userID string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/h/"+id, nil)
		if userID != "" {
			req = req.WithContext(contextWithUser(req, userID))
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	if rec := serve(userAda); rec.Code != http.StatusNoContent || seen.UserID != userAda || seen.Role != RoleAdmin {
		t.Errorf("admin: status %d, membership %+v", rec.Code, seen)
	}
	wantError(t, serve(userBob), 403, "forbidden")
	wantError(t, serve(userEve), 404, "not_found")
	wantError(t, serve(""), 401, "unauthenticated")
}
