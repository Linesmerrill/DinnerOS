package recommendations

import (
	"slices"
	"strings"
	"unicode"

	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// Dishes are what a meal is called, across cuisines: tacos are Mexican,
// Korean, or fish; pasta is Italian or not. A weekday rule can name them
// ("tacos on Tuesday", "pasta on Monday") beside or instead of a cuisine,
// and Autopilot learns which a day leans on from history (decision 637).

// DishOptions are the dishes offered when building a weekday rule. Values
// are singular; a member can type any other dish ("gnocchi").
var DishOptions = []Option{
	{Value: "taco", Label: "Tacos"},
	{Value: "enchilada", Label: "Enchiladas"},
	{Value: "burrito", Label: "Burritos"},
	{Value: "quesadilla", Label: "Quesadillas"},
	{Value: "fajita", Label: "Fajitas"},
	{Value: "pasta", Label: "Pasta"},
	{Value: "pizza", Label: "Pizza & flatbreads"},
	{Value: "burger", Label: "Burgers"},
	{Value: "sandwich", Label: "Sandwiches"},
	{Value: "bowl", Label: "Bowls"},
	{Value: "stir-fry", Label: "Stir-fries"},
	{Value: "noodle", Label: "Noodles"},
	{Value: "curry", Label: "Curries"},
	{Value: "soup", Label: "Soups & stews"},
	{Value: "chili", Label: "Chili"},
	{Value: "salad", Label: "Salads"},
	{Value: "meatball", Label: "Meatballs"},
	{Value: "meatloaf", Label: "Meatloaf"},
	{Value: "risotto", Label: "Risotto"},
}

// dishSynonyms are name words that are one of the offered dishes.
var dishSynonyms = map[string]string{
	"flatbread": "pizza", "stew": "soup", "chowder": "soup", "melt": "sandwich", "panini": "sandwich",
	"sub": "sandwich", "wrap": "sandwich", "smashburger": "burger", "slider": "burger", "ramen": "noodle",
	"lo mein": "noodle", "pad thai": "noodle", "udon": "noodle", "meatloave": "meatloaf",
}

// dishTrailers are words that can follow a dish without being the meal:
// "Tacos De Canasta", "Taco Bar", "Chili Verde", "Chili Con Carne".
var dishTrailers = map[string]bool{
	"de": true, "canasta": true, "con": true, "carne": true, "verde": true, "rojo": true, "al": true,
	"pastor": true, "bar": true, "night": true,
}

// dishPhraseBreaks end a name's main part, like "with": "Ramen in Dashi
// Broth", "Chicken over Lemony Spaghetti".
var dishPhraseBreaks = map[string]bool{"with": true, "in": true, "over": true, "on": true}

// canonicalDish is a dish as a rule stores it and an item carries it:
// lowercase, singular, "stir-fry" with its hyphen.
func canonicalDish(v string) string {
	v = strings.ToLower(strings.Join(strings.Fields(v), " "))
	if v == "" {
		return ""
	}
	words := strings.Fields(v)
	words[len(words)-1] = singularDish(words[len(words)-1])
	v = strings.Join(words, " ")
	return strings.ReplaceAll(v, "stir fry", "stir-fry")
}

// singularDish is a dish word in the singular: "tacos" → "taco", "curries"
// → "curry", "sandwiches" → "sandwich". Words that end in s anyway
// ("couscous", "hummus") stay.
func singularDish(w string) string {
	switch {
	case strings.HasSuffix(w, "ss") || strings.HasSuffix(w, "us") || len(w) <= 3:
		return w
	case strings.HasSuffix(w, "ies"):
		return strings.TrimSuffix(w, "ies") + "y"
	case strings.HasSuffix(w, "ches") || strings.HasSuffix(w, "shes") || strings.HasSuffix(w, "xes"):
		return strings.TrimSuffix(w, "es")
	case strings.HasSuffix(w, "s"):
		return strings.TrimSuffix(w, "s")
	}
	return w
}

// recipeDishes are the dishes a recipe is: every word of its name, singular
// (so a typed dish like "gnocchi" or "tostada" matches), and its meal
// categories ("pasta" for a rigatoni). An offered dish counts only where it
// ends a part of the name before "with", "in", "over", or "on" (parts split
// at "&", "and", and commas; "De Canasta" or "Verde" may follow it): that
// word is what the meal is, and words before it describe it.
// "Taco Soup" is a soup, "Sweet Chili Pork & Rice" no chili, and "Tacos
// with Side Salad" tacos (decision 638).
func recipeDishes(r recipes.Recipe, categories []string) []string {
	var out []string
	add := func(v string) {
		if v != "" && !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	name := strings.ToLower(r.Name)
	name = strings.ReplaceAll(name, "stir fry", "stir-fry")
	name = strings.NewReplacer("&", " | ", ",", " | ", "+", " | ").Replace(name)
	var words []string
	for _, f := range strings.Fields(name) {
		w := strings.TrimFunc(f, func(c rune) bool { return !unicode.IsLetter(c) && c != '-' && c != '|' })
		if w == "and" {
			w = "|"
		}
		if w != "" {
			words = append(words, w)
		}
	}
	main, named := true, false
	for i, w := range words {
		if w == "|" {
			continue
		}
		if dishPhraseBreaks[w] {
			main = false
		}
		d := singularDish(w)
		dish, span := offeredDishAt(words, i)
		if dish == "" {
			add(d)
			continue
		}
		end := i + span
		for end < len(words) && dishTrailers[words[end]] {
			end++
		}
		if main && (end == len(words) || words[end] == "|" || dishPhraseBreaks[words[end]]) {
			if isOfferedDish(d) {
				add(d)
			}
			add(dish)
			named = true
		}
		if !isOfferedDish(d) {
			// A synonym word ("stew", "ramen") is still a word of the name.
			add(d)
		}
	}
	for _, c := range categories {
		// When the name says what the meal is, a category guessed from
		// another of its words ("salad" for a side salad, "stir-fry" for pad
		// thai) is no second dish.
		if d := canonicalDish(c); named && isOfferedDish(d) && !slices.Contains(out, d) {
			continue
		}
		switch c {
		case "tacos":
			// The tacos category is "Tacos & Mexican": its dish words come
			// from the name, not the category.
		case "sandwich":
			add("sandwich")
		default:
			add(canonicalDish(c))
		}
	}
	return out
}

// offeredDishAt is the offered dish the name word at i stands for, and how
// many words it takes: alone ("tacos", "stew") or with the next ("pad
// thai"). It's "" when the word is no dish.
func offeredDishAt(words []string, i int) (string, int) {
	d := singularDish(words[i])
	if i+1 < len(words) {
		if syn, ok := dishSynonyms[d+" "+singularDish(words[i+1])]; ok {
			return syn, 2
		}
	}
	if isOfferedDish(d) {
		return d, 1
	}
	if syn, ok := dishSynonyms[d]; ok {
		return syn, 1
	}
	return "", 0
}

func isOfferedDish(d string) bool {
	return slices.ContainsFunc(DishOptions, func(o Option) bool { return o.Value == d })
}

// RecipeDishes are the dishes a recipe is, for matching a weekday rule's
// dishes outside Autopilot (the menu's "Taco night" row).
func RecipeDishes(r recipes.Recipe) []string {
	return recipeDishes(r, recipeMealCategories(r, nil))
}

// DishLabel is a dish as people read it: "Tacos" for "taco"; a typed dish
// reads as typed, capitalized.
func DishLabel(v string) string {
	for _, o := range DishOptions {
		if o.Value == v {
			return o.Label
		}
	}
	if v == "" {
		return ""
	}
	return strings.ToUpper(v[:1]) + v[1:]
}

// dishTypes are the dishes that are offered dish kinds, for learning which a
// weekday leans on: "taco", never "chicken" or "with".
func dishTypes(dishes []string) []string {
	var out []string
	for _, d := range dishes {
		if isOfferedDish(d) {
			out = append(out, d)
		}
	}
	return out
}
