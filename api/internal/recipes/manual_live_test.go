package recipes

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// TestLiveImport reads real recipe pages over the network, the way the
// server does in production. It is off unless DINNEROS_LIVE_IMPORT=1, so CI
// never runs it; scripts/live-import-check.sh turns it on (docs/development.md).
//
//   - DINNEROS_LIVE_IMPORT_URLS: links to read, separated by spaces. The
//     default is the Fry's recipe from build 115's bug report.
//   - DINNEROS_LIVE_IMPORT_HTML: a page saved from a browser after it
//     rendered (Safari's Web Inspector, Copy outerHTML of <html>), read the way
//     the server reads what the phone's web view sends.
func TestLiveImport(t *testing.T) {
	if os.Getenv("DINNEROS_LIVE_IMPORT") != "1" {
		t.Skip("live import check: set DINNEROS_LIVE_IMPORT=1 (scripts/live-import-check.sh)")
	}
	links := strings.Fields(os.Getenv("DINNEROS_LIVE_IMPORT_URLS"))
	if len(links) == 0 {
		links = []string{frysRecipeURL}
	}
	for _, link := range links {
		t.Run(link, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			start := time.Now()
			d, err := (&WebFetcher{}).Fetch(ctx, link)
			elapsed := time.Since(start).Round(time.Millisecond)
			u, _ := url.Parse(link)
			var fe *FetchError
			switch {
			case u != nil && NeedsBrowser(u.Hostname()):
				// The server never fetches these; the phone's web view does.
				if !errors.As(err, &fe) || fe.Code != CodePageNeedsPhone {
					t.Fatalf("want page_needs_phone, got %v", err)
				}
				t.Logf("sent to the phone in %s: %q", elapsed, fe.Message)
			case errors.As(err, &fe):
				t.Fatalf("%s after %s: %q", fe.Code, elapsed, fe.Message)
			case err != nil:
				t.Fatalf("after %s: %v", elapsed, err)
			default:
				t.Logf("read in %s: %q, %d servings, %d ingredients", elapsed, d.Name, d.Servings, len(d.Ingredients))
				if d.Name == "" || len(d.Ingredients) < 2 {
					t.Errorf("read %q with %d ingredients; want a name and at least 2", d.Name, len(d.Ingredients))
				}
			}
		})
	}
	if path := os.Getenv("DINNEROS_LIVE_IMPORT_HTML"); path != "" {
		page, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		d, method, ok := ParsePage(string(page))
		if !ok {
			t.Fatalf("%s: no recipe found", path)
		}
		t.Logf("%s: %q by %s, %d servings, %d ingredients", path, d.Name, method, d.Servings, len(d.Ingredients))
		for _, line := range d.Ingredients {
			t.Logf("  %q -> %s %s %s (%s)", line.RawText, line.Quantity, line.Unit, line.Name, line.Note)
		}
		if len(d.Ingredients) < 2 {
			t.Errorf("only %d ingredients", len(d.Ingredients))
		}
	}
}
