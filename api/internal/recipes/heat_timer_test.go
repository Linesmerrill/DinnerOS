package recipes

import (
	"strings"
	"testing"
)

func TestHeatingOilInAPanGetsAFiveMinuteTimer(t *testing.T) {
	lines := []RecipeIngredient{
		instructionLine("", "Bell Pepper", "1", "count"),
		instructionLine("", "Chicken Breasts", "10", "oz"),
		instructionLine("", "Olive Oil", "2", "tbsp"),
	}
	cases := []struct {
		step  string
		added bool
	}{
		{"Heat a large drizzle of oil in a medium pan over medium-high heat. Add bell pepper; cook 5 minutes.", true},
		{"In a large skillet, heat a drizzle of olive oil over medium heat.", true},
		{"Heat 2 tablespoons olive oil in a large skillet over medium high heat\nAdd the chicken.", true},
		// Already timed, goes on past the heat, or isn't oil in a pan: no time added.
		{"Heat a drizzle of oil in a pan over medium heat until shimmering, 2 minutes.", false},
		{"Heat a drizzle of oil in a large pan over medium heat, then add chicken.", false},
		{"Heat a large pot of salted water over high heat.", false},
		{"Heat oven to 425 degrees.", false},
	}
	for _, c := range cases {
		in := Annotate(instructionRecipe(lines, c.step), 2, nil, true)
		st := in.Steps[0]
		if joined(st) != st.Text {
			t.Errorf("%q: segments %q differ from text %q", c.step, joined(st), st.Text)
		}
		if added := strings.Contains(st.Text, "heat, 5 minutes"); added != c.added {
			t.Errorf("%q: added %v, want %v: %q", c.step, added, c.added, st.Text)
		}
		var heat []StepTimer
		for _, timer := range st.Timers {
			if timer.Text == "5 minutes" && timer.Subject == heatPanSubject {
				heat = append(heat, timer)
			}
		}
		if c.added && (len(heat) != 1 || heat[0].StartSeconds != HeatPanSeconds) {
			t.Errorf("%q: heat timers %+v", c.step, st.Timers)
		}
		if c.added && st.Original != c.step {
			t.Errorf("%q: original = %q", c.step, st.Original)
		}
		for _, f := range CheckSteps(in) {
			if f.Code != FindingUnusedIngredient {
				t.Errorf("%q: %s %q", c.step, f.Code, f.Detail)
			}
		}
	}
}

func TestATimeWrittenForHeatingThePanIsNamedHeatPan(t *testing.T) {
	r := instructionRecipe([]RecipeIngredient{instructionLine("", "Olive Oil", "2", "tbsp")},
		"Heat a drizzle of oil in a pan over medium heat until shimmering, 2 minutes.")
	timers := Annotate(r, 2, nil, true).Steps[0].Timers
	if len(timers) != 1 || timers[0].Subject != heatPanSubject {
		t.Errorf("timers = %+v", timers)
	}
}
