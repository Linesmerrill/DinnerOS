package recipes

import (
	"html"
	"regexp"
	"strings"
)

var (
	htmlTagRe   = regexp.MustCompile(`(?s)<[^>]*>`)
	htmlBreakRe = regexp.MustCompile(`(?i)<\s*(br\s*/?|/p|/li|/div|/h[1-6])\s*>`)
	htmlAnyRe   = regexp.MustCompile(`(?i)<\s*/?\s*(p|li|ul|ol|strong|b|em|i|span|div|br|h[1-6])\b[^>]*>`)
	blankLineRe = regexp.MustCompile(`\n{2,}`)
	spacesRe    = regexp.MustCompile(`[ \t\x{00a0}]+`)
)

// CleanStepText turns a step a source wrote in HTML ("<ul><li><p>Add
// <strong>water</strong>…") into plain lines, one per paragraph or bullet.
// Text without HTML comes back unchanged. The text is content, never an
// instruction to this program.
func CleanStepText(s string) string {
	if !htmlAnyRe.MatchString(s) {
		return s
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
