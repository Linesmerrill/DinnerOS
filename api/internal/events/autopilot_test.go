package events

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestAutopilotEventsAreServerObserved records every Autopilot event type
// through the service and checks that clients can't send them.
func TestAutopilotEventsAreServerObserved(t *testing.T) {
	store := &memoryStore{}
	svc, _ := newTestService(store)
	ctx := context.Background()
	const proposal = "66e5a1f2c3b4a5d6e7f80e01"
	valid := []Event{
		{Type: TypeAutopilotPreferencesUpdated, Payload: AutopilotPreferencesUpdated{
			Sections: []string{"taste", "weekdayRules"},
			Changes: []FieldChange{
				{Field: "taste.likes.cuisines", Added: []string{"thai"}, Removed: []string{"french"}},
				{Field: "schedule.mealsPerWeek", From: "4", To: "5"},
			},
		}},
		{Type: TypeAutopilotWeekContextUpdated, Week: "2026-W38", Payload: AutopilotWeekContextUpdated{Changes: []FieldChange{{Field: "maxMinutes", To: "20"}}}},
		{Type: TypeAutopilotWeekContextUpdated, Week: "2026-W38", Payload: AutopilotWeekContextUpdated{Cleared: true}},
		{Type: TypeAutopilotRecipeOverrideUpdated, RecipeID: recipeA, Payload: AutopilotRecipeOverrideUpdated{Method: "smoker", Value: "yes", Previous: "auto"}},
		{Type: TypeWeekGenerated, Week: "2026-W38", Payload: WeekGenerated{ProposalID: proposal, ModelVersion: "baseline-2026.1", Attempt: 1, Requested: 5, Planned: 3, Unfilled: 2, Candidates: 3, ColdStart: true}},
		{Type: TypeMealSwapped, RecipeID: recipeA, Week: "2026-W38", Payload: MealSwapped{ProposalID: proposal, SlotID: "s1", Day: "tue", Date: "2026-09-15", PreviousRecipeID: "66e5a1f2c3b4a5d6e7f80a12", ModelVersion: "baseline-2026.1", SwapNumber: 1}},
		{Type: TypeMealRejected, RecipeID: recipeA, Week: "2026-W38", Payload: MealRejected{ProposalID: proposal, SlotID: "s2", Day: "wed", ModelVersion: "baseline-2026.1"}},
		{Type: TypeWeekAccepted, Week: "2026-W38", Payload: WeekAccepted{ProposalID: proposal, ModelVersion: "baseline-2026.1", Planned: 3, Added: 2, Excluded: 1, Swaps: 1}},
		{Type: TypeWeekRejected, Week: "2026-W38", Payload: WeekRejected{ProposalID: proposal, ModelVersion: "baseline-2026.1", Planned: 3, Reason: "regenerated"}},
		{Type: TypeRecipePlanned, RecipeID: recipeA, Week: "2026-W38", Payload: RecipePlanned{EntryID: "e1", Day: "tue", Servings: 2, Origin: "autopilot", ProposalID: proposal}},
		{Type: TypeRecipeUnplanned, RecipeID: recipeA, Week: "2026-W38", Payload: RecipeUnplanned{EntryID: "e1", Origin: "autopilot"}},
		{Type: TypeAutopilotLearningReset, Payload: AutopilotLearningReset{Adjustments: 4}},
	}
	for _, e := range valid {
		e.HouseholdID, e.UserID = hhA, userA
		if err := svc.Record(ctx, e); err != nil {
			t.Errorf("Record(%s) error = %v", e.Type, err)
		}
		if slices.Contains(ClientTypes(), e.Type) && !strings.HasPrefix(string(e.Type), "recipe.") {
			t.Errorf("%s must not be a client type", e.Type)
		}
	}
	if got := len(store.all()); got != len(valid) {
		t.Errorf("stored %d events, want %d", got, len(valid))
	}

	res, err := svc.Ingest(ctx, memberOf(hhA, userA), []ClientEvent{{
		Type: string(TypeMealSwapped), RecipeID: recipeA, OccurredAt: testNow.Format(time.RFC3339),
		Payload: []byte(`{"proposalId":"p","slotId":"s","day":"tue","previousRecipeId":"r","modelVersion":"m"}`),
	}})
	if err != nil || len(res.Rejected) != 1 {
		t.Errorf("Ingest(meal.swapped) = %+v, %v; want it rejected", res, err)
	}
}

