package recommendations

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
)

func newTestMongoStore(t *testing.T) (*MongoStore, *mongo.Database) {
	t.Helper()
	client := mongotest.Client(t)
	for range 2 { // applying indexes twice is a no-op
		if err := client.EnsureIndexes(context.Background(), Indexes()...); err != nil {
			t.Fatalf("EnsureIndexes() error = %v", err)
		}
	}
	return NewMongoStore(client.Database()), client.Database()
}

func TestIntegrationIndexes(t *testing.T) {
	_, db := newTestMongoStore(t)
	for _, set := range Indexes() {
		specs, err := db.Collection(set.Collection).Indexes().ListSpecifications(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for _, model := range set.Indexes {
			var opts options.IndexOptions
			for _, apply := range model.Options.List() {
				_ = apply(&opts)
			}
			if !slices.ContainsFunc(specs, func(s mongo.IndexSpecification) bool { return s.Name == *opts.Name && s.Unique != nil && *s.Unique }) {
				t.Errorf("%s: unique index %s missing", set.Collection, *opts.Name)
			}
		}
	}
}

func TestIntegrationStoreProfilesAndContexts(t *testing.T) {
	store, _ := newTestMongoStore(t)
	ctx := context.Background()
	hh := bson.NewObjectID().Hex()

	if _, err := store.GetProfile(ctx, hh); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetProfile(missing) error = %v", err)
	}
	p := DefaultProfile(hh)
	p.Taste.Likes.Cuisines = []string{"thai"}
	p.Equipment = []string{"smoker"}
	p.WeekdayRules = []WeekdayRule{{Day: "sun", Label: "Smoker night", Proteins: []string{"pork"}, Methods: []string{"smoker"}, TimeBand: "long", Frequency: "at_most_once"}}
	p.Sections = map[Section]Change{SectionTaste: {UpdatedBy: userAda, UpdatedAt: testNow}}
	p.CreatedBy, p.CreatedAt, p.UpdatedBy, p.UpdatedAt = userAda, testNow, userAda, testNow
	saved, err := store.SaveProfile(ctx, p)
	if err != nil || saved.Version != 1 {
		t.Fatalf("SaveProfile(new) = %+v, %v", saved, err)
	}
	got, err := store.GetProfile(ctx, hh)
	if err != nil || !reflect.DeepEqual(got, saved) {
		t.Fatalf("GetProfile() =\n%+v, %v\nwant\n%+v", got, err, saved)
	}
	if _, err := store.SaveProfile(ctx, p); !errors.Is(err, ErrConflict) {
		t.Errorf("second insert error = %v", err)
	}
	got.Novelty = "adventurous"
	if again, err := store.SaveProfile(ctx, got); err != nil || again.Version != 2 {
		t.Errorf("replace = %+v, %v", again, err)
	}
	if _, err := store.SaveProfile(ctx, got); !errors.Is(err, ErrConflict) {
		t.Errorf("stale replace error = %v", err)
	}

	c := WeekContext{HouseholdID: hh, Week: testWeek, Busy: true, MaxMinutes: 20, Days: []DayOverride{{Day: "fri", Servings: 6}}, Note: "guests", UpdatedBy: userAda, UpdatedAt: testNow}
	savedCtx, err := store.SaveWeekContext(ctx, c)
	if err != nil || savedCtx.Version != 1 {
		t.Fatal(err)
	}
	if gotCtx, err := store.GetWeekContext(ctx, hh, testWeek); err != nil || !reflect.DeepEqual(gotCtx, savedCtx) {
		t.Errorf("GetWeekContext() = %+v, %v", gotCtx, err)
	}
	for _, week := range []string{"2026-W30", "2026-W36", "2025-W50"} {
		if _, err := store.SaveWeekContext(ctx, WeekContext{HouseholdID: hh, Week: week, Busy: week != "2026-W36", UpdatedBy: userAda, UpdatedAt: testNow}); err != nil {
			t.Fatal(err)
		}
	}
	if busy, err := store.BusyWeeks(ctx, hh, "2026-W01", "2026-W38"); err != nil || !slices.Equal(busy, []string{"2026-W30", testWeek}) {
		t.Errorf("BusyWeeks() = %v, %v", busy, err)
	}
	if busy, err := store.BusyWeeks(ctx, bson.NewObjectID().Hex(), "2026-W01", "2026-W38"); err != nil || len(busy) != 0 {
		t.Errorf("BusyWeeks(other household) = %v, %v", busy, err)
	}
	if _, err := store.SaveWeekContext(ctx, c); !errors.Is(err, ErrConflict) {
		t.Errorf("second context insert error = %v", err)
	}
	if deleted, err := store.DeleteWeekContext(ctx, hh, testWeek); err != nil || deleted.Note != "guests" {
		t.Errorf("DeleteWeekContext() = %+v, %v", deleted, err)
	}
	if _, err := store.DeleteWeekContext(ctx, hh, testWeek); !errors.Is(err, ErrNotFound) {
		t.Errorf("second delete error = %v", err)
	}
}

