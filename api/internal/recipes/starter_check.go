package recipes

import (
	"net/url"
	"path"
	"regexp"
	"strings"
)

// A starter recipe whose name was changed without its photo, subtitle and
// description (a "Lemon Herb Salmon" showing enchiladas) reads as broken. The
// source page and the photo's file name usually name the dish, so a name that
// shares too few words with either one is flagged for a person to fix.

var (
	slugWordRe   = regexp.MustCompile(`[a-z]+`)
	slugIDTailRe = regexp.MustCompile(`(?:-[0-9a-f]{6,})+$`)
)

// slugStopWords say nothing about which dish it is.
var slugStopWords = map[string]bool{
	"and": true, "with": true, "the": true, "style": true, "for": true, "over": true, "your": true,
	"hellofresh": true, "recipe": true, "recipes": true, "jpg": true, "jpeg": true, "png": true, "webp": true,
}

func dishWords(s string) []string {
	var out []string
	for _, w := range slugWordRe.FindAllString(strings.ToLower(s), -1) {
		if len(w) >= 3 && !slugStopWords[w] {
			out = append(out, w)
		}
	}
	return out
}

// slugOf is the words in a URL's last path segment, without the IDs sites
// append ("street-cart-style-chicken-bowls-d9b9cadb.jpg").
func slugOf(raw string) []string {
	u, err := url.Parse(raw)
	if err != nil || u.Path == "" {
		return nil
	}
	base := strings.ToLower(path.Base(u.Path))
	base = strings.TrimSuffix(base, path.Ext(base))
	return dishWords(slugIDTailRe.ReplaceAllString(base, ""))
}

func sameWord(a, b string) bool {
	if a == b {
		return true
	}
	// "taco" and "tacos", "enchilada" and "enchiladas".
	short, long := a, b
	if len(short) > len(long) {
		short, long = long, short
	}
	return len(short) >= 4 && strings.HasPrefix(long, short)
}

// overlap is the share of words found in other.
func overlap(words, other []string) float64 {
	if len(words) == 0 {
		return 0
	}
	n := 0
	for _, w := range words {
		for _, o := range other {
			if sameWord(w, o) {
				n++
				break
			}
		}
	}
	return float64(n) / float64(len(words))
}

// NameMismatch reports a recipe whose name doesn't name the dish its source
// page or photo does: it returns the slug that disagrees, or "" when the name
// fits (or there is nothing with words to compare, such as a DinnerOS photo
// named by ID). At least two slug words are needed to judge.
func NameMismatch(r Recipe) string {
	name := dishWords(r.Name)
	if len(name) == 0 {
		return ""
	}
	for _, raw := range []string{r.SourceURL, r.ImageURL} {
		slug := slugOf(raw)
		if len(slug) < 2 {
			continue
		}
		if overlap(name, slug) < 0.5 && overlap(slug, name) < 0.5 {
			return strings.Join(slug, "-")
		}
	}
	return ""
}
