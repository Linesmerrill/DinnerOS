package recommendations

import (
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

func TestCuisinePhotosAreDistinctAndSkipPhotolessRecipes(t *testing.T) {
	catalog := []recipes.Recipe{
		{ID: "1", Name: "Butter Chicken", Cuisines: []string{"Indian"}},
		{ID: "2", Name: "Palak Paneer", Cuisines: []string{"Indian"}, ImageURL: "https://img.example/palak.jpg"},
		{ID: "3", Name: "Carbonara", Cuisines: []string{"Italian"}, ImageURL: "https://img.example/carbonara.jpg"},
		{ID: "4", Name: "Arrabbiata", Cuisines: []string{"Italian"}, ImageURL: "https://img.example/arrabbiata.jpg"},
		{ID: "5", Name: "Typed Supper", Cuisines: []string{"Thai"}},
		{ID: "6", Name: "Garlic Knots", Cuisines: []string{"Italian"}, ImageURL: "https://img.example/knots.jpg", IsAddon: true},
	}
	v := buildVocabulary(catalog)
	photo := map[string]string{}
	for _, o := range v.Cuisines {
		photo[o.Value] = o.ImageURL
	}
	if photo["indian"] != "https://img.example/palak.jpg" {
		t.Errorf("indian = %q, want the one Indian recipe with a photo", photo["indian"])
	}
	// By name: Arrabbiata before Carbonara; the add-on never counts.
	if photo["italian"] != "https://img.example/arrabbiata.jpg" {
		t.Errorf("italian = %q", photo["italian"])
	}
	if photo["thai"] != "" {
		t.Errorf("thai has no recipe with a photo, got %q", photo["thai"])
	}
	seen := map[string]string{}
	for _, o := range v.Cuisines {
		if o.ImageURL == "" {
			continue
		}
		if other, ok := seen[o.ImageURL]; ok {
			t.Errorf("%s and %s show the same photo", other, o.Value)
		}
		seen[o.ImageURL] = o.Value
	}
	// Same library, same photos.
	again := buildVocabulary(catalog)
	for i := range v.Cuisines {
		if v.Cuisines[i].ImageURL != again.Cuisines[i].ImageURL {
			t.Errorf("%s changed photo between calls", v.Cuisines[i].Value)
		}
	}
}

func TestCuisinePhotoReachesJSON(t *testing.T) {
	out := optionsJSON([]Option{{Value: "indian", Label: "Indian", RecipeCount: 1, ImageURL: "https://img.example/a.jpg"}}, true)
	if out[0].ImageURL != "https://img.example/a.jpg" {
		t.Errorf("imageUrl = %q", out[0].ImageURL)
	}
}
