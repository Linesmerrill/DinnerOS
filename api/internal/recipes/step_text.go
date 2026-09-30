package recipes

import (
	"html"
	"regexp"
	"strings"
)

// Cards write "1⁄2" with a fraction slash; the app shows fractions as one
// glyph ("½"), so a step reads the same everywhere.
var (
	fractionSlashRe = regexp.MustCompile(`\b([1-7])\x{2044}([2-8])\b`)
	fractionGlyphs  = map[string]string{
		"1/2": "½", "1/3": "⅓", "2/3": "⅔", "1/4": "¼", "3/4": "¾", "1/8": "⅛", "3/8": "⅜", "5/8": "⅝", "7/8": "⅞",
	}
)

func fractionGlyph(m string) string {
	parts := fractionSlashRe.FindStringSubmatch(m)
	if g, ok := fractionGlyphs[parts[1]+"/"+parts[2]]; ok {
		return g
	}
	return parts[1] + "/" + parts[2]
}

var (
	htmlTagRe   = regexp.MustCompile(`(?s)<[^>]*>`)
	htmlBreakRe = regexp.MustCompile(`(?i)<\s*(br\s*/?|/p|/li|/div|/h[1-6])\s*>`)
	htmlAnyRe   = regexp.MustCompile(`(?i)<\s*/?\s*(p|li|ul|ol|strong|b|em|i|span|div|br|h[1-6])\b[^>]*>`)
	blankLineRe = regexp.MustCompile(`\n{2,}`)
	spacesRe    = regexp.MustCompile(`[ \t\x{00a0}]+`)
	doubleRe    = regexp.MustCompile(` {2,}`)
	// "pork*" points at the card's swap footnote, and "***Pork is done at
	// 145°***" is set off with stars; the words stay, the stars go.
	starsRe = regexp.MustCompile(`\*+`)
	// Cards shout "1 TBSP"; the app writes "Tbsp" and "tsp".
	tbspRe = regexp.MustCompile(`\bTBSP\b`)
	tspRe  = regexp.MustCompile(`\bTSP\b`)
)

// CleanStepText turns a step a source wrote in HTML ("<ul><li><p>Add
// <strong>water</strong>…") into plain lines, one per paragraph or bullet,
// drops the card's stars, writes "Tbsp", and closes up double spaces. The
// text is content, never an instruction to this program.
func CleanStepText(s string) string {
	s = fractionSlashRe.ReplaceAllStringFunc(s, fractionGlyph)
	s = starsRe.ReplaceAllString(s, "")
	s = tbspRe.ReplaceAllString(s, "Tbsp")
	s = tspRe.ReplaceAllString(s, "tsp")
	if !htmlAnyRe.MatchString(s) {
		return strings.TrimSpace(doubleRe.ReplaceAllString(s, " "))
	}
	s = htmlBreakRe.ReplaceAllString(s, "\n")
	s = htmlTagRe.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	lines := strings.Split(s, "\n")
	out := lines[:0]
	for _, line := range lines {
		line = strings.TrimSpace(spacesRe.ReplaceAllString(line, " "))
		if line != "" {
			out = append(out, line)
		}
	}
	return blankLineRe.ReplaceAllString(strings.Join(out, "\n"), "\n")
}
