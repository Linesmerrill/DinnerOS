package recipes

import (
	"errors"
	"net/http"
	"testing"
)

func TestHasBlockedWordMatchesWholeWordsAndPrefixes(t *testing.T) {
	for _, s := range []string{
		"Shit Chili", "a FUCKING good stew", "Bullshit beans", "sh1t sauce", "porn-style nachos",
		"tag: #fuck", "Nazi noodles",
	} {
		if !HasBlockedWord(s) {
			t.Errorf("HasBlockedWord(%q) = false; want true", s)
		}
	}
}

// Kitchen words that look rude, and numbers that leetspeak would read as
// letters, are not blocked.
func TestHasBlockedWordLeavesKitchenWordsAlone(t *testing.T) {
	for _, s := range []string{
		"Spotted dick with custard", "Cock-a-leekie soup", "Faggots and peas", "Rice balls",
		"Chicken breast and thighs", "Prick the potatoes with a fork", "Naked burrito bowl",
		"Retard the dough overnight", "Roast at 455 for 20 minutes", "Shiitake stir-fry",
		"Spicy rapeseed oil dressing", "Scunthorpe pudding", "Pass the asparagus",
		"1/2 cup water", "Bake at 350°F",
	} {
		if HasBlockedWord(s) {
			t.Errorf("HasBlockedWord(%q) = true; want false", s)
		}
	}
}

func TestObjectionableReadsEveryShownField(t *testing.T) {
	clean := Recipe{
		Name: "Tomato Soup", Headline: "Quick", Description: "Warm.", Tags: []string{"easy"},
		Cuisines: []string{"Italian"}, Steps: []Step{{Index: 1, Text: "Simmer."}},
		Ingredients: []RecipeIngredient{{Name: "tomato"}},
	}
	if Objectionable(clean) {
		t.Fatal("a clean recipe is objectionable")
	}
	for name, dirty := range map[string]func(r *Recipe){
		"name":        func(r *Recipe) { r.Name = "Shit Soup" },
		"headline":    func(r *Recipe) { r.Headline = "fuck yes" },
		"description": func(r *Recipe) { r.Description = "bitch please" },
		"step":        func(r *Recipe) { r.Steps = []Step{{Index: 1, Text: "Stir, you bastard."}} },
		"ingredient":  func(r *Recipe) { r.Ingredients = []RecipeIngredient{{Name: "shit"}} },
		"tag":         func(r *Recipe) { r.Tags = []string{"porn"} },
		"cuisine":     func(r *Recipe) { r.Cuisines = []string{"nazi"} },
	} {
		r := clean
		dirty(&r)
		if !Objectionable(r) {
			t.Errorf("a blocked word in the %s was missed", name)
		}
	}
}

// A blocked word keeps a recipe out of the catalog whatever the household
// shares, and a hand share is refused rather than quietly dropped.
func TestBlockedWordsNeverReachTheCatalog(t *testing.T) {
	service, _, publisher := newSharingService(t)
	stored := importOne(t, service, hhAda, SourceManual, "m-1", "Shit Chili")
	if Publishable(Recipe{Name: "Shit Chili", Source: SourceManual, SourceRecipeID: "m-1", SharedToCatalog: true}) {
		t.Error("Publishable accepted a recipe with a blocked word")
	}
	if _, err := service.Share(t.Context(), hhAda, stored.ID, true); !errors.Is(err, ErrObjectionable) {
		t.Fatalf("Share = %v; want ErrObjectionable", err)
	}
	stored.SharedToCatalog = true
	if n, err := service.PublishToCatalog(t.Context(), []Recipe{stored}); err != nil || n != 0 {
		t.Fatalf("PublishToCatalog = %d, %v; want nothing published", n, err)
	}
	if len(publisher.published) != 0 {
		t.Errorf("published %v; want nothing", publisher.names())
	}
	if !sharedUnder(SharingAll, Recipe{Name: "Tomato Soup", Source: SourceManual, SourceRecipeID: "m-2"}) {
		t.Error("sharing everything skipped a clean recipe")
	}
	if sharedUnder(SharingAll, Recipe{Name: "Shit Chili", Source: SourceManual, SourceRecipeID: "m-1"}) {
		t.Error("sharing everything let a blocked word through")
	}
	// Un-sharing is never refused.
	if _, err := service.Share(t.Context(), hhAda, stored.ID, false); err != nil {
		t.Errorf("un-sharing = %v", err)
	}
}

func TestSharingEndpointRefusesBlockedWords(t *testing.T) {
	s := newRecipeTestServer(t, nil)
	created := s.do(t, http.MethodPost, recipesPath(hhAda), mustJSON(t, DraftResponse{
		Name: "Shit Chili", Ingredients: []DraftIngredientWire{{Name: "beans"}},
	}), userAda)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d (%s)", created.Code, created.Body.String())
	}
	id := decodeBody[RecipeResponse](t, created).ID
	rec := s.do(t, http.MethodPut, recipesPath(hhAda)+"/"+id+"/sharing", mustJSON(t, SharingRequest{SharedToCatalog: true}), userAda)
	wantError(t, rec, http.StatusUnprocessableEntity, "objectionable_words")
}

func TestCatalogPublisherKeyIsOpaqueAndPerHousehold(t *testing.T) {
	typed := Recipe{ID: "r1", HouseholdID: hhAda, Source: SourceManual, Name: "Soup"}
	key := CatalogPublisherKey(typed)
	if key == "" || key == hhAda {
		t.Fatalf("CatalogPublisherKey = %q; want an opaque hash", key)
	}
	other := typed
	other.ID = "r2"
	if CatalogPublisherKey(other) != key {
		t.Error("two recipes from one household have different publishers")
	}
	other.HouseholdID = "66e5a1f2c3b4a5d6e7f80002"
	if CatalogPublisherKey(other) == key {
		t.Error("two households share a publisher key")
	}
	if got := CatalogPublisherKey(Recipe{HouseholdID: hhAda, Source: SourceHelloFresh, SourceRecipeID: "hf-1", Name: "Kit"}); got != "" {
		t.Errorf("an importer's recipe has publisher %q; want none", got)
	}
}
