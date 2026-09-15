package invitations

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/ratelimit"
)

// fakeTokens accepts bearer tokens of the form "token-<userID>".
type fakeTokens struct{}

func (fakeTokens) ValidateAccessToken(token string) (string, error) {
	if id, ok := strings.CutPrefix(token, "token-"); ok && id != "" {
		return id, nil
	}
	return "", errors.New("invalid token")
}

type invitationTestServer struct {
	*testEnv
	router *chi.Mux
}

func newInvitationTestServer(t *testing.T, acceptBurst int) *invitationTestServer {
	t.Helper()
	env := newTestEnv(t)
	opts := HandlerOptions{Service: env.svc, Authorizer: env.households, Tokens: fakeTokens{}}
	if acceptBurst > 0 {
		opts.AcceptRateLimit = ratelimit.New(ratelimit.Options{Burst: acceptBurst, Now: env.clock.Now}).Middleware
	}
	r := chi.NewRouter()
	r.Route("/api/v1", NewHandler(opts).Mount)
	return &invitationTestServer{testEnv: env, router: r}
}

func (s *invitationTestServer) do(t *testing.T, method, path, body, userID string) *httptest.ResponseRecorder {
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

const invitationsPath = "/api/v1/households/" + householdID + "/invitations"

func TestInvitationLifecycleHandlers(t *testing.T) {
	srv := newInvitationTestServer(t, 0)

	rec := srv.do(t, http.MethodPost, invitationsPath, `{"email":"Cat@Example.com","role":"member"}`, userAda)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body %s", rec.Code, rec.Body.String())
	}
	created := decodeBody[CreateInvitationResponse](t, rec)
	if created.Invitation.ID == "" || created.Invitation.Email != "cat@example.com" || created.Invitation.Role != households.RoleMember ||
		created.Invitation.ExpiresAt.IsZero() || created.Invitation.CreatedAt.IsZero() || len(created.Code) != 11 || !created.EmailDelivered {
		t.Errorf("created = %+v", created)
	}
	var raw map[string]map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &raw)
	for _, field := range []string{"token", "tokenHash", "codeHash", "code"} {
		if _, ok := raw["invitation"][field]; ok {
			t.Errorf("invitation object exposes %q: %s", field, rec.Body.String())
		}
	}

	rec = srv.do(t, http.MethodGet, invitationsPath, "", userAda)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d", rec.Code)
	}
	list := decodeBody[InvitationListResponse](t, rec)
	if len(list.Items) != 1 || list.Items[0].ID != created.Invitation.ID {
		t.Errorf("list = %+v", list)
	}
	token := strings.TrimPrefix(srv.email.last().AcceptURL, DefaultAcceptURLBase+"#token=")
	for _, secret := range []string{created.Code, token, "Hash"} {
		if strings.Contains(rec.Body.String(), secret) {
			t.Errorf("list leaks %q: %s", secret, rec.Body.String())
		}
	}

	rec = srv.do(t, http.MethodPost, "/api/v1/invitations/accept", `{"code":"`+strings.ToLower(created.Code)+`"}`, userCat)
	if rec.Code != http.StatusOK {
		t.Fatalf("accept status = %d, body %s", rec.Code, rec.Body.String())
	}
	accepted := decodeBody[AcceptInvitationResponse](t, rec)
	if accepted.Household.ID != householdID || accepted.Household.Name != "The Lines" || accepted.Role != households.RoleMember ||
		len(accepted.Permissions) == 0 {
		t.Errorf("accepted = %+v", accepted)
	}
	wantError(t, srv.do(t, http.MethodPost, "/api/v1/invitations/accept", `{"token":"`+token+`"}`, userDan), 404, "invitation_invalid")

	if rec := srv.do(t, http.MethodGet, invitationsPath, "", userAda); strings.TrimSpace(rec.Body.String()) != `{"items":[]}` {
		t.Errorf("list after accept = %s", rec.Body.String())
	}
}

