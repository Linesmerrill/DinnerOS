package cuisine

import (
	"strings"
	"unicode"
)

// A recipe the household typed or linked often has no cuisine, and Autopilot
// can only learn "Mexican on Tuesdays" from meals that say they're Mexican.
// Guess reads the cuisine a person would from the recipe: a tag that names
// one, a cuisine named in the title ("Thai Basil Chicken"), or a dish that
// belongs to one ("Carnitas Tacos", "Chicken Tikka Masala", "Pesto Penne").
// It stays quiet when the title doesn't say: "Chicken Curry" could be Thai,
// Indian or Japanese, and a wrong cuisine teaches Autopilot the wrong habit
// (decision 721).

// dishCuisines are dish and ingredient words that belong to one cuisine.
// Each is matched as whole words, with or without a plural "s" or "es".
var dishCuisines = map[string][]string{
	"mexican": {
		"taco", "carnitas", "enchilada", "burrito", "quesadilla", "fajita", "tostada", "tamale", "pozole", "posole",
		"mole", "chilaquiles", "elote", "al pastor", "birria", "carne asada", "barbacoa", "chimichanga", "taquito",
		"flauta", "huevos rancheros", "guacamole", "pico de gallo", "chipotle", "salsa verde", "tinga",
		"cochinita pibil", "menudo", "sopa de lima",
	},
	"italian": {
		"pesto", "penne", "spaghetti", "lasagna", "lasagne", "risotto", "gnocchi", "carbonara", "bolognese",
		"marinara", "parmigiana", "chicken parm", "eggplant parm", "alfredo", "rigatoni", "fettuccine", "fettuccini",
		"linguine", "ravioli", "tortellini", "ziti", "pizza", "calzone", "bruschetta", "piccata", "marsala",
		"cacciatore", "osso buco", "focaccia", "minestrone", "arrabbiata", "puttanesca", "cacio e pepe",
		"tagliatelle", "pappardelle", "saltimbocca", "scampi", "bucatini", "orecchiette", "farfalle", "caprese",
		"panzanella", "polenta", "ragu", "arancini", "stromboli", "frittata", "primavera",
	},
	"indian": {
		"tikka", "masala", "tandoori", "korma", "vindaloo", "biryani", "dal", "dhal", "daal", "saag", "palak",
		"paneer", "naan", "samosa", "chana", "aloo", "butter chicken", "rogan josh", "jalfrezi", "pakora", "bhaji",
		"keema", "makhani", "dosa",
	},
	"thai": {
		"pad see ew", "pad kra pao", "tom yum", "tom kha", "panang", "massaman", "larb", "khao soi", "green curry",
		"red curry", "yellow curry", "drunken noodle",
	},
	"japanese": {
		"teriyaki", "ramen", "sushi", "katsu", "tempura", "miso", "udon", "soba", "yakitori", "donburi", "gyoza",
		"okonomiyaki", "karaage", "tonkatsu", "yakisoba", "sukiyaki", "onigiri", "furikake", "ponzu",
	},
	"chinese": {
		"kung pao", "kung po", "lo mein", "chow mein", "general tso", "general tsos", "orange chicken",
		"sweet and sour", "mapo", "char siu", "mongolian beef", "egg foo young", "wonton", "szechuan", "sichuan",
		"dan dan", "chow fun", "moo shu", "bao", "potsticker", "hot and sour", "sesame chicken", "beef and broccoli",
	},
	"korean":     {"bulgogi", "bibimbap", "kimchi", "gochujang", "japchae", "galbi", "kalbi", "tteokbokki"},
	"vietnamese": {"pho", "banh mi", "bun cha", "nuoc cham"},
	"greek":      {"gyro", "souvlaki", "moussaka", "spanakopita", "tzatziki", "pastitsio", "avgolemono", "dolmades"},
	"french": {
		"coq au vin", "ratatouille", "bourguignon", "croque monsieur", "croque madame", "quiche", "cassoulet",
		"bouillabaisse", "nicoise", "au poivre", "bearnaise", "beurre blanc", "crepe",
	},
	"spanish": {"paella", "tapas", "gazpacho", "patatas bravas", "romesco"},
	"middle eastern": {
		"shawarma", "falafel", "hummus", "tabbouleh", "tabouli", "kofta", "kofte", "shakshuka", "zaatar", "za atar",
		"fattoush", "baba ganoush", "labneh", "manakish",
	},
	"persian":    {"tahdig", "ghormeh sabzi", "fesenjan"},
	"moroccan":   {"tagine"},
	"cajun":      {"jambalaya", "gumbo", "etouffee", "po boy"},
	"tex-mex":    {"nachos", "queso"},
	"hawaiian":   {"poke", "loco moco", "huli huli", "kalua"},
	"jamaican":   {"jerk"},
	"cuban":      {"ropa vieja", "mojo", "cubano"},
	"german":     {"schnitzel", "bratwurst", "sauerbraten", "spaetzle", "spatzle", "sauerkraut"},
	"british":    {"shepherds pie", "fish and chips", "bangers and mash", "cottage pie", "toad in the hole", "beef wellington"},
	"hungarian":  {"goulash", "paprikash"},
	"polish":     {"pierogi", "pierogie", "kielbasa"},
	"russian":    {"stroganoff", "borscht", "pelmeni"},
	"peruvian":   {"lomo saltado", "ceviche", "aji amarillo"},
	"brazilian":  {"feijoada", "churrasco", "moqueca"},
	"filipino":   {"adobo", "lumpia", "pancit", "sinigang", "sisig"},
	"indonesian": {"nasi goreng", "rendang", "gado gado", "mie goreng"},
	"ethiopian":  {"injera", "doro wat", "berbere"},
	"southern":   {"biscuits and gravy", "shrimp and grits", "collard greens", "hush puppies", "chicken and waffles"},
}

