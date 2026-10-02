package recipes

import (
	"regexp"
	"slices"
	"strings"
)

// A step that heats oil in a pan ("Heat a large drizzle of oil in a medium pan
// over medium-high heat.") gives no time, but the cook waits for the pan all
// the same. When the sentence ends at the heat, the steps add ", 5 minutes"
// there, so the time draws as a timer, named "Heat Pan".

// HeatPanSeconds is how long the pan heats.
const HeatPanSeconds = 300

const heatPanSubject = "Heat Pan"

var (
	overHeatRe  = regexp.MustCompile(`(?i)\bover\s+(?:low|medium[- ]low|medium|medium[- ]high|high)\s+heat\b[ \t]*(?:[.;\n]|$)`)
	heatStartRe = regexp.MustCompile(`(?i)^\s*(?:in an? [^,]{1,40},\s*)?heat\s`)
	panFatRe    = regexp.MustCompile(`(?i)\b(?:oil|butter|ghee)\b`)
	panWordRe   = regexp.MustCompile(`(?i)\b(?:pan|skillet|pot|wok|saucepan)\b`)
)

// heatsPan reports whether a sentence heats fat in a pan: it starts with
// "Heat" (or "In a large pan, heat") and names both. A pot of water or the
// oven isn't a pan heating.
func heatsPan(sentence string) bool {
	return heatStartRe.MatchString(sentence) && panFatRe.MatchString(sentence) && panWordRe.MatchString(sentence) &&
		!strings.Contains(strings.ToLower(sentence), "water")
}

// withHeatTimer adds the pan's time to a rendered step, in its text and in
// the text segment the sentence ends in. A sentence that already gives a time,
// or goes on past the heat ("…over medium heat, then add the onion"), is left
// as written.
func withHeatTimer(st InstructionStep) InstructionStep {
	var joined strings.Builder
	for _, seg := range st.Segments {
		joined.WriteString(seg.Text)
	}
	if st.Text == "" || joined.String() != st.Text {
		return st
	}
	text := st.Text
	matches := overHeatRe.FindAllStringIndex(text, -1)
	for i := len(matches) - 1; i >= 0; i-- {
		at := matches[i][0] + len(strings.TrimRight(strings.TrimRight(text[matches[i][0]:matches[i][1]], ".;\n"), " \t"))
		start := strings.LastIndexAny(text[:matches[i][0]], ".;!?\n") + 1
		sentence := text[start:at]
		if !heatsPan(sentence) || durationRe.MatchString(sentence) {
			continue
		}
		segments, ok := insertInText(st.Segments, at, ", 5 minutes")
		if !ok {
			continue
		}
		if st.Original == "" {
			st.Original = st.Text
		}
		text = text[:at] + ", 5 minutes" + text[at:]
		st.Segments = segments
	}
	st.Text = text
	return st
}

// insertInText puts words at a position in the segments' joined text, inside
// the text segment that holds it; never inside an ingredient's name.
func insertInText(segments []Segment, at int, words string) ([]Segment, bool) {
	pos := 0
	for i, seg := range segments {
		end := pos + len(seg.Text)
		if at > pos && at <= end && seg.Kind == SegmentText {
			out := slices.Clone(segments)
			out[i].Text = seg.Text[:at-pos] + words + seg.Text[at-pos:]
			return out, true
		}
		pos = end
	}
	return segments, false
}
