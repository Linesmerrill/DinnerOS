package pantry

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

const (
	hhAda = testHousehold
	hhBob = otherHousehold

	userAda    = "ada"    // edits hhAda's pantry
	userViewer = "viewer" // may view hhAda but not edit its pantry
	userBob    = "bob"    // edits hhBob's pantry
	userCat    = "cat"    // no household
)

// fakeTokens accepts bearer tokens of the form "token-<userID>".
type fakeTokens struct{}

func (fakeTokens) ValidateAccessToken(token string) (string, error) {
	if id, ok := strings.CutPrefix(token, "token-"); ok && id != "" {
		return id, nil
	}
	return "", errors.New("invalid token")
}

// fakeAuthorizer grants permissions per (household, user), mirroring
// households.Service.Authorize: unknown pairs are ErrNotFound, missing
// permissions ErrForbidden.
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

var testAuthorizer = fakeAuthorizer{
	hhAda + "/" + userAda:    {households.PermHouseholdView, households.PermPantryEdit},
	hhAda + "/" + userViewer: {households.PermHouseholdView},
	hhBob + "/" + userBob:    {households.PermHouseholdView, households.PermPantryEdit},
}

type pantryTestServer struct {
	router  *chi.Mux
	catalog *fakeCatalog
}

func newPantryTestServer(t *testing.T) *pantryTestServer {
	t.Helper()
	svc, _, catalog := newTestService(t, "Olive Oil", "Salt")
	r := chi.NewRouter()
	r.Route("/api/v1", NewHandler(HandlerOptions{Service: svc, Authorizer: testAuthorizer, Tokens: fakeTokens{}}).Mount)
	return &pantryTestServer{router: r, catalog: catalog}
}

func (s *pantryTestServer) do(t *testing.T, method, path, body, userID string) *httptest.ResponseRecorder {
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

func wantStatus(t *testing.T, rec *httptest.ResponseRecorder, status int) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, status, rec.Body.String())
	}
}

func wantError(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	wantStatus(t, rec, status)
	if got := decodeBody[httpx.ErrorResponse](t, rec).Error.Code; got != code {
		t.Fatalf("error code = %q, want %q (body %s)", got, code, rec.Body.String())
	}
}

func pantryPath(householdID string) string { return "/api/v1/households/" + householdID + "/pantry" }

