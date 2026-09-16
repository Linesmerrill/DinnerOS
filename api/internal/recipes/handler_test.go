package recipes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

const (
	hhAda = "66e5a1f2c3b4a5d6e7f80a01"
	hhBob = "66e5a1f2c3b4a5d6e7f80b01"

	userAda    = "ada"    // imports into hhAda
	userViewer = "viewer" // may view hhAda but not import
	userBob    = "bob"    // imports into hhBob
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
	hhAda + "/" + userAda:    {households.PermHouseholdView, households.PermRecipesImport},
	hhAda + "/" + userViewer: {households.PermHouseholdView},
	hhBob + "/" + userBob:    {households.PermHouseholdView, households.PermRecipesImport},
}

// testGlobalBodyLimit stands in for HTTP_MAX_BODY_BYTES; imports must be able
// to exceed it.
const testGlobalBodyLimit = 1 << 10

type recipeTestServer struct {
	router *chi.Mux
	store  *memoryStore
}

func newRecipeTestServer(t *testing.T, configure func(*HandlerOptions)) *recipeTestServer {
	t.Helper()
	store := newMemoryStore()
	opts := HandlerOptions{Service: NewService(store), Authorizer: testAuthorizer, Tokens: fakeTokens{}, ImportMaxBytes: 64 << 10}
	if configure != nil {
		configure(&opts)
	}
	r := chi.NewRouter()
	r.Use(httpx.LimitBody(testGlobalBodyLimit))
	r.Route("/api/v1", NewHandler(opts).Mount)
	return &recipeTestServer{router: r, store: store}
}

