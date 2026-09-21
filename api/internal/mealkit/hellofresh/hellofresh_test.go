package hellofresh

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/mealkit"
)

// newTestClient points a Client at srv with no waiting.
func newTestClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	f := mealkit.NewFetcher()
	f.MinInterval = 0
	f.Jitter = 0
	f.Sleep = func(context.Context, time.Duration) error { return nil }
	return New(Options{Fetcher: f, BaseURL: srv.URL})
}

const recipeIDA = "6512aa11bb22cc33dd44ee55"
const recipeIDB = "6512aa11bb22cc33dd44ee66"

// pageWith wraps a recipe object in the page shape the client reads.
func pageWith(recipe string) string {
	return `<html><head><script id="__NEXT_DATA__" type="application/json">` +
		`{"props":{"pageProps":{"ssrPayload":{"recipe":` + recipe + `}}}}` +
		`</script></head><body>ignored</body></html>`
}

const fullRecipe = `{
  "name": "Sheet Pan Chicken",
  "headline": "with roasted carrots",
  "description": "A weeknight tray bake.",
  "imagePath": "/image/chicken.jpg",
  "canonicalLink": "%PREFIX%sheet-pan-chicken-` + recipeIDA + `",
  "prepTime": "PT30M",
  "totalTime": "PT45M",
  "difficulty": 2,
  "cuisines": [{"name": "American"}, {"name": "American"}],
  "tags": [{"name": "Quick"}],
  "allergens": [{"name": "Milk"}],
  "nutrition": [{"name": "Energy (kcal)", "amount": 650, "unit": "kcal"}],
  "ingredients": [
    {"id": "ing-1", "name": "Chicken Breasts", "slug": "chicken", "imagePath": "/image/chicken.png", "shipped": true},
    {"id": "ing-2", "name": "Olive Oil", "shipped": false},
    {"id": "ing-3", "name": "Mystery Paste", "shipped": true}
  ],
  "yields": [
    {"yields": 2, "ingredients": [
      {"id": "ing-1", "amount": 12, "unit": "ounce"},
      {"id": "ing-2", "amount": null, "unit": ""},
      {"id": "ing-3", "amount": 1, "unit": "dollop"}
    ]},
    {"yields": 4, "ingredients": [{"id": "ing-1", "amount": 24, "unit": "ounce"}]}
  ],
  "steps": [
    {"index": 2, "instructions": "Roast until golden.", "images": [{"path": "/image/step2.jpg"}]},
    {"index": 1, "instructions": "• Heat the oven.\n• Toss the carrots."}
  ]
}`

func TestRecipeNormalizesIntoTheSharedImportContract(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(pageWith(strings.ReplaceAll(fullRecipe, "%PREFIX%", srv.URL+"/recipes/"))))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	ordered := mealkit.OrderedRecipe{
		SourceRecipeID: recipeIDA, Name: "Sheet Pan Chicken",
		URL: srv.URL + "/recipes/sheet-pan-chicken-" + recipeIDA, Weeks: []string{"2026-W30", "2026-W33"},
	}
	recipe, review, err := c.Recipe(context.Background(), ordered)
	if err != nil {
		t.Fatalf("Recipe() error = %v", err)
	}
	if recipe.Source != mealkit.SourceHelloFresh || recipe.SourceRecipeID != recipeIDA || recipe.Name != "Sheet Pan Chicken" {
		t.Fatalf("recipe identity = %+v", recipe)
	}
	if recipe.PrepMinutes != 30 || recipe.TotalMinutes != 45 {
		t.Errorf("durations = %d/%d", recipe.PrepMinutes, recipe.TotalMinutes)
	}
	if len(recipe.Servings) != 2 || recipe.Servings[0] != 2 || recipe.Servings[1] != 4 {
		t.Errorf("servings = %v", recipe.Servings)
	}
	if len(recipe.OrderWeeks) != 2 {
		t.Errorf("orderWeeks = %v; the weeks the household received it must carry over", recipe.OrderWeeks)
	}
	if len(recipe.Cuisines) != 1 {
		t.Errorf("cuisines = %v, want the repeat removed", recipe.Cuisines)
	}
	// Steps are re-indexed from 1 in source order, bullets one per line.
	if len(recipe.Steps) != 2 || recipe.Steps[0].Index != 1 || recipe.Steps[1].Index != 2 {
		t.Fatalf("steps = %+v", recipe.Steps)
	}
	if recipe.Steps[0].Text != "Heat the oven.\nToss the carrots." {
		t.Errorf("step text = %q", recipe.Steps[0].Text)
	}
	// Amounts map to DinnerOS unit codes; an unknown one is flagged, never
	// guessed, and a missing amount stays missing.
	chicken := recipe.Ingredients[0]
	if chicken.Amounts[0].Unit != "oz" || chicken.Amounts[0].Quantity == nil || *chicken.Amounts[0].Quantity != 12 {
		t.Errorf("chicken amounts = %+v", chicken.Amounts)
	}
	if chicken.Amounts[0].RawText != "12 ounce Chicken Breasts" {
		t.Errorf("rawText = %q", chicken.Amounts[0].RawText)
	}
	if oil := recipe.Ingredients[1]; !oil.PantryStaple || oil.Amounts[0].Quantity != nil {
		t.Errorf("oil = %+v", oil)
	}
	if paste := recipe.Ingredients[2]; paste.Amounts[0].Unit != "" {
		t.Errorf("an unknown unit was guessed: %+v", paste.Amounts[0])
	}
	var flaggedUnit bool
	for _, item := range review {
		if item.Field == "ingredients.Mystery Paste.unit" && item.Value == "dollop" {
			flaggedUnit = true
		}
	}
	if !flaggedUnit {
		t.Errorf("review = %+v, want the unknown unit flagged", review)
	}
}

