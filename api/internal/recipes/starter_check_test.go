package recipes

import "testing"

const hfImage = "https://img.hellofresh.com/f_auto,fl_lossy,q_auto,w_1200/hellofresh_s3/image/"

func TestNameMismatchFindsARenamedStarter(t *testing.T) {
	for _, tc := range []struct {
		r    Recipe
		want string
	}{
		// Renamed without their photo, subtitle and description (build 120 review).
		{Recipe{Name: "Lemon Herb Salmon", SourceURL: "https://www.hellofresh.com/recipes/one-pan-chicken-enchiladas-verdes-6616f05917c952d87280a8df",
			ImageURL: hfImage + "661569bf11089628acd331bc-786278c9-36cf85b7.jpeg"}, "one-pan-chicken-enchiladas-verdes"},
		{Recipe{Name: "Mushroom Risotto", SourceURL: "https://www.hellofresh.com/recipes/pork-sausage-roasted-pepper-pasta-60d335519f98735c223a359f"},
			"pork-sausage-roasted-pepper-pasta"},
		{Recipe{Name: "Neighbor Chili", ImageURL: hfImage + "street-cart-style-chicken-bowls-d9b9cadb.jpg"}, "street-cart-chicken-bowls"},
		// Names that fit.
		{Recipe{Name: "Street Cart-Style Chicken Bowls", SourceURL: "https://www.hellofresh.com/recipes/street-cart-style-chicken-bowls-63ecaed802dfc404e006c190",
			ImageURL: hfImage + "street-cart-style-chicken-bowls-d9b9cadb.jpg"}, ""},
		{Recipe{Name: "One-Pan Chicken Enchiladas Verdes", SourceURL: "https://www.hellofresh.com/recipes/one-pan-chicken-enchiladas-verdes-6616f05917c952d87280a8df"}, ""},
		{Recipe{Name: "Orange and Cashew Chicken Stir-Fry", ImageURL: hfImage + "orange-and-cashew-chicken-stir-fry-fcc117e7.jpg"}, ""},
		{Recipe{Name: "Beef Taco", SourceURL: "https://example.com/recipes/easy-beef-tacos"}, ""},
		// Nothing with words to compare.
		{Recipe{Name: "Ginger Chicken & Rice Bowls", ImageURL: "https://dinneros.app/img/recipes/6aa943c326393a8c809cf029.jpg"}, ""},
		{Recipe{Name: "Lemon Garlic Chicken"}, ""},
	} {
		if got := NameMismatch(tc.r); got != tc.want {
			t.Errorf("NameMismatch(%q) = %q, want %q", tc.r.Name, got, tc.want)
		}
	}
}
