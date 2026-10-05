package recipes

import (
	"fmt"
	"regexp"
	"strings"
)

// The meal-kit card ends with how hot to cook its meat; the steps do too.
// These are USDA's safe minimum internal temperatures
// (fsis.usda.gov, "Safe Minimum Internal Temperature Chart"), for the meat
// and seafood the household is actually cooking: a protein swap ("Ground Beef"
// for "Ground Pork") reads as the swap, and a left-out one isn't listed.

// SafeTemp is one ingredient's safe minimum internal temperature.
type SafeTemp struct {
	// Name is the ingredient as the household cooks it.
	Name       string
	Fahrenheit int
	// RestMinutes is the rest after cooking that the temperature counts on
	// (3 for whole cuts of beef, pork, lamb, and veal), or 0.
	RestMinutes int
	// Text is the line to show: "Chicken Breasts: 165°F".
	Text string
}

// SafeTempsSource is the line shown under the temperatures.
const SafeTempsSource = "Safe minimum internal temperatures, from the USDA."

var (
	// Not raw meat, whatever the name says: stock, seasonings, cured and
	// ready-to-eat products.
	notRawMeatRe = regexp.MustCompile(`(?i)\b(stock|broth|bouillon|concentrate|base|seasoning|spice|gravy|sauce|bits|jerky|fat|drippings|demi|glace|flavou?r|pepperoni|salami|prosciutto|pancetta|bacon|deli|sliced\s+ham|cooked|smoked|fully|rotisserie|imitation|canned|buns?|pulled|salad|kit|gyoza|dumplings?|potstickers?|wontons?)\b`)
	poultryRe    = regexp.MustCompile(`(?i)\b(chicken|turkey|duck|goose|poultry|cornish hen)\b`)
	// Ground meat counts only with a meat named ("ground cumin" isn't); a
	// sausage is ground meat by itself.
	groundRe  = regexp.MustCompile(`(?i)\b(ground|mince|minced)\b`)
	sausageRe = regexp.MustCompile(`(?i)\b(sausages?|bratwurst|chorizo|meatballs?|kielbasa)\b`)
	redMeatRe = regexp.MustCompile(`(?i)\b(beef|pork|lamb|veal|steaks?|sirloin|ribeye|tenderloin|chops|loin|brisket|flank|skirt|ham|venison|bison)\b`)
	seafoodRe = regexp.MustCompile(`(?i)\b(salmon|cod|tilapia|halibut|trout|tuna|mahi|catfish|haddock|pollock|snapper|barramundi|swordfish|fish|shrimp|prawns?|scallops?|crab|lobster|mussels|clams|seafood)\b`)
)

// safeTempFor returns the temperature for an ingredient name, and false when
// it isn't raw meat or seafood.
func safeTempFor(name string) (fahrenheit, rest int, ok bool) {
	if notRawMeatRe.MatchString(name) {
		return 0, 0, false
	}
	switch {
	case poultryRe.MatchString(name):
		return 165, 0, true
	case sausageRe.MatchString(name), groundRe.MatchString(name) && redMeatRe.MatchString(name):
		return 160, 0, true
	case redMeatRe.MatchString(name):
		return 145, 3, true
	case seafoodRe.MatchString(name):
		return 145, 0, true
	}
	return 0, 0, false
}

// safeTemps lists the temperatures for the meat and seafood the household
// cooks in the recipe, in the recipe's order, one per name.
func safeTemps(ingredients []IngredientState) []SafeTemp {
	var out []SafeTemp
	seen := map[string]bool{}
	for _, ing := range ingredients {
		if ing.LeftOut != nil || ing.Component != nil {
			continue
		}
		name := ing.Name
		if ing.SwapName != "" {
			name = ing.SwapName
		}
		name = strings.TrimSpace(name)
		f, rest, ok := safeTempFor(name)
		if !ok || seen[strings.ToLower(name)] {
			continue
		}
		seen[strings.ToLower(name)] = true
		text := fmt.Sprintf("%s: %d°F", titleWords(name), f)
		if rest > 0 {
			text += fmt.Sprintf(", then rest %d minutes", rest)
		}
		out = append(out, SafeTemp{Name: titleWords(name), Fahrenheit: f, RestMinutes: rest, Text: text})
	}
	return out
}
