package recipes

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/photosearch"
)

type fakeSearcher struct {
	mu      sync.Mutex
	queries []string
	pages   []int
	picks   []string
	err     error
	picked  map[string]photosearch.Picked
}

func (f *fakeSearcher) Search(_ context.Context, q string, page int) (photosearch.Page, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queries, f.pages = append(f.queries, q), append(f.pages, page)
	if f.err != nil {
		return photosearch.Page{}, f.err
	}
	return photosearch.Page{
		Photos: []photosearch.Result{{
			ID: "abc_123", ThumbnailURL: "https://images.unsplash.com/photo-1?w=400", Width: 4000, Height: 3000,
			Credit: photosearch.Credit{Name: "Ana", ProfileURL: "https://unsplash.com/@ana?utm_medium=referral&utm_source=dinneros"},
		}},
		NextPage: page + 1,
	}, nil
}

func (f *fakeSearcher) Pick(_ context.Context, id string) (photosearch.Picked, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return photosearch.Picked{}, f.err
	}
	p, ok := f.picked[id]
	if !ok {
		return photosearch.Picked{}, photosearch.ErrNotFound
	}
	f.picks = append(f.picks, id)
	return p, nil
}

type memoryCredits struct {
	mu      sync.Mutex
	credits map[string]photosearch.Credit
}

func newMemoryCredits() *memoryCredits {
	return &memoryCredits{credits: map[string]photosearch.Credit{}}
}

func (m *memoryCredits) SavePhotoCredit(_ context.Context, imageURL string, c photosearch.Credit) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.credits[imageURL] = c
	return nil
}

func (m *memoryCredits) PhotoCredit(_ context.Context, imageURL string) (photosearch.Credit, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.credits[imageURL]
	return c, ok, nil
}

func newPhotoSearchTestServer(t *testing.T, searcher *fakeSearcher) (*recipeTestServer, *memoryPhotos) {
	t.Helper()
	photos := newMemoryPhotos()
	srv := newRecipeTestServer(t, func(o *HandlerOptions) {
		o.Service.WithPhotos(photos).WithPhotoCredits(newMemoryCredits())
		if searcher != nil {
			o.Service.WithPhotoSearch(searcher)
		}
	})
	return srv, photos
}

func TestPhotoSearchRoute(t *testing.T) {
	searcher := &fakeSearcher{}
	srv, _ := newPhotoSearchTestServer(t, searcher)
	path := photosPath(hhAda) + "/search"

	rec := srv.do(t, http.MethodGet, path+"?q=No%20Bake%20Cookies&page=2", "", userAda)
	if rec.Code != http.StatusOK {
		t.Fatalf("search = %d %s", rec.Code, rec.Body.String())
	}
	got := decodeBody[PhotoSearchResponse](t, rec)
	if len(got.Photos) != 1 || got.Photos[0] != (PhotoSearchResult{
		ID: "abc_123", ThumbnailURL: "https://images.unsplash.com/photo-1?w=400", Width: 4000, Height: 3000, Credit: "Ana",
		CreditURL: "https://unsplash.com/@ana?utm_medium=referral&utm_source=dinneros",
	}) || got.NextPage != 3 || got.SourceName != "Unsplash" || !strings.Contains(got.SourceURL, "utm_medium=referral") {
		t.Errorf("search = %+v", got)
	}
	if searcher.queries[0] != "No Bake Cookies" || searcher.pages[0] != 2 {
		t.Errorf("searched %q page %d", searcher.queries[0], searcher.pages[0])
	}
	if !strings.Contains(rec.Body.String(), `"thumbnailUrl"`) || !strings.Contains(rec.Body.String(), `"nextPage":3`) {
		t.Errorf("body = %s", rec.Body.String())
	}

	rec = srv.do(t, http.MethodGet, path+"?q=%20%20", "", userAda)
	wantError(t, rec, http.StatusBadRequest, "validation_failed")
	if !strings.Contains(rec.Body.String(), "Type what to search for.") {
		t.Errorf("blank body = %s", rec.Body.String())
	}
	wantError(t, srv.do(t, http.MethodGet, path+"?q=a&page=0", "", userAda), http.StatusBadRequest, "validation_failed")
	if rec := srv.do(t, http.MethodGet, path+"?q=a&page=999", "", userAda); rec.Code != http.StatusOK ||
		len(decodeBody[PhotoSearchResponse](t, rec).Photos) != 0 {
		t.Errorf("past the last page = %d %s", rec.Code, rec.Body.String())
	}
	if rec := srv.do(t, http.MethodGet, path+"?q=a", "", userViewer); rec.Code != http.StatusForbidden {
		t.Errorf("viewer = %d", rec.Code)
	}
	if rec := srv.do(t, http.MethodGet, path+"?q=a", "", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("signed out = %d", rec.Code)
	}

	searcher.err = errors.New("unsplash down")
	rec = srv.do(t, http.MethodGet, path+"?q=brownies", "", userAda)
	wantError(t, rec, http.StatusBadGateway, "photo_search_failed")
	if !strings.Contains(rec.Body.String(), "Couldn’t search for photos. Try again.") || strings.Contains(rec.Body.String(), "unsplash down") {
		t.Errorf("failure body = %s", rec.Body.String())
	}
}

