package recipes

import "testing"

func TestIngredientPhotoDropsDishPhotos(t *testing.T) {
	const hf = "https://img.hellofresh.com/f_auto,fl_lossy,q_auto,w_1200/hellofresh_s3/ingredient/"
	for _, tc := range []struct{ in, want string }{
		// Diced chicken thighs pictured cooked in a skillet (Hark's build 122 review).
		{hf + "63e2899e70f16f525809c3b0-07d5337c.png", ""},
		{hf + "63E2899E70F16F525809C3B0-07d5337c.png", ""},
		// Chopped chicken breast and breast strips read as ground meat at
		// thumbnail size; they show the raw chicken breasts photo instead.
		{hf + "64e58fc941604e0cb91563c1-63d315d8.png", hf + "59c2b202c288007f56514711-4f7e3ff4.png"},
		{hf + "5ab2a22030006c610e61b892-5690f4a0.png", hf + "59c2b202c288007f56514711-4f7e3ff4.png"},
		// Raw chicken cutlets are the ingredient itself.
		{hf + "59c2b202c288007f56514711-4f7e3ff4.png", hf + "59c2b202c288007f56514711-4f7e3ff4.png"},
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

// The catalog's Grape Tomatoes photo shows roma tomatoes (Hark's build 121
// review, round 3): grape and cherry tomato rows drop it, and any other
// ingredient filed with the same photo keeps it.
func TestAPhotoOfAnotherFoodIsDroppedForThatIngredientOnly(t *testing.T) {
	const roma = "https://img.hellofresh.com/f_auto,fl_lossy,q_auto,w_1200/hellofresh_s3/ingredient/64b6744fd4684302b8753e64-05a2d0c8.png"
	for _, key := range []string{"grape tomatoes", "Grape Tomatoes", "grape tomato", "cherry tomatoes", " cherry tomato "} {
		if got := IngredientPhotoFor(key, roma); got != "" {
			t.Errorf("IngredientPhotoFor(%q) = %q, want no photo", key, got)
		}
	}
	for _, key := range []string{"roma tomatoes", "tomatoes", ""} {
		if got := IngredientPhotoFor(key, roma); got != roma {
			t.Errorf("IngredientPhotoFor(%q) = %q, want the photo", key, got)
		}
	}
	// The other rules still apply.
	const hf = "https://img.hellofresh.com/f_auto,fl_lossy,q_auto,w_1200/hellofresh_s3/ingredient/"
	if got := IngredientPhotoFor("diced chicken thighs", hf+"63e2899e70f16f525809c3b0-07d5337c.png"); got != "" {
		t.Errorf("a dish photo came back: %q", got)
	}
	if got := IngredientPhotoFor("chicken breasts", hf+"64e58fc941604e0cb91563c1-63d315d8.png"); got != hf+"59c2b202c288007f56514711-4f7e3ff4.png" {
		t.Errorf("the swap was lost: %q", got)
	}

	d := ingredientDoc{Key: "grape tomatoes", Name: "Grape Tomatoes", ImageURL: roma}
	if got := d.toIngredient().ImageURL; got != "" {
		t.Errorf("Grape Tomatoes read with the roma photo: %q", got)
	}
}
