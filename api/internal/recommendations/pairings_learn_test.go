package recommendations

import (
	"fmt"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// week builds "2026-Www".
func week(n int) string { return fmt.Sprintf("2026-W%02d", n) }

func TestPairingStatsMath(t *testing.T) {
	st := PairingStats{Category: "pasta", AddonID: rBread, Together: 8, CategoryWeeks: 10, AddonWeeks: 9, TotalWeeks: 20}
	if got := st.Confidence(); got != 0.8 {
		t.Errorf("Confidence() = %v, want 0.8", got)
	}
	if got := st.OtherWeeks(); got != 10 {
		t.Errorf("OtherWeeks() = %d, want 10", got)
	}
	if got := st.OtherRate(); got != 0.1 {
		t.Errorf("OtherRate() = %v, want 0.1", got)
	}
	if got := st.BaseRate(); got != 0.45 {
		t.Errorf("BaseRate() = %v, want 0.45", got)
	}
	if !st.Qualified() {
		t.Errorf("%+v should qualify", st)
	}
}

func TestPairingStatsQualified(t *testing.T) {
	for _, tt := range []struct {
		name string
		st   PairingStats
		want bool
	}{
		{"strong and specific", PairingStats{Together: 8, CategoryWeeks: 10, AddonWeeks: 9, TotalWeeks: 20}, true},
		{"too few weeks together", PairingStats{Together: 2, CategoryWeeks: 2, AddonWeeks: 2, TotalWeeks: 20}, false},
		{"confidence too low", PairingStats{Together: 4, CategoryWeeks: 10, AddonWeeks: 4, TotalWeeks: 20}, false},
		// An add-on the household has nearly every week isn't a pairing: its
		// rate outside the category is just as high.
		{"weekly habit", PairingStats{Together: 18, CategoryWeeks: 20, AddonWeeks: 36, TotalWeeks: 40}, false},
		{"small lead over other weeks", PairingStats{Together: 9, CategoryWeeks: 10, AddonWeeks: 17, TotalWeeks: 20}, false},
		// Almost every week has the category, so there is nothing to compare.
		{"no weeks to compare", PairingStats{Together: 18, CategoryWeeks: 20, AddonWeeks: 18, TotalWeeks: 22}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.st.Qualified(); got != tt.want {
				t.Errorf("Qualified() = %v, want %v (confidence %.2f, other %.2f, lift %.2f)",
					got, tt.want, tt.st.Confidence(), tt.st.OtherRate(), tt.st.Lift())
			}
		})
	}
}

// TestPairingHistoryCountsUniqueWeeks checks the inflation guard: an add-on
// whose order weeks repeat (many weekly menu aliases collapse into one
// imported recipe) still counts one week per ISO week, and a week that is
// both ordered and planned counts once.
func TestPairingHistoryCountsUniqueWeeks(t *testing.T) {
	pasta := recipes.Recipe{ID: rRigatoni, Name: "Rigatoni"}
	bread := recipes.Recipe{ID: rBread, Name: "Garlic Bread", IsAddon: true}
	for i := 1; i <= 6; i++ {
		pasta.OrderWeeks = append(pasta.OrderWeeks, week(i))
		// The same week three times, as a collapsed alias would report it.
		bread.OrderWeeks = append(bread.OrderWeeks, week(i), week(i), week(i))
	}
	soup := recipes.Recipe{ID: rSoup, Name: "Onion Soup", OrderWeeks: []string{week(7), week(8), week(9), week(10), week(11), week(12)}}
	catalog := []recipes.Recipe{pasta, bread, soup}
	plans := []planning.Plan{{
		// Week 6 is already an order week: it must not count twice.
		Week: mustWeek(t, week(6)), Entries: []planning.Entry{{RecipeID: rRigatoni}, {RecipeID: rBread}},
	}}
	h := buildPairingHistory(catalog, nil, plans, "")

	var pastaBread PairingStats
	for _, st := range h.stats() {
		if st.Category == "pasta" && st.AddonID == rBread {
			pastaBread = st
		}
	}
	want := PairingStats{Category: "pasta", AddonID: rBread, Together: 6, CategoryWeeks: 6, AddonWeeks: 6, TotalWeeks: 12}
	if pastaBread != want {
		t.Fatalf("stats = %+v, want %+v", pastaBread, want)
	}
	if got := h.learned()["pasta"]; len(got) != 1 || got[0].AddonID != rBread {
		t.Errorf("learned pasta pairings = %+v", got)
	}
	if got := h.learned()["soup"]; len(got) != 0 {
		t.Errorf("soup had no add-ons, learned %+v", got)
	}
}

// TestPairingHistoryWeeklyHabitIsNotLearned mirrors the local data, where an
// imported add-on appears in almost every week: raw confidence is high for
// every category, so only a category the add-on really tracks qualifies.
func TestPairingHistoryWeeklyHabitIsNotLearned(t *testing.T) {
	pasta := recipes.Recipe{ID: rRigatoni, Name: "Rigatoni"}
	tacos := recipes.Recipe{ID: rTacos, Name: "Beef Tacos"}
	bread := recipes.Recipe{ID: rBread, Name: "Garlic Bread", IsAddon: true}
	for i := 1; i <= 20; i++ {
		bread.OrderWeeks = append(bread.OrderWeeks, week(i))
		if i <= 10 {
			pasta.OrderWeeks = append(pasta.OrderWeeks, week(i))
		} else {
			tacos.OrderWeeks = append(tacos.OrderWeeks, week(i))
		}
	}
	h := buildPairingHistory([]recipes.Recipe{pasta, tacos, bread}, nil, nil, "")
	learned := h.learned()
	if len(learned) != 0 {
		t.Errorf("an add-on in every week is a weekly habit, not a pairing: %+v", learned)
	}
	for _, st := range h.stats() {
		if st.Confidence() != 1 || st.OtherRate() != 1 {
			t.Errorf("stats = %+v", st)
		}
	}
}

func TestBuildPairingHistorySkipsTheWeekBeingPlanned(t *testing.T) {
	pasta := recipes.Recipe{ID: rRigatoni, Name: "Rigatoni", OrderWeeks: []string{week(1), week(2)}}
	bread := recipes.Recipe{ID: rBread, Name: "Garlic Bread", IsAddon: true, OrderWeeks: []string{week(1), week(2)}}
	h := buildPairingHistory([]recipes.Recipe{pasta, bread}, nil, nil, week(2))
	for _, st := range h.stats() {
		if st.TotalWeeks != 1 {
			t.Errorf("stats = %+v, want only week 1", st)
		}
	}
}

// Overrides decide the categories history is counted under.
func TestBuildPairingHistoryUsesOverrides(t *testing.T) {
	pasta := recipes.Recipe{ID: rRigatoni, Name: "Rigatoni", OrderWeeks: []string{week(1)}}
	overrides := map[string]RecipeOverride{rRigatoni: {MealCategories: map[string]bool{"pasta": false, "soup": true}}}
	bread := recipes.Recipe{ID: rBread, Name: "Garlic Bread", IsAddon: true, OrderWeeks: []string{week(1)}}
	h := buildPairingHistory([]recipes.Recipe{pasta, bread}, overrides, nil, "")
	stats := h.stats()
	if len(stats) != 1 || stats[0].Category != "soup" {
		t.Errorf("stats = %+v, want one soup pairing", stats)
	}
}
