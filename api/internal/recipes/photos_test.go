package recipes

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

type memoryPhotos struct {
	mu     sync.Mutex
	photos map[string]Photo
}

func newMemoryPhotos() *memoryPhotos { return &memoryPhotos{photos: map[string]Photo{}} }

func (m *memoryPhotos) SavePhoto(_ context.Context, p Photo) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.photos[p.ID] = p
	return nil
}

func (m *memoryPhotos) GetPhoto(_ context.Context, id string) (Photo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.photos[id]
	if !ok {
		return Photo{}, ErrPhotoNotFound
	}
	return p, nil
}

var (
	testJPEG = append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, []byte("jpeg bytes")...)
	testPNG  = append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1A, '\n'}, []byte("png bytes")...)
	testHEIC = append([]byte{0, 0, 0, 0x18, 'f', 't', 'y', 'p', 'h', 'e', 'i', 'c'}, []byte("heic bytes")...)
)

func newPhotoTestServer(t *testing.T) (*recipeTestServer, *memoryPhotos) {
	t.Helper()
	photos := newMemoryPhotos()
	srv := newRecipeTestServer(t, func(o *HandlerOptions) { o.Service.WithPhotos(photos) })
	return srv, photos
}

func photosPath(householdID string) string {
	return "/api/v1/households/" + householdID + "/recipe-photos"
}

func (s *recipeTestServer) upload(t *testing.T, householdID, userID, contentType string, body []byte, header map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, photosPath(householdID), bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	if userID != "" {
		req.Header.Set("Authorization", "Bearer token-"+userID)
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, req)
	return rec
}

// uploadedPhoto uploads a JPEG and returns its imageUrl.
func uploadedPhoto(t *testing.T, srv *recipeTestServer) string {
	t.Helper()
	rec := srv.upload(t, hhAda, userAda, "image/jpeg", testJPEG, nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, body %s", rec.Code, rec.Body.String())
	}
	return decodeBody[PhotoUploadResponse](t, rec).ImageURL
}