func TestPantryHandlersFlow(t *testing.T) {
	srv := newPantryTestServer(t)
	base := pantryPath(hhAda)

	rec := srv.do(t, http.MethodPost, base, `{"name":"Olive Oil","quantity":"1 1/2","unit":"cup","isStaple":true,"expiresOn":"2027-03-01","note":"big tin"}`, userAda)
	wantStatus(t, rec, http.StatusCreated)
	oil := decodeBody[PantryItemResponse](t, rec)
	if oil.ID == "" || oil.HouseholdID != hhAda || oil.IngredientID == nil || *oil.IngredientID != srv.catalog.id("Olive Oil") ||
		oil.Key != "olive oil" || oil.DisplayName != "Olive Oil" || oil.Category != "pantry" ||
		*oil.Quantity != "3/2" || *oil.QuantityValue != 1.5 || *oil.Unit != "cup" || oil.Status != StatusInStock || !oil.IsStaple ||
		*oil.ExpiresOn != "2027-03-01" || oil.Note != "big tin" || oil.UpdatedBy != userAda || !oil.UpdatedAt.Equal(testNow) {
		t.Fatalf("created = %s", rec.Body.String())
	}

	// Adding the same ingredient merges into it.
	rec = srv.do(t, http.MethodPost, base, `{"name":"olive oil","status":"low"}`, userAda)
	wantStatus(t, rec, http.StatusOK)
	if merged := decodeBody[PantryItemResponse](t, rec); merged.ID != oil.ID || merged.Status != StatusLow || *merged.Quantity != "3/2" {
		t.Errorf("merged = %s", rec.Body.String())
	}

	// "Have some", with no amount and no catalog match.
	rec = srv.do(t, http.MethodPost, base, `{"name":"Za'atar"}`, userAda)
	wantStatus(t, rec, http.StatusCreated)
	for _, field := range []string{`"ingredientId":null`, `"quantity":null`, `"quantityValue":null`, `"unit":null`, `"expiresOn":null`, `"note":""`} {
		if !strings.Contains(rec.Body.String(), field) {
			t.Errorf("free-text item missing %s: %s", field, rec.Body.String())
		}
	}
	zaatar := decodeBody[PantryItemResponse](t, rec)

	// Viewers can list and filter.
	rec = srv.do(t, http.MethodGet, base, "", userViewer)
	wantStatus(t, rec, http.StatusOK)
	if list := decodeBody[PantryListResponse](t, rec); len(list.Items) != 2 || list.Items[0].ID != oil.ID || list.Items[1].ID != zaatar.ID {
		t.Errorf("list = %s", rec.Body.String())
	}
	if filtered := decodeBody[PantryListResponse](t, srv.do(t, http.MethodGet, base+"?status=low&category=pantry&staple=true&q=OIL", "", userViewer)); len(filtered.Items) != 1 {
		t.Errorf("filtered = %+v", filtered)
	}

	// PATCH clears with null and leaves absent fields alone.
	rec = srv.do(t, http.MethodPatch, base+"/"+oil.ID, `{"quantity":null,"expiresOn":null,"note":"","displayName":"EVOO"}`, userAda)
	wantStatus(t, rec, http.StatusOK)
	if patched := decodeBody[PantryItemResponse](t, rec); patched.Quantity != nil || patched.Unit != nil || patched.ExpiresOn != nil || patched.Note != "" ||
		patched.DisplayName != "EVOO" || patched.Status != StatusLow || !patched.IsStaple {
		t.Errorf("patched = %s", rec.Body.String())
	}

	// Bulk status after shopping.
	const ghost = "ffffffffffffffffffffffff"
	rec = srv.do(t, http.MethodPost, base+"/bulk", `{"items":[{"id":"`+oil.ID+`","status":"in_stock"},{"id":"`+ghost+`","status":"out"},{"id":"`+zaatar.ID+`","status":"out"}]}`, userAda)
	wantStatus(t, rec, http.StatusOK)
	bulk := decodeBody[BulkStatusResponse](t, rec)
	if len(bulk.Items) != 2 || bulk.Items[0].Status != StatusInStock || bulk.Items[1].Status != StatusOut || !slices.Equal(bulk.Missing, []string{ghost}) {
		t.Errorf("bulk = %s", rec.Body.String())
	}
	if rec = srv.do(t, http.MethodPost, base+"/bulk", `{"items":[{"id":"`+oil.ID+`","status":"low"}]}`, userAda); !strings.Contains(rec.Body.String(), `"missing":[]`) {
		t.Errorf("bulk without missing IDs = %s", rec.Body.String())
	}

	// Default staples: olive oil is already there.
	rec = srv.do(t, http.MethodPost, base+"/staples/defaults", "", userAda)
	wantStatus(t, rec, http.StatusOK)
	if staples := decodeBody[DefaultStaplesResponse](t, rec); len(staples.Items) != len(defaultStaples)-1 || staples.Skipped != 1 || !staples.Items[0].IsStaple {
		t.Errorf("staples = %s", rec.Body.String())
	}
	if rec = srv.do(t, http.MethodPost, base+"/staples/defaults", "", userAda); strings.TrimSpace(rec.Body.String()) != `{"items":[],"skipped":9}` {
		t.Errorf("second staples call = %s", rec.Body.String())
	}

	// Delete.
	rec = srv.do(t, http.MethodDelete, base+"/"+zaatar.ID, "", userAda)
	wantStatus(t, rec, http.StatusNoContent)
	if rec.Body.Len() != 0 {
		t.Errorf("delete body = %q", rec.Body.String())
	}
	wantError(t, srv.do(t, http.MethodDelete, base+"/"+zaatar.ID, "", userAda), 404, "not_found")

	// Another household sees none of it, even by ID.
	if rec = srv.do(t, http.MethodGet, pantryPath(hhBob), "", userBob); strings.TrimSpace(rec.Body.String()) != `{"items":[]}` {
		t.Errorf("bob's pantry = %s", rec.Body.String())
	}
	wantError(t, srv.do(t, http.MethodPatch, pantryPath(hhBob)+"/"+oil.ID, `{"note":"mine"}`, userBob), 404, "not_found")
}

