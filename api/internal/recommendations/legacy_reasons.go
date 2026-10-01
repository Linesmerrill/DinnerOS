package recommendations

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// A proposal keeps the reasons it was made with, so one saved before
// Autopilot's wording changed still reads the old way until its week passes.
// currentReason rewrites the wordings retired since, as a saved proposal is
// read, so every screen shows today's words without reshuffling the picks.

var (
	oldFavorRe    = regexp.MustCompile(`^Lately you (favor|pass on) (.+)$`)
	oldFitsRe     = regexp.MustCompile(`^Fits the (\d+) min you have free (\w+)$`)
	oldLongerRe   = regexp.MustCompile(`^Longer than the (\d+) min you have free (\w+)$`)
	oldUnknownRe  = regexp.MustCompile(`^Cook time unknown for the (\d+) min you have free (\w+)$`)
	proteinLabels = func() map[string]bool {
		labels := map[string]bool{}
		for _, o := range ProteinOptions {
			labels[o.Label] = true
		}
		return labels
	}()
)

// openEveningMinutes mirrors the baseline model: an evening with this much
// free isn't a limit worth naming.
const openEveningMinutes = 210

// currentReason returns a saved reason in today's words, and false when
// today's model wouldn't give it at all (a "limit" that was an open evening).
func currentReason(r Reason) (Reason, bool) {
	if m := oldFavorRe.FindStringSubmatch(r.Text); m != nil {
		// Proteins and cuisines were capitalized; meal categories and time
		// bands ("pasta", "quick meals") weren't.
		label := m[2]
		switch {
		case proteinLabels[label]:
			label = strings.ToLower(label)
		case label != strings.ToLower(label):
			label += " food"
		}
		if m[1] == "pass on" {
			r.Text = "You've been skipping " + label + " lately"
		} else {
			r.Text = "You've been choosing more " + label + " lately"
		}
		return r, true
	}
	for _, old := range []struct {
		re   *regexp.Regexp
		text string
	}{
		{oldFitsRe, "Fits the %s you have free %s evening"},
		{oldLongerRe, "Longer than the %s you have free %s evening"},
		{oldUnknownRe, "Cook time unknown, and you only have %s free %s evening"},
	} {
		m := old.re.FindStringSubmatch(r.Text)
		if m == nil {
			continue
		}
		minutes, _ := strconv.Atoi(m[1])
		if minutes >= openEveningMinutes {
			return r, false
		}
		r.Text = fmt.Sprintf(old.text, spanText(minutes), m[2])
		return r, true
	}
	return r, true
}

// spanText says a length of time for people: "45 min", "2 hr", "1 hr 30 min".
func spanText(minutes int) string {
	h, m := minutes/60, minutes%60
	switch {
	case h == 0:
		return fmt.Sprintf("%d min", m)
	case m == 0:
		return fmt.Sprintf("%d hr", h)
	}
	return fmt.Sprintf("%d hr %d min", h, m)
}