const pickedPhoto = "https://images.unsplash.com/photo-1?ixid=abc&w=1080"

func TestPickingAPhotoHotlinksItAndCreditsThePhotographer(t *testing.T) {
	searcher := &fakeSearcher{picked: map[string]photosearch.Picked{
		"abc_123": {ImageURL: pickedPhoto, Credit: photosearch.Credit{Name: "Ana Lima", ProfileURL: "https://unsplash.com/@ana?utm_medium=referral&utm_source=dinneros"}},
	}}
	srv, photos := newPhotoSearchTestServer(t, searcher)
	path := photosPath(hhAda) + "/from-search"

	rec := srv.do(t, http.MethodPost, path, `{"id":"abc_123"}`, userAda)
	if rec.Code != http.StatusCreated {
		t.Fatalf("from search = %d %s", rec.Code, rec.Body.String())
	}
	got := decodeBody[PhotoPickResponse](t, rec)
	if got.ImageURL != pickedPhoto || got.Credit == nil || got.Credit.Name != "Ana Lima" ||
		got.Credit.SourceName != "Unsplash" || got.Credit.SourceURL != "https://unsplash.com/?utm_medium=referral&utm_source=dinneros" {
		t.Fatalf("pick = %+v %+v", got, got.Credit)
	}
	// Hotlinked, never copied.
	if len(photos.photos) != 0 {
		t.Errorf("a picked photo was stored: %d", len(photos.photos))
	}
	if len(searcher.picks) != 1 {
		t.Errorf("picks = %v", searcher.picks)
	}

	// Saved with a recipe, the recipe carries the credit.
	created := decodeBody[RecipeResponse](t, srv.do(t, http.MethodPost, recipesPath(hhAda), mustJSON(t, DraftResponse{
		Name: "Brownies", ImageURL: pickedPhoto, Ingredients: []DraftIngredientWire{{Name: "cocoa", Quantity: "1", Unit: "cup"}}, Steps: []string{"Bake."},
	}), userAda))
	if created.ImageURL != pickedPhoto || created.ImageCredit == nil || created.ImageCredit.Name != "Ana Lima" ||
		!strings.Contains(created.ImageCredit.ProfileURL, "utm_source=dinneros") {
		t.Fatalf("created = %q %+v", created.ImageURL, created.ImageCredit)
	}
	fetched := decodeBody[RecipeResponse](t, srv.do(t, http.MethodGet, recipesPath(hhAda)+"/"+created.ID, "", userAda))
	if fetched.ImageCredit == nil || fetched.ImageCredit.Name != "Ana Lima" {
		t.Errorf("fetched credit = %+v", fetched.ImageCredit)
	}

	// An edit can switch to a picked photo, and back to none.
	edit := func(body string) RecipeResponse {
		rec := srv.do(t, http.MethodPut, recipesPath(hhAda)+"/"+created.ID+"/edits", body, userAda)
		if rec.Code != http.StatusOK {
			t.Fatalf("edit %s = %d %s", body, rec.Code, rec.Body.String())
		}
		return decodeBody[RecipeResponse](t, rec)
	}
	if r := edit(`{"imageUrl":""}`); r.ImageURL != "" || r.ImageCredit != nil {
		t.Errorf("removed = %q %+v", r.ImageURL, r.ImageCredit)
	}
	if r := edit(`{"imageUrl":"` + pickedPhoto + `"}`); r.ImageCredit == nil {
		t.Errorf("edited to a picked photo without credit")
	}
	// Any other outside address is still refused.
	if rec := srv.do(t, http.MethodPut, recipesPath(hhAda)+"/"+created.ID+"/edits", `{"imageUrl":"https://elsewhere.test/a.jpg"}`, userAda); rec.Code < 400 {
		t.Errorf("outside photo = %d", rec.Code)
	}

	for _, tc := range []struct {
		body, code, msg string
		status          int
	}{
		{`{"id":"999"}`, "not_found", "That photo isn’t there anymore. Pick another one.", http.StatusNotFound},
		{`{"id":""}`, "validation_failed", "Pick a photo first.", http.StatusBadRequest},
	} {
		rec := srv.do(t, http.MethodPost, path, tc.body, userAda)
		wantError(t, rec, tc.status, tc.code)
		if !strings.Contains(rec.Body.String(), tc.msg) {
			t.Errorf("%s body = %s", tc.body, rec.Body.String())
		}
	}
	searcher.err = errors.New("unsplash down")
	rec = srv.do(t, http.MethodPost, path, `{"id":"abc_123"}`, userAda)
	wantError(t, rec, http.StatusBadGateway, "photo_search_failed")
	if strings.Contains(rec.Body.String(), "unsplash down") {
		t.Errorf("leaked the error: %s", rec.Body.String())
	}
	if rec := srv.do(t, http.MethodPost, path, `{"id":"abc_123"}`, userViewer); rec.Code != http.StatusForbidden {
		t.Errorf("viewer = %d", rec.Code)
	}
}

