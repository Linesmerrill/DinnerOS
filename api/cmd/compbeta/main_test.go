package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
	"github.com/Linesmerrill/DinnerOS/api/internal/plus"
)

type memComps struct {
	mu sync.Mutex
	m  map[string]plus.Comp
}

func (s *memComps) Get(_ context.Context, id string) (plus.Comp, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.m[id]
	return c, ok, nil
}

func (s *memComps) Grant(_ context.Context, c plus.Comp) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[c.HouseholdID]; ok {
		return false, nil
	}
	s.m[c.HouseholdID] = c
	return true, nil
}

func (s *memComps) PurgeHousehold(context.Context, string) error { return nil }

func TestGrantBeta(t *testing.T) {
	ctx := t.Context()
	at := time.Date(2026, 11, 13, 23, 0, 0, 0, time.UTC)
	store := &memComps{m: map[string]plus.Comp{"b": {HouseholdID: "b", Reason: "earlier"}}}
	ids := []string{"a", "b", "starter", "c", "review", "tester"}
	skip := map[string]string{"starter": "starter", "review": "-exclude", "tester": "PLUS_TEST_HOUSEHOLDS"}

	dry, err := grant(ctx, store, ids, skip, at, false)
	if err != nil || dry.granted != 2 || dry.existing != 1 || len(dry.skipped) != 3 || len(store.m) != 1 {
		t.Fatalf("dry run = %+v, %v, store %v", dry, err, store.m)
	}
	got, err := grant(ctx, store, ids, skip, at, true)
	if err != nil || got.granted != 2 || got.existing != 1 || strings.Join(got.skipped, ",") != "starter,review,tester" {
		t.Fatalf("apply = %+v, %v", got, err)
	}
	// The review and test households still see the paywall.
	for _, id := range []string{"review", "tester"} {
		if _, ok := store.m[id]; ok {
			t.Errorf("%s was comped", id)
		}
	}
	if c := store.m["a"]; c.Reason != plus.CompReasonBeta || !c.GrantedAt.Equal(at) || !c.Until.IsZero() {
		t.Errorf("comp = %+v, want beta with no end", c)
	}
	if store.m["b"].Reason != "earlier" {
		t.Error("an existing comp was replaced")
	}
	if _, ok := store.m["starter"]; ok {
		t.Error("the starter library household was comped")
	}
	// Running again changes nothing.
	if again, _ := grant(ctx, store, ids, skip, at, true); again.granted != 0 || again.existing != 3 {
		t.Errorf("second run = %+v", again)
	}
}

func TestIntegrationCompBeta(t *testing.T) {
	client := mongotest.Client(t)
	ctx := t.Context()
	db := client.Database()
	a, b, demo, tester := bson.NewObjectID(), bson.NewObjectID(), bson.NewObjectID(), bson.NewObjectID()
	docs := []any{}
	for _, id := range []bson.ObjectID{a, b, demo, tester} {
		docs = append(docs, bson.D{{Key: "_id", Value: id}})
	}
	if _, err := db.Collection("households").InsertMany(ctx, docs); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"MONGODB_URI": os.Getenv("MONGODB_TEST_URI"), "MONGODB_DATABASE": db.Name(),
		"STARTER_RECIPES_HOUSEHOLD_ID": b.Hex(), "PLUS_TEST_HOUSEHOLDS": tester.Hex()}
	var out bytes.Buffer
	if err := run(ctx, []string{"-exclude", strings.ToUpper(demo.Hex()), "-apply"}, func(k string) string { return env[k] }, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "comped 1 of 4 households") || !strings.Contains(out.String(), demo.Hex()+" (-exclude)") ||
		!strings.Contains(out.String(), tester.Hex()+" (PLUS_TEST_HOUSEHOLDS)") {
		t.Errorf("out = %q", out.String())
	}
	comps := plus.NewMongoComps(db)
	if _, ok, _ := comps.Get(ctx, a.Hex()); !ok {
		t.Error("household a has no comp")
	}
	for _, id := range []bson.ObjectID{b, demo, tester} {
		if _, ok, _ := comps.Get(ctx, id.Hex()); ok {
			t.Errorf("%s was comped", id.Hex())
		}
	}
}
