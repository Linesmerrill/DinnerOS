package shopping

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/providers"
)

func product(id, name, size string, stock providers.StockLevel) SearchResult {
	r := SearchResult{Product: providers.Product{ProductID: id, Name: name, Size: size, Stock: stock}}
	if a := providers.ParseSize(size); a != nil {
		r.PackageSize = &PackageSize{Quantity: a.Quantity.String(), Unit: a.Unit.Code}
	}
	return r
}

func TestPickSuggestionPrefersTheSameThing(t *testing.T) {
	cases := []struct {
		name    string
		pref    Preference
		terms   SearchTerms
		results []SearchResult
		want    string // product ID, or "" for no pick
	}{
		{
			name: "the saved product's words beat a closer ingredient match",
			pref: Preference{IngredientName: "Long Green Pepper", DisplayName: "Fresh Poblano"},
			results: []SearchResult{
				product("1", "Fresh Green Serrano Peppers", "1 lb", providers.StockHigh),
				product("2", "Macayo Diced Green Poblano Peppers", "7 oz", providers.StockHigh),
				product("3", "Fresh Poblano Peppers", "1 ct", providers.StockHigh),
			},
			want: "3",
		},
		{
			name: "a different kind is no pick, even in stock, when the saved kind is out",
			pref: Preference{IngredientName: "Long Green Pepper", DisplayName: "Fresh Poblano"},
			results: []SearchResult{
				product("1", "Fresh Large Green Bell Pepper", "1 ct", providers.StockHigh),
				product("2", "Fresh Poblano Peppers", "1 ct", providers.StockOut),
			},
			want: "",
		},
		{
			name: "brand words in a long saved name don't block a match",
			pref: Preference{IngredientName: "Dried Rosemary", DisplayName: "McCormick Gourmet Organic Crushed Rosemary, 1 oz"},
			results: []SearchResult{
				product("1", "Simple Truth Organic Crushed Rosemary", "1.07 oz", providers.StockHigh),
			},
			want: "1",
		},
		{
			name: "a product that only mentions the ingredient sinks",
			pref: Preference{IngredientName: "Balsamic Vinegar", DisplayName: "Balsamic Vinegar"},
			results: []SearchResult{
				product("1", "Triscuit Balsamic Vinegar & Basil Whole Grain Wheat Crackers", "8.5 oz", providers.StockHigh),
				product("2", "Pompeian Balsamic Vinegar", "16 oz", providers.StockHigh),
			},
			want: "2",
		},
		{
			name: "two words spelled as one still match",
			pref: Preference{IngredientName: "Panko Breadcrumbs", DisplayName: "Panko Breadcrumbs"},
			results: []SearchResult{
				product("1", "Gorton's Popcorn Shrimp, Breaded with Crunchy Panko Breadcrumbs, Frozen", "14 oz", providers.StockHigh),
				product("2", "Kroger Japanese Style Panko Bread Crumbs", "8 oz", providers.StockHigh),
			},
			want: "2",
		},
		{
			name: "a synonym is the same thing",
			pref: Preference{IngredientName: "Scallions", DisplayName: "Scallions"},
			results: []SearchResult{
				product("1", "Mila Caramelized Scallion Noodles", "5.68 oz", providers.StockLow),
				product("2", "Green Onions", "1 each", providers.StockHigh),
			},
			want: "2",
		},
		{
			name: "nothing that names the ingredient means no pick",
			pref: Preference{IngredientName: "Shawarma Seasoning Blend", DisplayName: "Shawarma Seasoning Blend"},
			results: []SearchResult{
				product("1", "Kroger Cajun Seasoning Blend Shaker", "3.18 oz", providers.StockHigh),
				product("2", "Kroger Chicken Seasoning Blend Shaker", "3 oz", providers.StockHigh),
			},
			want: "",
		},
		{
			name: "out of stock is never the pick",
			pref: Preference{IngredientName: "Zucchini", DisplayName: "Zucchini"},
			results: []SearchResult{
				product("1", "Organic Zucchini Squash", "1 lb", providers.StockOut),
				product("2", "Zucchini Tray", "3 ct", providers.StockHigh),
			},
			want: "2",
		},
		{
			name: "the closest size wins between equals",
			pref: Preference{IngredientName: "Jasmine Rice", DisplayName: "Jasmine Rice", PackageSize: &PackageSize{Quantity: "2", Unit: "lb"}},
			results: []SearchResult{
				product("1", "Jasmine Rice", "10 lb", providers.StockHigh),
				product("2", "Jasmine Rice", "32 oz", providers.StockHigh),
			},
			want: "2",
		},
		{
			name:  "a form the search warns against sinks",
			pref:  Preference{IngredientName: "Garlic", DisplayName: "Garlic"},
			terms: SearchTermsFor("Garlic", ingredients.CategoryProduce),
			results: []SearchResult{
				product("1", "Garlic Powder", "3 oz", providers.StockHigh),
				product("2", "Garlic", "1 ct", providers.StockHigh),
			},
			want: "2",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := pickSuggestion(c.pref, c.terms, c.results)
			id := ""
			if got.Product != nil {
				id = got.Product.ProductID
			}
			if id != c.want {
				t.Errorf("pick = %q, want %q (alternatives %d)", id, c.want, len(got.Alternatives))
			}
			if len(got.Alternatives) > suggestionAlternatives {
				t.Errorf("alternatives = %d", len(got.Alternatives))
			}
		})
	}
	if got := pickSuggestion(Preference{IngredientName: "Lime"}, SearchTerms{}, nil); got.Product != nil || got.Alternatives != nil {
		t.Errorf("no results = %+v", got)
	}
}

