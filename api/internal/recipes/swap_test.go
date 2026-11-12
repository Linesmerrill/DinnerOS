package recipes

import (
	"errors"
	"strings"
	"testing"
)

// Ramen for lo mein: every step that names the ramen names the lo mein, the
// step's capitals are kept, and the boil time is found and can be replaced
// with lo mein's (decision 667).
func TestSwapStepsRenamesAndFindsTheCookTime(t *testing.T) {
	steps := []string{
		"Bring a large pot of salted water to a boil.",
		"Add ramen noodles to the boiling water; cook until tender, 2-3 minutes. Drain and rinse.",
		"Ramen noodles go in last: toss the ramen with the sauce, then cook 1 minute more.",
		"Stir in the bell pepper and cook 4 minutes.",
	}
	res, err := SwapSteps(SwapInput{OldName: "Ramen Noodles", NewName: "Lo Mein Noodles", Steps: steps})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		steps[0],
		"Add lo mein noodles to the boiling water; cook until tender, 2-3 minutes. Drain and rinse.",
		"Lo mein noodles go in last: toss the lo mein with the sauce, then cook 1 minute more.",
		steps[3],
	}
	for i := range want {
		if res.Steps[i] != want[i] {
			t.Errorf("step %d = %q, want %q", i+1, res.Steps[i], want[i])
		}
	}
	if res.Changed[0] || !res.Changed[1] || !res.Changed[2] || res.Changed[3] {
		t.Errorf("changed = %v", res.Changed)
	}
	if res.CookTime == nil || res.CookTime.Text() != "4 to 6 minutes" || res.CookTime.Suggested() != 6 {
		t.Fatalf("cook time = %+v", res.CookTime)
	}
	// The boil in step 2, and the minute more in step 3; never the pepper's.
	if len(res.Times) != 2 || res.Times[0].Step != 1 || res.Times[0].Text != "2-3 minutes" || res.Times[0].Minutes != 3 ||
		res.Times[1].Step != 2 || res.Times[1].Text != "1 minute" {
		t.Errorf("times = %+v", res.Times)
	}

	all, _ := SwapSteps(SwapInput{OldName: "Ramen Noodles", NewName: "Lo Mein Noodles", Steps: steps, Minutes: 6})
	// Only the boil takes the picked time; the finish keeps its minute.
	if !strings.Contains(all.Steps[1], "tender, 6 minutes") || !strings.Contains(all.Steps[2], "cook 1 minute more") {
		t.Errorf("with 6 minutes, all steps = %q", all.Steps)
	}
	picked, err := SwapSteps(SwapInput{OldName: "Ramen Noodles", NewName: "Lo Mein Noodles", Steps: steps[:2], Minutes: 6})
	if err != nil {
		t.Fatal(err)
	}
	if want := "Add lo mein noodles to the boiling water; cook until tender, 6 minutes. Drain and rinse."; picked.Steps[1] != want {
		t.Errorf("with 6 minutes: %q, want %q", picked.Steps[1], want)
	}
	// Already lo mein: applying the time again doesn't rename anything twice.
	again, _ := SwapSteps(SwapInput{OldName: "Ramen Noodles", NewName: "Lo Mein Noodles", Steps: picked.Steps, Minutes: 7})
	if !strings.Contains(again.Steps[1], "lo mein noodles") || !strings.Contains(again.Steps[1], "7 minutes") ||
		strings.Contains(again.Steps[1], "lo lo") {
		t.Errorf("again: %q", again.Steps[1])
	}
}

// A name with nothing in common is swapped whole; one the table doesn't know
// has no cook time, and words inside other words are left alone.
func TestSwapStepsLeavesOtherWordsAlone(t *testing.T) {
	res, err := SwapSteps(SwapInput{
		OldName: "Sour Cream", NewName: "Greek Yogurt",
		Steps: []string{"Mix sour cream with lime. Top with more sour cream and creamy avocado."},
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := "Mix greek yogurt with lime. Top with more greek yogurt and creamy avocado."; res.Steps[0] != want {
		t.Errorf("step = %q, want %q", res.Steps[0], want)
	}
	if res.CookTime != nil || len(res.Times) != 0 {
		t.Errorf("cook time = %+v, times = %+v", res.CookTime, res.Times)
	}
}

func TestSwapStepsRejectsWhatItCantRead(t *testing.T) {
	for _, in := range []SwapInput{
		{OldName: "", NewName: "Lo Mein", Steps: []string{"a"}},
		{OldName: "Ramen", NewName: "Lo Mein", Steps: []string{"a"}, Minutes: 999},
		{OldName: "Ramen", NewName: strings.Repeat("a", 201)},
	} {
		if _, err := SwapSteps(in); !errors.Is(err, ErrInvalidEdit) {
			t.Errorf("SwapSteps(%+v) err = %v", in, err)
		}
	}
	if got := cookTimeSentence("Lo Mein Noodles", "4 to 6 minutes"); got != "Lo mein noodles usually cook 4 to 6 minutes." {
		t.Errorf("sentence = %q", got)
	}
}