func TestRevokeInvitationHandler(t *testing.T) {
	srv := newInvitationTestServer(t, 0)
	created := decodeBody[CreateInvitationResponse](t, srv.do(t, http.MethodPost, invitationsPath, `{"email":"cat@example.com","role":"admin"}`, userAda))
	path := invitationsPath + "/" + created.Invitation.ID

	for i := range 2 {
		if rec := srv.do(t, http.MethodDelete, path, "", userAda); rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
			t.Fatalf("revoke #%d status = %d body %q", i+1, rec.Code, rec.Body.String())
		}
	}
	wantError(t, srv.do(t, http.MethodPost, "/api/v1/invitations/accept", `{"code":"`+created.Code+`"}`, userCat), 404, "invitation_invalid")
	wantError(t, srv.do(t, http.MethodDelete, invitationsPath+"/999999999999999999999999", "", userAda), 404, "not_found")
	wantError(t, srv.do(t, http.MethodDelete, invitationsPath+"/not-an-id", "", userAda), 404, "not_found")
}

func TestCreateInvitationHandlerEmailFailure(t *testing.T) {
	srv := newInvitationTestServer(t, 0)
	srv.email.err = errEmailDown
	rec := srv.do(t, http.MethodPost, invitationsPath, `{"email":"cat@example.com","role":"member"}`, userAda)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	body := decodeBody[CreateInvitationResponse](t, rec)
	if body.EmailDelivered || body.Code == "" {
		t.Errorf("body = %+v, want code with emailDelivered=false", body)
	}
	if !strings.Contains(rec.Body.String(), `"emailDelivered":false`) {
		t.Errorf("emailDelivered not serialized: %s", rec.Body.String())
	}
}

func TestInvitationHandlerErrors(t *testing.T) {
	srv := newInvitationTestServer(t, 0)
	created := decodeBody[CreateInvitationResponse](t, srv.do(t, http.MethodPost, invitationsPath, `{"email":"cat@example.com","role":"member"}`, userAda))
	one := invitationsPath + "/" + created.Invitation.ID
	otherPath := "/api/v1/households/" + otherHousehold + "/invitations"

	tests := []struct {
		name         string
		method, path string
		body         string
		userID       string
		status       int
		code         string
	}{
		{"create unauthenticated", "POST", invitationsPath, `{"email":"a@example.com","role":"member"}`, "", 401, "unauthenticated"},
		{"list unauthenticated", "GET", invitationsPath, "", "", 401, "unauthenticated"},
		{"revoke unauthenticated", "DELETE", one, "", "", 401, "unauthenticated"},
		{"accept unauthenticated", "POST", "/api/v1/invitations/accept", `{"code":"` + created.Code + `"}`, "", 401, "unauthenticated"},

		{"create by member", "POST", invitationsPath, `{"email":"a@example.com","role":"member"}`, userBob, 403, "forbidden"},
		{"list by member", "GET", invitationsPath, "", userBob, 403, "forbidden"},
		{"revoke by member", "DELETE", one, "", userBob, 403, "forbidden"},

		{"create by non-member", "POST", invitationsPath, `{"email":"a@example.com","role":"member"}`, userCat, 404, "not_found"},
		{"list by non-member", "GET", invitationsPath, "", userCat, 404, "not_found"},
		{"revoke by non-member", "DELETE", one, "", userCat, 404, "not_found"},
		{"list unknown household", "GET", "/api/v1/households/999999999999999999999999/invitations", "", userAda, 404, "not_found"},
		{"list other household", "GET", otherPath, "", userAda, 404, "not_found"},

		{"create empty body", "POST", invitationsPath, ``, userAda, 400, "invalid_request"},
		{"create unknown field", "POST", invitationsPath, `{"email":"a@example.com","role":"member","token":"x"}`, userAda, 400, "invalid_request"},
		{"create invalid email", "POST", invitationsPath, `{"email":"nope","role":"member"}`, userAda, 400, "validation_failed"},
		{"create missing email", "POST", invitationsPath, `{"role":"member"}`, userAda, 400, "validation_failed"},
		{"create missing role", "POST", invitationsPath, `{"email":"a@example.com"}`, userAda, 400, "validation_failed"},
		{"create invalid role", "POST", invitationsPath, `{"email":"a@example.com","role":"owner"}`, userAda, 400, "validation_failed"},

		{"accept empty body", "POST", "/api/v1/invitations/accept", ``, userCat, 400, "invalid_request"},
		{"accept neither", "POST", "/api/v1/invitations/accept", `{}`, userCat, 400, "validation_failed"},
		{"accept both", "POST", "/api/v1/invitations/accept", `{"token":"abc","code":"` + created.Code + `"}`, userCat, 400, "validation_failed"},
		{"accept huge token", "POST", "/api/v1/invitations/accept", `{"token":"` + strings.Repeat("a", 300) + `"}`, userCat, 400, "validation_failed"},
		{"accept unknown code", "POST", "/api/v1/invitations/accept", `{"code":"00000-00000"}`, userCat, 404, "invitation_invalid"},
		{"accept unknown token", "POST", "/api/v1/invitations/accept", `{"token":"` + strings.Repeat("A", TokenLength) + `"}`, userCat, 404, "invitation_invalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantError(t, srv.do(t, tt.method, tt.path, tt.body, tt.userID), tt.status, tt.code)
		})
	}
}