func TestIntegrationStoreProposalsAndOverrides(t *testing.T) {
	store, _ := newTestMongoStore(t)
	ctx := context.Background()
	hh := bson.NewObjectID().Hex()

	p := Proposal{
		ID: newID(), HouseholdID: hh, Week: testWeek, Status: StatusProposed, Attempt: 1, ModelVersion: "baseline-test", InputsHash: "abc",
		Requested: 5, Planned: 1, Candidates: 3, ColdStart: true,
		Slots: []Slot{{
			ID: "sun", Day: "sun", RecipeID: rTenderloin, RecipeName: "Smoky Pork Tenderloin", CookMinutes: 90, TimeBand: "long", Servings: 4,
			Score: 1.25, Signals: map[string]float64{"rule": 1}, Reasons: []Reason{{Code: "rule", Text: "Sunday smoker night · Pork · Long cook OK"}},
			SwapCount: 1, RejectedRecipeIDs: []string{rThighs},
		}},
		Unfilled:  []Unfilled{{Day: "sat", Code: "no_candidates", Text: "No remaining recipe fits Saturday."}},
		Messages:  []Message{{Code: "cold_start", Text: "Leaning on your taste profile."}},
		Objective: Objective{Meals: 1.25, Total: 1.25}, SwapCount: 1,
		GeneratedBy: userAda, GeneratedAt: testNow, UpdatedAt: testNow,
		Context: ProposalContext{
			Season: "fall", OrderDate: "2026-09-19", Holidays: []Holiday{{Day: "mon", Name: "Labor Day", Kind: "cookout"}},
			Device: DeviceSignals{Days: []DeviceDaySignals{{Day: "tue", Busyness: "busy", EveningFreeMinutes: 25}, {Day: "wed", TemperatureBand: "cold", Precipitation: "rain"}}},
		},
	}
	saved, err := store.SaveProposal(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.GetProposal(ctx, hh, testWeek)
	if err != nil || !reflect.DeepEqual(got, saved) {
		t.Fatalf("GetProposal() =\n%+v, %v\nwant\n%+v", got, err, saved)
	}

	// Accepting records the decision; generating again replaces the document.
	got.Status, got.DecidedBy, got.DecidedAt, got.ExcludedSlots = StatusAccepted, userAlan, testNow, []string{"sun"}
	accepted, err := store.SaveProposal(ctx, got)
	if err != nil {
		t.Fatal(err)
	}
	if back, _ := store.GetProposal(ctx, hh, testWeek); back.DecidedBy != userAlan || !back.DecidedAt.Equal(testNow) || back.Version != 2 || back.ExcludedSlots[0] != "sun" {
		t.Errorf("accepted proposal = %+v", back)
	}
	next := p
	next.ID, next.Version, next.Attempt = newID(), accepted.Version, 2
	if _, err := store.SaveProposal(ctx, next); err != nil {
		t.Fatal(err)
	}
	if back, _ := store.GetProposal(ctx, hh, testWeek); back.ID != next.ID || back.Version != 3 || back.DecidedBy != "" {
		t.Errorf("regenerated proposal = %+v", back)
	}
	if _, err := store.SaveProposal(ctx, next); !errors.Is(err, ErrConflict) {
		t.Errorf("stale proposal save error = %v", err)
	}

	o := RecipeOverride{HouseholdID: hh, RecipeID: rTenderloin, Methods: map[string]bool{"smoker": false}, UpdatedBy: userAda, UpdatedAt: testNow}
	if err := store.SaveOverride(ctx, o); err != nil {
		t.Fatal(err)
	}
	o.Methods["grill"] = true
	if err := store.SaveOverride(ctx, o); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveOverride(ctx, RecipeOverride{HouseholdID: hh, RecipeID: rCurry, Methods: map[string]bool{"slow-cooker": true}, UpdatedBy: userAda, UpdatedAt: testNow}); err != nil {
		t.Fatal(err)
	}
	list, err := store.ListOverrides(ctx, hh)
	if err != nil || len(list) != 2 || list[0].RecipeID != rTenderloin || !list[0].Methods["grill"] || list[0].Methods["smoker"] {
		t.Errorf("ListOverrides() = %+v, %v", list, err)
	}
	if err := store.SaveOverride(ctx, RecipeOverride{HouseholdID: hh, RecipeID: rCurry, UpdatedBy: userAda, UpdatedAt: testNow}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetOverride(ctx, hh, rCurry); !errors.Is(err, ErrNotFound) {
		t.Errorf("an override without methods should be deleted: %v", err)
	}
	if got, err := store.GetOverride(ctx, hh, rTenderloin); err != nil || got.UpdatedBy != userAda {
		t.Errorf("GetOverride() = %+v, %v", got, err)
	}
}
