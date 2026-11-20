package recipes

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/net/html"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

const frysRecipeURL = "https://www.frysfood.com/r/plant-based-calzones-recipe/62519060-3b1b-4fcd-90ac-b604886fdd81"

func readPageFixture(t *testing.T, name string) string {
	t.Helper()
	page, err := os.ReadFile(filepath.Join("testdata", "pages", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(page)
}

// The marked-lists reader reads everything a rendered store page shows, even
// what linkDraft then drops (decision 670), so a later policy change has it.
func TestMarkedListsReadTheWholeRenderedPage(t *testing.T) {
	root, err := html.Parse(strings.NewReader(readPageFixture(t, "kroger-rendered.html")))
	if err != nil {
		t.Fatal(err)
	}
	d, ok := recipeFromMarkedLists(root)
	if !ok {
		t.Fatal("the rendered page read as no recipe")
	}
	if d.Name != "Smoky Black Bean Hand Pies" {
		t.Errorf("name = %q; want the h1 without its trailing Recipe", d.Name)
	}
	if d.PrepMinutes != 70 || d.TotalMinutes != 95 {
		t.Errorf("prep %d, total %d; want 70 and 95 from \"1 hour 10 minutes\" and \"1 hour 35 minutes\"", d.PrepMinutes, d.TotalMinutes)
	}
	if len(d.Steps) != 6 {
		t.Fatalf("%d steps, want 6: %q", len(d.Steps), d.Steps)
	}
	if d.Steps[0] != "Stir the warm water and yeast together and let it sit until foamy." {
		t.Errorf("step 1 = %q; want it without its Step 1 label", d.Steps[0])
	}
	for _, step := range d.Steps {
		if strings.HasPrefix(step, "Step") {
			t.Errorf("a step kept its label: %q", step)
		}
	}
	want := []Nutrient{{"Calories", 480, "kcal"}, {"Fat", 16, "g"}, {"Sodium", 910, "mg"}, {"Protein", 19, "g"}}
	if !slices.Equal(d.Nutrition, want) {
		t.Errorf("nutrition = %+v; want %+v", d.Nutrition, want)
	}
	if d.ImageURL != "https://www.frysfood.com/content/v2/binary/image/imageset/hand-pies.jpg" {
		t.Errorf("image = %q; want it made absolute against the recipe's url", d.ImageURL)
	}
	for _, line := range d.Ingredients {
		if strings.Contains(line.RawText, "Not part of this recipe") {
			t.Error("a list item outside the recipe was read as an ingredient")
		}
		if strings.Contains(line.RawText, "⁄") {
			t.Errorf("the fraction slash survived in %q", line.RawText)
		}
	}
}

func TestMarkedListsNeedARecipeScopeOrARecipeTestID(t *testing.T) {
	for _, tc := range []struct {
		name, page string
		ok         bool
	}{
		{"a product's ingredients on a store page", `<h1>Sea Salt Crackers</h1><ul>
			<li data-testid="ProductDetail-ingredients">wheat flour</li><li class="ingredient">salt</li></ul>`, false},
		{"recipe test ids without a scope", `<h1>Quick Slaw Recipe</h1><ul>
			<li data-testid="RecipeDetail-ingredients">2 cups shredded cabbage</li></ul>`, true},
		{"classes inside a Recipe scope", `<div itemscope itemtype="https://schema.org/Recipe"><h2>Quick Slaw</h2><ul>
			<li class="recipe-ingredient">2 cups shredded cabbage</li>
			<li class="recipe-ingredient-group-title">Dressing</li></ul></div>`, true},
		{"no ingredients", `<div itemscope itemtype="https://schema.org/Recipe"><h1>Quick Slaw</h1></div>`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, err := html.Parse(strings.NewReader(tc.page))
			if err != nil {
				t.Fatal(err)
			}
			d, ok := recipeFromMarkedLists(root)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v (%+v)", ok, tc.ok, d)
			}
			if ok && (d.Name != "Quick Slaw" || len(d.Ingredients) != 1) {
				t.Errorf("read %q with %d lines; want Quick Slaw with 1 (a group title isn't a line)", d.Name, len(d.Ingredients))
			}
		})
	}
}

