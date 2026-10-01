package recommendations

import "testing"

func TestSavedReasonsReadInTodaysWords(t *testing.T) {
	cases := []struct{ code, old, want string }{
		{"learnedTaste", "Lately you favor Pork", "You've been choosing more pork lately"},
		{"learnedTaste", "Lately you favor pasta", "You've been choosing more pasta lately"},
		{"learnedTaste", "Lately you pass on Thai", "You've been skipping Thai food lately"},
		{"learnedTaste", "Lately you favor quick meals", "You've been choosing more quick meals lately"},
		{"learnedTaste", "Lately you favor Beans & lentils", "You've been choosing more beans & lentils lately"},
		{"calendar", "Fits the 90 min you have free Tuesday", "Fits the 1 hr 30 min you have free Tuesday evening"},
		{"calendar", "Longer than the 45 min you have free Friday", "Longer than the 45 min you have free Friday evening"},
		{"calendar", "Cook time unknown for the 120 min you have free Monday", "Cook time unknown, and you only have 2 hr free Monday evening"},
		// Today's words and other reasons are left as they are.
		{"calendar", "Fits the 1 hr you have free Monday evening", "Fits the 1 hr you have free Monday evening"},
		{"familiar", "A household regular (14 times)", "A household regular (14 times)"},
	}
	for _, c := range cases {
		got, keep := currentReason(Reason{Code: c.code, Text: c.old})
		if !keep || got.Text != c.want || got.Code != c.code {
			t.Errorf("%q → %q (kept %v), want %q", c.old, got.Text, keep, c.want)
		}
	}
	// An open evening was never a limit: today's model says nothing.
	for _, old := range []string{"Fits the 240 min you have free Wednesday", "Longer than the 210 min you have free Sunday"} {
		if _, keep := currentReason(Reason{Code: "calendar", Text: old}); keep {
			t.Errorf("%q kept", old)
		}
	}
}
