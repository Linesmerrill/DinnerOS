package planning

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

const (
	recipePorkTacos = "66e5a1f2c3b4a5d6e7f81005"
	ingSouthwest    = "66e5a1f2c3b4a5d6e7f82010"
	ingTomatoPaste  = "66e5a1f2c3b4a5d6e7f82011"
)

// fakeSpecialties is a SpecialtySource with fixed specialties.
type fakeSpecialties struct {
	specs grocery.Specialties
	err   error
	lines []grocery.Line
}

func (f *fakeSpecialties) GrocerySpecialties(_ context.Context, _ string, lines []grocery.Line) (grocery.Specialties, error) {
	f.lines = lines
	return f.specs, f.err
}

func qty(s string) *ingredients.Quantity {
	q, err := ingredients.ParseQuantity(s)
	if err != nil {
		panic(err)
	}
	return &q
}

func porkTacosRecipe() recipes.Recipe {
	return recipes.Recipe{
		ID: recipePorkTacos, HouseholdID: hhAda, Name: "Smoky Pork Tacos", Servings: []int{2},
		Ingredients: []recipes.RecipeIngredient{
			ingredientLine("", "Tex-Mex Paste", "condiments", false, amt(2, "1", "count")),
			ingredientLine(ingSouthwest, "Southwest Spice Blend", "spices", false, amt(2, "1", "tbsp")),
			ingredientLine("", "Sweet Soy Glaze", "condiments", false, amt(2, "2", "tbsp")),
			ingredientLine(ingOnion, "Yellow Onion", "produce", false, amt(2, "1", "count")),
		},
	}
}

func testSpecialties() grocery.Specialties {
	return grocery.Specialties{
		"name:tex mex paste": {
			ID: "tex-mex-paste", Key: "tex mex paste", Name: "Tex-Mex Paste",
			UnitSizes: []grocery.UnitSize{{Unit: "count", Quantity: *qty("2"), SizeUnit: "tbsp"}},
			Choice: &grocery.Choice{
				Type: grocery.ChoiceStoreAlternative, OptionID: "tex-mex-paste.store", OptionName: "Tomato paste and chili spices",
				Per:        grocery.Measure{Quantity: *qty("1"), Unit: "tbsp"},
				Components: []grocery.Component{{IngredientKey: ingTomatoPaste, Name: "Tomato Paste", Category: "condiments", Quantity: qty("2"), Unit: "tsp"}},
			},
		},
		ingSouthwest: {
			ID: "southwest-spice-blend", Key: "southwest spice blend", Name: "Southwest Spice Blend",
			Choice: &grocery.Choice{
				Type: grocery.ChoiceHouseMadeBatch, OptionID: "southwest-spice-blend.batch", OptionName: "Southwest spice blend (house blend)",
				Yield: grocery.Measure{Quantity: *qty("12"), Unit: "tbsp"}, PantryKey: "name:southwest spice blend",
				Components: []grocery.Component{{IngredientKey: "name:ground cumin", Name: "Ground Cumin", Category: "spices", Quantity: qty("2"), Unit: "tbsp"}},
			},
		},
		"name:sweet soy glaze": {
			ID: "sweet-soy-glaze", Key: "sweet soy glaze", Name: "Sweet Soy Glaze",
			Suggestions: []grocery.OptionRef{{ID: "sweet-soy-glaze.store", Type: grocery.ChoiceStoreAlternative, Name: "Soy and honey", IsDefault: true}},
		},
	}
}

