package menu

import (
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/events"
)

// A meal marked made and then "didn't make it" isn't counted as cooked; one
// made again after that is, once (decision 662).
func TestStillCookedDropsMealsTakenBack(t *testing.T) {
	at := time.Date(2026, 11, 9, 19, 0, 0, 0, time.UTC)
	cooked := func(entry string, h int, implicit bool) events.Event {
		return events.Event{Type: events.TypeRecipeCooked, RecipeID: "r", OccurredAt: at.Add(time.Duration(h) * time.Hour),
			Payload: events.RecipeCooked{EntryID: entry, Implicit: implicit}}
	}
	skipped := func(entry string, h int) events.Event {
		return events.Event{Type: events.TypeRecipeSkipped, RecipeID: "r", OccurredAt: at.Add(time.Duration(h) * time.Hour),
			Payload: events.RecipeSkipped{EntryID: entry}}
	}
	list := []events.Event{
		cooked("a", 0, true), skipped("a", 1), // assumed made, then didn't make it
		cooked("b", 0, false), skipped("b", 1), cooked("b", 2, false), // made after all
		cooked("c", 0, false), // made
		cooked("", 0, false),  // not planned: always counts
		skipped("d", 0),       // never made
	}
	got := stillCooked(list)
	var entries []string
	for _, e := range got {
		entries = append(entries, entryOf(e))
	}
	want := []string{"b", "c", ""}
	if len(entries) != len(want) {
		t.Fatalf("still cooked = %q, want %q", entries, want)
	}
	for i := range want {
		if entries[i] != want[i] {
			t.Fatalf("still cooked = %q, want %q", entries, want)
		}
	}
}