func TestSpokenMinutes(t *testing.T) {
	for in, want := range map[string]int{
		"1 hour 20 minutes": 80, "30 minutes": 30, "2 hrs": 120, "45 mins": 45, "1 hr 5 min": 65, "": 0, "overnight": 0,
	} {
		if got := spokenMinutes(in); got != want {
			t.Errorf("spokenMinutes(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestKrogerFamilySitesNeedABrowser(t *testing.T) {
	for _, host := range []string{
		"www.kroger.com", "kroger.com", "www.frysfood.com", "WWW.RALPHS.COM", "www.kingsoopers.com.",
		"www.smithsfoodanddrug.com", "www.fredmeyer.com", "www.qfc.com", "www.dillons.com", "www.bakersplus.com",
		"www.citymarket.com", "www.gerbes.com", "www.jaycfoods.com", "www.pay-less.com", "www.food4less.com",
		"www.foodsco.net", "www.marianos.com", "www.picknsave.com", "www.metromarket.net", "www.harristeeter.com",
	} {
		if !NeedsBrowser(host) {
			t.Errorf("%s isn't treated as browser-only", host)
		}
	}
	for _, host := range []string{"notkroger.com", "kroger.com.example.test", "www.allrecipes.com", "frysfood.co"} {
		if NeedsBrowser(host) {
			t.Errorf("%s is treated as browser-only", host)
		}
	}
}

// The server doesn't fetch a Kroger-family page: it answers right away so the
// app opens the page in its web view (decision 725).
func TestFetchSendsKrogerFamilyPagesToThePhone(t *testing.T) {
	dialed := false
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		dialed = true
		return nil, http.ErrHandlerTimeout
	})}
	start := time.Now()
	_, err := (&WebFetcher{HTTPClient: client}).Fetch(t.Context(), frysRecipeURL)
	fe := fetchErrorOf(t, err)
	if fe.Code != CodePageNeedsPhone || fe.Message != "That site only shows its recipes in a web browser." {
		t.Errorf("error = %+v; want page_needs_phone", fe)
	}
	if dialed || time.Since(start) > time.Second {
		t.Error("the server tried to fetch a page it can't read")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func fetchErrorOf(t *testing.T, err error) *FetchError {
	t.Helper()
	fe, ok := err.(*FetchError)
	if !ok {
		t.Fatalf("error %v (%T) isn't a FetchError", err, err)
	}
	return fe
}

// Each way a page can fail says so in its own plain sentence (decision 726).
func TestFetchSaysWhyAPageCouldNotBeRead(t *testing.T) {
	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
		want    string
	}{
		{"turned away", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) }, msgBlocked},
		{"too many requests", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTooManyRequests) }, msgBlocked},
		{"missing", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) }, msgNotLoaded},
		{"broken", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) }, msgNotLoaded},
		{"not a page", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write([]byte("%PDF"))
		}, msgNotPage},
		{"no recipe", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(readPageFixture(t, "kroger-shell.html")))
		}, msgNoRecipe},
		{"connection dropped", func(w http.ResponseWriter, _ *http.Request) {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close()
			}
		}, msgNotLoaded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()
			_, err := (&WebFetcher{HTTPClient: srv.Client(), AllowPrivateHosts: true}).Fetch(t.Context(), srv.URL)
			fe := fetchErrorOf(t, err)
			if fe.Code != CodeFetchFailed || fe.Message != tc.want {
				t.Errorf("error = %+v; want fetch_failed %q", fe, tc.want)
			}
		})
	}
	// Refused before any request.
	for raw, want := range map[string]string{
		"http://10.0.0.5/":                 msgNotPublic,
		"http://localhost:8080/nope":       msgNotPublic,
		"https://intranet/":                msgNotAddress,
		"ftp://example.com/":               "Only links that start with http or https can be read.",
		"https://user:secret@example.com/": "Links with a username or password in them can’t be read.",
	} {
		_, err := (&WebFetcher{}).Fetch(t.Context(), raw)
		if fe := fetchErrorOf(t, err); fe.Message != want {
			t.Errorf("%s: %q, want %q", raw, fe.Message, want)
		}
	}
	// Every message is a sentence a person would say.
	for _, msg := range []string{msgNoRecipe, msgBlocked, msgNotLoaded, msgNeedsPhone, msgNotAddress, msgNotPublic, msgNotPage} {
		if !strings.HasSuffix(msg, ".") || strings.ToUpper(msg[:1]) != msg[:1] || strings.Contains(msg, "—") {
			t.Errorf("%q doesn't read as a sentence", msg)
		}
	}
}

