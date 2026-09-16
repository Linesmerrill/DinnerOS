package baseline

import (
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot"
)

// TestLongCookRule: "Sunday is a longer meal" prefers a genuine long cook — an
// hour or more, or a whole or large cut — over a familiar, well-rated quick
// smoker meal (the 35-minute pork tenderloin), and still fills the day with
// the quicker meal when nothing long is left.
func TestLongCookRule(t *testing.T) {
	p := New(Options{})
	sunday := func(catalog ...autopilot.Item) autopilot.Input {
		in := input(catalog...)
		in.Preferences.PlanDays = []autopilot.Day{autopilot.Sunday}
		in.Preferences.MealsPerWeek = 1
		in.Preferences.Equipment = []string{"smoker"}
		in.Preferences.Rules = []autopilot.WeekdayRule{{
			Day: autopilot.Sunday, Label: "Sunday smoker night", Proteins: []string{"chicken", "pork"}, Methods: []string{"smoker"},
			TimeBand: autopilot.BandLong, Frequency: autopilot.AtMostOnce,
		}}
		// The tenderloin is a well-rated household regular; the rest are new.
		for _, w := range []string{"2026-W01", "2026-W08", "2026-W15", "2026-W22"} {
			in.History = append(in.History, autopilot.Interaction{ItemID: "pork-tenderloin", Kind: autopilot.KindCooked, Week: w})
		}
		in.Ratings = []autopilot.Rating{rate("pork-tenderloin", 5)}
		return in
	}
	tenderloin := meal("pork-tenderloin", proteins("pork"), methods("smoker"), minutes(35))
	shoulder := meal("smoked-pork-shoulder", proteins("pork"), methods("smoker"), minutes(300))
	longCook := func(it *autopilot.Item) { it.LongCook = true }
	// A whole chicken whose recipe card understates the time.
	wholeChicken := meal("whole-chicken", proteins("chicken"), methods("smoker"), minutes(30), longCook)

	for _, tt := range []struct {
		name    string
		catalog []autopilot.Item
		want    string
	}{
		{"an hour or more wins over the familiar quick cook", []autopilot.Item{tenderloin, shoulder}, "smoked-pork-shoulder"},
		{"a whole cut wins whatever its stated time", []autopilot.Item{tenderloin, wholeChicken}, "whole-chicken"},
		{"with nothing long, the quicker match still fills the day", []autopilot.Item{tenderloin}, "pork-tenderloin"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			res := generate(t, p, sunday(tt.catalog...))
			sun := slotOn(res, autopilot.Sunday)
			if sun == nil || sun.ItemID != tt.want {
				t.Fatalf("sunday = %s; want %s", describe(res), tt.want)
			}
		})
	}

	t.Run("signals", func(t *testing.T) {
		ranked := rank(t, p, sunday(tenderloin, shoulder, wholeChicken), autopilot.Sunday)
		signals := map[string]map[string]float64{}
		for _, rec := range ranked.Items {
			signals[rec.ItemID] = rec.Signals
		}
		if s := signals["smoked-pork-shoulder"]; s[SignalTimeFit] != 1 || s[SignalRule] != 1 {
			t.Errorf("shoulder signals = %v; want full time fit and rule", s)
		}
		if s := signals["whole-chicken"]; s[SignalTimeFit] != 1 || s[SignalRule] != 1 {
			t.Errorf("whole chicken signals = %v; a long cut is a long cook", s)
		}
		if s := signals["pork-tenderloin"]; s[SignalTimeFit] != -0.5 || s[SignalRule] != shortOnLongRuleFactor {
			t.Errorf("tenderloin signals = %v; want the short-cook penalty and half the rule", s)
		}
	})

	t.Run("an unknown cook time is neutral", func(t *testing.T) {
		unknown := meal("smoked-mystery", proteins("pork"), methods("smoker"), minutes(0))
		ranked := rank(t, p, sunday(unknown), autopilot.Sunday)
		if s := ranked.Items[0].Signals; s[SignalTimeFit] != 0 || s[SignalRule] != 1 {
			t.Errorf("unknown-time signals = %v", s)
		}
	})

	t.Run("a day without a long-cook rule is unchanged", func(t *testing.T) {
		in := sunday(tenderloin, shoulder)
		in.Preferences.Rules[0].TimeBand = ""
		ranked := rank(t, p, in, autopilot.Sunday)
		for _, rec := range ranked.Items {
			if rec.Signals[SignalTimeFit] != 0 || rec.Signals[SignalRule] != 1 {
				t.Errorf("%s signals = %v", rec.ItemID, rec.Signals)
			}
		}
	})
}
