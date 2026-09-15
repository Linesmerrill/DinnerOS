package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Fixtures are synthetic; real imported data never enters the repository.

func recipePage(name string) string {
	return fmt.Sprintf(`<html><head></head><body>
<script id="__NEXT_DATA__" type="application/json">{"props":{"pageProps":{"ssrPayload":{"recipe":{"id":"abc-en-US","name":%q,"reviews":[{"text":"someone else's review"}],"yields":[{"yields":2,"ingredients":[{"id":"i1","amount":0.5,"unit":"ounce"}]}]}}}}}</script>
</body></html>`, name)
}

func TestExtractRecipe(t *testing.T) {
	raw, err := ExtractRecipe([]byte(recipePage("Synthetic Tacos")))
	if err != nil {
		t.Fatalf("ExtractRecipe() error = %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["name"] != "Synthetic Tacos" {
		t.Errorf("name = %v", got["name"])
	}
	if _, ok := got["reviews"]; ok {
		t.Error("reviews were not dropped")
	}
	if _, ok := got["yields"]; !ok {
		t.Error("yields missing")
	}
}

func TestExtractRecipeErrors(t *testing.T) {
	if _, err := ExtractRecipe([]byte("<html>no data</html>")); !errors.Is(err, ErrNoPageData) {
		t.Errorf("error = %v, want ErrNoPageData", err)
	}
	page := `<script id="__NEXT_DATA__" type="application/json">{"props":{"pageProps":{"ssrPayload":{"recipe":null}}}}</script>`
	if _, err := ExtractRecipe([]byte(page)); !errors.Is(err, ErrNoRecipe) {
		t.Errorf("error = %v, want ErrNoRecipe", err)
	}
}

func TestUniqueRecipesMergesWeeks(t *testing.T) {
	h := History{Weeks: []HistoryWeek{
		{Week: "2026-W02", Meals: []HistoryMeal{{ID: "bbbbbbbbbbbbbbbbbbbbbbbb", Name: "B", WebsiteURL: "u2"}, {ID: "aaaaaaaaaaaaaaaaaaaaaaaa", Name: "A", WebsiteURL: "u1"}}},
		{Week: "2026-W01", Meals: []HistoryMeal{{ID: "aaaaaaaaaaaaaaaaaaaaaaaa", Name: "A", WebsiteURL: "u1"}}},
		{Week: "2026-W01", Meals: []HistoryMeal{{ID: ""}}},
	}}
	got := h.UniqueRecipes()
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].DeliveredID != "aaaaaaaaaaaaaaaaaaaaaaaa" || strings.Join(got[0].Weeks, ",") != "2026-W01,2026-W02" {
		t.Errorf("got[0] = %+v", got[0])
	}
	if strings.Join(got[1].Weeks, ",") != "2026-W02" {
		t.Errorf("got[1].Weeks = %v", got[1].Weeks)
	}
}

func testFetcher(prefix string) *Fetcher {
	f := NewFetcher(slog.New(slog.NewTextHandler(io.Discard, nil)))
	f.AllowedPrefix = prefix
	f.Delay = time.Millisecond
	f.Sleep = func(context.Context, time.Duration) error { return nil }
	f.Now = func() time.Time { return time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC) }
	return f
}

func TestFetchAllIsIdempotentAndRecordsFailures(t *testing.T) {
	var hits atomic.Int32
	var flaky atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch r.URL.Path {
		case "/recipes/ok":
			fmt.Fprint(w, recipePage("OK Recipe"))
		case "/recipes/flaky":
			if flaky.Add(1) == 1 {
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			fmt.Fprint(w, recipePage("Flaky Recipe"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	prefix := srv.URL + "/recipes/"
	recipes := []OrderedRecipe{
		{DeliveredID: "aaaaaaaaaaaaaaaaaaaaaaaa", URL: prefix + "ok"},
		{DeliveredID: "bbbbbbbbbbbbbbbbbbbbbbbb", URL: prefix + "flaky"},
		{DeliveredID: "cccccccccccccccccccccccc", URL: prefix + "missing"},
		{DeliveredID: "../../etc/passwd", URL: prefix + "ok"},
		{DeliveredID: "dddddddddddddddddddddddd", URL: "https://example.com/recipes/x"},
	}
	out := t.TempDir()
	f := testFetcher(prefix)

	stats, err := f.FetchAll(context.Background(), recipes, out)
	if err != nil {
		t.Fatalf("FetchAll() error = %v", err)
	}
	if stats.Fetched != 2 || stats.Skipped != 0 || len(stats.Failures) != 3 {
		t.Fatalf("stats = %+v", stats)
	}

	data, err := os.ReadFile(filepath.Join(out, "recipes", "aaaaaaaaaaaaaaaaaaaaaaaa.json"))
	if err != nil {
		t.Fatal(err)
	}
	var raw RawRecipe
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if raw.DeliveredID != "aaaaaaaaaaaaaaaaaaaaaaaa" || !strings.Contains(string(raw.Recipe), "OK Recipe") {
		t.Errorf("raw = %+v", raw)
	}
	if _, err := os.Stat(filepath.Join(out, "fetch-failures.json")); err != nil {
		t.Errorf("failures file missing: %v", err)
	}

	before := hits.Load()
	stats, err = f.FetchAll(context.Background(), recipes[:2], out)
	if err != nil {
		t.Fatalf("second FetchAll() error = %v", err)
	}
	if stats.Skipped != 2 || stats.Fetched != 0 {
		t.Errorf("second run stats = %+v, want 2 skipped", stats)
	}
	if hits.Load() != before {
		t.Errorf("second run made %d requests, want 0", hits.Load()-before)
	}
}

func TestFetchAllStopsOnForbidden(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	prefix := srv.URL + "/recipes/"
	recipes := []OrderedRecipe{
		{DeliveredID: "aaaaaaaaaaaaaaaaaaaaaaaa", URL: prefix + "a"},
		{DeliveredID: "bbbbbbbbbbbbbbbbbbbbbbbb", URL: prefix + "b"},
	}
	_, err := testFetcher(prefix).FetchAll(context.Background(), recipes, t.TempDir())
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("error = %v, want ErrBlocked", err)
	}
	if hits.Load() != 1 {
		t.Errorf("requests = %d, want 1 (no retries after 403)", hits.Load())
	}
}

func TestFetchAllRespectsLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, recipePage("R"))
	}))
	defer srv.Close()

	prefix := srv.URL + "/recipes/"
	recipes := []OrderedRecipe{
		{DeliveredID: "aaaaaaaaaaaaaaaaaaaaaaaa", URL: prefix + "a"},
		{DeliveredID: "bbbbbbbbbbbbbbbbbbbbbbbb", URL: prefix + "b"},
	}
	f := testFetcher(prefix)
	f.Limit = 1
	stats, err := f.FetchAll(context.Background(), recipes, t.TempDir())
	if err != nil || stats.Fetched != 1 {
		t.Fatalf("stats = %+v err = %v, want 1 fetched", stats, err)
	}
}