func TestTransportErrorsThatMeanTheSiteTurnedUsAway(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{errString("http2: stream error: stream ID 1; INTERNAL_ERROR; received from peer"), msgBlocked},
		{errString("read tcp: connection reset by peer"), msgNotLoaded},
		{errString("dial tcp: lookup nope.invalid: no such host"), msgNotLoaded},
		{errBlockedAddress, msgNotPublic},
	} {
		if got := fetchErrorOf(t, transportFetchError(tc.err)).Message; got != tc.want {
			t.Errorf("%v: %q, want %q", tc.err, got, tc.want)
		}
	}
}

type errString string

func (e errString) Error() string { return string(e) }

// The Fry's link from the bug report, through the parse endpoint.
func TestParseEndpointReadsAKrogerFamilyPage(t *testing.T) {
	// A page the phone sends is larger than the tests' 1 KB body limit, as it is
	// in production (about 1 MB).
	store := newMemoryStore()
	r := chi.NewRouter()
	r.Use(httpx.LimitBody(1 << 20))
	r.Route("/api/v1", NewHandler(HandlerOptions{
		Service: NewService(store), Authorizer: testAuthorizer, Tokens: fakeTokens{}, ImportMaxBytes: 64 << 10,
	}).Mount)
	s := &recipeTestServer{router: r, store: store}
	path := recipesPath(hhAda) + "/parse"

	// Without the page: the server answers right away that the phone must load it.
	rec := s.do(t, http.MethodPost, path, mustJSON(t, ParseRecipeRequest{URL: frysRecipeURL}), userAda)
	wantError(t, rec, http.StatusUnprocessableEntity, CodePageNeedsPhone)
	if !strings.Contains(rec.Body.String(), "That site only shows its recipes in a web browser.") {
		t.Errorf("body = %s; want the plain message", rec.Body.String())
	}

	// The page as it's sent, before its scripts run: no recipe in it.
	rec = s.do(t, http.MethodPost, path, mustJSON(t, ParseRecipeRequest{URL: frysRecipeURL, HTML: readPageFixture(t, "kroger-shell.html")}), userAda)
	wantError(t, rec, http.StatusUnprocessableEntity, CodeFetchFailed)
	if !strings.Contains(rec.Body.String(), "We couldn’t find a recipe on that page.") {
		t.Errorf("body = %s; want the plain message", rec.Body.String())
	}

	// The page as the phone's web view rendered it: the recipe, kept to what a page may give.
	rec = s.do(t, http.MethodPost, path, mustJSON(t, ParseRecipeRequest{URL: frysRecipeURL, HTML: readPageFixture(t, "kroger-rendered.html")}), userAda)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	resp := decodeBody[DraftResponse](t, rec)
	if resp.Name != "Smoky Black Bean Hand Pies" || len(resp.Ingredients) != 12 || !resp.FromURL || resp.SourceURL != frysRecipeURL {
		t.Fatalf("draft = %+v", resp)
	}
	if resp.Ingredients[0].Quantity != "2/3" || resp.Ingredients[0].Unit != "cup" || resp.Ingredients[0].Name != "warm water" {
		t.Errorf("first line = %+v", resp.Ingredients[0])
	}
	if len(resp.Steps) != 0 || resp.ImageURL != "" || len(resp.Nutrition) != 0 || resp.PrepMinutes != 0 || resp.Description != "" {
		t.Errorf("a page's recipe kept more than its title, servings, and lines (decision 670): %+v", resp)
	}
	if len(s.store.recipes) != 0 {
		t.Error("parsing stored a recipe")
	}
}

// Write Your Own after a failed import keeps the link as the recipe's source.
func TestATypedRecipeKeepsTheLinkItCameFrom(t *testing.T) {
	s := newRecipeTestServer(t, nil)
	rec := s.do(t, http.MethodPost, recipesPath(hhAda), mustJSON(t, DraftResponse{
		Name: "Hand Pies", SourceURL: frysRecipeURL, Ingredients: []DraftIngredientWire{{Name: "black beans", Quantity: "1", Unit: "can"}},
		Steps: []string{"Bake."},
	}), userAda)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	resp := decodeBody[RecipeResponse](t, rec)
	if resp.Source != SourceManual || resp.SourceURL != frysRecipeURL || len(resp.Steps) != 1 {
		t.Errorf("stored %+v; want a typed recipe with its steps and the link it came from", resp)
	}
}
