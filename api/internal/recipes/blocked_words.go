package recipes

import (
	_ "embed"
	"errors"
	"strings"
	"unicode"
)

// Shared Recipes word filter (decision 744). Apple asks apps where people
// share content to filter objectionable material before it is posted. A
// recipe whose words match blocked_words.txt is never published to the
// catalog, whatever the household's sharing choice, and sharing one by hand
// is refused with ObjectionableMessage.

//go:embed blocked_words.txt
var blockedWordsFile string

// ErrObjectionable means a recipe has words Shared Recipes doesn't take.
var ErrObjectionable = errors.New("recipes: objectionable words")

// ObjectionableMessage is what the app shows when sharing is refused.
const ObjectionableMessage = "Shared Recipes can’t take some of the words in this recipe. Change them, or keep it just for your household."

var blockedExact, blockedPrefixes = loadBlockedWords(blockedWordsFile)

func loadBlockedWords(file string) (map[string]bool, []string) {
	exact := map[string]bool{}
	var prefixes []string
	for line := range strings.SplitSeq(file, "\n") {
		w := strings.ToLower(strings.TrimSpace(line))
		if w == "" || strings.HasPrefix(w, "#") {
			continue
		}
		if p, ok := strings.CutSuffix(w, "*"); ok {
			prefixes = append(prefixes, p)
			continue
		}
		exact[w] = true
	}
	return exact, prefixes
}

// leet undoes the usual letter swaps ("sh1t", "@ss") inside a word that has
// at least one letter, so an oven temperature is never read as a word.
var leet = strings.NewReplacer("0", "o", "1", "i", "3", "e", "4", "a", "5", "s", "7", "t", "@", "a", "$", "s")

// Objectionable reports whether any text a recipe shows in Shared Recipes
// (name, headline, description, steps, ingredient names, cuisines, tags)
// has a blocked word.
func Objectionable(r Recipe) bool {
	texts := []string{r.Name, r.Headline, r.Description}
	for _, s := range r.Steps {
		texts = append(texts, s.Text)
	}
	for _, line := range r.Ingredients {
		texts = append(texts, line.Name)
	}
	texts = append(texts, r.Cuisines...)
	texts = append(texts, r.Tags...)
	for _, t := range texts {
		if HasBlockedWord(t) {
			return true
		}
	}
	return false
}

// HasBlockedWord reports whether s has a blocked word.
func HasBlockedWord(s string) bool {
	for _, raw := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return unicode.IsSpace(r) || (unicode.IsPunct(r) && r != '@' && r != '$') || r == '-' || r == '/'
	}) {
		word := raw
		if strings.IndexFunc(raw, unicode.IsLetter) >= 0 {
			word = leet.Replace(raw)
		}
		word = strings.TrimFunc(word, func(r rune) bool { return !unicode.IsLetter(r) })
		if word == "" {
			continue
		}
		if blockedExact[word] {
			return true
		}
		for _, p := range blockedPrefixes {
			if strings.HasPrefix(word, p) {
				return true
			}
		}
	}
	return false
}
