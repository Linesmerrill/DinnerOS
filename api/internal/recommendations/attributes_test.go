package recommendations

import (
	"slices"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

var defaultBands = autopilot.TimeBands{QuickMaxMinutes: 20, MediumMaxMinutes: 35}

func recipeWith(name string, ingredients ...string) recipes.Recipe {
	return recipes.Recipe{ID: "66e5a1f2c3b4a5d6e7f81f01", Name: name, Servings: []int{2}, TotalMinutes: 30, Ingredients: line(ingredients...)}
}

// TestCookTimeBands covers the inconsistent prep and total shapes seen in
// imported catalogs.
func TestCookTimeBands(t *testing.T) {
	for _, tt := range []struct {
		prep, total, want int
		band              string
	}{
		{20, 5, 20, "quick"},
		{30, 10, 30, "medium"},
		{15, 0, 15, "quick"},
		{10, 40, 40, "long"},
		{0, 0, 0, "medium"},
	} {
		r := recipeWith("Dinner", "Rice")
		r.PrepMinutes, r.TotalMinutes = tt.prep, tt.total
		a := attributes(r, nil, defaultBands)
		if a.CookMinutes != tt.want || a.TimeBand != tt.band || a.item(r).CookMinutes != tt.want {
			t.Errorf("prep %d, total %d = %d min (%s); want %d (%s)", tt.prep, tt.total, a.CookMinutes, a.TimeBand, tt.want, tt.band)
		}
	}
	r := recipeWith("Dinner", "Rice")
	r.TotalMinutes = 25
	if got := attributes(r, nil, autopilot.TimeBands{QuickMaxMinutes: 25, MediumMaxMinutes: 40}).TimeBand; got != "quick" {
		t.Errorf("custom bands: 25 min = %s, want quick", got)
	}
}

func TestClassification(t *testing.T) {
	tests := []struct {
		name        string
		recipe      recipes.Recipe
		proteins    []string
		allergens   []string
		diets       []string
		spicy       bool
		spicyReason string
	}{
		{"stock is not a protein, but not vegetarian", recipeWith("Soup", "Onion", "Chicken Stock Concentrate"),
			nil, nil, []string{"gluten-free", "dairy-free"}, false, ""},
		{"chicken", recipeWith("Bowl", "Boneless Chicken Breast", "Rice"),
			[]string{"chicken"}, nil, []string{"gluten-free", "dairy-free"}, false, ""},
		{"sausages", recipeWith("Pasta", "Italian Sausage", "Chicken Sausage", "Penne"),
			[]string{"chicken", "pork"}, []string{"wheat"}, []string{"dairy-free"}, false, ""},
		{"fish sauce is an allergen, not a protein", recipeWith("Noodles", "Fish Sauce", "Rice Noodles", "Lime"),
			nil, []string{"fish"}, []string{"pescatarian", "gluten-free", "dairy-free"}, false, ""},
		{"beans but not green beans", recipeWith("Chili", "Black Beans", "Green Beans", "Eggplant"),
			[]string{"legumes"}, nil, []string{"vegetarian", "pescatarian", "vegan", "gluten-free", "dairy-free"}, false, ""},
		{"vegan curry", recipeWith("Curry", "Chickpeas", "Coconut Milk", "Curry Paste", "Jasmine Rice"),
			[]string{"legumes"}, nil, []string{"vegetarian", "pescatarian", "vegan", "gluten-free", "dairy-free"}, false, ""},
		{"salmon", recipeWith("Salmon", "Salmon Fillet", "Soy Sauce"),
			[]string{"fish"}, []string{"fish", "wheat", "soy"}, []string{"pescatarian", "dairy-free"}, false, ""},
		{"peanut butter and oyster sauce", recipeWith("Stir Fry", "Peanut Butter", "Oyster Sauce", "Oyster Mushrooms"),
			nil, []string{"shellfish", "peanuts"}, []string{"pescatarian", "gluten-free", "dairy-free"}, false, ""},
		{"dairy and eggs", recipeWith("Carbonara", "Spaghetti", "Parmesan Cheese", "Eggs", "Bacon"),
			[]string{"pork", "eggs"}, []string{"milk", "eggs", "wheat"}, nil, false, ""},
		{"honey is not vegan", recipeWith("Salad", "Honey", "Lettuce"),
			nil, nil, []string{"vegetarian", "pescatarian", "gluten-free", "dairy-free"}, false, ""},
		{"spicy ingredient", recipeWith("Tofu", "Tofu", "Sriracha"),
			[]string{"tofu"}, []string{"soy"}, []string{"vegetarian", "pescatarian", "vegan", "gluten-free", "dairy-free"}, true, "Sriracha"},
		{"mild chili powder", recipeWith("Chili", "Chili Powder", "Tomato"),
			nil, nil, []string{"vegetarian", "pescatarian", "vegan", "gluten-free", "dairy-free"}, false, ""},
		{"no ingredient data", recipeWith("Mystery"), nil, nil, nil, false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := attributes(tt.recipe, nil, defaultBands)
			if !slices.Equal(a.Proteins, tt.proteins) {
				t.Errorf("proteins = %v, want %v", a.Proteins, tt.proteins)
			}
			if !slices.Equal(a.Allergens, tt.allergens) {
				t.Errorf("allergens = %v, want %v", a.Allergens, tt.allergens)
			}
			if !slices.Equal(a.Diets, tt.diets) {
				t.Errorf("diets = %v, want %v", a.Diets, tt.diets)
			}
			if a.Spicy != tt.spicy || a.SpicyEvidence != tt.spicyReason {
				t.Errorf("spicy = %v (%q), want %v (%q)", a.Spicy, a.SpicyEvidence, tt.spicy, tt.spicyReason)
			}
		})
	}

	labeled := recipeWith("Mystery")
	labeled.Allergens = []string{"Milk", "Tree Nuts", "Gluten", "Crustaceans", "Mustard"}
	labeled.Tags = []string{"Vegetarian", "Spicy"}
	a := attributes(labeled, nil, defaultBands)
	if !slices.Equal(a.Allergens, []string{"milk", "shellfish", "tree-nuts", "wheat"}) {
		t.Errorf("labeled allergens = %v", a.Allergens)
	}
	if !slices.Equal(a.Diets, []string{"vegetarian", "pescatarian"}) || a.SpicyEvidence != "Tagged spicy" {
		t.Errorf("tagged diets = %v, spicy %q; a tag asserts a diet when there is no ingredient data", a.Diets, a.SpicyEvidence)
	}
}