func TestPantryHandlerErrors(t *testing.T) {
	srv := newPantryTestServer(t)
	base := pantryPath(hhAda)
	item := decodeBody[PantryItemResponse](t, srv.do(t, http.MethodPost, base, `{"name":"Rice"}`, userAda))
	one := base + "/" + item.ID
	bulk := base + "/bulk"
	staples := base + "/staples/defaults"
	validBulk := `{"items":[{"id":"` + item.ID + `","status":"out"}]}`

	tests := []struct {
		name         string
		method, path string
		body         string
		userID       string
		status       int
		code         string
	}{
		{"list unauthenticated", "GET", base, "", "", 401, "unauthenticated"},
		{"add unauthenticated", "POST", base, `{"name":"Beans"}`, "", 401, "unauthenticated"},
		{"patch unauthenticated", "PATCH", one, `{"note":"x"}`, "", 401, "unauthenticated"},
		{"delete unauthenticated", "DELETE", one, "", "", 401, "unauthenticated"},
		{"bulk unauthenticated", "POST", bulk, validBulk, "", 401, "unauthenticated"},
		{"staples unauthenticated", "POST", staples, "", "", 401, "unauthenticated"},

		{"list by non-member", "GET", base, "", userCat, 404, "not_found"},
		{"add by member of another household", "POST", base, `{"name":"Beans"}`, userBob, 404, "not_found"},
		{"patch by non-member", "PATCH", one, `{"note":"x"}`, userCat, 404, "not_found"},
		{"delete by non-member", "DELETE", one, "", userBob, 404, "not_found"},
		{"bulk by non-member", "POST", bulk, validBulk, userCat, 404, "not_found"},
		{"staples by non-member", "POST", staples, "", userCat, 404, "not_found"},
		{"malformed household", "GET", pantryPath("not-a-household"), "", userAda, 404, "not_found"},

		{"add without pantry.edit", "POST", base, `{"name":"Beans"}`, userViewer, 403, "forbidden"},
		{"patch without pantry.edit", "PATCH", one, `{"note":"x"}`, userViewer, 403, "forbidden"},
		{"delete without pantry.edit", "DELETE", one, "", userViewer, 403, "forbidden"},
		{"bulk without pantry.edit", "POST", bulk, validBulk, userViewer, 403, "forbidden"},
		{"staples without pantry.edit", "POST", staples, "", userViewer, 403, "forbidden"},

		{"add bad unit code", "POST", base, `{"name":"Beans","quantity":"1","unit":"dollop"}`, userAda, 400, "validation_failed"},
		{"add negative quantity", "POST", base, `{"name":"Beans","quantity":"-2","unit":"cup"}`, userAda, 400, "validation_failed"},
		{"add invalid quantity", "POST", base, `{"name":"Beans","quantity":"lots","unit":"cup"}`, userAda, 400, "validation_failed"},
		{"add zero quantity", "POST", base, `{"name":"Beans","quantity":"0"}`, userAda, 400, "validation_failed"},
		{"add numeric quantity", "POST", base, `{"name":"Beans","quantity":2}`, userAda, 400, "invalid_request"},
		{"add without name", "POST", base, `{"status":"low"}`, userAda, 400, "validation_failed"},
		{"add bad status", "POST", base, `{"name":"Beans","status":"plenty"}`, userAda, 400, "validation_failed"},
		{"add bad date", "POST", base, `{"name":"Beans","expiresOn":"soon"}`, userAda, 400, "validation_failed"},
		{"add unknown ingredient", "POST", base, `{"ingredientId":"ffffffffffffffffffffffff"}`, userAda, 400, "validation_failed"},
		{"add unknown field", "POST", base, `{"name":"Beans","householdId":"x"}`, userAda, 400, "invalid_request"},
		{"add empty body", "POST", base, ``, userAda, 400, "invalid_request"},

		{"patch bad unit code", "PATCH", one, `{"quantity":"1","unit":"dollop"}`, userAda, 400, "validation_failed"},
		{"patch negative quantity", "PATCH", one, `{"quantity":"-1"}`, userAda, 400, "validation_failed"},
		{"patch invalid quantity", "PATCH", one, `{"quantity":"1/0"}`, userAda, 400, "validation_failed"},
		{"patch null display name", "PATCH", one, `{"displayName":null}`, userAda, 400, "validation_failed"},
		{"patch null status", "PATCH", one, `{"status":null}`, userAda, 400, "validation_failed"},
		{"patch nothing", "PATCH", one, `{}`, userAda, 400, "validation_failed"},
		{"patch wrong type", "PATCH", one, `{"isStaple":"yes"}`, userAda, 400, "invalid_request"},
		{"patch unknown item", "PATCH", base + "/ffffffffffffffffffffffff", `{"note":"x"}`, userAda, 404, "not_found"},
		{"patch malformed item", "PATCH", base + "/not-an-id", `{"note":"x"}`, userAda, 404, "not_found"},
		{"delete unknown item", "DELETE", base + "/ffffffffffffffffffffffff", "", userAda, 404, "not_found"},

		{"bulk empty", "POST", bulk, `{"items":[]}`, userAda, 400, "validation_failed"},
		{"bulk bad status", "POST", bulk, `{"items":[{"id":"` + item.ID + `","status":"gone"}]}`, userAda, 400, "validation_failed"},
		{"bulk malformed", "POST", bulk, `{"items":`, userAda, 400, "invalid_request"},

		{"list bad status", "GET", base + "?status=gone", "", userAda, 400, "validation_failed"},
		{"list bad category", "GET", base + "?category=snacks", "", userAda, 400, "validation_failed"},
		{"list bad staple", "GET", base + "?staple=maybe", "", userAda, 400, "validation_failed"},
		{"list long search", "GET", base + "?q=" + strings.Repeat("a", 101), "", userAda, 400, "validation_failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantError(t, srv.do(t, tt.method, tt.path, tt.body, tt.userID), tt.status, tt.code)
		})
	}

	// None of the rejected requests changed the item.
	list := decodeBody[PantryListResponse](t, srv.do(t, http.MethodGet, base, "", userAda))
	if len(list.Items) != 1 || list.Items[0].Status != StatusInStock || list.Items[0].Note != "" {
		t.Errorf("pantry after rejected requests = %+v", list.Items)
	}
}