func (s *recipeTestServer) do(t *testing.T, method, path, body, userID string) *httptest.ResponseRecorder {
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

func recipesPath(householdID string) string { return "/api/v1/households/" + householdID + "/recipes" }

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// handlerFixture is larger than testGlobalBodyLimit.
func handlerFixture() ImportFile {
	tacos := testRecipe("r-tacos", "Beef Tacos", "2026-W10", "2026-W12")
	tacos.Description = strings.Repeat("Crispy and quick. ", 80)
	tacos.Ingredients[0].Amounts[0].Quantity = qty(0.5)
	cake := testRecipe("r-cake", "Lava Cake")
	cake.IsAddon = true
	file := testFile(tacos, cake)
	file.Review = []ImportReviewItem{{SourceRecipeID: "r-cake", RecipeName: "Lava Cake", Field: "steps", Reason: "recipe has no steps"}}
	return file
}

func TestRecipeHandlersImportListGet(t *testing.T) {
	srv := newRecipeTestServer(t, nil)
	body := mustJSON(t, handlerFixture())
	if len(body) <= testGlobalBodyLimit {
		t.Fatalf("fixture is %d bytes; it must exceed the global limit to prove the override", len(body))
	}

	rec := srv.do(t, http.MethodPost, recipesPath(hhAda)+"/import", body, userAda)
	if rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, body %s", rec.Code, rec.Body.String())
	}
	result := decodeBody[ImportResultResponse](t, rec)
	if result.Created != 2 || result.IngredientsCreated != 2 || result.ReviewItems != 1 || result.Errors == nil || len(result.Errors) != 0 {
		t.Errorf("import result = %+v (body %s)", result, rec.Body.String())
	}
	if again := decodeBody[ImportResultResponse](t, srv.do(t, http.MethodPost, recipesPath(hhAda)+"/import", body, userAda)); again.Unchanged != 2 {
		t.Errorf("re-import = %+v, want 2 unchanged", again)
	}

	// Viewers may list and read.
	rec = srv.do(t, http.MethodGet, recipesPath(hhAda)+"?sort=popular&limit=1", "", userViewer)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, body %s", rec.Code, rec.Body.String())
	}
	list := decodeBody[RecipeListResponse](t, rec)
	if len(list.Items) != 1 || list.Items[0].Name != "Beef Tacos" || list.Items[0].TimesOrdered != 2 || list.Items[0].LastOrderedWeek != "2026-W12" || list.NextCursor == "" {
		t.Fatalf("list = %+v", list)
	}
	next := decodeBody[RecipeListResponse](t, srv.do(t, http.MethodGet, recipesPath(hhAda)+"?sort=popular&limit=1&cursor="+list.NextCursor, "", userViewer))
	if len(next.Items) != 1 || next.Items[0].Name != "Lava Cake" || !next.Items[0].IsAddon || next.NextCursor != "" {
		t.Errorf("second page = %+v", next)
	}
	if addons := decodeBody[RecipeListResponse](t, srv.do(t, http.MethodGet, recipesPath(hhAda)+"?addons=true&q=CAKE", "", userAda)); len(addons.Items) != 1 {
		t.Errorf("addons search = %+v", addons)
	}

	id := list.Items[0].ID
	rec = srv.do(t, http.MethodGet, recipesPath(hhAda)+"/"+id, "", userViewer)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d, body %s", rec.Code, rec.Body.String())
	}
	got := decodeBody[RecipeResponse](t, rec)
	if got.ID != id || got.HouseholdID != hhAda || got.SourceRecipeID != "r-tacos" || len(got.Ingredients) != 2 || got.SourceAliases == nil || got.Utensils == nil {
		t.Fatalf("recipe = %+v", got)
	}
	garlic, salt := got.Ingredients[0], got.Ingredients[1]
	if garlic.Category != "produce" || *garlic.Amounts[0].Quantity != "1/2" || *garlic.Amounts[0].QuantityValue != 0.5 || garlic.Amounts[0].Unit != "clove" {
		t.Errorf("garlic = %+v", garlic)
	}
	if salt.Category != "spices" || !salt.PantryStaple || salt.Amounts[0].Quantity != nil || salt.Amounts[0].QuantityValue != nil {
		t.Errorf("salt = %+v", salt)
	}
	if !strings.Contains(rec.Body.String(), `"quantity":null`) {
		t.Errorf("missing quantity not serialized as null: %s", rec.Body.String())
	}

	// Another household's recipe is invisible, even by ID through a household
	// the caller does belong to.
	wantError(t, srv.do(t, http.MethodGet, recipesPath(hhBob)+"/"+id, "", userBob), 404, "not_found")
	if empty := srv.do(t, http.MethodGet, recipesPath(hhBob), "", userBob); strings.TrimSpace(empty.Body.String()) != `{"items":[]}` {
		t.Errorf("bob's list = %s", empty.Body.String())
	}
}