func TestCookingMethods(t *testing.T) {
	suits := func(a RecipeAttributes, method string) MethodAttribute {
		for _, m := range a.Methods {
			if m.Method == method {
				return m
			}
		}
		t.Fatalf("method %s missing", method)
		return MethodAttribute{}
	}
	tests := []struct {
		name     string
		recipe   recipes.Recipe
		method   string
		want     bool
		evidence string
	}{
		{"pork tenderloin", recipeWith("Pork", "Pork Tenderloin", "Rub"), "smoker", true, "Pork Tenderloin"},
		{"bone-in thighs", recipeWith("Chicken", "Bone-In Chicken Thighs"), "smoker", true, "Bone-In Chicken Thighs"},
		{"whole chicken", recipeWith("Roast", "Whole Chicken"), "smoker", true, "Whole Chicken"},
		{"baby back ribs", recipeWith("Ribs", "Baby Back Ribs"), "smoker", true, "Baby Back Ribs"},
		{"pork chops", recipeWith("Chops", "Pork Chops"), "smoker", true, "Pork Chops"},
		{"brisket", recipeWith("Brisket", "Beef Brisket"), "smoker", true, "Beef Brisket"},
		{"ground pork", recipeWith("Bowl", "Ground Pork"), "smoker", false, ""},
		{"diced shoulder", recipeWith("Stew", "Diced Pork Shoulder"), "smoker", false, ""},
		{"boneless thighs", recipeWith("Bowl", "Boneless Chicken Thighs"), "smoker", false, ""},
		{"smoked name with chicken", recipeWith("Smoked Chicken Sandwich", "Chicken Breast", "Bun"), "smoker", true, "Tagged or named for smoking"},
		{"smoked salmon is not a smoker meal", recipeWith("Smoked Salmon Bagel", "Smoked Salmon", "Bagel"), "smoker", false, ""},
		{"smoked paprika is not a smoker meal", recipeWith("Paprika Chicken", "Smoked Paprika", "Chicken Breast"), "smoker", false, ""},
		{"grilled name", recipeWith("Grilled Steak Salad", "Steak"), "grill", true, "Named or tagged grilled steak salad"},
		{"air fryer tag", func() recipes.Recipe {
			r := recipeWith("Wings", "Chicken Wings")
			r.Tags = []string{"Air Fryer"}
			return r
		}(), "air-fryer", true, "Named or tagged air fryer"},
		{"slow cooker utensil", func() recipes.Recipe { r := recipeWith("Stew", "Beef"); r.Utensils = []string{"Slow Cooker"}; return r }(), "slow-cooker", true, "Named or tagged slow cooker"},
		{"pressure cooker", recipeWith("Instant Pot Chili", "Beans"), "pressure-cooker", true, "Named or tagged instant pot chili"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := suits(attributes(tt.recipe, nil, defaultBands), tt.method)
			if m.Suits != tt.want || m.HeuristicSuits != tt.want || m.Source != SourceHeuristic || m.Evidence != tt.evidence {
				t.Errorf("%s = %+v, want suits %v with evidence %q", tt.method, m, tt.want, tt.evidence)
			}
		})
	}

	r := recipeWith("Pork", "Pork Tenderloin")
	override := &RecipeOverride{Methods: map[string]bool{"smoker": false, "grill": true}}
	a := attributes(r, override, defaultBands)
	if m := suits(a, "smoker"); m.Suits || !m.HeuristicSuits || m.Source != SourceOverride {
		t.Errorf("overridden smoker = %+v", m)
	}
	if m := suits(a, "grill"); !m.Suits || m.HeuristicSuits || m.Source != SourceOverride {
		t.Errorf("overridden grill = %+v", m)
	}
	if item := a.item(r); !slices.Equal(item.Methods, []string{"grill"}) || !slices.Equal(item.Proteins, []string{"pork"}) {
		t.Errorf("item = %+v; overrides decide the provider's methods", item)
	}
}