func TestRecipeFailsCleanlyOnAPageThatChanged(t *testing.T) {
	cases := map[string]string{
		"no embedded data": `<html><body>Recipes moved!</body></html>`,
		"no recipe":        pageWith(`null`),
		"wrong shape":      pageWith(`{"name": 42}`),
		"no name":          pageWith(`{"name": "   "}`),
	}
	for name, page := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(page))
		}))
		c := newTestClient(t, srv)
		ordered := mealkit.OrderedRecipe{SourceRecipeID: recipeIDA, URL: srv.URL + "/recipes/x-" + recipeIDA}
		_, _, err := c.Recipe(context.Background(), ordered)
		var parse *mealkit.ParseError
		if !errors.As(err, &parse) {
			t.Errorf("%s = %v, want a ParseError", name, err)
		} else if strings.Contains(parse.Detail, "Recipes moved") {
			t.Errorf("%s quotes the fetched page: %q", name, parse.Detail)
		}
		srv.Close()
	}
}

func TestRecipeRefusesAURLOutsideHelloFreshsRecipePages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("a request was made for a URL outside the allowed prefix")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, _, err := c.Recipe(context.Background(),
		mealkit.OrderedRecipe{SourceRecipeID: recipeIDA, URL: "https://evil.example.com/recipes/x"})
	var parse *mealkit.ParseError
	if !errors.As(err, &parse) {
		t.Fatalf("Recipe() = %v, want a ParseError", err)
	}
}

