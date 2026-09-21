package hellofresh

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
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

// deliveriesStub answers the two account endpoints the client reads, and
// fails the test for anything else: this client must never browse. prefix is
// the stub's own recipe-page prefix, which is only known once it is listening.
func deliveriesStub(t *testing.T, prefix func() string, seen *[]*http.Request) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		*seen = append(*seen, r)
		if r.Header.Get("Authorization") != "Bearer session-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case plansPath:
			_, _ = w.Write([]byte(`{"items":[{"id":"sub-42","status":"active"}]}`))
		case pastDeliveriesPath:
			switch r.URL.Query().Get("from") {
			case "2026-W39":
				_, _ = w.Write([]byte(`{"weeks":[
					{"week":"2026-W38","menuId":"menu-1","meals":[
						{"id":"` + recipeIDA + `","name":"Sheet Pan Chicken",
						 "websiteURL":"` + prefix() + `sheet-pan-chicken-` + recipeIDA + `"}
					],"addons":[
						{"id":"` + recipeIDB + `","name":"Garlic Bread",
						 "websiteURL":"https://evil.example.com/recipes/garlic-bread"}
					]},
					{"week":"2026-W37","meals":[{"id":"not-a-recipe-id","name":"Junk"}]}
				]}`))
			case "2026-W36":
				// The same recipe again, an earlier week: it merges.
				_, _ = w.Write([]byte(`{"weeks":[{"week":"2026-W33","meals":[
					{"id":"` + recipeIDA + `","name":"Sheet Pan Chicken"}
				]}]}`))
			default:
				_, _ = w.Write([]byte(`{"weeks":[]}`))
			}
		default:
			t.Errorf("requested %q; only the account's own endpoints may be fetched", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func TestOrderHistoryWalksTheAccountsOwnDeliveredWeeks(t *testing.T) {
	var seen []*http.Request
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deliveriesStub(t, func() string { return srv.URL + "/recipes/" }, &seen)(w, r)
	}))
	defer srv.Close()

	got, err := newTestClient(t, srv).OrderHistory(context.Background(), mealkit.Tokens{AccessToken: "session-token"})
	if err != nil {
		t.Fatalf("OrderHistory() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("recipes = %+v", got)
	}
	// Repeat deliveries merge into one recipe carrying every week.
	if got[0].SourceRecipeID != recipeIDA || len(got[0].Weeks) != 2 ||
		got[0].Weeks[0] != "2026-W33" || got[0].Weeks[1] != "2026-W38" {
		t.Errorf("first = %+v", got[0])
	}
	// An add-on was delivered too, and is imported flagged as one.
	if !got[1].IsAddon || got[1].SourceRecipeID != recipeIDB {
		t.Errorf("add-on = %+v", got[1])
	}
	// A websiteURL pointing off HelloFresh is replaced, never followed.
	if !strings.HasPrefix(got[1].URL, srv.URL+"/recipes/") || strings.Contains(got[1].URL, "evil.example.com") {
		t.Errorf("add-on URL = %q", got[1].URL)
	}

	// The subscription is read from the account's own plans, not guessed, and
	// nothing else is requested.
	var deliveries []url.Values
	for i, r := range seen {
		switch r.URL.Path {
		case plansPath:
			if i != 0 {
				t.Errorf("the plan was read at request %d; it is needed first", i)
			}
		case pastDeliveriesPath:
			deliveries = append(deliveries, r.URL.Query())
		}
	}
	// The walk starts at the current ISO week and steps back until a page is
	// empty, so it terminates rather than paging forever.
	if len(deliveries) != 3 {
		t.Fatalf("past-deliveries requests = %d: %v", len(deliveries), deliveries)
	}
	for i, want := range []string{"2026-W39", "2026-W36", "2026-W32"} {
		if got := deliveries[i].Get("from"); got != want {
			t.Errorf("request %d from = %q, want %q", i, got, want)
		}
	}
	for field, want := range map[string]string{
		"country": "US", "locale": "en-US", "rating-scale": "5", "subscription": "sub-42",
	} {
		if got := deliveries[0].Get(field); got != want {
			t.Errorf("%s = %q, want %q", field, got, want)
		}
	}
}

func TestOrderHistoryFailsCleanlyWhenTheResponseChanged(t *testing.T) {
	for name, handler := range map[string]http.HandlerFunc{
		"the plans endpoint": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`<html>we redesigned the API</html>`))
		},
		"the deliveries endpoint": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == plansPath {
				_, _ = w.Write([]byte(`{"items":[{"id":"sub-42"}]}`))
				return
			}
			_, _ = w.Write([]byte(`<html>we redesigned the API</html>`))
		},
	} {
		srv := httptest.NewServer(handler)
		_, err := newTestClient(t, srv).OrderHistory(context.Background(), mealkit.Tokens{AccessToken: "t"})
		var parse *mealkit.ParseError
		switch {
		case !errors.As(err, &parse):
			t.Errorf("%s: OrderHistory() = %v, want a ParseError", name, err)
		case !strings.Contains(parse.Detail, "changed"):
			t.Errorf("%s: detail = %q; it should say what happened", name, parse.Detail)
		case strings.Contains(parse.Detail, "we redesigned"):
			t.Errorf("%s: the message quotes the fetched page: %q", name, parse.Detail)
		}
		srv.Close()
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

func TestRefreshAsksForANewSignInRatherThanGuessingAContract(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("a request was made to %q; this build does not know HelloFresh's refresh request", r.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	for name, tokens := range map[string]mealkit.Tokens{
		"with a refresh token": {AccessToken: "a", RefreshToken: "r"},
		"without one":          {},
	} {
		if _, err := c.Refresh(context.Background(), tokens); !errors.Is(err, mealkit.ErrAuthExpired) {
			t.Errorf("Refresh() %s = %v, want ErrAuthExpired", name, err)
		}
	}
}

// There is no SignIn: the member signs in on HelloFresh's own page and the app
// sends us the session that login produced. This build must not grow a
// password path back.
func TestTheSourceHasNoPasswordPath(t *testing.T) {
	var source any = New(Options{})
	if _, ok := source.(interface {
		SignIn(context.Context, string, string) (mealkit.Tokens, error)
	}); ok {
		t.Error("the HelloFresh client still has a password sign-in")
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

func TestISOWeeksRoundTripAndRejectDatesThatDoNotExist(t *testing.T) {
	for name, tc := range map[string]struct {
		week string
		ok   bool
	}{
		"a plain week":       {"2026-W38", true},
		"the first week":     {"2026-W01", true},
		"a 53-week year":     {"2020-W53", true},
		"no 53rd week":       {"2025-W53", false},
		"not a week at all":  {"2026-W99", false},
		"nothing":            {"", false},
		"a date, not a week": {"2026-08-10", false},
	} {
		monday, ok := parseISOWeek(tc.week)
		if ok != tc.ok {
			t.Errorf("%s: parseISOWeek(%q) ok = %v, want %v", name, tc.week, ok, tc.ok)
			continue
		}
		if !ok {
			continue
		}
		if monday.Weekday() != time.Monday {
			t.Errorf("%s: %v is not a Monday", name, monday)
		}
		if got := isoWeekString(monday); got != tc.week {
			t.Errorf("%s: round trip = %q, want %q", name, got, tc.week)
		}
	}
	// The walk starts from the clock, in the form the endpoint wants.
	if got := isoWeekString(time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)); got != "2026-W39" {
		t.Errorf("isoWeekString(2026-09-21) = %q, want 2026-W39", got)
	}
}
