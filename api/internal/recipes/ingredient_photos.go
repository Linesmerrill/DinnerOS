package recipes

import (
	_ "embed"
	"net/url"
	"path"
	"strings"
)

// An ingredient's thumbnail shows the ingredient, or nothing: the app then
// draws its one fallback. A meal kit's catalog sometimes pictures an
// ingredient as a finished dish (diced chicken thighs in a skillet), and a
// dish beside raw lemons and scallions reads as a mistake. dish_photos.txt
// lists those photos, and they are dropped wherever an ingredient is read.

//go:embed dish_photos.txt
var dishPhotosFile string

var dishPhotoIDs = loadDishPhotos(dishPhotosFile)

func loadDishPhotos(file string) map[string]bool {
	ids := map[string]bool{}
	for line := range strings.SplitSeq(file, "\n") {
		line, _, _ = strings.Cut(line, "#")
		if id := strings.ToLower(strings.TrimSpace(line)); id != "" {
			ids[id] = true
		}
	}
	return ids
}

// photoSwaps are catalog photos that show the right ingredient but read as
// another at thumbnail size, each with the file to show instead (build 122,
// Hark's review item 16). The chopped chicken breast photo is a pile of small
// pink pieces that reads as ground meat beside "Chicken Breasts", so chicken
// breast rows show the raw chicken breasts of the cutlets photo.
var photoSwaps = map[string]string{
	"64e58fc941604e0cb91563c1": "59c2b202c288007f56514711-4f7e3ff4.png", // chopped chicken breast
	"5ab2a22030006c610e61b892": "59c2b202c288007f56514711-4f7e3ff4.png", // chicken breast strips
}

// wrongFoodPhotos are catalog photos of another food than the ingredient
// they are filed under, by photo ID and the ingredient keys they are wrong
// for. The photo stays right for any other ingredient that uses it. The
// catalog's Grape Tomatoes photo shows roma tomatoes (Hark's build 121
// review, round 3); the catalog has no grape or cherry tomato photo, so the
// row shows the app's fallback instead.
var wrongFoodPhotos = map[string][]string{
	"64b6744fd4684302b8753e64": {"grape tomato", "grape tomatoes", "cherry tomato", "cherry tomatoes"},
}

// catalogPhotoBase is where the meal kit's catalog keeps its ingredient photos.
const catalogPhotoBase = "https://img.hellofresh.com/f_auto,fl_lossy,q_auto,w_1200/hellofresh_s3/ingredient/"

// keyPhotos are catalog photos for common ingredients that some recipes name
// without a photo or with a wrong one (Hark's build 121 review, round 4: the
// starter pesto's basil, pepper flakes and black pepper, and the meal kit's
// tofu and pork chops showed letters). Each is the catalog's own photo of that
// ingredient, or of the same food under its catalog name (black pepper is the
// catalog's Pepper, red pepper flakes its Chili Flakes). A stored photo wins,
// unless it is wrong for the ingredient.
var keyPhotos = map[string]string{
	"grape tomato":      "554a385afd2cb9324b8b456c-344873b8.png",
	"grape tomatoes":    "554a385afd2cb9324b8b456c-344873b8.png",
	"cherry tomato":     "554a385afd2cb9324b8b456c-344873b8.png",
	"cherry tomatoes":   "554a385afd2cb9324b8b456c-344873b8.png",
	"basil":             "64b67452a7c1f8a1e13bae58-dd6aeeec.png",
	"fresh basil":       "64b67452a7c1f8a1e13bae58-dd6aeeec.png",
	"black pepper":      "5566dc00f8b25e5b298b4568-b5daf661.png",
	"red pepper flakes": "554a35f4fd2cb9cf488b4569-6f0638f4.png",
	"chili flakes":      "554a35f4fd2cb9cf488b4569-6f0638f4.png",
	"pork chop":         "554a3a04fd2cb9324b8b456e-d68749c2.png",
	"pork chops":        "554a3a04fd2cb9324b8b456e-d68749c2.png",
	"tofu":              "68d57d91d232ff05cfc441d1-8909ca87.png",
	// The catalog has no pine nuts (Hark's build 121 review, round 5): a public domain photo of
	// shelled pine nuts on Wikimedia Commons (File:P_armandii_nuts.jpg, "released into the
	// public domain ... worldwide").
	"pine nuts":         pineNutsPhoto,
	"pine nut":          pineNutsPhoto,
	"toasted pine nuts": pineNutsPhoto,
}

const pineNutsPhoto = "https://upload.wikimedia.org/wikipedia/commons/thumb/6/63/P_armandii_nuts.jpg/500px-P_armandii_nuts.jpg"

// photoID is the catalog's ID for a photo URL: the start of its file name,
// lower case, or "" when raw isn't a URL.
func photoID(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	base := strings.ToLower(path.Base(u.Path))
	base = strings.TrimSuffix(base, path.Ext(base))
	id, _, _ := strings.Cut(base, "-")
	return id
}

// IngredientPhotoFor is IngredientPhoto for the ingredient keyed key: also
// "" when the photo shows another food than that ingredient.
// When that leaves no photo, a known catalog photo for the key fills in.
func IngredientPhotoFor(key, raw string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	photo := IngredientPhoto(raw)
	for _, k := range wrongFoodPhotos[photoID(photo)] {
		if k == key {
			photo = ""
		}
	}
	if photo == "" {
		if file, ok := keyPhotos[key]; ok {
			if strings.HasPrefix(file, "https://") {
				return file
			}
			return catalogPhotoBase + file
		}
	}
	return photo
}

// IngredientPhoto returns raw, "" when it is a photo of a cooked dish rather
// than of the ingredient, or the photo to show instead of one that reads as
// another ingredient.
func IngredientPhoto(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if _, err := url.Parse(raw); err != nil {
		return raw
	}
	id := photoID(raw)
	if dishPhotoIDs[id] {
		return ""
	}
	if file, ok := photoSwaps[id]; ok {
		cut := strings.LastIndex(raw, "/")
		return raw[:cut+1] + file
	}
	return raw
}