func TestSavedProductQuery(t *testing.T) {
	for _, c := range []struct{ name, display, want string }{
		{"Long Green Pepper", "Fresh Poblano", "poblano"},
		{"Onion", "Fresh Yellow Onion", "yellow onion"},
		{"Peanuts", "Planters Lightly Salted Dry Roasted Peanuts, 16 oz", "planters lightly salted dry roasted"},
		{"Soy Sauce", "Soy Sauce", ""},
		{"Lime", "Great Value Fresh 2 lb Bag", ""},
	} {
		if got := savedProductQuery(Preference{IngredientName: c.name, DisplayName: c.display}); got != c.want {
			t.Errorf("savedProductQuery(%q) = %q, want %q", c.display, got, c.want)
		}
	}
}

func TestSizeCloseness(t *testing.T) {
	lb := amountFromSize(&PackageSize{Quantity: "2", Unit: "lb"})
	if got := sizeCloseness(lb, amountFromSize(&PackageSize{Quantity: "32", Unit: "oz"})); got < 1.19 {
		t.Errorf("same size = %v", got)
	}
	if got := sizeCloseness(lb, amountFromSize(&PackageSize{Quantity: "8", Unit: "lb"})); got != 0 {
		t.Errorf("four times = %v", got)
	}
	if got := sizeCloseness(lb, amountFromSize(&PackageSize{Quantity: "1", Unit: "count"})); got != 0 {
		t.Errorf("different kinds = %v", got)
	}
	if sizeCloseness(nil, lb) != 0 {
		t.Error("unknown size scored")
	}
}

// catalogKroger is the fake Kroger with a product search per term.
type catalogKroger struct {
	*fakeKroger
	mu       sync.Mutex
	products map[string][]providers.Product
	searches []string
	fail     map[string]bool
}

func (k *catalogKroger) SearchProducts(_ context.Context, q providers.SearchQuery) ([]providers.Product, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.searches = append(k.searches, q.Term)
	if k.fail[q.Term] {
		return nil, errors.New("store down")
	}
	return k.products[q.Term], nil
}

func (f *fixture) withCatalogKroger(t *testing.T) *catalogKroger {
	t.Helper()
	base := f.withKroger(t)
	k := &catalogKroger{fakeKroger: base, products: map[string][]providers.Product{}, fail: map[string]bool{}}
	svc := NewService(ServiceOptions{
		Store: f.store, Providers: providers.NewRegistry(providers.NewWalmart(providers.WalmartOptions{}), k),
		Grocery: f.plans, Catalog: f.recipes, Pantry: f.pantry, Plans: f.plans, Events: f.events,
		Members: fakeMembers{}, TokenSecret: []byte("a test signing key that is long enough for hmac"),
	})
	svc.now = func() time.Time { return *f.clock }
	f.svc = svc
	return k
}