func TestRecipeHandlerErrors(t *testing.T) {
	srv := newRecipeTestServer(t, nil)
	valid := mustJSON(t, testFile(testRecipe("r1", "Tacos")))
	imported := decodeBody[ImportResultResponse](t, srv.do(t, http.MethodPost, recipesPath(hhAda)+"/import", valid, userAda))
	if imported.Created != 1 {
		t.Fatalf("setup import = %+v", imported)
	}
	id := decodeBody[RecipeListResponse](t, srv.do(t, http.MethodGet, recipesPath(hhAda), "", userAda)).Items[0].ID
	list, one, imp := recipesPath(hhAda), recipesPath(hhAda)+"/"+id, recipesPath(hhAda)+"/import"

	tests := []struct {
		name         string
		method, path string
		body         string
		userID       string
		status       int
		code         string
	}{
		{"list unauthenticated", "GET", list, "", "", 401, "unauthenticated"},
		{"get unauthenticated", "GET", one, "", "", 401, "unauthenticated"},
		{"import unauthenticated", "POST", imp, valid, "", 401, "unauthenticated"},

		{"list by non-member", "GET", list, "", userCat, 404, "not_found"},
		{"get by non-member", "GET", one, "", userBob, 404, "not_found"},
		{"import by non-member", "POST", imp, valid, userBob, 404, "not_found"},
		{"list malformed household", "GET", recipesPath("not-a-household"), "", userAda, 404, "not_found"},
		{"import without permission", "POST", imp, valid, userViewer, 403, "forbidden"},

		{"get malformed id", "GET", list + "/not-an-id", "", userAda, 404, "not_found"},
		{"get unknown id", "GET", list + "/ffffffffffffffffffffffff", "", userAda, 404, "not_found"},

		{"list bad sort", "GET", list + "?sort=spicy", "", userAda, 400, "validation_failed"},
		{"list bad limit", "GET", list + "?limit=ten", "", userAda, 400, "validation_failed"},
		{"list zero limit", "GET", list + "?limit=0", "", userAda, 400, "validation_failed"},
		{"list bad addons", "GET", list + "?addons=maybe", "", userAda, 400, "validation_failed"},
		{"list bad cursor", "GET", list + "?cursor=%21%21", "", userAda, 400, "validation_failed"},
		{"list long search", "GET", list + "?q=" + strings.Repeat("a", 101), "", userAda, 400, "validation_failed"},

		{"import empty body", "POST", imp, ``, userAda, 400, "invalid_request"},
		{"import malformed", "POST", imp, `{"version":`, userAda, 400, "invalid_request"},
		{"import unknown field", "POST", imp, `{"version":1,"source":"hellofresh","recipes":[],"householdId":"x"}`, userAda, 400, "invalid_request"},
		{"import wrong version", "POST", imp, `{"version":2,"source":"hellofresh","recipes":[]}`, userAda, 400, "validation_failed"},
		{"import unknown source", "POST", imp, `{"version":1,"source":"grocer","recipes":[]}`, userAda, 400, "validation_failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantError(t, srv.do(t, tt.method, tt.path, tt.body, tt.userID), tt.status, tt.code)
		})
	}
}

func TestImportHandlerReportsRejectedRecipes(t *testing.T) {
	srv := newRecipeTestServer(t, nil)
	bad := testRecipe("r-bad", "Mystery Stew")
	bad.Ingredients[0].Amounts[0].Unit = "dollop"
	rec := srv.do(t, http.MethodPost, recipesPath(hhAda)+"/import", mustJSON(t, testFile(testRecipe("r-ok", "Tacos"), bad)), userAda)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	res := decodeBody[ImportResultResponse](t, rec)
	if res.Created != 1 || len(res.Errors) != 1 {
		t.Fatalf("result = %+v", res)
	}
	if e := res.Errors[0]; e.Index != 1 || e.SourceRecipeID != "r-bad" || e.Name != "Mystery Stew" || len(e.Problems) != 1 || !strings.Contains(e.Problems[0], `"dollop"`) {
		t.Errorf("error = %+v", e)
	}
}

func TestImportHandlerBodyLimit(t *testing.T) {
	srv := newRecipeTestServer(t, func(o *HandlerOptions) { o.ImportMaxBytes = 4 << 10 })
	r := testRecipe("r-big", "Big")
	r.Description = strings.Repeat("x", 8<<10)
	wantError(t, srv.do(t, http.MethodPost, recipesPath(hhAda)+"/import", mustJSON(t, testFile(r)), userAda), 413, "payload_too_large")

	// Other routes keep the global limit; the override is not a global change.
	if got := NewHandler(HandlerOptions{}).opts.ImportMaxBytes; got != DefaultImportMaxBytes {
		t.Errorf("default ImportMaxBytes = %d, want %d", got, DefaultImportMaxBytes)
	}
}

