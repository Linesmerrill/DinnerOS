package recipes

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

type fakeFindings struct {
	mu    sync.Mutex
	calls int
	got   []Finding
	done  chan struct{}
}

func (f *fakeFindings) RecordFindings(_ context.Context, _ string, _ Recipe, _ int, findings []Finding, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.got = append(f.got, findings...)
	f.done <- struct{}{}
	return nil
}

func TestFindingLogRecordsWhatReadsWrongOncePerHour(t *testing.T) {
	rec := &fakeFindings{done: make(chan struct{}, 4)}
	log := newFindingLog(rec, slog.New(slog.DiscardHandler))
	clock := time.Date(2026, 9, 30, 18, 0, 0, 0, time.UTC)
	log.now = func() time.Time { return clock }
	// Saffron is listed but no step names it.
	r := instructionRecipe([]RecipeIngredient{instructionLine("", "Saffron", "1", "count")}, "Boil water.")
	in := Annotate(r, 2, nil, true)

	if got := log.check(context.Background(), "hh", r, in); len(got) != 1 || got[0].Code != FindingUnusedIngredient {
		t.Fatalf("findings = %+v", got)
	}
	<-rec.done
	// Opened again within the hour: nothing more is written.
	if got := log.check(context.Background(), "hh", r, in); got != nil {
		t.Errorf("rechecked within the hour: %+v", got)
	}
	clock = clock.Add(2 * time.Hour)
	log.check(context.Background(), "hh", r, in)
	<-rec.done
	if rec.calls != 2 {
		t.Errorf("recorded %d times, want 2", rec.calls)
	}
	// A clean recipe records nothing.
	clean := instructionRecipe([]RecipeIngredient{instructionLine("", "Rice", "1", "cup")}, "Cook rice.")
	if got := log.check(context.Background(), "hh", clean, Annotate(clean, 2, nil, true)); got != nil {
		t.Errorf("clean recipe findings = %+v", got)
	}
	// Without a recorder there is no log, and checking is a no-op.
	var none *findingLog
	if none.check(context.Background(), "hh", r, in) != nil || newFindingLog(nil, nil) != nil {
		t.Error("nil log did something")
	}
}

func TestIntegrationFindingsCountUpAndGoWithTheHousehold(t *testing.T) {
	_, store, db := newTestMongoService(t)
	ctx := context.Background()
	hh := bson.NewObjectID().Hex()
	r := Recipe{ID: "r1", Name: "Test Dinner", Source: "manual", Steps: []Step{{Index: 1, Text: "Add 1½ cup water."}}}
	f := []Finding{{Code: FindingUnitPlural, Step: 1, Detail: "1½ cup"}, {Code: FindingUnusedIngredient, Detail: "Saffron"}}
	at := time.Date(2026, 9, 30, 18, 0, 0, 0, time.UTC)
	for range 2 {
		if err := store.RecordFindings(ctx, hh, r, 2, f, at); err != nil {
			t.Fatal(err)
		}
		at = at.Add(time.Hour)
	}
	var docs []bson.M
	cur, err := db.Collection(FindingsCollection).Find(ctx, bson.D{{Key: "recipeId", Value: "r1"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := cur.All(ctx, &docs); err != nil {
		t.Fatal(err)
	}
	if len(docs) != 2 {
		t.Fatalf("docs = %+v", docs)
	}
	for _, d := range docs {
		if d["count"] != int32(2) || d["recipeName"] != "Test Dinner" {
			t.Errorf("doc = %+v", d)
		}
		if d["code"] == FindingUnitPlural && d["stepText"] != "Add 1½ cup water." {
			t.Errorf("step text = %v", d["stepText"])
		}
	}
	if err := store.PurgeHousehold(ctx, hh); err != nil {
		t.Fatal(err)
	}
	if n, _ := db.Collection(FindingsCollection).CountDocuments(ctx, bson.D{{Key: "recipeId", Value: "r1"}}); n != 0 {
		t.Errorf("%d findings left after the household was deleted", n)
	}
}