func TestVocabulary(t *testing.T) {
	v := buildVocabulary(testRecipes())
	if v.CatalogRecipes != 11 {
		t.Errorf("catalog recipes = %d, want the 11 main meals (add-ons don't count)", v.CatalogRecipes)
	}
	find := func(options []Option, value string) (Option, bool) {
		i := slices.IndexFunc(options, func(o Option) bool { return o.Value == value })
		if i < 0 {
			return Option{}, false
		}
		return options[i], true
	}
	if o, ok := find(v.Cuisines, "north american"); !ok || o.RecipeCount != 4 || o.Label != "North American" || v.Cuisines[0].Value != "north american" {
		t.Errorf("north american = %+v; American, Southern, and Tex-Mex recipes count for their region, most used first: %+v", o, v.Cuisines[:3])
	}
	if o, ok := find(v.Cuisines, "american"); ok {
		t.Errorf("american = %+v; it's an alias of north american", o)
	}
	if o, ok := find(v.Cuisines, "thai"); !ok || o.RecipeCount != 0 || o.Label != "Thai" {
		t.Errorf("starter cuisine thai = %+v, %v", o, ok)
	}
	if o, ok := find(v.Tags, "quick"); !ok || o.RecipeCount != 1 {
		t.Errorf("tag quick = %+v, %v", o, ok)
	}
	if o, _ := find(v.Proteins, "beef"); o.RecipeCount != 3 {
		t.Errorf("beef = %+v; tacos, burgers, and the other household's chili are counted only for this catalog", o)
	}
	if len(v.Diets) != len(DietOptions) || len(v.Equipment) != len(EquipmentOptions) || len(v.Days) != 7 {
		t.Error("fixed lists are missing")
	}
	many := testRecipes()
	for i := range 100 {
		r := recipeWith("Dinner", "Rice")
		r.Cuisines = []string{string(rune('a'+i%26)) + string(rune('a'+i/26))}
		many = append(many, r)
	}
	if got := len(buildVocabulary(many).Cuisines); got != MaxVocabularyOptions {
		t.Errorf("cuisines = %d, want the cap %d", got, MaxVocabularyOptions)
	}
}