func TestNormalizeOrderKeepsOnlyWhatThisBuildWillFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("NormalizeOrder made a request to %q; it must not", r.URL.Path)
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	prefix := srv.URL + "/recipes/"

	t.Run("a good entry survives whole", func(t *testing.T) {
		got, ok := c.NormalizeOrder(mealkit.OrderedRecipe{
			SourceRecipeID: recipeIDA, Name: "Sheet Pan Chicken",
			URL:     prefix + "sheet-pan-chicken-" + recipeIDA,
			Weeks:   []string{"2026-W38", "2026-W33", "2026-W38", "not-a-week"},
			IsAddon: true,
		})
		if !ok {
			t.Fatal("a good entry was refused")
		}
		if got.URL != prefix+"sheet-pan-chicken-"+recipeIDA || !got.IsAddon {
			t.Errorf("entry = %+v", got)
		}
		// Weeks are deduplicated, sorted, and anything that is not an ISO
		// week is dropped rather than repaired.
		if len(got.Weeks) != 2 || got.Weeks[0] != "2026-W33" || got.Weeks[1] != "2026-W38" {
			t.Errorf("weeks = %v", got.Weeks)
		}
	})

	t.Run("a URL pointing elsewhere is rebuilt, never followed", func(t *testing.T) {
		got, ok := c.NormalizeOrder(mealkit.OrderedRecipe{
			SourceRecipeID: recipeIDB, Name: "Garlic Bread",
			URL: "https://evil.example.com/recipes/garlic-bread",
		})
		if !ok {
			t.Fatal("the entry was refused entirely; only its URL was wrong")
		}
		if !strings.HasPrefix(got.URL, prefix) || strings.Contains(got.URL, "evil.example.com") {
			t.Errorf("URL = %q", got.URL)
		}
		if got.URL != prefix+"garlic-bread-"+recipeIDB {
			t.Errorf("URL = %q, want one built from the id and name we validated", got.URL)
		}
	})

	t.Run("anything that is not a recipe id is refused", func(t *testing.T) {
		for name, id := range map[string]string{
			"empty":       "",
			"not hex":     "zzzzzzzzzzzzzzzzzzzzzzzz",
			"too short":   "6512aa11bb22",
			"a path":      "../../etc/passwd",
			"upper case":  strings.ToUpper(recipeIDA),
			"with spaces": recipeIDA[:23] + " 5",
		} {
			if _, ok := c.NormalizeOrder(mealkit.OrderedRecipe{SourceRecipeID: id, Name: "x"}); ok {
				t.Errorf("%s (%q) was accepted", name, id)
			}
		}
	})

	t.Run("a name long enough to be an attack is trimmed to a label", func(t *testing.T) {
		got, ok := c.NormalizeOrder(mealkit.OrderedRecipe{
			SourceRecipeID: recipeIDA, Name: strings.Repeat("a", maxOrderNameBytes*3),
		})
		if !ok {
			t.Fatal("the entry was refused")
		}
		if len(got.Name) > maxOrderNameBytes {
			t.Errorf("name is %d bytes", len(got.Name))
		}
	})

	t.Run("the weeks on one recipe are bounded", func(t *testing.T) {
		weeks := make([]string, mealkit.MaxOrderWeeks*2)
		for i := range weeks {
			weeks[i] = "2026-W" + string(rune('0'+i%10)) + string(rune('0'+(i/10)%10))
		}
		got, ok := c.NormalizeOrder(mealkit.OrderedRecipe{SourceRecipeID: recipeIDA, Weeks: weeks})
		if !ok {
			t.Fatal("the entry was refused")
		}
		if len(got.Weeks) > mealkit.MaxOrderWeeks {
			t.Errorf("weeks = %d, want at most %d", len(got.Weeks), mealkit.MaxOrderWeeks)
		}
	})
}

// The client holds no credential and calls no account endpoint. This test
// fails if a sign-in, a refresh, or an order-history reader is ever added back:
// all of that now happens in the member's own browser session, in the app.
func TestTheSourceHasNoAccountAccessAtAll(t *testing.T) {
	var source any = New(Options{})
	if _, ok := source.(interface {
		SignIn(context.Context, string, string) (any, error)
	}); ok {
		t.Error("the HelloFresh client has a password sign-in again")
	}
	if _, ok := source.(interface {
		Refresh(context.Context, any) (any, error)
	}); ok {
		t.Error("the HelloFresh client has a token refresh again")
	}
	if _, ok := source.(interface {
		OrderHistory(context.Context, any) ([]mealkit.OrderedRecipe, error)
	}); ok {
		t.Error("the HelloFresh client reads the account's order history again")
	}
}

func TestRedactExcerptKeepsShapeAndDropsAnythingTokenish(t *testing.T) {
	body := []byte(`{"access_token":"ya29.SECRETVALUE","refreshToken":"r-SECRET",` +
		`"user":{"country":"US"},"jwt":"eyJhbGciOiJIUzI1NiJ9.payloadpart.sigpart"}`)
	got := redactExcerpt(body)
	for _, secret := range []string{"SECRETVALUE", "r-SECRET", "payloadpart"} {
		if strings.Contains(got, secret) {
			t.Errorf("the excerpt carries %q: %s", secret, got)
		}
	}
	if !strings.Contains(got, `"country"`) {
		t.Errorf("the excerpt dropped the shape that makes it useful: %s", got)
	}
	long := redactExcerpt([]byte(strings.Repeat("a", maxExcerptBytes*2)))
	if len([]rune(long)) > maxExcerptBytes+1 {
		t.Errorf("the excerpt is %d runes; it must be bounded", len([]rune(long)))
	}
}
