package recipes

import "testing"

func TestIngredientPhotoDropsDishPhotos(t *testing.T) {
	const hf = "https://img.hellofresh.com/f_auto,fl_lossy,q_auto,w_1200/hellofresh_s3/ingredient/"
	for _, tc := range []struct{ in, want string }{
		// Diced chicken thighs pictured cooked in a skillet (Hark's build 122 review).
		{hf + "63e2899e70f16f525809c3b0-07d5337c.png", ""},
		{hf + "63E2899E70F16F525809C3B0-07d5337c.png", ""},
		// Raw chicken and a lemon are the ingredients themselves.
		{hf + "64e58fc941604e0cb91563c1-63d315d8.png", hf + "64e58fc941604e0cb91563c1-63d315d8.png"},
		{"https://img.hellofresh.com/hellofresh_s3/image/554a302ffd2cb9324b8b4569.png", "https://img.hellofresh.com/hellofresh_s3/image/554a302ffd2cb9324b8b4569.png"},
		{"", ""},
		{"  ", ""},
	} {
		if got := IngredientPhoto(tc.in); got != tc.want {
			t.Errorf("IngredientPhoto(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if len(dishPhotoIDs) == 0 {
		t.Fatal("dish_photos.txt listed nothing")
	}
}

func TestReadIngredientsDropDishPhotos(t *testing.T) {
	d := ingredientDoc{Name: "Diced Chicken Thighs",
		ImageURL: "https://img.hellofresh.com/hellofresh_s3/ingredient/63e2899e70f16f525809c3b0-07d5337c.png"}
	if got := d.toIngredient().ImageURL; got != "" {
		t.Errorf("a dish photo stood in for the ingredient: %q", got)
	}
}
