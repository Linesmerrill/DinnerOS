package substitutes

import (
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

// TestSeedStoreAlternatives is the shopping audit: a household that never
// wants to make a jar must still be able to cook every curated specialty
// ingredient from a normal grocery store. Every entry needs a store
// alternative, or a note saying what to do when there is no honest one.
func TestSeedStoreAlternatives(t *testing.T) {
	seed, err := LoadSeed()
	if err != nil {
		t.Fatalf("LoadSeed() error = %v", err)
	}
	for _, sp := range seed.Specialties {
		stores := 0
		for _, o := range sp.Options {
			if o.Type == TypeStoreAlternative {
				stores++
			}
		}
		if stores == 0 && sp.Note == "" {
			t.Errorf("%s has no store alternative and no note: a household on the similar strategy has nothing to buy", sp.ID)
		}
		if n := utf8.RuneCountInString(sp.Note); n > MaxNoteLength {
			t.Errorf("%s: note is %d characters, over %d", sp.ID, n, MaxNoteLength)
		}
		// Both strategies must land on something for every curated entry.
		similar := strategyOption(sp, StrategySimilar)
		switch {
		case similar == nil:
			t.Errorf("%s: the similar strategy picks nothing", sp.ID)
		case stores > 0 && similar.Type != TypeStoreAlternative:
			t.Errorf("%s: similar picked a %s though a store alternative exists", sp.ID, similar.Type)
		}
		if closest := strategyOption(sp, StrategyClosest); closest == nil {
			t.Errorf("%s: the closest strategy picks nothing", sp.ID)
		}
		if strategyOption(sp, StrategyAsk) != nil {
			t.Errorf("%s: ask must pick nothing", sp.ID)
		}
	}
}

func TestSlug(t *testing.T) {
	for in, want := range map[string]string{
		"Tex-Mex Paste":                 "tex-mex-paste",
		"Sweet and Smoky BBQ Seasoning": "sweet-and-smoky-bbq-seasoning",
		"Sweet & Smoky BBQ Seasoning":   "sweet-and-smoky-bbq-seasoning",
		"Za'atar Spice":                 "zaatar-spice",
		"  Crème Fraîche ":              "creme-fraiche",
	} {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func toGrocerySizes(t *testing.T, sizes []UnitSize) []grocery.UnitSize {
	t.Helper()
	var out []grocery.UnitSize
	for _, s := range sizes {
		q, err := ingredients.ParseQuantity(s.Quantity)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, grocery.UnitSize{Unit: s.Per, Quantity: q, SizeUnit: s.Unit})
	}
	return out
}

// TestEmbeddedSeed checks the curated data a household gets.
func TestEmbeddedSeed(t *testing.T) {
	seed, err := LoadSeed()
	if err != nil {
		t.Fatalf("LoadSeed() error = %v", err)
	}
	ids := map[string]Specialty{}
	for _, sp := range seed.Specialties {
		ids[sp.ID] = sp
	}
	// The most frequent specialty ingredients that aren't plain store products.
	for _, id := range []string{
		"chicken-stock-concentrate", "sweet-soy-glaze", "southwest-spice-blend", "sweet-thai-chili-sauce", "tex-mex-paste",
		"beef-stock-concentrate", "veggie-stock-concentrate", "ponzu-sauce", "cream-sauce-base", "smoky-red-pepper-crema",
		"fry-seasoning", "szechuan-paste", "tuscan-heat-spice", "mushroom-stock-concentrate", "pork-ramen-stock-concentrate",
		"mexican-spice-blend", "fajita-spice-blend", "shawarma-spice-blend", "bulgogi-sauce", "umami-ginger-sauce",
		"blackening-spice", "brown-sugar-bourbon-seasoning", "sesame-dressing", "miso-sauce-concentrate",
		"cheese-roux-concentrate", "garlic-ginger-scallion-paste", "sweet-and-smoky-bbq-seasoning",
		"tunisian-spice-blend", "cuban-spice-blend",
	} {
		if _, ok := ids[id]; !ok {
			t.Errorf("seed is missing %s", id)
		}
	}
	// Ordinary store products and produce mixes are never flagged.
	keys := map[string]bool{}
	for _, sp := range seed.Specialties {
		keys[sp.Key] = true
		for _, k := range sp.AliasKeys {
			keys[k] = true
		}
	}
	for _, plain := range []string{"hoisin sauce", "bbq sauce", "gochujang sauce", "soy sauce", "tomato paste", "hot sauce", "italian seasoning", "coleslaw mix"} {
		if keys[plain] {
			t.Errorf("%q is a store product and must not be a specialty ingredient", plain)
		}
	}

	brands := []string{"hellofresh", "hello fresh", "blue apron", "home chef", "green chef", "everyplate", "every plate", "marley spoon", "dinnerly", "sunbasket", "gousto"}
	batches := 0
	for _, sp := range seed.Specialties {
		sizes := toGrocerySizes(t, sp.UnitSizes)
		if len(sp.UnitSizes) == 0 {
			t.Errorf("%s has no unit sizes, so recipes that count packets can't convert", sp.ID)
		}
		texts := append([]string{sp.Name}, sp.Aliases...)
		for _, o := range sp.Options {
			texts = append(texts, o.Name, o.Notes)
			texts = append(texts, o.Steps...)
			for _, c := range o.Ingredients {
				texts = append(texts, c.Name)
				ck := ingredients.NormalizeName(c.Name)
				if keys[ck] {
					t.Errorf("%s: ingredient %q is itself a specialty ingredient", o.ID, c.Name)
				}
				if c.Category == "" {
					category, confident := ingredients.Categorize(c.Name)
					if !confident || category == ingredients.CategoryMeatSeafood {
						t.Errorf("%s: ingredient %q categorizes as %s (confident %v); give it a category", o.ID, c.Name, category, confident)
					}
				}
			}
			switch o.Type {
			case TypeStoreAlternative:
				// Packet counts and the recipe's usual units convert to Per.
				for _, from := range append([]string{"tsp", "tbsp"}, sp.UnitSizes[0].Per) {
					if from == "tsp" || from == "tbsp" {
						if u, _ := ingredients.LookupUnit(o.Per.Unit); u.Kind != ingredients.KindVolume && !u.Discrete() {
							continue
						}
					}
					if _, ok := grocery.ConvertMeasure(ingredients.NewQuantity(1, 1).Rat(), from, o.Per.Unit, sizes); !ok {
						t.Errorf("%s: 1 %s doesn't convert to per %s", o.ID, from, o.Per.Unit)
					}
				}
			case TypeHouseMadeBatch:
				batches++
				if len(o.Steps) == 0 {
					t.Errorf("%s: a batch needs steps", o.ID)
				}
				for _, size := range sp.UnitSizes {
					if _, ok := grocery.ConvertMeasure(ingredients.NewQuantity(1, 1).Rat(), size.Per, o.Yield.Unit, sizes); !ok {
						t.Errorf("%s: 1 %s doesn't convert to the yield's %s", o.ID, size.Per, o.Yield.Unit)
					}
				}
			}
		}
		for _, text := range texts {
			lower := strings.ToLower(text)
			for _, b := range brands {
				if strings.Contains(lower, b) {
					t.Errorf("%s: %q names a brand", sp.ID, text)
				}
			}
		}
	}
	if batches < 15 {
		t.Errorf("seed has %d house-made batches, want at least 15", batches)
	}

	again, err := LoadSeed()
	if err != nil || again.Specialties[0].ContentHash != seed.Specialties[0].ContentHash || len(seed.Specialties[0].ContentHash) != 64 {
		t.Errorf("content hash isn't stable: %v", err)
	}
}

func TestParseSeedRejectsBadData(t *testing.T) {
	valid := `{"version": 1, "specialties": [{
		"id": "tex-mex-paste", "name": "Tex-Mex Paste", "aliases": ["Tex Mex Spice Paste"], "category": "condiments",
		"unitSizes": [{"per": "count", "quantity": "2", "unit": "tbsp"}],
		"defaultOptionId": "tex-mex-paste.store",
		"options": [
			{"id": "tex-mex-paste.store", "type": "store_alternative", "name": "Store mix", "per": {"quantity": "1", "unit": "tbsp"},
			 "ingredients": [{"name": "Tomato Paste", "quantity": "2", "unit": "tsp"}]},
			{"id": "tex-mex-paste.batch", "type": "house_made_batch", "name": "House blend", "yield": {"quantity": "10", "unit": "tbsp"},
			 "shelfLifeDays": 14, "steps": ["Stir."], "ingredients": [{"name": "Tomato Paste", "quantity": "6", "unit": "tbsp"}, {"name": "Salt"}]}
		]}]}`
	seed, err := ParseSeed([]byte(valid))
	if err != nil || len(seed.Specialties) != 1 {
		t.Fatalf("valid seed: %+v, %v", seed, err)
	}
	sp := seed.Specialties[0]
	if sp.Key != "tex mex paste" || !slices.Equal(sp.AliasKeys, []string{"tex mex spice paste"}) || sp.Options[1].Ingredients[1].Quantity != "" || sp.SeedVersion != 1 {
		t.Errorf("parsed = %+v", sp)
	}
	for name, change := range map[string][2]string{
		"unknown field":      {`"category": "condiments"`, `"category": "condiments", "brand": "x"`},
		"wrong id":           {`"id": "tex-mex-paste", "name"`, `"id": "texmex", "name"`},
		"alias repeats name": {`"Tex Mex Spice Paste"`, `"Tex Mex Paste"`},
		"unknown category":   {`"category": "condiments"`, `"category": "sauces"`},
		"volume unit size":   {`"per": "count"`, `"per": "tbsp"`},
		"missing default":    {`"defaultOptionId": "tex-mex-paste.store"`, `"defaultOptionId": "tex-mex-paste.other"`},
		"option prefix":      {`"id": "tex-mex-paste.batch"`, `"id": "batch"`},
		"unknown unit":       {`"quantity": "2", "unit": "tsp"`, `"quantity": "2", "unit": "dollop"`},
		"store no quantity":  {`{"name": "Tomato Paste", "quantity": "2", "unit": "tsp"}`, `{"name": "Tomato Paste"}`},
		"store no per":       {`"per": {"quantity": "1", "unit": "tbsp"},`, ``},
		"batch no yield":     {`"yield": {"quantity": "10", "unit": "tbsp"},`, ``},
		"batch no shelf":     {`"shelfLifeDays": 14,`, ``},
		"zero version":       {`"version": 1`, `"version": 0`},
		"bad fraction":       {`"quantity": "6"`, `"quantity": "six"`},
	} {
		data := strings.Replace(valid, change[0], change[1], 1)
		if data == valid {
			t.Fatalf("%s: the change didn't apply", name)
		}
		if _, err := ParseSeed([]byte(data)); err == nil {
			t.Errorf("%s: ParseSeed() error = nil", name)
		}
	}
	two := valid[:strings.LastIndex(valid, `}]}`)] + strings.Replace(`}]}`, `}]}`, `}, {"id": "tex-mex-spice-paste", "name": "Tex Mex Spice Paste", "category": "condiments", "defaultOptionId": "tex-mex-spice-paste.a",
		"options": [{"id": "tex-mex-spice-paste.a", "type": "store_alternative", "name": "A", "per": {"quantity": "1", "unit": "tbsp"}, "ingredients": [{"name": "Salt", "quantity": "1", "unit": "tsp"}]}]}]}`, 1)
	if _, err := ParseSeed([]byte(two)); err == nil || !strings.Contains(err.Error(), "already") {
		t.Errorf("a name that is another specialty's alias: error = %v", err)
	}
}