func TestGroceryListAppliesSpecialties(t *testing.T) {
	ctx := context.Background()
	svc, reader := newTestService(t, newMemoryStore())
	reader.put(porkTacosRecipe())
	source := &fakeSpecialties{specs: testSpecialties()}
	if svc.WithSpecialties(source) != svc {
		t.Fatal("WithSpecialties must return the service it configures")
	}
	mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipePorkTacos, Servings: 2})

	g, err := svc.GroceryList(ctx, hhAda, testWeek)
	if err != nil {
		t.Fatalf("GroceryList() error = %v", err)
	}
	if !g.SpecialtiesApplied || len(source.lines) != 4 || len(g.Batches) != 1 {
		t.Fatalf("list = %+v, lines %d", g, len(source.lines))
	}
	data, err := json.Marshal(newGroceryListResponse(g))
	if err != nil {
		t.Fatal(err)
	}
	var resp GroceryListResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatal(err)
	}
	items := map[string]GroceryItemResponse{}
	for _, c := range resp.Categories {
		for _, it := range c.Items {
			items[it.Name] = it
		}
	}
	if _, ok := items["Tex-Mex Paste"]; ok {
		t.Error("Tex-Mex Paste is still listed")
	}
	// 1 packet = 2 tbsp of paste → 4 tsp of tomato paste.
	paste := items["Tomato Paste"]
	if paste.QuantityText != "4 tsp" || paste.Specialty || len(paste.Via) != 1 || paste.Via[0].Text != "for Tex-Mex Paste in Smoky Pork Tacos" ||
		paste.Via[0].Kind != grocery.ViaStoreAlternative || paste.Via[0].Yield != nil || paste.Via[0].Batches != nil {
		t.Errorf("tomato paste = %+v", paste)
	}
	cumin := items["Ground Cumin"]
	if cumin.QuantityText != "2 tbsp" || len(cumin.Via) != 1 || cumin.Via[0].Text != "to make Southwest Spice Blend (makes about 12 tbsp)" ||
		cumin.Via[0].Batches == nil || *cumin.Via[0].Batches != 1 || cumin.Via[0].Yield.Text != "12 tbsp" ||
		len(cumin.Recipes) != 1 || cumin.Recipes[0].Name != "Smoky Pork Tacos" {
		t.Errorf("cumin = %+v", cumin)
	}
	glaze := items["Sweet Soy Glaze"]
	if !glaze.Specialty || glaze.SpecialtyDetail == nil || glaze.SpecialtyDetail.ChoiceType != nil || len(glaze.SpecialtyDetail.SuggestedOptions) != 1 ||
		!glaze.SpecialtyDetail.SuggestedOptions[0].IsDefault || !strings.HasPrefix(glaze.SpecialtyDetail.Text, "Specialty ingredient: choose") {
		t.Errorf("glaze = %+v detail %+v", glaze, glaze.SpecialtyDetail)
	}
	if onion := items["Yellow Onion"]; onion.Specialty || onion.SpecialtyDetail != nil || onion.Via == nil || len(onion.Via) != 0 {
		t.Errorf("onion = %+v", onion)
	}
	b := resp.Batches[0]
	if b.SpecialtyID != "southwest-spice-blend" || b.Status != grocery.BatchMake || b.Reason != grocery.BatchMissing || b.Batches != 1 ||
		b.PantryItemID != nil || b.Needed == nil || b.Needed.Text != "1 tbsp" || b.Text != "Make a batch (makes about 12 tbsp)" ||
		!slices.Equal([]string{b.Recipes[0].Name}, []string{"Smoky Pork Tacos"}) {
		t.Errorf("batch = %+v", b)
	}

	// A batch in the pantry keeps the line, marked house-made.
	specs := testSpecialties()
	specs[ingSouthwest].Choice.Stock = &grocery.BatchStock{ItemID: "item-1", Status: "in_stock", Remaining: qty("9"), RemainingUnit: "tbsp"}
	source.specs = specs
	g, err = svc.GroceryList(ctx, hhAda, testWeek)
	if err != nil {
		t.Fatal(err)
	}
	resp = newGroceryListResponse(g)
	var blend *GroceryItemResponse
	for _, c := range resp.Categories {
		for i := range c.Items {
			if c.Items[i].Name == "Southwest Spice Blend" {
				blend = &c.Items[i]
			}
		}
	}
	if blend == nil || blend.SpecialtyDetail == nil || !blend.SpecialtyDetail.HouseMade || blend.SpecialtyDetail.Text != "In pantry (house-made)" ||
		blend.IngredientKey != "name:southwest spice blend" {
		t.Errorf("house-made item = %+v", blend)
	}
	if rb := resp.Batches[0]; rb.Status != grocery.BatchInPantry || rb.Text != "In pantry (house-made)" || rb.PantryItemID == nil || rb.Remaining.Text != "9 tbsp" {
		t.Errorf("in-pantry batch = %+v", rb)
	}

	// A failure to load specialties fails the list.
	source.err = errors.New("unavailable")
	if _, err := svc.GroceryList(ctx, hhAda, testWeek); !errors.Is(err, source.err) {
		t.Errorf("GroceryList() with a failing source error = %v", err)
	}
}

func TestGroceryListWithoutSpecialties(t *testing.T) {
	svc, reader := newTestService(t, newMemoryStore())
	reader.put(porkTacosRecipe())
	mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipePorkTacos, Servings: 2})
	g, err := svc.GroceryList(context.Background(), hhAda, testWeek)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(newGroceryListResponse(g))
	body := string(data)
	for _, want := range []string{`"specialtiesApplied":false`, `"batches":[]`, `"via":[]`, `"specialty":false`, `"specialtyDetail":null`} {
		if !strings.Contains(body, want) {
			t.Errorf("response lacks %s: %s", want, body)
		}
	}
}
