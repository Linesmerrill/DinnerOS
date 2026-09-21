package ingredients

import "regexp"

// This file answers one question about a catalog ingredient: does it make the
// dish hot? Cooking instructions bold a spicy ingredient differently
// (docs/api.md#cooking-instructions), and the heat has to be decided once, for
// the catalog, rather than by each client.
//
// Heat is not a grocery category — chili flakes and cinnamon are both
// "spices", gochujang and ketchup are both "condiments" — so it can't be read
// off Categorize. It is a second classification over the same normalized
// names, written the same way: ordered phrase rules, with the exceptions
// first, so "Sweet Thai Chili Sauce" and "Bell Pepper" don't inherit the heat
// of "chili" and "pepper".

// mildPhrases are names that contain a hot word but aren't hot. They win over
// hotPhrases.
var mildPhrases = []string{
	"sweet chili", "sweet thai chili", "thai sweet chili", "sweet red chili",
	"bell pepper", "green pepper", "long green pepper", "banana pepper",
	"peppercorn", "peppercorns", "black pepper", "white pepper", "lemon pepper",
	"pepper jack", "pepperoni", "peppermint", "chile relleno",
}

// hotPhrases are the names that mean heat. A phrase matches on word
// boundaries inside the normalized name, so "Crushed Red Pepper Flakes" and
// "Jalapeño, sliced" both match.
var hotPhrases = []string{
	"chili flakes", "chile flakes", "red pepper flakes", "crushed red pepper", "pepper flakes",
	"chili powder", "chile powder", "chipotle powder", "cayenne", "cayenne pepper",
	"crushed chili", "chili garlic", "chili oil", "chili paste", "chile paste", "chili crisp",
	"chili sauce", "hot sauce", "hot honey", "sriracha", "sambal", "harissa", "gochujang",
	"gochugaru", "calabrian chili", "chipotle", "adobo chipotle", "chipotle in adobo",
	"jalapeno", "serrano", "habanero", "scotch bonnet", "poblano", "ancho", "guajillo",
	"thai chili", "birds eye chili", "chili pepper", "chile pepper", "hot pepper",
	"hot chili", "chili", "chile", "chiles", "szechuan", "sichuan", "spicy",
	"blackening", "cajun", "buffalo sauce", "pepper jelly", "horseradish", "wasabi",
	"curry paste", "diavola", "arrabbiata", "piri piri", "peri peri", "nduja",
}

var (
	compiledMild = compilePhrases(mildPhrases)
	compiledHot  = compilePhrases(hotPhrases)
)

func compilePhrases(phrases []string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, 0, len(phrases))
	for _, p := range phrases {
		out = append(out, regexp.MustCompile(`(^| )`+regexp.QuoteMeta(normalizeName(p))+`( |$)`))
	}
	return out
}

// Spicy reports whether an ingredient name brings heat to a dish. It is
// deliberately conservative: a name no rule recognizes is not spicy, so the
// signal means something when it appears.
func Spicy(name string) bool {
	n := normalizeName(name)
	if n == "" {
		return false
	}
	for _, p := range compiledMild {
		if p.MatchString(n) {
			return false
		}
	}
	for _, p := range compiledHot {
		if p.MatchString(n) {
			return true
		}
	}
	return false
}
