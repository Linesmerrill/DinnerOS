package planning

import (
	"context"
	"errors"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/events"
)

func TestAddEntries(t *testing.T) {
	runAddEntriesContract(t, newMemoryStore(), hhAda, hhBob)
}

func TestIntegrationAddEntries(t *testing.T) {
	store, db := newTestMongoStore(t)
	runAddEntriesContract(t, store, hhAda, hhBob)
	raw := loadRawPlan(t, db, hhAda, "2026-W37")
	if len(raw.Entries) == 0 || raw.Entries[0]["origin"] != "autopilot" || raw.Entries[0]["proposalId"] != "proposal-1" {
		t.Errorf("stored autopilot entry = %+v", raw.Entries[:1])
	}
	manual := loadRawPlan(t, db, hhAda, testWeek)
	if _, ok := manual.Entries[0]["origin"]; ok {
		t.Errorf("manual entries must not store an origin: %+v", manual.Entries[0])
	}
}

func runAddEntriesContract(t *testing.T, store Store, hh, otherHH string) {
	t.Helper()
	ctx := context.Background()
	recorder := &fakeRecorder{}
	svc, _ := newTestService(t, store)
	svc.WithEvents(recorder, nil)
	const week = "2026-W37"

	p, added, err := svc.AddEntries(ctx, hh, userAda, week, []NewEntry{
		{RecipeID: recipeTacos, Day: "tue", Servings: 4, Origin: OriginAutopilot, ProposalID: "proposal-1"},
		{RecipeID: recipeSoup, Day: "sun", Servings: 2, Origin: OriginAutopilot, ProposalID: "proposal-1"},
	})
	if err != nil || len(added) != 2 || len(p.Entries) != 2 {
		t.Fatalf("AddEntries() = %+v, %+v, %v", p, added, err)
	}
	if added[0].Origin != OriginAutopilot || added[0].ProposalID != "proposal-1" || added[1].RecipeName != "Onion Soup" || added[1].Day != Sunday {
		t.Errorf("added entries = %+v", added)
	}
	got := recorder.recorded()
	if len(got) != 2 || got[0].Payload != (events.RecipePlanned{EntryID: added[0].ID, Day: "tue", Date: "2026-09-08", Servings: 4, Origin: "autopilot", ProposalID: "proposal-1"}) {
		t.Errorf("recorded = %+v", got)
	}

	// Manual entries default to the manual origin, in responses too.
	mp, manual := mustAdd(t, svc, hh, userAda, testWeek, NewEntry{RecipeID: recipeSalad, Servings: 2})
	if manual.Origin != OriginManual || newEntryResponse(mp, Entry{}).Origin != OriginManual {
		t.Errorf("manual origin = %q", manual.Origin)
	}

	// One bad entry adds nothing.
	for name, batch := range map[string][]NewEntry{
		"servings":      {{RecipeID: recipeTacos, Servings: 2}, {RecipeID: recipeSalad, Servings: 4}},
		"recipe":        {{RecipeID: recipeTacos, Servings: 2}, {RecipeID: recipeBobs, Servings: 2}},
		"origin":        {{RecipeID: recipeTacos, Servings: 2, Origin: "robot"}},
		"day":           {{RecipeID: recipeTacos, Servings: 2, Day: "someday"}},
		"empty":         {},
		"no recipe id":  {{Servings: 2}},
		"long note":     {{RecipeID: recipeTacos, Servings: 2, Note: string(make([]byte, MaxNoteLength+1))}},
		"missing store": {{RecipeID: "ffffffffffffffffffffffff", Servings: 2}},
	} {
		if _, _, err := svc.AddEntries(ctx, hh, userAda, week, batch); !errors.Is(err, ErrInvalidEntry) && !errors.Is(err, ErrRecipeNotFound) {
			t.Errorf("%s: AddEntries() error = %v", name, err)
		}
	}
	if p, _ := svc.Get(ctx, hh, week); len(p.Entries) != 2 {
		t.Errorf("failed batches changed the plan: %d entries", len(p.Entries))
	}

	// The batch must fit as a whole.
	fill := make([]NewEntry, MaxEntriesPerWeek-3)
	for i := range fill {
		fill[i] = NewEntry{RecipeID: recipeSalad, Servings: 2}
	}
	if _, _, err := svc.AddEntries(ctx, hh, userAda, week, fill); err != nil {
		t.Fatalf("fill: %v", err)
	}
	two := []NewEntry{{RecipeID: recipeSalad, Servings: 2}, {RecipeID: recipeSalad, Servings: 2}}
	if _, _, err := svc.AddEntries(ctx, hh, userAda, week, two); !errors.Is(err, ErrPlanFull) {
		t.Errorf("AddEntries(over the limit) error = %v, want ErrPlanFull", err)
	}
	if p, _, err := svc.AddEntries(ctx, hh, userAda, week, two[:1]); err != nil || len(p.Entries) != MaxEntriesPerWeek {
		t.Errorf("AddEntries(last slot) = %d entries, %v", len(p.Entries), err)
	}
	if _, err := svc.SetStatus(ctx, hh, testWeek, "finalized"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.AddEntries(ctx, hh, userAda, testWeek, two[:1]); !errors.Is(err, ErrFinalized) {
		t.Errorf("AddEntries(finalized) error = %v", err)
	}

	// Removing an autopilot entry records its origin.
	if _, err := svc.SetStatus(ctx, hh, testWeek, "draft"); err != nil {
		t.Fatal(err)
	}
	recorder.mu.Lock()
	recorder.events = nil
	recorder.mu.Unlock()
	if _, err := svc.DeleteEntry(ctx, hh, userAda, week, added[1].ID); err != nil {
		t.Fatal(err)
	}
	if got := recorder.recorded(); len(got) != 1 || got[0].Payload.(events.RecipeUnplanned).Origin != "autopilot" {
		t.Errorf("unplanned events = %+v", got)
	}

	// ListPlans returns stored plans in the range, in week order.
	plans, err := svc.ListPlans(ctx, hh, "2026-W30", testWeek)
	if err != nil || len(plans) != 2 || plans[0].Week.String() != week || plans[1].Week.String() != testWeek || len(plans[1].Entries) != 1 {
		t.Errorf("ListPlans() = %+v, %v", plans, err)
	}
	if plans, err := svc.ListPlans(ctx, otherHH, "2026-W30", testWeek); err != nil || len(plans) != 0 {
		t.Errorf("ListPlans(other household) = %+v, %v", plans, err)
	}
	for _, r := range [][2]string{{"2026-W38", "2026-W30"}, {"2026-W01", "2026-W40"}, {"bad", "2026-W40"}} {
		if _, err := svc.ListPlans(ctx, hh, r[0], r[1]); err == nil {
			t.Errorf("ListPlans(%s, %s) error = nil", r[0], r[1])
		}
	}
}