// TestImportExtendsServerReadDeadline uploads a body slower than the server's
// ReadTimeout. Only the import route's extended deadline lets it finish.
func TestImportExtendsServerReadDeadline(t *testing.T) {
	body := []byte(mustJSON(t, handlerFixture()))
	upload := func(t *testing.T, timeout time.Duration) (int, error) {
		t.Helper()
		srv := newRecipeTestServer(t, func(o *HandlerOptions) { o.ImportTimeout = timeout })
		ts := httptest.NewUnstartedServer(srv.router)
		ts.Config.ReadTimeout = 250 * time.Millisecond
		ts.Start()
		defer ts.Close()

		pr, pw := io.Pipe()
		go func() {
			half := len(body) / 2
			_, _ = pw.Write(body[:half])
			time.Sleep(750 * time.Millisecond)
			_, _ = io.Copy(pw, bytes.NewReader(body[half:]))
			_ = pw.Close()
		}()
		req, err := http.NewRequest(http.MethodPost, ts.URL+recipesPath(hhAda)+"/import", pr)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer token-"+userAda)
		resp, err := ts.Client().Do(req)
		if err != nil {
			return 0, err
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		return resp.StatusCode, nil
	}

	if status, err := upload(t, 10*time.Second); err != nil || status != http.StatusOK {
		t.Errorf("extended deadline: status %d, err %v; want 200", status, err)
	}
	if status, err := upload(t, 0); err == nil && status == http.StatusOK {
		t.Errorf("without extension the slow upload succeeded; the test no longer exercises the deadline")
	}
}

func TestImportReviewsEndpoint(t *testing.T) {
	srv := newRecipeTestServer(t, nil)
	reviewsPath := recipesPath(hhAda) + "/import-reviews"
	if rec := srv.do(t, http.MethodPost, recipesPath(hhAda)+"/import", mustJSON(t, handlerFixture()), userAda); rec.Code != http.StatusOK {
		t.Fatalf("import status = %d (%s)", rec.Code, rec.Body.String())
	}

	rec := srv.do(t, http.MethodGet, reviewsPath, "", userAda)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	got := decodeBody[ImportReviewListResponse](t, rec)
	if len(got.Items) != 1 {
		t.Fatalf("items = %d, want 1 (%s)", len(got.Items), rec.Body.String())
	}
	item := got.Items[0]
	if item.Field != "steps" || item.SourceRecipeID != "r-cake" || item.RecipeName != "Lava Cake" {
		t.Errorf("item = %+v", item)
	}
	if item.Status != ReviewStatusOpen {
		t.Errorf("status = %q, want %q", item.Status, ReviewStatusOpen)
	}
	if item.CreatedAt.IsZero() {
		t.Error("createdAt is zero")
	}

	// The item names its recipe, so a client can open it.
	list := decodeBody[RecipeListResponse](t, srv.do(t, http.MethodGet, recipesPath(hhAda), "", userAda))
	var cakeID string
	for _, s := range list.Items {
		if s.Name == "Lava Cake" {
			cakeID = s.ID
		}
	}
	if cakeID == "" {
		t.Fatal("Lava Cake was not imported")
	}
	if item.RecipeID != cakeID {
		t.Errorf("recipeId = %q, want %q", item.RecipeID, cakeID)
	}

	// status=all is the same open item here; an unknown status is rejected.
	if all := decodeBody[ImportReviewListResponse](t, srv.do(t, http.MethodGet, reviewsPath+"?status=all", "", userAda)); len(all.Items) != 1 {
		t.Errorf("status=all items = %d, want 1", len(all.Items))
	}
	wantError(t, srv.do(t, http.MethodGet, reviewsPath+"?status=resolved", "", userAda), http.StatusBadRequest, "validation_failed")
	wantError(t, srv.do(t, http.MethodGet, reviewsPath+"?limit=0", "", userAda), http.StatusBadRequest, "validation_failed")

	// Review items are import bookkeeping: viewing the household is not enough.
	if rec := srv.do(t, http.MethodGet, reviewsPath, "", userViewer); rec.Code != http.StatusForbidden {
		t.Errorf("viewer status = %d, want %d", rec.Code, http.StatusForbidden)
	}
	// Another household's import is not visible here.
	if other := decodeBody[ImportReviewListResponse](t, srv.do(t, http.MethodGet, recipesPath(hhBob)+"/import-reviews", "", userBob)); len(other.Items) != 0 {
		t.Errorf("hhBob items = %d, want 0", len(other.Items))
	}
}
