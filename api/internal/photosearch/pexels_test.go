package photosearch

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type fakePexels struct {
	api, images *httptest.Server
	searches    atomic.Int32
	downloads   atomic.Int32
	lastQuery   atomic.Value
	lastImage   atomic.Value
	imageBytes  []byte
}

func newFakePexels(t *testing.T) *fakePexels {
	t.Helper()
	f := &fakePexels{imageBytes: []byte{0xFF, 0xD8, 0xFF, 0xE0, 1, 2, 3}}
	f.images = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.downloads.Add(1)
		f.lastImage.Store(r.URL.String())
		if r.Header.Get("Authorization") != "" {
			t.Error("the key was sent to the image host")
		}
		_, _ = w.Write(f.imageBytes)
	}))
	t.Cleanup(f.images.Close)
	imageHost := strings.TrimPrefix(f.images.URL, "http://")
	f.api = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "test-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.URL.Path == "/v1/search":
			f.searches.Add(1)
			f.lastQuery.Store(r.URL.RawQuery)
			next := `"https://api.pexels.com/v1/search/?page=2"`
			if r.URL.Query().Get("page") == "2" {
				next = `""`
			}
			fmt.Fprintf(w, `{"page":1,"photos":[
			  {"id":101,"width":4000,"height":3000,"photographer":"Ana","src":{"original":"http://%[1]s/photos/101/a.jpeg","medium":"http://%[1]s/photos/101/a.jpeg?h=350"}},
			  {"id":102,"width":10,"height":10,"photographer":"Elsewhere","src":{"original":"http://evil.example/x.jpg","medium":"http://evil.example/x.jpg"}}
			],"next_page":%[2]s}`, imageHost, next)
		case r.URL.Path == "/v1/photos/101":
			fmt.Fprintf(w, `{"id":101,"photographer":"Ana","src":{"original":"http://%s/photos/101/a.jpeg?fm=webp#x"}}`, imageHost)
		case r.URL.Path == "/v1/photos/103":
			fmt.Fprint(w, `{"id":103,"photographer":"Bo","src":{"original":"http://169.254.169.254/latest/meta-data"}}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(f.api.Close)
	return f
}

func (f *fakePexels) client(now func() time.Time) *Pexels {
	return NewPexels(PexelsOptions{
		APIKey: "test-key", APIBase: f.api.URL, ImageScheme: "http",
		ImageHost: strings.TrimPrefix(f.images.URL, "http://"), MaxBytes: 1 << 10, Now: now,
	})
}

func TestSearchReturnsOnlyPhotosOnTheImageHost(t *testing.T) {
	f := newFakePexels(t)
	page, err := f.client(nil).Search(t.Context(), "  No   Bake Cookies ", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Photos) != 1 || page.Photos[0].ID != "101" || page.Photos[0].Credit != "Ana" || page.Photos[0].Width != 4000 {
		t.Fatalf("photos = %+v", page.Photos)
	}
	if !strings.HasPrefix(page.Photos[0].ThumbnailURL, f.images.URL+"/photos/101/") {
		t.Errorf("thumbnail = %q", page.Photos[0].ThumbnailURL)
	}
	if page.NextPage != 2 {
		t.Errorf("next page = %d; want 2", page.NextPage)
	}
	if q, _ := f.lastQuery.Load().(string); !strings.Contains(q, "query=No+Bake+Cookies") || !strings.Contains(q, "per_page=30") {
		t.Errorf("query sent = %q", q)
	}
	last, err := f.client(nil).Search(t.Context(), "cookies", 2)
	if err != nil || last.NextPage != 0 {
		t.Errorf("last page next = %d, %v; want 0", last.NextPage, err)
	}
	if _, err := f.client(nil).Search(t.Context(), "   ", 1); err == nil {
		t.Error("a blank search was sent")
	}
}

func TestSearchPagesAreCachedForAnHour(t *testing.T) {
	f := newFakePexels(t)
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	p := f.client(func() time.Time { return now })
	for _, q := range []string{"brownies", "Brownies", " brownies "} {
		if _, err := p.Search(t.Context(), q, 1); err != nil {
			t.Fatal(err)
		}
	}
	if n := f.searches.Load(); n != 1 {
		t.Errorf("searches sent = %d; want 1", n)
	}
	now = now.Add(61 * time.Minute)
	if _, err := p.Search(t.Context(), "brownies", 1); err != nil {
		t.Fatal(err)
	}
	if n := f.searches.Load(); n != 2 {
		t.Errorf("searches after an hour = %d; want 2", n)
	}
}

func TestSearchCacheStaysBounded(t *testing.T) {
	f := newFakePexels(t)
	p := f.client(nil)
	for i := range cacheEntries + 20 {
		p.store(fmt.Sprint(i), Page{})
	}
	if len(p.cache) > cacheEntries {
		t.Errorf("cache holds %d pages; want at most %d", len(p.cache), cacheEntries)
	}
}

func TestFetchDownloadsFromPexelsOwnAddress(t *testing.T) {
	f := newFakePexels(t)
	img, err := f.client(nil).Fetch(t.Context(), "101")
	if err != nil {
		t.Fatal(err)
	}
	if string(img.Data) != string(f.imageBytes) || img.Credit != "Ana" {
		t.Errorf("image = %+v", img)
	}
	got, _ := f.lastImage.Load().(string)
	if !strings.HasPrefix(got, "/photos/101/a.jpeg?") || !strings.Contains(got, "w=1600") ||
		!strings.Contains(got, "auto=compress") || strings.Contains(got, "fm=webp") {
		t.Errorf("downloaded %q", got)
	}
}

func TestFetchRefusesWhatIsntAPexelsPhoto(t *testing.T) {
	f := newFakePexels(t)
	p := f.client(nil)
	for _, id := range []string{"", "abc", "101/../103", "http://169.254.169.254/", "999"} {
		if _, err := p.Fetch(t.Context(), id); !errors.Is(err, ErrNotFound) {
			t.Errorf("Fetch(%q) = %v; want ErrNotFound", id, err)
		}
	}
	// Pexels' answer points off its image host: nothing is downloaded.
	if _, err := p.Fetch(t.Context(), "103"); err == nil {
		t.Error("a photo off the image host was downloaded")
	}
	if n := f.downloads.Load(); n != 0 {
		t.Errorf("downloads = %d; want 0", n)
	}
}

func TestFetchStopsAtTheSizeLimit(t *testing.T) {
	f := newFakePexels(t)
	f.imageBytes = make([]byte, 2<<10)
	if _, err := f.client(nil).Fetch(t.Context(), "101"); !errors.Is(err, ErrTooBig) {
		t.Errorf("err = %v; want ErrTooBig", err)
	}
}

func TestCleanQuery(t *testing.T) {
	if got := CleanQuery("  a \t b\n"); got != "a b" {
		t.Errorf("CleanQuery = %q", got)
	}
	if got := CleanQuery(strings.Repeat("é", 150)); len([]rune(got)) != maxQueryRunes {
		t.Errorf("long query kept %d runes", len([]rune(got)))
	}
}