// notCuisines are phrases where a cuisine's name isn't the cuisine: fries
// aren't French food, and a cod from the Pacific isn't Pacific Islander.
var notCuisines = []string{"french fries", "french fry", "french bread", "greek yogurt", "pacific", "english muffin"}

// nameKeys are the cuisine names a title may say, from the table: every
// standard value and alias. A region in a title ("Asian Slaw") says too
// little to guess from.
var nameKeys = func() map[string]string {
	m := map[string]string{}
	for _, e := range table {
		if !e.Standard {
			continue
		}
		m[e.Value] = e.Value
		for _, a := range e.Aliases {
			m[a] = e.Value
		}
	}
	return m
}()

var accents = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ä", "a", "ã", "a", "é", "e", "è", "e", "ê", "e", "ë", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i", "ó", "o", "ò", "o", "ô", "o", "ö", "o", "õ", "o", "ở", "o",
	"ú", "u", "ù", "u", "û", "u", "ü", "u", "ñ", "n", "ç", "c",
)

// words is s lowercase, without accents or apostrophes, with every other
// non-letter a space, padded with spaces so " phrase " matches whole words.
func words(s string) string {
	s = accents.Replace(strings.ToLower(s))
	s = strings.NewReplacer("'", "", "’", "").Replace(s)
	s = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return ' '
	}, s)
	return " " + strings.Join(strings.Fields(s), " ") + " "
}

// find is where phrase first appears in text as whole words, plural or not,
// or -1. text comes from words; phrase may hold a hyphen ("tex-mex"), which
// words turned into a space.
func find(text, phrase string) int {
	phrase = strings.TrimSpace(words(phrase))
	best := -1
	for _, form := range []string{phrase, phrase + "s", phrase + "es"} {
		if i := strings.Index(text, " "+form+" "); i >= 0 && (best < 0 || i < best) {
			best = i
		}
	}
	return best
}

// Guess returns the cuisine a recipe most likely is, from its tags and name,
// as one standard canonical value, or nil when nothing says. A tag naming a
// standard cuisine wins, then one named in the title, then the first dish
// word in it.
func Guess(name string, tags []string) []string {
	for _, t := range tags {
		if c := Canonical(t); IsStandard(c) {
			return []string{c}
		}
	}
	text := words(name)
	for _, phrase := range notCuisines {
		text = strings.ReplaceAll(text, " "+strings.TrimSpace(words(phrase))+" ", " ")
	}
	if c := earliest(text, nameKeys); c != "" {
		return []string{c}
	}
	dishes := map[string]string{}
	for c, list := range dishCuisines {
		for _, d := range list {
			dishes[d] = c
		}
	}
	if c := earliest(text, dishes); c != "" {
		return []string{c}
	}
	for _, t := range tags {
		if c := earliest(words(t), dishes); c != "" {
			return []string{c}
		}
	}
	return nil
}

// earliest is the cuisine of the key found first in text, the longer key on
// a tie ("pad see ew" before "pad"), or "".
func earliest(text string, keys map[string]string) string {
	best, bestAt, bestLen := "", -1, 0
	for key, c := range keys {
		at := find(text, key)
		if at < 0 {
			continue
		}
		if bestAt < 0 || at < bestAt || at == bestAt && (len(key) > bestLen || len(key) == bestLen && c < best) {
			best, bestAt, bestLen = c, at, len(key)
		}
	}
	return best
}

// Fill is a recipe's cuisines for reading: its own as Shown, or a guess
// when none of its own is a standard cuisine and the household never chose
// (chosen false). A recipe stored with only "Southern Europe" shows what its
// title names, or nothing.
func Fill(own []string, chosen bool, name string, tags []string) []string {
	if c := Shown(own); len(c) > 0 || chosen {
		return c
	}
	return Guess(name, tags)
}
