package push

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

type fakeAccessTokens struct{}

func (fakeAccessTokens) ValidateAccessToken(token string) (string, error) {
	if id, ok := strings.CutPrefix(token, "token-"); ok && id != "" {
		return id, nil
	}
	return "", errors.New("invalid token")
}

func TestDeviceTokenHandlers(t *testing.T) {
	store := &memoryTokens{}
	r := chi.NewRouter()
	r.Route("/api/v1", NewHandler(HandlerOptions{Service: NewService(store, nil), Tokens: fakeAccessTokens{}}).Mount)
	do := func(method, body, userID string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/v1/me/device-tokens", strings.NewReader(body))
		if userID != "" {
			req.Header.Set("Authorization", "Bearer token-"+userID)
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
	upper := strings.ToUpper(testDeviceToken)

	if rec := do(http.MethodPut, `{"token":"`+testDeviceToken+`","environment":"sandbox"}`, ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated = %d", rec.Code)
	}
	rec := do(http.MethodPut, `{"token":"`+upper+`","environment":"sandbox"}`, userAda)
	var resp DeviceTokenResponse
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &resp) != nil ||
		resp.Token != testDeviceToken || resp.Environment != EnvironmentSandbox || resp.Platform != PlatformIOS {
		t.Fatalf("register = %d %s", rec.Code, rec.Body)
	}
	// Re-registering from another account (a shared iPad) moves the token.
	if rec := do(http.MethodPut, `{"token":"`+testDeviceToken+`","environment":"production","platform":"ios"}`, userBob); rec.Code != http.StatusOK {
		t.Fatalf("re-register = %d %s", rec.Code, rec.Body)
	}
	if got, _ := store.ListByUsers(context.Background(), []string{userAda, userBob}); len(got) != 1 || got[0].UserID != userBob || got[0].Environment != EnvironmentProduction {
		t.Errorf("tokens = %+v", got)
	}

	for name, body := range map[string]string{
		"bad environment": `{"token":"` + testDeviceToken + `","environment":"staging"}`,
		"not hex":         `{"token":"not-a-token","environment":"sandbox"}`,
		"odd length":      `{"token":"abc","environment":"sandbox"}`,
		"bad platform":    `{"token":"` + testDeviceToken + `","environment":"sandbox","platform":"android"}`,
	} {
		if rec := do(http.MethodPut, body, userAda); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "validation_failed") {
			t.Errorf("%s = %d %s", name, rec.Code, rec.Body)
		}
	}
	if rec := do(http.MethodPut, `{"token":"`+testDeviceToken+`","environment":"sandbox","extra":1}`, userAda); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown field = %d", rec.Code)
	}

	// Ada can't delete Bob's registration; Bob can, and repeating is fine.
	if rec := do(http.MethodDelete, `{"token":"`+testDeviceToken+`"}`, userAda); rec.Code != http.StatusNoContent {
		t.Errorf("delete other's = %d", rec.Code)
	}
	if got, _ := store.ListByUsers(context.Background(), []string{userBob}); len(got) != 1 {
		t.Errorf("another user's delete removed the token: %+v", got)
	}
	for range 2 {
		if rec := do(http.MethodDelete, `{"token":"`+upper+`"}`, userBob); rec.Code != http.StatusNoContent {
			t.Errorf("delete = %d %s", rec.Code, rec.Body)
		}
	}
	if got, _ := store.ListByUsers(context.Background(), []string{userBob}); len(got) != 0 {
		t.Errorf("tokens after delete = %+v", got)
	}
}
