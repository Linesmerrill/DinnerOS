package recommendations

import (
	"context"
	"net/http"
	"slices"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// Names are made up, in the shapes meal-kit menus use.
func TestHeuristicMealCategory(t *testing.T) {
	for _, tt := range []struct {
		name     string
		tags     []string
		cuisines []string
		want     string
	}{
		{name: "Creamy Garlic Spaghetti", want: "pasta"},
		{name: "Chicken Sausage Cavatappi Bolognese", want: "pasta"},
		{name: "Beef Tenderloin over Lemony Spaghetti", want: "pasta"},
		{name: "Creamy Asparagus & Bacon Pasta Bake", want: "pasta"},
		// The last keyword wins: the dish is the head noun.
		{name: "Spicy Beef Taco Rigatoni", want: "pasta"},
		{name: "Chicken & Potato Mushroom Soup", want: "soup"},
		{name: "Homemade Chicken Noodle Soup", want: "soup"},
		{name: "Pearl Pasta e Fagioli Soup", want: "soup"},
		{name: "Creamy Chicken & Potato Stew", want: "soup"},
		{name: "One-Pot Beefy Taco Soup", want: "soup"},
		{name: "One-Pot Cowboy Turkey & Bean Chili", want: "soup"},
		{name: "One-Pot Chicken Sausage Chili Verde", want: "soup"},
		// "Chili" is a flavor unless it ends the name.
		{name: "Sweet Chili Beef & Green Bean Bowls", want: "bowl"},
		{name: "Chili Ginger Pork Noodles", want: "stir-fry"},
		{name: "Chili Chili Bang Bang Chicken", want: ""},
		{name: "Lemony Parmesan Shrimp Salad", want: "salad"},
		{name: "Vegan One-Pan Corn & Bean Taco Salad", want: "salad"},
		{name: "Chicken & Greek Salad Pita Pockets", want: "sandwich"},
		{name: "Ancho BBQ Burgers", want: "sandwich"},
		{name: "Gochujang Chicken Wraps & Creamy Sesame Slaw", want: "sandwich"},
		{name: "One-Pan Santa Fe Pork Tacos", want: "tacos"},
		{name: "Black Bean & Blue Corn Crunch Burritos", want: "tacos"},
		{name: "One-Pan Beef Enchiladas Verdes", want: "tacos"},
		{name: "Saucy Beef Burrito Bowls", want: "bowl"},
		{name: "Indian-Style Chicken Curry", want: "curry"},
		{name: "Curried Turkey Tacos with Mango Salsa", want: "tacos"},
		{name: "Coconut Curry Chicken", want: "curry"},
		{name: "Mexican Chicken & Rice Bowls", want: "bowl"},
		{name: "Sesame Sweet Soy Fried Rice", want: "bowl"},
		{name: "Szechuan Pork Noodle Stir-Fry", want: "stir-fry"},
		{name: "Sweet & Spicy Chicken Lo Mein", want: "stir-fry"},
		{name: "Honey Miso Chicken Ramen", want: "stir-fry"},
		{name: "Chicken & Mushroom Ramen in Dashi Broth", want: "soup"},
		{name: "Margherita Flatbread", want: "pizza"},
		// Nothing matches the name.
		{name: "Brown Sugar Bourbon Pork Chops", want: ""},
		{name: "Meatloaf à la Mom", want: ""},
		// Tags, then cuisines, stand in for a name without a keyword.
		{name: "Nonna's Weeknight Dinner", tags: []string{"Pasta"}, want: "pasta"},
		{name: "Nonna's Weeknight Dinner", tags: []string{"soup-salad"}, want: ""},
		{name: "Fiery Chicken", cuisines: []string{"Mexican"}, want: "tacos"},
		{name: "Fiery Chicken", cuisines: []string{"Italian"}, want: ""},
	} {
		t.Run(tt.name+"/"+tt.want, func(t *testing.T) {
			got, evidence := heuristicMealCategory(recipes.Recipe{Name: tt.name, Tags: tt.tags, Cuisines: tt.cuisines})
			if got != tt.want {
				t.Errorf("heuristicMealCategory(%q) = %q (%s), want %q", tt.name, got, evidence, tt.want)
			}
			if got != "" && evidence == "" {
				t.Errorf("a category needs evidence: %q", tt.name)
			}
		})
	}
}

func TestMealCategoryOverrides(t *testing.T) {
	r := recipes.Recipe{ID: rRigatoni, Name: "Rigatoni"}
	if got := recipeMealCategories(r, nil); !slices.Equal(got, []string{"pasta"}) {
		t.Fatalf("categories = %v", got)
	}
	override := &RecipeOverride{MealCategories: map[string]bool{"pasta": false, "soup": true}}
	if got := recipeMealCategories(r, override); !slices.Equal(got, []string{"soup"}) {
		t.Errorf("overridden categories = %v, want [soup]", got)
	}
	attrs := mealCategoryAttributes(r, override)
	for _, a := range attrs {
		switch a.Category {
		case "pasta":
			if a.Suits || !a.HeuristicSuits || a.Source != SourceOverride || a.Evidence == "" {
				t.Errorf("pasta attribute = %+v", a)
			}
		case "soup":
			if !a.Suits || a.HeuristicSuits || a.Source != SourceOverride {
				t.Errorf("soup attribute = %+v", a)
			}
		default:
			if a.Suits || a.Source != SourceHeuristic {
				t.Errorf("%s attribute = %+v", a.Category, a)
			}
		}
	}
	if len(attrs) != len(MealCategoryOptions) {
		t.Errorf("attributes = %d, want one per category", len(attrs))
	}
	// Add-ons are never in a category.
	if got := recipeMealCategories(recipes.Recipe{Name: "Garlic Bread", IsAddon: true}, nil); got != nil {
		t.Errorf("add-on categories = %v", got)
	}
}

func TestSetRecipeMealCategories(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	a, err := env.svc.SetRecipeOverrides(ctx, hhA, userAda, rSoup, nil, map[string]*bool{"soup": ptr(false), "pasta": ptr(true)})
	if err != nil {
		t.Fatal(err)
	}
	categories := map[string]MealCategoryAttribute{}
	for _, c := range a.MealCategories {
		categories[c.Category] = c
	}
	if categories["soup"].Suits || !categories["pasta"].Suits || categories["pasta"].Source != SourceOverride {
		t.Fatalf("attributes = %+v", a.MealCategories)
	}
	if a.Override == nil || len(a.Override.MealCategories) != 2 {
		t.Fatalf("override = %+v", a.Override)
	}
	recorded := env.events.ofType(events.TypeAutopilotRecipeOverrideUpdated)
	if len(recorded) != 2 {
		t.Fatalf("events = %d, want 2", len(recorded))
	}
	payload := recorded[0].Payload.(events.AutopilotRecipeOverrideUpdated)
	if payload.Category == "" || payload.Method != "" || payload.Value == "" {
		t.Errorf("payload = %+v", payload)
	}

	// Methods and categories live in one override and don't overwrite each
	// other.
	if _, err := env.svc.SetRecipeOverrides(ctx, hhA, userAda, rSoup, map[string]*bool{"grill": ptr(true)}, nil); err != nil {
		t.Fatal(err)
	}
	a, err = env.svc.RecipeAttributes(ctx, hhA, rSoup)
	if err != nil {
		t.Fatal(err)
	}
	if a.Override == nil || len(a.Override.MealCategories) != 2 || !a.Override.Methods["grill"] {
		t.Fatalf("override after a method change = %+v", a.Override)
	}

	// Clearing every override deletes it.
	if _, err := env.svc.SetRecipeOverrides(ctx, hhA, userAda, rSoup, map[string]*bool{"grill": nil}, map[string]*bool{"soup": nil, "pasta": nil}); err != nil {
		t.Fatal(err)
	}
	if a, err = env.svc.RecipeAttributes(ctx, hhA, rSoup); err != nil || a.Override != nil {
		t.Errorf("override after clearing = %+v, %v", a.Override, err)
	}

	if _, err := env.svc.SetRecipeOverrides(ctx, hhA, userAda, rSoup, nil, map[string]*bool{"stew": ptr(true)}); !isInvalid(err) {
		t.Errorf("unknown category = %v, want invalid", err)
	}
	if _, err := env.svc.SetRecipeOverrides(ctx, hhA, userAda, rSoup, nil, nil); !isInvalid(err) {
		t.Errorf("empty override = %v, want invalid", err)
	}
}

func TestMealCategoriesInAttributesAndVocabulary(t *testing.T) {
	router, env := newTestRouter(t)
	rec := do(t, router, http.MethodGet, "/recipes/"+rRigatoni+"/attributes", "", userView)
	wantStatus(t, rec, http.StatusOK, "")
	attrs := decodeBody[AttributesResponse](t, rec)
	if len(attrs.MealCategories) != len(MealCategoryOptions) {
		t.Fatalf("mealCategories = %+v", attrs.MealCategories)
	}
	i := slices.IndexFunc(attrs.MealCategories, func(c MealCategoryJSON) bool { return c.Category == "pasta" })
	if i < 0 || !attrs.MealCategories[i].Suits || attrs.MealCategories[i].Label == "" || attrs.MealCategories[i].Evidence == "" {
		t.Errorf("pasta = %+v", attrs.MealCategories)
	}

	rec = do(t, router, http.MethodPut, "/recipes/"+rRigatoni+"/override", `{"mealCategories": {"soup": true}}`, userAda)
	wantStatus(t, rec, http.StatusOK, "")
	if o := decodeBody[AttributesResponse](t, rec).Override; o == nil || !o.MealCategories["soup"] {
		t.Errorf("override = %+v", o)
	}
	wantStatus(t, do(t, router, http.MethodPut, "/recipes/"+rRigatoni+"/override", `{}`, userAda), http.StatusBadRequest, "validation_failed")

	rec = do(t, router, http.MethodGet, "/vocabulary", "", userView)
	wantStatus(t, rec, http.StatusOK, "")
	vocab := decodeBody[VocabularyResponse](t, rec)
	if len(vocab.MealCategories) != len(MealCategoryOptions) || len(vocab.PairingFrequencies) != 2 || vocab.Limits.MaxPairingRules != MaxPairingRules {
		t.Fatalf("vocabulary = %+v", vocab)
	}
	i = slices.IndexFunc(vocab.MealCategories, func(o OptionJSON) bool { return o.Value == "pasta" })
	if i < 0 || vocab.MealCategories[i].RecipeCount == nil || *vocab.MealCategories[i].RecipeCount != 1 {
		t.Errorf("pasta count = %+v", vocab.MealCategories[i])
	}
	_ = env
}
