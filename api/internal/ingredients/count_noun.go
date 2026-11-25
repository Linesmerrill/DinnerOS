package ingredients

import "strings"

// CountNoun is the word a bare count of an item reads with, so "3" on its own
// reads "3 onions" (Hark's build 121 review: Yellow Onions showed just "3").
//
// A name written in the plural ("Yellow Onions", "Large Eggs") counts its last
// word: "1 onion", "3 onions". Any other name counts itself: "1 lemon",
// "2 lemons", "1 coconut milk". Both are lowercase; ok is false for an empty
// name.
func CountNoun(name string) (one, other string, ok bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if i := strings.IndexAny(name, "(,"); i > 0 {
		name = strings.TrimSpace(name[:i])
	}
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		return "", "", false
	}
	last := name
	if i := strings.LastIndexByte(name, ' '); i >= 0 {
		last = name[i+1:]
	}
	if s := singularWord(last); s != last {
		return s, last, true
	}
	head := strings.TrimSuffix(name, last)
	return name, head + pluralWord(last), true
}

// singularWord is a plural word's singular, or the word as it is when it
// doesn't read as a plural.
func singularWord(w string) string {
	switch w {
	case "leaves":
		return "leaf"
	case "loaves":
		return "loaf"
	case "halves":
		return "half"
	}
	switch {
	case len(w) <= 3, !strings.HasSuffix(w, "s"), strings.HasSuffix(w, "ss"), strings.HasSuffix(w, "us"):
		return w
	case strings.HasSuffix(w, "ies"):
		return w[:len(w)-3] + "y"
	case strings.HasSuffix(w, "oes"), strings.HasSuffix(w, "ches"), strings.HasSuffix(w, "shes"),
		strings.HasSuffix(w, "xes"), strings.HasSuffix(w, "sses"):
		return w[:len(w)-2]
	}
	return w[:len(w)-1]
}

// pluralWord is a singular word's plural: "lemon" → "lemons", "tomato" →
// "tomatoes", "cherry" → "cherries", "avocado" → "avocados".
func pluralWord(w string) string {
	switch {
	case w == "tomato", w == "potato":
		return w + "es"
	case strings.HasSuffix(w, "s"), strings.HasSuffix(w, "x"), strings.HasSuffix(w, "z"),
		strings.HasSuffix(w, "ch"), strings.HasSuffix(w, "sh"):
		return w + "es"
	case strings.HasSuffix(w, "y") && len(w) > 1 && !strings.ContainsRune("aeiou", rune(w[len(w)-2])):
		return w[:len(w)-1] + "ies"
	}
	return w + "s"
}
