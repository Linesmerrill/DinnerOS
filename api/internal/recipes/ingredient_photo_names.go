package recipes

import (
	"context"
	"regexp"
	"slices"
	"strings"
)

// An ingredient with no photo of its own borrows the photo of the plain
// ingredient its name describes: "Baby Spinach" shows Spinach's photo, and
// "Diced Chicken Thighs" shows Chicken Thighs' (build 122, Hark's review
// items 4 and 5: both drew the fallback fork and knife). The rule is about
// names, not a list of ingredients, so "Sliced Mushrooms" or "Boneless Pork
// Chops" find a photo the same way. Only the catalog's own photos are used.

// photoDescriptors are words that say how an ingredient is cut, sized or
// raised, not what it is. Leading ones are dropped to find the plain
// ingredient. "Ground", "crushed" and "frozen" are not here: ground beef and
// crushed tomatoes are different things from beef and tomatoes.
var photoDescriptors = []string{
	"baby", "bone", "boneless", "chopped", "coarsely", "cubed", "diced", "fine", "finely", "fresh",
	"grated", "halved", "in", "julienned", "large", "medium", "minced", "organic", "peeled",
	"quartered", "roughly", "shredded", "skinless", "sliced", "small", "thick", "thin", "thinly",
	"trimmed", "whole",
}

// photoBase is key without its leading descriptors ("diced chicken thighs"
// is "chicken thighs"), or key itself when it has none or is nothing else.
func photoBase(key string) string {
	words := strings.Fields(key)
	i := 0
	for i < len(words)-1 && slices.Contains(photoDescriptors, words[i]) {
		i++
	}
	return strings.Join(words[i:], " ")
}

// numberForms are key and its other number: "chicken thigh" and "chicken
// thighs", "tomatoes" and "tomato".
func numberForms(key string) []string {
	out := []string{key}
	switch {
	case strings.HasSuffix(key, "es"):
		out = append(out, strings.TrimSuffix(key, "es"), strings.TrimSuffix(key, "s"))
	case strings.HasSuffix(key, "s"):
		out = append(out, strings.TrimSuffix(key, "s"))
	default:
		out = append(out, key+"s", key+"es")
	}
	return out
}

// photoSynonyms are names for the same food that share no words, so the
// descriptor rule can't find one from the other (build 123, Hark's review
// item 10: "Bow Tie Pasta" and "Basil Pesto" drew the fork and knife). Each
// group is one food; a member borrows any other member's photo, in order.
var photoSynonyms = [][]string{
	{"farfalle", "bow tie pasta", "bowtie pasta", "bow-tie pasta"},
	{"pesto", "basil pesto", "pesto sauce", "genovese pesto"},
	{"scallions", "green onions", "spring onions"},
	{"cilantro", "fresh coriander"},
	{"chickpeas", "garbanzo beans"},
	{"bell pepper", "sweet pepper"},
	{"zucchini", "courgette"},
}

// pastaWord is the word a pasta shape is sold with or without: "rigatoni"
// and "rigatoni pasta" are the same box.
const pastaWord = " pasta"

// sameFoodForms are key's other names: its synonyms and, for a pasta shape,
// the name with or without "pasta". Not key itself.
func sameFoodForms(key string) []string {
	var out []string
	if shape, ok := strings.CutSuffix(key, pastaWord); ok && shape != "" {
		out = append(out, shape)
	} else if !strings.Contains(key, " ") && key != "pasta" {
		// A one-word shape ("rigatoni") may be filed as "rigatoni pasta".
		out = append(out, key+pastaWord)
	}
	for _, f := range numberForms(key) {
		for _, group := range photoSynonyms {
			if !slices.Contains(group, f) {
				continue
			}
			for _, other := range group {
				if other == f {
					continue
				}
				out = append(out, other)
				if shape, ok := strings.CutSuffix(other, pastaWord); ok {
					out = append(out, shape)
				} else if strings.HasSuffix(f, pastaWord) {
					out = append(out, other+pastaWord)
				}
			}
		}
	}
	return out
}

// photoKeys are the catalog keys whose photo an ingredient keyed key may
// borrow, best first: its own other number, then the plain ingredient in
// either number, then the same food by another name. Never key itself.
func photoKeys(key string) []string {
	var out []string
	add := func(k string) {
		if k != key && k != "" && !slices.Contains(out, k) {
			out = append(out, k)
		}
	}
	base := photoBase(key)
	for _, k := range append(numberForms(key), numberForms(base)...) {
		add(k)
	}
	for _, from := range []string{key, base} {
		for _, k := range sameFoodForms(from) {
			for _, f := range numberForms(k) {
				add(f)
			}
		}
	}
	return out
}

// maxPhotoRelatives bounds the search for described forms of the same plain
// ingredient ("boneless chicken thighs" for "diced chicken thighs").
const maxPhotoRelatives = 200

// fillMissingPhotos gives each ingredient without a photo the photo of the
// plain ingredient its name describes, or of another form of it, when the
// catalog has one. Two catalog queries however many ingredients there are.
// A failed lookup leaves the photos as they were: a thumbnail is never worth
// failing a recipe over.
func (s *Service) fillMissingPhotos(ctx context.Context, list []Ingredient) {
	var keys, bases []string
	for _, ing := range list {
		if ing.ImageURL != "" || ing.Key == "" {
			continue
		}
		for _, k := range photoKeys(ing.Key) {
			if !slices.Contains(keys, k) {
				keys = append(keys, k)
			}
		}
		for _, b := range numberForms(photoBase(ing.Key)) {
			if !slices.Contains(bases, b) {
				bases = append(bases, regexp.QuoteMeta(b))
			}
		}
	}
	if len(keys) == 0 {
		return
	}
	photos := map[string]string{}
	if found, err := s.store.FindIngredients(ctx, nil, keys); err == nil {
		for _, ing := range found {
			if ing.ImageURL != "" {
				photos[ing.Key] = ing.ImageURL
			}
		}
	}
	// Other forms of the same plain ingredient, keyed by that plain
	// ingredient: "boneless chicken thighs" under "chicken thighs".
	relatives := map[string]string{}
	pattern := `^(?:(?:` + strings.Join(photoDescriptors, "|") + `) )+(?:` + strings.Join(bases, "|") + `)$`
	if found, err := s.store.SearchIngredients(ctx, pattern, maxPhotoRelatives); err == nil {
		for _, ing := range found {
			if ing.ImageURL == "" {
				continue
			}
			for _, b := range numberForms(photoBase(ing.Key)) {
				if _, ok := relatives[b]; !ok {
					relatives[b] = ing.ImageURL
				}
			}
		}
	}
	for i := range list {
		if list[i].ImageURL != "" || list[i].Key == "" {
			continue
		}
		for _, k := range photoKeys(list[i].Key) {
			if photo := photos[k]; photo != "" {
				list[i].ImageURL = photo
				break
			}
		}
		if list[i].ImageURL == "" {
			list[i].ImageURL = relatives[photoBase(list[i].Key)]
		}
	}
}