func TestAcceptInvitationIsRateLimited(t *testing.T) {
	srv := newInvitationTestServer(t, 2)
	for range 2 {
		wantError(t, srv.do(t, http.MethodPost, "/api/v1/invitations/accept", `{"code":"00000-00000"}`, userCat), 404, "invitation_invalid")
	}
	// Unauthenticated guesses count against the same bucket and are limited first.
	rec := srv.do(t, http.MethodPost, "/api/v1/invitations/accept", `{"code":"00000-00000"}`, "")
	wantError(t, rec, 429, "rate_limited")
	if rec.Header().Get("Retry-After") == "" {
		t.Error("missing Retry-After")
	}
	// Other invitation routes use their own limiter.
	if rec := srv.do(t, http.MethodGet, invitationsPath, "", userAda); rec.Code != http.StatusOK {
		t.Errorf("list status = %d, want 200", rec.Code)
	}
}

const (
	acceptPath  = "/api/v1/invitations/accept"
	previewPath = "/api/v1/invitations/preview"
)

func TestPreviewInvitationHandler(t *testing.T) {
	srv := newInvitationTestServer(t, 0)
	created := decodeBody[CreateInvitationResponse](t, srv.do(t, http.MethodPost, invitationsPath, `{"email":"cat@example.com","role":"admin"}`, userAda))
	token := strings.TrimPrefix(srv.email.last().AcceptURL, DefaultAcceptURLBase+"#token=")

	for _, body := range []string{`{"token":"` + token + `"}`, `{"code":"` + strings.ToLower(created.Code) + `"}`} {
		// No Authorization header: previews work signed out.
		rec := srv.do(t, http.MethodPost, previewPath, body, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("preview status = %d, body %s", rec.Code, rec.Body.String())
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
			t.Errorf("Cache-Control = %q, want no-store", cc)
		}
		var raw map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
			t.Fatal(err)
		}
		if len(raw) != 4 {
			t.Errorf("preview fields = %v, want exactly householdName, inviterName, role, expiresAt", raw)
		}
		got := decodeBody[InvitationPreviewResponse](t, rec)
		want := InvitationPreviewResponse{HouseholdName: "The Lines", InviterName: "Ada", Role: households.RoleAdmin, ExpiresAt: created.Invitation.ExpiresAt}
		if got != want {
			t.Errorf("preview = %+v, want %+v", got, want)
		}
		for _, secret := range []string{token, created.Code, "Hash", "cat@example.com"} {
			if strings.Contains(rec.Body.String(), secret) {
				t.Errorf("preview leaks %q: %s", secret, rec.Body.String())
			}
		}
	}

	// Previewing doesn't use the invitation up.
	if rec := srv.do(t, http.MethodPost, acceptPath, `{"token":"`+token+`"}`, userCat); rec.Code != http.StatusOK {
		t.Errorf("accept after preview status = %d, body %s", rec.Code, rec.Body.String())
	}
}

