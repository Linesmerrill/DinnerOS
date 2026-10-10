package ingredients

// sameIngredients are names that are one ingredient to buy and keep: a
// recipe's "Yellow Onion" is the onion the household already buys, and
// listing both makes the list say "2 onions" and "2 yellow onions" for the
// same bag. The first name of each group is the one kept; the rest are
// variants of it (decision 640). Only names that are truly one product
// belong here: red onion, sweet potatoes, and smoked paprika are not.
var sameIngredients = [][]string{
	{"onion", "yellow onion"},
	{"tomato", "roma tomato"},
	{"soy sauce", "kikkoman traditionally brewed soy sauce"},
	{"cream cheese", "philadelphia cream cheese"},
	{"chicken cutlets", "organic chicken cutlets"},
	{"mini cucumber", "persian cucumber"},
}

// sameAs maps each variant's key to its kept key.
var sameAs = func() map[string]string {
	out := map[string]string{}
	for _, group := range sameIngredients {
		for _, v := range group[1:] {
			out[v] = group[0]
		}
	}
	return out
}()

// SameIngredientKey is the key a normalized name is kept under: "yellow
// onion" is "onion". A name with no variant is its own key.
func SameIngredientKey(key string) string {
	if kept, ok := sameAs[key]; ok {
		return kept
	}
	return key
}

// SameIngredientKeys are every key that is the same ingredient as key, the
// kept one first: "onion" and "yellow onion" for either. A name with no
// variant is just itself.
func SameIngredientKeys(key string) []string {
	kept := SameIngredientKey(key)
	for _, group := range sameIngredients {
		if group[0] == kept {
			return append([]string(nil), group...)
		}
	}
	return []string{key}
}

// SameIngredientVariants are the variant → kept pairs, for merging catalog
// ingredients that were imported under a variant's name.
func SameIngredientVariants() map[string]string {
	out := make(map[string]string, len(sameAs))
	for v, k := range sameAs {
		out[v] = k
	}
	return out
}
