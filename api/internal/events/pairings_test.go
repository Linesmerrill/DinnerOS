package events

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestPairingEventsAreServerObserved(t *testing.T) {
	store := &memoryStore{}
	svc, _ := newTestService(store)
	ctx := context.Background()
	const (
		proposal = "66e5a1f2c3b4a5d6e7f80e01"
		rule     = "66e5a1f2c3b4a5d6e7f80f01"
		key      = "recipe:66e5a1f2c3b4a5d6e7f81008"
	)
	valid := []Event{
		{Type: TypePairingSuggested, RecipeID: recipeA, Week: "2026-W38", Payload: PairingSuggested{
			Key: key, Kind: "recipe", Source: "rule", Frequency: "always", MealCategory: "pasta", RuleID: rule, EntryID: "e1", Day: "mon",
		}},
		{Type: TypePairingSuggested, RecipeID: recipeA, Week: "2026-W38", Payload: PairingSuggested{
			Key: key, Kind: "recipe", Source: "learned", Frequency: "suggest", MealCategory: "pasta", Confidence: 0.78,
			ProposalID: proposal, SlotID: "mon", Day: "mon",
		}},
		{Type: TypePairingAccepted, RecipeID: recipeA, Week: "2026-W38", Payload: PairingAccepted{
			Key: key, Kind: "recipe", Source: "rule", Frequency: "always", EntryID: "e1", Day: "mon", AddedEntryID: "e2",
		}},
		{Type: TypePairingAccepted, RecipeID: recipeA, Week: "2026-W38", Payload: PairingAccepted{
			Key: "grocery:club crackers", Kind: "grocery_item", Source: "rule", Frequency: "suggest", EntryID: "e1",
			GroceryItemID: "66e5a1f2c3b4a5d6e7f80f02",
		}},
		{Type: TypePairingDismissed, RecipeID: recipeA, Week: "2026-W38", Payload: PairingDismissed{
			Key: key, Kind: "recipe", Source: "learned", Frequency: "suggest", EntryID: "e1", Reason: "dismissed",
		}},
		{Type: TypePairingRuleCreated, Payload: PairingRuleCreated{
			RuleID: rule, Key: key, Kind: "recipe", MealCategory: "pasta", Frequency: "suggest", Confidence: 0.78, Merged: true,
		}},
	}
	for _, e := range valid {
		e.HouseholdID, e.UserID = hhA, userA
		if err := svc.Record(ctx, e); err != nil {
			t.Errorf("Record(%s) error = %v", e.Type, err)
		}
		if slices.Contains(ClientTypes(), e.Type) {
			t.Errorf("%s must not be a client type", e.Type)
		}
	}
	if got := len(store.all()); got != len(valid) {
		t.Errorf("stored %d events, want %d", got, len(valid))
	}

	res, err := svc.Ingest(ctx, memberOf(hhA, userA), []ClientEvent{{
		Type: string(TypePairingAccepted), RecipeID: recipeA, OccurredAt: testNow.Format(time.RFC3339),
		Payload: []byte(`{"key":"` + key + `","kind":"recipe","source":"rule","frequency":"always"}`),
	}})
	if err != nil || len(res.Rejected) != 1 {
		t.Errorf("Ingest(pairing.accepted) = %+v, %v; want it rejected", res, err)
	}
}

func TestPairingPayloadValidation(t *testing.T) {
	store := &memoryStore{}
	svc, _ := newTestService(store)
	ctx := context.Background()
	valid := PairingSuggested{Key: "recipe:66e5a1f2c3b4a5d6e7f81008", Kind: "recipe", Source: "rule", Frequency: "always"}
	for _, tt := range []struct {
		name    string
		payload Payload
		want    string
	}{
		{"a key without a prefix", func() Payload { p := valid; p.Key = "garlic bread"; return p }(), "recipe: or grocery:"},
		{"no key", func() Payload { p := valid; p.Key = ""; return p }(), "key must be"},
		{"an unknown kind", func() Payload { p := valid; p.Kind = "side"; return p }(), "kind must be one of"},
		{"an unknown source", func() Payload { p := valid; p.Source = "guess"; return p }(), "source must be one of"},
		{"an unknown frequency", func() Payload { p := valid; p.Frequency = "often"; return p }(), "frequency must be one of"},
		{"a confidence above 1", func() Payload { p := valid; p.Confidence = 1.5; return p }(), "confidence must be"},
		{"an unknown day", func() Payload { p := valid; p.Day = "funday"; return p }(), "day must be one of"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := svc.Record(ctx, Event{HouseholdID: hhA, UserID: userA, Type: TypePairingSuggested, RecipeID: recipeA, Payload: tt.payload, OccurredAt: testNow})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want one about %q", err, tt.want)
			}
		})
	}
	// A dismissal needs a reason, and a rule needs its category.
	if err := svc.Record(ctx, Event{
		HouseholdID: hhA, UserID: userA, Type: TypePairingDismissed, RecipeID: recipeA, OccurredAt: testNow,
		Payload: PairingDismissed{Key: valid.Key, Kind: "recipe", Source: "rule", Frequency: "always"},
	}); err == nil || !strings.Contains(err.Error(), "reason is required") {
		t.Errorf("error = %v, want one about the reason", err)
	}
	if err := svc.Record(ctx, Event{
		HouseholdID: hhA, UserID: userA, Type: TypePairingRuleCreated, OccurredAt: testNow,
		Payload: PairingRuleCreated{RuleID: "r1", Key: valid.Key, Kind: "recipe", Frequency: "suggest"},
	}); err == nil || !strings.Contains(err.Error(), "mealCategory is required") {
		t.Errorf("error = %v, want one about the meal category", err)
	}
}

func TestRecipeOverrideEventTakesMethodOrCategory(t *testing.T) {
	store := &memoryStore{}
	svc, _ := newTestService(store)
	ctx := context.Background()
	record := func(p AutopilotRecipeOverrideUpdated) error {
		return svc.Record(ctx, Event{
			HouseholdID: hhA, UserID: userA, Type: TypeAutopilotRecipeOverrideUpdated, RecipeID: recipeA, OccurredAt: testNow, Payload: p,
		})
	}
	if err := record(AutopilotRecipeOverrideUpdated{Category: "pasta", Value: "yes", Previous: "auto"}); err != nil {
		t.Errorf("a category override = %v", err)
	}
	if err := record(AutopilotRecipeOverrideUpdated{Value: "yes"}); err == nil || !strings.Contains(err.Error(), "exactly one of method or category") {
		t.Errorf("error = %v", err)
	}
	if err := record(AutopilotRecipeOverrideUpdated{Method: "smoker", Category: "pasta", Value: "yes"}); err == nil {
		t.Error("both a method and a category should be rejected")
	}
}