// TestPreviewErrorsMatchAccept checks that preview and accept give identical
// errors, so preview can't distinguish invitations that accept doesn't.
func TestPreviewErrorsMatchAccept(t *testing.T) {
	srv := newInvitationTestServer(t, 0)
	create := func(email string) CreateInvitationResponse {
		return decodeBody[CreateInvitationResponse](t, srv.do(t, http.MethodPost, invitationsPath, `{"email":"`+email+`","role":"member"}`, userAda))
	}
	revoked := create("revoked@example.com")
	srv.do(t, http.MethodDelete, invitationsPath+"/"+revoked.Invitation.ID, "", userAda)
	used := create("used@example.com")
	srv.do(t, http.MethodPost, acceptPath, `{"code":"`+used.Code+`"}`, userCat)
	expired := create("expired@example.com")

	bodies := map[string]string{
		"unknown code":    `{"code":"00000-00000"}`,
		"unknown token":   `{"token":"` + strings.Repeat("A", TokenLength) + `"}`,
		"malformed token": `{"token":"short"}`,
		"revoked":         `{"code":"` + revoked.Code + `"}`,
		"used":            `{"code":"` + used.Code + `"}`,
		"neither":         `{}`,
		"both":            `{"token":"abc","code":"00000-00000"}`,
		"huge token":      `{"token":"` + strings.Repeat("a", 300) + `"}`,
		"empty body":      ``,
		"unknown field":   `{"code":"00000-00000","email":"a@example.com"}`,
	}
	compare := func(name, body string) {
		t.Helper()
		accept := srv.do(t, http.MethodPost, acceptPath, body, userDan)
		preview := srv.do(t, http.MethodPost, previewPath, body, "")
		a := decodeBody[httpx.ErrorResponse](t, accept).Error
		p := decodeBody[httpx.ErrorResponse](t, preview).Error
		if accept.Code != preview.Code || a.Code != p.Code || a.Message != p.Message || accept.Code < 400 {
			t.Errorf("%s: accept %d %+v, preview %d %+v; want the same error", name, accept.Code, a, preview.Code, p)
		}
	}
	for name, body := range bodies {
		compare(name, body)
	}
	srv.clock.Advance(TTL)
	compare("expired", `{"code":"`+expired.Code+`"}`)
}

func TestPreviewSharesAcceptRateLimit(t *testing.T) {
	srv := newInvitationTestServer(t, 3)
	wantError(t, srv.do(t, http.MethodPost, previewPath, `{"code":"00000-00000"}`, ""), 404, "invitation_invalid")
	wantError(t, srv.do(t, http.MethodPost, acceptPath, `{"code":"00000-00000"}`, userCat), 404, "invitation_invalid")
	wantError(t, srv.do(t, http.MethodPost, previewPath, `{"code":"00000-00000"}`, ""), 404, "invitation_invalid")

	// Previews and accepts draw from one bucket, so previewing adds no guesses.
	rec := srv.do(t, http.MethodPost, previewPath, `{"code":"00000-00000"}`, "")
	wantError(t, rec, 429, "rate_limited")
	if rec.Header().Get("Retry-After") == "" {
		t.Error("missing Retry-After")
	}
	wantError(t, srv.do(t, http.MethodPost, acceptPath, `{"code":"00000-00000"}`, userCat), 429, "rate_limited")
}
