package photosearch

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeUnsplash struct {
	mu        sync.Mutex
	searches  []url.Values
	downloads []string
	auth      []string
	host      string
}

func (f *fakeUnsplash) photo(id, imageHost string) string {
	return fmt.Sprintf(`{"id":%q,"width":4000,"height":3000,
		"urls":{"regular":"http://%s/photo-%s?ixid=abc&w=1080","small":"http://%s/photo-%s?ixid=abc&w=400"},
		"links":{"download_location":"http://%s/photos/%s/download?ixid=abc"},
		"user":{"name":"Ana  Lima","username":"ana","links":{"html":"https://unsplash.com/@ana"}}}`,
		id, imageHost, id, imageHost, id, f.host, id)
}

func newFakeUnsplash(t *testing.T) (*fakeUnsplash, *Unsplash) {
	t.Helper()
	f := &fakeUnsplash{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.auth = append(f.auth, r.Header.Get("Authorization"))
		f.mu.Unlock()
		const imageHost = "images.test"
		switch {
		case r.URL.Path == "/search/photos":
			f.mu.Lock()
			f.searches = append(f.searches, r.URL.Query())
			f.mu.Unlock()
			// A result with no photographer page is left out.
			fmt.Fprintf(w, `{"total_pages":3,"results":[%s,{"id":"bad","urls":{"small":"http://elsewhere.test/x"},"user":{"name":"X"}}]}`,
				f.photo("abc_123", imageHost))
		case strings.HasSuffix(r.URL.Path, "/download"):
			f.mu.Lock()
			f.downloads = append(f.downloads, r.URL.String())
			f.mu.Unlock()
			fmt.Fprint(w, `{"url":"http://images.test/photo-abc_123"}`)
		case r.URL.Path == "/photos/abc_123":
			fmt.Fprint(w, f.photo("abc_123", imageHost))
		case r.URL.Path == "/photos/elsewhere":
			fmt.Fprint(w, f.photo("elsewhere", "evil.test"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	f.host = strings.TrimPrefix(srv.URL, "http://")
	return f, NewUnsplash(UnsplashOptions{
		AccessKey: "test-key", APIBase: srv.URL, ImageHost: "images.test", ImageScheme: "http",
	})
}

func TestUnsplashSearch(t *testing.T) {
	f, u := newFakeUnsplash(t)
	page, err := u.Search(context.Background(), "  chicken   tacos ", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Photos) != 1 || page.NextPage != 2 {
		t.Fatalf("page = %+v", page)
	}
	got := page.Photos[0]
	want := Result{
		ID: "abc_123", ThumbnailURL: "http://images.test/photo-abc_123?ixid=abc&w=400", Width: 4000, Height: 3000,
		Credit: Credit{Name: "Ana Lima", ProfileURL: "https://unsplash.com/@ana?utm_medium=referral&utm_source=dinneros"},
	}
	if got != want {
		t.Errorf("photo = %+v\nwant  %+v", got, want)
	}
	q := f.searches[0]
	if q.Get("query") != "chicken tacos" || q.Get("per_page") != "30" || q.Get("content_filter") != "high" {
		t.Errorf("query = %v", q)
	}
	if f.auth[0] != "Client-ID test-key" {
		t.Errorf("auth = %q", f.auth[0])
	}
	// The same search again is served from the cache.
	if _, err := u.Search(context.Background(), "Chicken Tacos", 1); err != nil || len(f.searches) != 1 {
		t.Errorf("searched again: %d, %v", len(f.searches), err)
	}
}

func TestUnsplashPickHotlinksAndCallsDownloadLocation(t *testing.T) {
	f, u := newFakeUnsplash(t)
	picked, err := u.Pick(context.Background(), "abc_123")
	if err != nil {
		t.Fatal(err)
	}
	if picked.ImageURL != "http://images.test/photo-abc_123?ixid=abc&w=1080" {
		t.Errorf("image = %q", picked.ImageURL)
	}
	if picked.Credit.Name != "Ana Lima" || !strings.Contains(picked.Credit.ProfileURL, "utm_source=dinneros") {
		t.Errorf("credit = %+v", picked.Credit)
	}
	if len(f.downloads) != 1 || !strings.HasPrefix(f.downloads[0], "/photos/abc_123/download") {
		t.Errorf("downloads = %v", f.downloads)
	}
}

func TestUnsplashPickRefusesOddIDsAndOtherHosts(t *testing.T) {
	f, u := newFakeUnsplash(t)
	if _, err := u.Pick(context.Background(), "../x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("odd id: %v", err)
	}
	if _, err := u.Pick(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing: %v", err)
	}
	if _, err := u.Pick(context.Background(), "elsewhere"); err == nil {
		t.Error("a photo on another host was picked")
	}
	if len(f.downloads) != 0 {
		t.Errorf("a refused pick counted a download: %v", f.downloads)
	}
}

func TestUnsplashCacheExpires(t *testing.T) {
	f, u := newFakeUnsplash(t)
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	u.opts.Now = func() time.Time { return now }
	_, _ = u.Search(context.Background(), "soup", 1)
	now = now.Add(2 * time.Hour)
	_, _ = u.Search(context.Background(), "soup", 1)
	if len(f.searches) != 2 {
		t.Errorf("searches = %d, want 2 after the cache expired", len(f.searches))
	}
}

func TestReferralLinksAndImageHost(t *testing.T) {
	if SourceURL() != "https://unsplash.com/?utm_medium=referral&utm_source=dinneros" {
		t.Errorf("source = %q", SourceURL())
	}
	for raw, want := range map[string]bool{
		"https://images.unsplash.com/photo-1?ixid=a&w=1080": true,
		"http://images.unsplash.com/photo-1":                false,
		"https://images.unsplash.com/":                      false,
		"https://u:p@images.unsplash.com/photo-1":           false,
		"https://images.unsplash.com.evil.test/photo-1":     false,
		"https://plus.unsplash.com/premium_photo-1":         false,
	} {
		if got := IsUnsplashImageURL(raw); got != want {
			t.Errorf("IsUnsplashImageURL(%q) = %v", raw, got)
		}
	}
}

func TestCleanQuery(t *testing.T) {
	if got := CleanQuery("  a \n b  "); got != "a b" {
		t.Errorf("got %q", got)
	}
	if got := CleanQuery(strings.Repeat("é", 150)); len([]rune(got)) != maxQueryRunes {
		t.Errorf("long query kept %d runes", len([]rune(got)))
	}
}
