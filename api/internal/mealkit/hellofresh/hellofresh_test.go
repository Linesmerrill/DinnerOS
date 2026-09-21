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
	return New(Options{
		Fetcher:         f,
		BaseURL:         srv.URL,
		RecipeURLPrefix: srv.URL + "/recipes/",
		Now:             func() time.Time { return time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC) },
	})
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

func TestOrderHistoryReadsOnlyTheAccountsOwnDeliveries(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer session-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Query().Get("offset") != "0" {
			_, _ = w.Write([]byte(`{"items":[]}`))
			return
		}
		_, _ = w.Write([]byte(`{"items":[
			{"week":"2026-W30","meals":[
				{"id":"` + recipeIDA + `","name":"Sheet Pan Chicken","slug":"sheet-pan-chicken"},
				{"id":"` + recipeIDB + `","name":"Tacos","slug":"tacos","isAddon":true}
			]},
			{"deliveryDate":"2026-08-10T00:00:00Z","recipes":[
				{"recipeId":"` + recipeIDA + `","name":"Sheet Pan Chicken","slug":"sheet-pan-chicken"},
				{"id":"not-a-recipe-id","name":"Junk"},
				{"id":"` + recipeIDB + `","name":"Elsewhere","websiteUrl":"https://evil.example.com/recipes/x"}
			]}
		]}`))
	}))
	defer srv.Close()

	got, err := newTestClient(t, srv).OrderHistory(context.Background(), mealkit.Tokens{AccessToken: "session-token"})
	if err != nil {
		t.Fatalf("OrderHistory() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("recipes = %+v", got)
	}
	// Repeat deliveries merge into one recipe carrying both weeks.
	if got[0].SourceRecipeID != recipeIDA || len(got[0].Weeks) != 2 {
		t.Errorf("first = %+v", got[0])
	}
	if got[0].Weeks[0] != "2026-W30" || got[0].Weeks[1] != "2026-W33" {
		t.Errorf("weeks = %v", got[0].Weeks)
	}
	// A websiteUrl pointing off HelloFresh is replaced, never followed.
	if !strings.HasPrefix(got[1].URL, srv.URL+"/recipes/") || strings.Contains(got[1].URL, "evil.example.com") {
		t.Errorf("second URL = %q", got[1].URL)
	}
	if !got[1].IsAddon {
		t.Error("the add-on flag was lost")
	}
	// Only the deliveries endpoint was requested: no catalog, no browsing.
	for _, p := range paths {
		if p != deliveryPath {
			t.Errorf("requested %q; only the account's deliveries may be fetched", p)
		}
	}
}

func TestOrderHistoryFailsCleanlyWhenTheResponseChanged(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<html>we redesigned the API</html>`))
	}))
	defer srv.Close()

	_, err := newTestClient(t, srv).OrderHistory(context.Background(), mealkit.Tokens{AccessToken: "t"})
	var parse *mealkit.ParseError
	if !errors.As(err, &parse) {
		t.Fatalf("OrderHistory() = %v, want a ParseError", err)
	}
	if !strings.Contains(parse.Detail, "layout") {
		t.Errorf("detail = %q; it should say what happened", parse.Detail)
	}
	if strings.Contains(parse.Detail, "we redesigned") {
		t.Errorf("the message quotes the fetched page: %q", parse.Detail)
	}
}

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
	recipe, review, err := c.Recipe(context.Background(), mealkit.Tokens{AccessToken: "t"}, ordered)
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
		_, _, err := c.Recipe(context.Background(), mealkit.Tokens{AccessToken: "t"}, ordered)
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
	_, _, err := c.Recipe(context.Background(), mealkit.Tokens{AccessToken: "t"},
		mealkit.OrderedRecipe{SourceRecipeID: recipeIDA, URL: "https://evil.example.com/recipes/x"})
	var parse *mealkit.ParseError
	if !errors.As(err, &parse) {
		t.Fatalf("Recipe() = %v, want a ParseError", err)
	}
}

func TestSignInAndRefreshExchangeTokensWithoutEchoingThePassword(t *testing.T) {
	var grants []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		grants = append(grants, r.Form.Get("grant_type"))
		_, _ = w.Write([]byte(`{"access_token":"a1","refresh_token":"r1","expires_in":3600}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	tokens, err := c.SignIn(context.Background(), "cook@example.com", "hunter2")
	if err != nil {
		t.Fatalf("SignIn() error = %v", err)
	}
	if tokens.AccessToken != "a1" || tokens.RefreshToken != "r1" || tokens.ExpiresAt.IsZero() {
		t.Fatalf("tokens = %+v", tokens)
	}
	if _, err := c.Refresh(context.Background(), tokens); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if len(grants) != 2 || grants[0] != "password" || grants[1] != "refresh_token" {
		t.Errorf("grants = %v", grants)
	}
	if _, err := c.Refresh(context.Background(), mealkit.Tokens{}); !errors.Is(err, mealkit.ErrAuthExpired) {
		t.Errorf("Refresh() without a refresh token = %v", err)
	}
}

func TestSignInReportsRejectedCredentialsAsExpiredAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	if _, err := newTestClient(t, srv).SignIn(context.Background(), "a@b.com", "wrong"); !errors.Is(err, mealkit.ErrAuthExpired) {
		t.Errorf("SignIn() = %v, want ErrAuthExpired", err)
	}
}

func TestISOWeekFallsBackToTheDeliveryDate(t *testing.T) {
	for name, tc := range map[string]struct{ week, date, want string }{
		"given week": {"2026-W05", "", "2026-W05"},
		"rfc3339":    {"", "2026-08-10T00:00:00Z", "2026-W33"},
		"plain date": {"", "2026-08-10", "2026-W33"},
		"bad week":   {"2026-W99", "", ""},
		"nothing":    {"", "", ""},
	} {
		if got := isoWeek(tc.week, tc.date); got != tc.want {
			t.Errorf("%s: isoWeek(%q, %q) = %q, want %q", name, tc.week, tc.date, got, tc.want)
		}
	}
}