func TestIntegrationMatchSuggestions(t *testing.T) {
	f := newFixture(t)
	k := f.withCatalogKroger(t)
	ctx := f.ctx
	f.save(t, "Ground Beef", "https://walmart.com/ip/Test-Beef/100000001", &PackageSize{Quantity: "16", Unit: "oz"})
	f.save(t, "Milk", "https://walmart.com/ip/Test-Milk/100000004", nil)
	f.save(t, "Tomato Paste", "https://walmart.com/ip/Test-Paste/100000005", nil)
	price := int64(499)
	for term, products := range map[string][]providers.Product{
		"fresh Ground Beef": {
			{ProductID: "0001111000101", Name: "Beef Jerky Snack", Size: "3 oz", Stock: providers.StockHigh},
			{ProductID: "0001111000102", Name: "Test Ground Beef 80/20", Size: "1 lb", Stock: providers.StockHigh, PriceCents: &price},
		},
		"Tomato Paste": {{ProductID: "0001111000103", Name: "Test Tomato Paste", Size: "6 oz", Stock: providers.StockHigh}},
	} {
		k.products[term] = products
	}

	page, err := f.svc.MatchSuggestions(ctx, testHousehold, "kroger", "walmart", "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 || len(page.Items) != 2 || page.Next == "" {
		t.Fatalf("page = %+v", page)
	}
	if page.Items[0].IngredientName != "Ground Beef" || page.Items[0].Product == nil || page.Items[0].Product.ProductID != "0001111000102" {
		t.Errorf("beef = %+v", page.Items[0])
	}
	if page.Items[0].From.ProductID != "100000001" || page.Items[0].Product.URL == "" {
		t.Errorf("beef source = %+v", page.Items[0])
	}
	if page.Items[1].IngredientName != "Milk" || page.Items[1].Product != nil {
		t.Errorf("milk with no results = %+v", page.Items[1])
	}
	next, err := f.svc.MatchSuggestions(ctx, testHousehold, "kroger", "walmart", page.Next, 2)
	if err != nil || len(next.Items) != 1 || next.Next != "" || next.Items[0].Product.ProductID != "0001111000103" {
		t.Fatalf("next = %+v, %v", next, err)
	}

	// A page again comes from the cache, without searching.
	searched := len(k.searches)
	if _, err := f.svc.MatchSuggestions(ctx, testHousehold, "kroger", "walmart", "", 2); err != nil || len(k.searches) != searched {
		t.Errorf("searches %d → %d, %v", searched, len(k.searches), err)
	}

	// Saving a pick takes it off the list.
	if _, _, err := f.svc.PutPreference(ctx, f.actor, "kroger", f.keys["Ground Beef"], PreferenceInput{ProductID: "0001111000102", DisplayName: "Test Ground Beef 80/20"}); err != nil {
		t.Fatal(err)
	}
	if after, _ := f.svc.MatchSuggestions(ctx, testHousehold, "kroger", "walmart", "", 10); after.Total != 2 {
		t.Errorf("after saving = %+v", after)
	}

	// One failed search leaves that item without a pick; all failing is an error.
	f.svc.suggestions = suggestionCache{}
	k.fail["Tomato Paste"] = true
	partial, err := f.svc.MatchSuggestions(ctx, testHousehold, "kroger", "walmart", "", 10)
	if err != nil || len(partial.Items) != 2 || partial.Items[1].Product != nil {
		t.Errorf("partial = %+v, %v", partial, err)
	}
	f.svc.suggestions = suggestionCache{}
	k.fail["Milk"] = true
	if _, err := f.svc.MatchSuggestions(ctx, testHousehold, "kroger", "walmart", "", 10); err == nil {
		t.Error("every search failing was not an error")
	}

	var v *ValidationError
	if _, err := f.svc.MatchSuggestions(ctx, testHousehold, "walmart", "walmart", "", 10); !errors.As(err, &v) {
		t.Errorf("same store = %v", err)
	}
	if _, err := f.svc.MatchSuggestions(ctx, testHousehold, "kroger", "target", "", 10); !errors.Is(err, ErrUnknownProvider) {
		t.Errorf("unknown source = %v", err)
	}
}

func TestIntegrationMatchSuggestionsHTTP(t *testing.T) {
	f := newFixture(t)
	k := f.withCatalogKroger(t)
	f.save(t, "Tomato Paste", "https://walmart.com/ip/Test-Paste/100000005", &PackageSize{Quantity: "6", Unit: "oz"})
	k.products["Tomato Paste"] = []providers.Product{{ProductID: "0001111000103", Name: "Test Tomato Paste", Size: "6 oz", Stock: providers.StockHigh}}
	r := chi.NewRouter()
	r.Route("/api/v1", NewHandler(HandlerOptions{
		Service: f.svc, Pantry: f.pantry, Tokens: fakeTokens{},
		Authorizer: fakeAuthorizer{testHousehold + "/" + testUser: households.RoleMember},
		Households: fakeHouseholds{testUser: {testHousehold}},
	}).Mount)
	get := func(query string) (int, map[string]any) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/households/"+testHousehold+"/shopping/kroger/suggestions"+query, nil)
		req.Header.Set("Authorization", "Bearer token-"+testUser)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	code, body := get("?from=walmart&limit=5")
	if code != http.StatusOK || body["total"] != float64(1) || body["next"] != nil {
		t.Fatalf("GET = %d %v", code, body)
	}
	item := body["items"].([]any)[0].(map[string]any)
	if item["ingredientName"] != "Tomato Paste" || item["product"].(map[string]any)["productId"] != "0001111000103" ||
		item["from"].(map[string]any)["provider"] != "walmart" || item["alternatives"] == nil {
		t.Errorf("item = %v", item)
	}
	if code, _ := get(""); code != http.StatusOK {
		t.Errorf("default from = %d", code)
	}
	if code, _ := get("?limit=0"); code != http.StatusBadRequest {
		t.Errorf("limit 0 = %d", code)
	}
	if !strings.Contains(strings.Join(k.searches, ","), "Tomato Paste") {
		t.Errorf("searches = %v", k.searches)
	}
}