func TestAnUploadedPhotoHasNoCredit(t *testing.T) {
	srv, _ := newPhotoSearchTestServer(t, &fakeSearcher{})
	own := "https://example.com/api/v1/recipe-photos/0123456789abcdef0123456789abcdef"
	created := decodeBody[RecipeResponse](t, srv.do(t, http.MethodPost, recipesPath(hhAda), mustJSON(t, DraftResponse{
		Name: "Soup", ImageURL: own, Ingredients: []DraftIngredientWire{{Name: "salt", Quantity: "1", Unit: "tsp"}}, Steps: []string{"Stir."},
	}), userAda))
	if created.ImageCredit != nil {
		t.Errorf("own photo credited: %+v", created.ImageCredit)
	}
}

func TestPhotoSearchOffWithoutAKey(t *testing.T) {
	srv, _ := newPhotoSearchTestServer(t, nil)
	for _, rec := range []*struct{ method, path, body string }{
		{http.MethodGet, photosPath(hhAda) + "/search?q=brownies", ""},
		{http.MethodPost, photosPath(hhAda) + "/from-search", `{"id":"101"}`},
	} {
		res := srv.do(t, rec.method, rec.path, rec.body, userAda)
		wantError(t, res, http.StatusServiceUnavailable, "photo_search_off")
		if !strings.Contains(res.Body.String(), "Photo search isn’t available right now.") {
			t.Errorf("%s body = %s", rec.path, res.Body.String())
		}
	}
}

func TestPhotoSearchStatus(t *testing.T) {
	for _, tc := range []struct {
		name     string
		searcher *fakeSearcher
		want     bool
	}{
		{"with a key", &fakeSearcher{}, true},
		{"without a key", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := newPhotoSearchTestServer(t, tc.searcher)
			rec := srv.do(t, http.MethodGet, photosPath(hhAda)+"/search-status", "", userAda)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d %s", rec.Code, rec.Body.String())
			}
			if got := decodeBody[PhotoSearchStatusResponse](t, rec); got.Available != tc.want {
				t.Errorf("available = %v, want %v", got.Available, tc.want)
			}
			// Never asks the provider.
			if tc.searcher != nil && len(tc.searcher.queries) != 0 {
				t.Errorf("searched %v", tc.searcher.queries)
			}
		})
	}
	srv, _ := newPhotoSearchTestServer(t, &fakeSearcher{})
	if rec := srv.do(t, http.MethodGet, photosPath(hhAda)+"/search-status", "", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("signed out = %d", rec.Code)
	}
}

// The App Review account's photos are chosen by the server and credited the
// same way as one a member picked (decision 759).
func TestKeepPhotoCredit(t *testing.T) {
	ctx := context.Background()
	img := "https://images.unsplash.com/photo-1570645314284-8b28f8fac62e?w=1080"
	credit := photosearch.Credit{Name: "Solstice Hannan", ProfileURL: "https://unsplash.com/@darkersolstice"}
	if err := NewService(nil).KeepPhotoCredit(ctx, img, credit); err == nil {
		t.Error("want an error without a credit store")
	}
	s := NewService(nil).WithPhotoCredits(newMemoryCredits())
	if err := s.KeepPhotoCredit(ctx, img, credit); err != nil {
		t.Fatal(err)
	}
	if got := s.PhotoCredit(ctx, img); got == nil || got.Name != credit.Name {
		t.Errorf("credit = %+v; want %s", got, credit.Name)
	}
	for _, bad := range []struct {
		url  string
		name string
	}{{"https://example.com/photo.jpg", "Someone"}, {img, " "}} {
		if err := s.KeepPhotoCredit(ctx, bad.url, photosearch.Credit{Name: bad.name}); err == nil {
			t.Errorf("%q by %q: want an error", bad.url, bad.name)
		}
	}
}
