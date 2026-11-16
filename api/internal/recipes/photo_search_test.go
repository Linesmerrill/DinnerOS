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
	err     error
	images  map[string]photosearch.Image
}

func (f *fakeSearcher) Search(_ context.Context, q string, page int) (photosearch.Page, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queries, f.pages = append(f.queries, q), append(f.pages, page)
	if f.err != nil {
		return photosearch.Page{}, f.err
	}
	return photosearch.Page{
		Photos:   []photosearch.Result{{ID: "101", ThumbnailURL: "https://images.pexels.com/photos/101/a.jpeg?h=350", Width: 4000, Height: 3000, Credit: "Ana"}},
		NextPage: page + 1,
	}, nil
}

func (f *fakeSearcher) Fetch(_ context.Context, id string) (photosearch.Image, error) {
	if f.err != nil {
		return photosearch.Image{}, f.err
	}
	img, ok := f.images[id]
	if !ok {
		return photosearch.Image{}, photosearch.ErrNotFound
	}
	return img, nil
}

func newPhotoSearchTestServer(t *testing.T, searcher *fakeSearcher) (*recipeTestServer, *memoryPhotos) {
	t.Helper()
	photos := newMemoryPhotos()
	srv := newRecipeTestServer(t, func(o *HandlerOptions) {
		o.Service.WithPhotos(photos)
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
		ID: "101", ThumbnailURL: "https://images.pexels.com/photos/101/a.jpeg?h=350", Width: 4000, Height: 3000, Credit: "Ana",
	}) || got.NextPage != 3 {
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

	searcher.err = errors.New("pexels down")
	rec = srv.do(t, http.MethodGet, path+"?q=brownies", "", userAda)
	wantError(t, rec, http.StatusBadGateway, "photo_search_failed")
	if !strings.Contains(rec.Body.String(), "Couldn't search for photos. Try again.") || strings.Contains(rec.Body.String(), "pexels down") {
		t.Errorf("failure body = %s", rec.Body.String())
	}
}

func TestPhotoFromSearchKeepsItLikeAnUpload(t *testing.T) {
	searcher := &fakeSearcher{images: map[string]photosearch.Image{
		"101": {Data: testJPEG, Credit: "Ana"},
		"102": {Data: []byte("<html>not a photo</html>")},
	}}
	srv, photos := newPhotoSearchTestServer(t, searcher)
	path := photosPath(hhAda) + "/from-search"

	rec := srv.do(t, http.MethodPost, path, `{"id":"101"}`, userAda)
	if rec.Code != http.StatusCreated {
		t.Fatalf("from search = %d %s", rec.Code, rec.Body.String())
	}
	imageURL := decodeBody[PhotoUploadResponse](t, rec).ImageURL
	if !IsOwnPhotoURL(imageURL) {
		t.Fatalf("imageUrl %q isn't one of ours", imageURL)
	}
	if len(photos.photos) != 1 {
		t.Fatalf("stored %d photos; want 1", len(photos.photos))
	}
	for _, p := range photos.photos {
		if p.HouseholdID != hhAda || p.ContentType != "image/jpeg" {
			t.Errorf("stored %+v", p)
		}
	}
	// It saves with a recipe like an uploaded photo.
	created := decodeBody[RecipeResponse](t, srv.do(t, http.MethodPost, recipesPath(hhAda), mustJSON(t, DraftResponse{
		Name: "Brownies", ImageURL: imageURL, Ingredients: []DraftIngredientWire{{Name: "cocoa", Quantity: "1", Unit: "cup"}}, Steps: []string{"Bake."},
	}), userAda))
	if created.ImageURL != imageURL {
		t.Errorf("recipe imageUrl = %q; want %q", created.ImageURL, imageURL)
	}

	for _, tc := range []struct {
		body, code, msg string
		status          int
	}{
		{`{"id":"999"}`, "not_found", "That photo isn't there anymore. Pick another one.", http.StatusNotFound},
		{`{"id":"102"}`, "photo_search_failed", "That photo can't be used. Pick another one.", http.StatusBadGateway},
		{`{"id":""}`, "validation_failed", "Pick a photo first.", http.StatusBadRequest},
	} {
		rec := srv.do(t, http.MethodPost, path, tc.body, userAda)
		wantError(t, rec, tc.status, tc.code)
		if !strings.Contains(rec.Body.String(), tc.msg) {
			t.Errorf("%s body = %s", tc.body, rec.Body.String())
		}
	}
	if len(photos.photos) != 1 {
		t.Errorf("a failed pick stored a photo: %d", len(photos.photos))
	}
	if rec := srv.do(t, http.MethodPost, path, `{"id":"101"}`, userViewer); rec.Code != http.StatusForbidden {
		t.Errorf("viewer = %d", rec.Code)
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
		if !strings.Contains(res.Body.String(), "Photo search isn't available right now.") {
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