func TestUploadPhotoStoresItAndServesIt(t *testing.T) {
	srv, photos := newPhotoTestServer(t)
	rec := srv.upload(t, hhAda, userAda, "image/jpeg", testJPEG, map[string]string{"X-Forwarded-Proto": "https"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	link := decodeBody[PhotoUploadResponse](t, rec).ImageURL
	if !strings.HasPrefix(link, "https://example.com/api/v1/recipe-photos/") || !IsOwnPhotoURL(link) {
		t.Fatalf("imageUrl = %q", link)
	}
	id := link[strings.LastIndex(link, "/")+1:]
	if p := photos.photos[id]; p.HouseholdID != hhAda || p.ContentType != "image/jpeg" || !bytes.Equal(p.Data, testJPEG) {
		t.Errorf("stored = %+v", p)
	}

	got := srv.do(t, http.MethodGet, "/api/v1/recipe-photos/"+id, "", "")
	if got.Code != http.StatusOK || !bytes.Equal(got.Body.Bytes(), testJPEG) {
		t.Fatalf("get = %d %q", got.Code, got.Body.String())
	}
	for k, want := range map[string]string{
		"Content-Type": "image/jpeg", "Cache-Control": "public, max-age=31536000, immutable", "X-Content-Type-Options": "nosniff",
	} {
		if v := got.Header().Get(k); v != want {
			t.Errorf("%s = %q, want %q", k, v, want)
		}
	}
	for _, missing := range []string{"0123456789abcdef0123456789abcdef", "not-an-id", strings.ToUpper(id)} {
		wantError(t, srv.do(t, http.MethodGet, "/api/v1/recipe-photos/"+missing, "", ""), http.StatusNotFound, "not_found")
	}
}

func TestUploadPhotoTakesPNGAndHEIC(t *testing.T) {
	srv, _ := newPhotoTestServer(t)
	for ct, body := range map[string][]byte{"image/png": testPNG, "image/heic": testHEIC} {
		rec := srv.upload(t, hhAda, userAda, ct, body, nil)
		if rec.Code != http.StatusCreated {
			t.Fatalf("%s status = %d", ct, rec.Code)
		}
		link := decodeBody[PhotoUploadResponse](t, rec).ImageURL
		if !strings.HasPrefix(link, "http://example.com/") {
			t.Errorf("%s imageUrl = %q", ct, link)
		}
		if got := srv.do(t, http.MethodGet, strings.TrimPrefix(link, "http://example.com"), "", ""); got.Header().Get("Content-Type") != ct {
			t.Errorf("%s served as %q", ct, got.Header().Get("Content-Type"))
		}
	}
}

func TestUploadPhotoRefusesWhatIsntAPhoto(t *testing.T) {
	srv, photos := newPhotoTestServer(t)
	if rec := srv.upload(t, hhAda, userViewer, "image/jpeg", testJPEG, nil); rec.Code != http.StatusForbidden {
		t.Errorf("viewer = %d", rec.Code)
	}
	if rec := srv.upload(t, hhAda, "", "image/jpeg", testJPEG, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("signed out = %d", rec.Code)
	}
	for name, tc := range map[string]struct {
		ct   string
		body []byte
	}{
		"text body":       {"image/jpeg", []byte("hello there")},
		"wrong type":      {"text/plain", testJPEG},
		"empty":           {"image/jpeg", nil},
		"gif":             {"image/gif", []byte("GIF89a....")},
		"png called heic": {"image/heic", []byte("not really")},
	} {
		rec := srv.upload(t, hhAda, userAda, tc.ct, tc.body, nil)
		wantError(t, rec, http.StatusBadRequest, "validation_failed")
		if !strings.Contains(rec.Body.String(), "That file isn't a photo.") {
			t.Errorf("%s: body %s", name, rec.Body.String())
		}
	}
	big := append(append([]byte{}, testJPEG...), make([]byte, MaxPhotoBytes)...)
	rec := srv.upload(t, hhAda, userAda, "image/jpeg", big, nil)
	if rec.Code != http.StatusRequestEntityTooLarge || !strings.Contains(rec.Body.String(), "That photo is too big.") {
		t.Errorf("too big = %d %s", rec.Code, rec.Body.String())
	}
	if len(photos.photos) != 0 {
		t.Errorf("stored %d photos", len(photos.photos))
	}
}

func TestIsOwnPhotoURL(t *testing.T) {
	const id = "0123456789abcdef0123456789abcdef"
	for raw, want := range map[string]bool{
		"https://api.tlps.dev/api/v1/recipe-photos/" + id:        true,
		"http://localhost:8082/api/v1/recipe-photos/" + id:       true,
		"ftp://api.tlps.dev/api/v1/recipe-photos/" + id:          false,
		"https://api.tlps.dev/api/v1/recipe-photos/" + id + "x":  false,
		"https://api.tlps.dev/api/v1/recipe-photos/ABC":          false,
		"https://api.tlps.dev/img/" + id:                         false,
		"https://u:p@api.tlps.dev/api/v1/recipe-photos/" + id:    false,
		"https://api.tlps.dev/api/v1/recipe-photos/" + id + "?a": false,
		"/api/v1/recipe-photos/" + id:                            false,
		"":                                                       false,
	} {
		if got := IsOwnPhotoURL(raw); got != want {
			t.Errorf("IsOwnPhotoURL(%q) = %v", raw, got)
		}
	}
}

// A typed recipe and one read from a page both keep the member's own photo;
// the page's photo is still dropped (decision 670).
func TestSavedRecipesKeepTheMembersOwnPhoto(t *testing.T) {
	srv, _ := newPhotoTestServer(t)
	own := uploadedPhoto(t, srv)
	typed := DraftResponse{Name: "Grandma's Chili", ImageURL: own, Ingredients: []DraftIngredientWire{{Name: "beans"}}, Steps: []string{"Simmer."}}
	got := decodeBody[RecipeResponse](t, srv.do(t, http.MethodPost, recipesPath(hhAda), mustJSON(t, typed), userAda))
	if got.ImageURL != own {
		t.Errorf("typed imageUrl = %q", got.ImageURL)
	}
	page := DraftResponse{
		Name: "Weeknight Chili", FromURL: true, SourceURL: "https://recipes.example.com/chili", ImageURL: own,
		Ingredients: []DraftIngredientWire{{Name: "Ground Beef", Quantity: "1", Unit: "lb"}},
	}
	got = decodeBody[RecipeResponse](t, srv.do(t, http.MethodPost, recipesPath(hhAda), mustJSON(t, page), userAda))
	if got.ImageURL != own {
		t.Errorf("page imageUrl = %q", got.ImageURL)
	}
	page.Name, page.ImageURL = "Other Chili", "https://recipes.example.com/chili.jpg"
	got = decodeBody[RecipeResponse](t, srv.do(t, http.MethodPost, recipesPath(hhAda), mustJSON(t, page), userAda))
	if got.ImageURL != "" {
		t.Errorf("kept the page's photo: %q", got.ImageURL)
	}
}

func TestEditSetsAndRemovesThePhoto(t *testing.T) {
	srv, _ := newPhotoTestServer(t)
	own := uploadedPhoto(t, srv)
	created := decodeBody[RecipeResponse](t, srv.do(t, http.MethodPost, recipesPath(hhAda), mustJSON(t, DraftResponse{
		Name: "Grandma's Chili", Ingredients: []DraftIngredientWire{{Name: "beans", Quantity: "2", Unit: "can"}}, Steps: []string{"Simmer."},
	}), userAda))
	path := recipesPath(hhAda) + "/" + created.ID + "/edits"

	// Only the photo: ingredients and steps stay.
	rec := srv.do(t, http.MethodPut, path, `{"imageUrl":"`+own+`"}`, userAda)
	if rec.Code != http.StatusOK {
		t.Fatalf("photo only = %d %s", rec.Code, rec.Body.String())
	}
	got := decodeBody[RecipeResponse](t, rec)
	if got.ImageURL != own || len(got.Ingredients) != 1 || len(got.Steps) != 1 {
		t.Errorf("after photo = %+v", got)
	}
	// With the rest of the edit, and an absent imageUrl keeps the photo.
	body := EditRecipeRequest{
		Ingredients: []EditIngredientRequest{{Name: "Black Beans", Amounts: []EditAmountRequest{{Servings: created.Servings[0], Quantity: "1", Unit: "bag"}}}},
		Steps:       []string{"Simmer longer."},
	}
	got = decodeBody[RecipeResponse](t, srv.do(t, http.MethodPut, path, mustJSON(t, body), userAda))
	if got.ImageURL != own || got.Ingredients[0].Name != "Black Beans" || got.Ingredients[0].Amounts[0].Unit != "bag" {
		t.Errorf("after edit = %+v", got)
	}
	empty := ""
	body.ImageURL = &empty
	if got = decodeBody[RecipeResponse](t, srv.do(t, http.MethodPut, path, mustJSON(t, body), userAda)); got.ImageURL != "" {
		t.Errorf("removed imageUrl = %q", got.ImageURL)
	}

	rec = srv.do(t, http.MethodPut, path, `{"imageUrl":"https://elsewhere.example.com/a.jpg"}`, userAda)
	wantError(t, rec, http.StatusBadRequest, "validation_failed")
	if !strings.Contains(rec.Body.String(), "That photo can't be used.") {
		t.Errorf("other photo body = %s", rec.Body.String())
	}
	if rec := srv.do(t, http.MethodPut, path, `{"imageUrl":"`+own+`"}`, userViewer); rec.Code != http.StatusForbidden {
		t.Errorf("viewer = %d", rec.Code)
	}
}

func TestEditPhotoOnlyForRecipesTheHouseholdAdded(t *testing.T) {
	srv, _ := newPhotoTestServer(t)
	own := uploadedPhoto(t, srv)
	srv.do(t, http.MethodPost, recipesPath(hhAda)+"/import", mustJSON(t, testFile(testRecipe("hf-1", "Garlic Noodles"))), userAda)
	id := recipeIDFor(t, srv, "Garlic Noodles")
	rec := srv.do(t, http.MethodPut, recipesPath(hhAda)+"/"+id+"/edits", `{"imageUrl":"`+own+`"}`, userAda)
	wantError(t, rec, http.StatusBadRequest, "validation_failed")
	if !strings.Contains(rec.Body.String(), "Only recipes you added can get a new photo.") {
		t.Errorf("body = %s", rec.Body.String())
	}
}

func TestBagIsAUnit(t *testing.T) {
	u, err := ingredients.LookupUnit("bag")
	if err != nil || u.Kind != ingredients.KindDiscrete || u.Plural != "bags" {
		t.Fatalf("bag = %+v, %v", u, err)
	}
	d := ParseText("Spinach Salad\nIngredients\n- 1 bag spinach\n- 2 bags croutons\nSteps\n1. Toss.")
	if len(d.Ingredients) != 2 || d.Ingredients[0].Unit != "bag" || d.Ingredients[1].Unit != "bag" || d.Ingredients[0].Name == "" {
		t.Errorf("parsed = %+v", d.Ingredients)
	}
}

func TestIntegrationMongoPhotos(t *testing.T) {
	svc, store, db := newTestMongoService(t)
	svc.WithPhotos(store)
	id, err := svc.SavePhoto(t.Context(), hhAda, testPNG)
	if err != nil {
		t.Fatal(err)
	}
	p, err := svc.Photo(t.Context(), id)
	if err != nil || p.ContentType != "image/png" || !bytes.Equal(p.Data, testPNG) || p.HouseholdID != hhAda || p.CreatedAt.IsZero() {
		t.Fatalf("photo = %+v, %v", p, err)
	}
	other, err := svc.SavePhoto(t.Context(), hhBob, testJPEG)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PurgeHousehold(t.Context(), hhAda); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Photo(t.Context(), id); err != ErrPhotoNotFound {
		t.Errorf("after purge = %v", err)
	}
	if _, err := svc.Photo(t.Context(), other); err != nil {
		t.Errorf("other household's photo = %v", err)
	}
	if n, _ := db.Collection(PhotosCollection).CountDocuments(t.Context(), map[string]any{}); n != 1 {
		t.Errorf("photos left = %d", n)
	}
}
