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

// IngredientPhoto returns raw, or "" when it is a photo of a cooked dish
// rather than of the ingredient.
func IngredientPhoto(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	base := strings.ToLower(path.Base(u.Path))
	base = strings.TrimSuffix(base, path.Ext(base))
	id, _, _ := strings.Cut(base, "-")
	if dishPhotoIDs[id] {
		return ""
	}
	return raw
}