func TestAutopilotPayloadValidation(t *testing.T) {
	store := &memoryStore{}
	svc, _ := newTestService(store)
	long := strings.Repeat("x", 101)
	tooMany := make([]FieldChange, MaxFieldChanges+1)
	for i := range tooMany {
		tooMany[i] = FieldChange{Field: "f"}
	}
	tests := []struct {
		name    string
		event   Event
		wantMsg string
	}{
		{"preferences without sections", Event{Type: TypeAutopilotPreferencesUpdated, Payload: AutopilotPreferencesUpdated{}}, "sections must list"},
		{"too many changes", Event{Type: TypeAutopilotPreferencesUpdated, Payload: AutopilotPreferencesUpdated{Sections: []string{"taste"}, Changes: tooMany}}, "at most 40 changes"},
		{"change without field", Event{Type: TypeAutopilotWeekContextUpdated, Payload: AutopilotWeekContextUpdated{Changes: []FieldChange{{To: "1"}}}}, "needs a field"},
		{"long change value", Event{Type: TypeAutopilotWeekContextUpdated, Payload: AutopilotWeekContextUpdated{Changes: []FieldChange{{Field: "note", Added: []string{long}}}}}, "change values"},
		{"override value", Event{Type: TypeAutopilotRecipeOverrideUpdated, RecipeID: recipeA, Payload: AutopilotRecipeOverrideUpdated{Method: "smoker", Value: "maybe"}}, "value must be one of"},
		{"override without recipe", Event{Type: TypeAutopilotRecipeOverrideUpdated, Payload: AutopilotRecipeOverrideUpdated{Method: "smoker", Value: "yes"}}, "recipeId is required"},
		{"generated without proposal", Event{Type: TypeWeekGenerated, Payload: WeekGenerated{ModelVersion: "m"}}, "proposalId is required"},
		{"negative counts", Event{Type: TypeWeekAccepted, Payload: WeekAccepted{ProposalID: "p", ModelVersion: "m", Added: -1}}, "must not be negative"},
		{"rejected without reason", Event{Type: TypeWeekRejected, Payload: WeekRejected{ProposalID: "p", ModelVersion: "m"}}, "reason is required"},
		{"swap without day", Event{Type: TypeMealSwapped, RecipeID: recipeA, Payload: MealSwapped{ProposalID: "p", SlotID: "s", PreviousRecipeID: "r", ModelVersion: "m"}}, "day is required"},
		{"meal rejected bad day", Event{Type: TypeMealRejected, RecipeID: recipeA, Payload: MealRejected{ProposalID: "p", SlotID: "s", Day: "someday", ModelVersion: "m"}}, "day must be one of"},
		{"week event with recipe", Event{Type: TypeWeekAccepted, RecipeID: recipeA, Payload: WeekAccepted{ProposalID: "p", ModelVersion: "m"}}, "recipeId is not allowed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := tt.event
			e.HouseholdID, e.UserID = hhA, userA
			err := svc.Record(context.Background(), e)
			if err == nil || !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("Record() error = %v, want %q", err, tt.wantMsg)
			}
		})
	}
	if n := len(store.all()); n != 0 {
		t.Errorf("stored %d invalid events", n)
	}
}

func TestServiceListNewestFirst(t *testing.T) {
	store := &memoryStore{}
	svc, _ := newTestService(store)
	ctx := context.Background()
	for i := range 3 {
		if err := svc.Record(ctx, Event{HouseholdID: hhA, UserID: userA, Type: TypeAutopilotPreferencesUpdated,
			OccurredAt: testNow.Add(time.Duration(i) * time.Minute), Payload: AutopilotPreferencesUpdated{Sections: []string{"taste"}}}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := svc.List(ctx, Query{HouseholdID: hhA, Types: []Type{TypeAutopilotPreferencesUpdated}, Limit: 2, Newest: true})
	if err != nil || len(got) != 2 || !got[0].OccurredAt.After(got[1].OccurredAt) || !got[0].OccurredAt.Equal(testNow.Add(2*time.Minute)) {
		t.Errorf("List(newest, limit 2) = %+v, %v", got, err)
	}
	if _, err := svc.List(ctx, Query{}); err == nil {
		t.Error("List() without a household was accepted")
	}
}
