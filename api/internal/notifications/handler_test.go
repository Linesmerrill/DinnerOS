package notifications

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

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

type fakeTokens struct{}

func (fakeTokens) ValidateAccessToken(token string) (string, error) {
	if id, ok := strings.CutPrefix(token, "token-"); ok && id != "" {
		return id, nil
	}
	return "", errors.New("invalid token")
}

// fakeAuthorizer lets userA and userB view hhA.
type fakeAuthorizer struct{}

func (fakeAuthorizer) Authorize(_ context.Context, householdID, userID string, perm households.Permission) (households.Membership, error) {
	if householdID != hhA || !slices.Contains([]string{userA, userB}, userID) {
		return households.Membership{}, households.ErrNotFound
	}
	m := memberOf(householdID, userID)
	if !m.Role.Can(perm) {
		return m, households.ErrForbidden
	}
	return m, nil
}

func TestNotificationHandlers(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()
	older, _, _ := svc.Create(ctx, lowNotification("a"))
	newer, _, _ := svc.Create(ctx, lowNotification("b"))

	r := chi.NewRouter()
	r.Route("/api/v1", NewHandler(HandlerOptions{Service: svc, Authorizer: fakeAuthorizer{}, Tokens: fakeTokens{}}).Mount)
	do := func(method, path, body, userID string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if userID != "" {
			req.Header.Set("Authorization", "Bearer token-"+userID)
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
	base := "/api/v1/households/" + hhA + "/notifications"

	rec := do(http.MethodGet, base+"?limit=1", "", userA)
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d %s", rec.Code, rec.Body.String())
	}
	var list ListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 || list.Items[0].ID != newer.ID || list.Items[0].Read || list.NextCursor == nil ||
		list.Items[0].Subject.Kind != SubjectPantryItem || list.Items[0].Type != TypePantryLow {
		t.Fatalf("list = %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"subject":{"kind":"pantry_item","id":"item1"}`) {
		t.Errorf("subject shape: %s", rec.Body.String())
	}

	rec = do(http.MethodGet, base+"/unread-count", "", userA)
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"unreadCount":2}` {
		t.Fatalf("unread-count = %d %s", rec.Code, rec.Body.String())
	}

	rec = do(http.MethodPost, base+"/read", `{"ids":["`+older.ID+`"]}`, userA)
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"unreadCount":1}` {
		t.Fatalf("read = %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodGet, base+"?unread=true", "", userA)
	if !strings.Contains(rec.Body.String(), newer.ID) || strings.Contains(rec.Body.String(), older.ID) || !strings.Contains(rec.Body.String(), `"nextCursor":null`) {
		t.Errorf("unread list = %s", rec.Body.String())
	}
	rec = do(http.MethodGet, base, "", userA)
	if !strings.Contains(rec.Body.String(), `"read":true`) {
		t.Errorf("list after read = %s", rec.Body.String())
	}
	rec = do(http.MethodPost, base+"/read", `{"all":true}`, userB)
	if strings.TrimSpace(rec.Body.String()) != `{"unreadCount":0}` {
		t.Errorf("read all = %s", rec.Body.String())
	}

	for _, tc := range []struct {
		method, path, body, user string
		status                   int
		code                     string
	}{
		{http.MethodGet, base, "", "", http.StatusUnauthorized, "unauthenticated"},
		{http.MethodGet, "/api/v1/households/" + hhB + "/notifications", "", userA, http.StatusNotFound, "not_found"},
		{http.MethodGet, base + "?unread=maybe", "", userA, http.StatusBadRequest, "validation_failed"},
		{http.MethodGet, base + "?limit=0", "", userA, http.StatusBadRequest, "validation_failed"},
		{http.MethodGet, base + "?limit=x", "", userA, http.StatusBadRequest, "validation_failed"},
		{http.MethodPost, base + "/read", `{}`, userA, http.StatusBadRequest, "validation_failed"},
		{http.MethodPost, base + "/read", `{"ids":"x"}`, userA, http.StatusBadRequest, "invalid_request"},
	} {
		rec := do(tc.method, tc.path, tc.body, tc.user)
		var resp httpx.ErrorResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if rec.Code != tc.status || resp.Error.Code != tc.code {
			t.Errorf("%s %s %s = %d %s, want %d %s", tc.method, tc.path, tc.body, rec.Code, rec.Body.String(), tc.status, tc.code)
		}
	}
}
