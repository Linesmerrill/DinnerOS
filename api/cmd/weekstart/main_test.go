package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
)

func TestIntegrationWeekStartBackfill(t *testing.T) {
	ctx := context.Background()
	client := mongotest.Client(t)
	db := client.Database()
	if err := client.EnsureIndexes(ctx, append(planning.Indexes(), households.Indexes()...)...); err != nil {
		t.Fatal(err)
	}
	hhStore := households.NewMongoStore(db)
	planStore := planning.NewMongoStore(db)
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	newHousehold := func(weekStart string) string {
		h, err := hhStore.CreateHousehold(ctx, households.Household{
			Name: "Home", TimeZone: "America/Denver", DefaultServings: 2, WeekStartsOn: weekStart,
			CreatedBy: bson.NewObjectID().Hex(), CreatedAt: now, UpdatedAt: now,
		})
		if err != nil {
			t.Fatal(err)
		}
		return h.ID
	}
	legacy, chosen := newHousehold(""), newHousehold("mon")
	w38 := planning.Week{Year: 2026, Number: 38}
	for _, id := range []string{legacy, chosen} {
		entries := []planning.Entry{
			{RecipeID: bson.NewObjectID().Hex(), RecipeName: "Sunday Roast", Day: planning.Sunday, Servings: 2, AddedBy: bson.NewObjectID().Hex(), AddedAt: now},
			{RecipeID: bson.NewObjectID().Hex(), RecipeName: "Monday Tacos", Day: planning.Monday, Servings: 2, AddedBy: bson.NewObjectID().Hex(), AddedAt: now},
		}
		if _, _, err := planStore.AddEntries(ctx, id, w38, entries, planning.MaxEntriesPerWeek, now); err != nil {
			t.Fatal(err)
		}
	}
	env := func(key string) string {
		switch key {
		case "MONGODB_URI":
			return os.Getenv("MONGODB_TEST_URI")
		case "MONGODB_DATABASE":
			return db.Name()
		}
		return ""
	}
	names := func(id string, w planning.Week) []string {
		p, err := planStore.GetPlan(ctx, id, w)
		if err != nil {
			return nil
		}
		var out []string
		for _, e := range p.Entries {
			out = append(out, e.RecipeName)
		}
		return out
	}

	var out bytes.Buffer
	if err := run(ctx, []string{"-all"}, env, &out); err != nil {
		t.Fatalf("dry run error = %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "mon → sun, would move 1 scheduled meals") || !strings.Contains(out.String(), "already chose mon") || len(names(legacy, w38)) != 2 {
		t.Fatalf("dry run output:\n%s", out.String())
	}
	if h, _ := hhStore.GetHousehold(ctx, legacy); h.WeekStartsOn != "" {
		t.Fatalf("dry run changed the household: %+v", h)
	}

	for range 2 { // re-running is a no-op
		out.Reset()
		if err := run(ctx, []string{"-all", "-apply"}, env, &out); err != nil {
			t.Fatalf("apply error = %v\n%s", err, out.String())
		}
	}
	if h, _ := hhStore.GetHousehold(ctx, legacy); h.WeekStartsOn != "sun" {
		t.Errorf("legacy household WeekStartsOn = %q, want sun", h.WeekStartsOn)
	}
	// Sunday Sep 20 is now Sunday of Sunday-first 2026-W39; Monday Sep 14 stays.
	if got := names(legacy, w38); len(got) != 1 || got[0] != "Monday Tacos" {
		t.Errorf("legacy 2026-W38 = %v", got)
	}
	if got := names(legacy, planning.Week{Year: 2026, Number: 39}); len(got) != 1 || got[0] != "Sunday Roast" {
		t.Errorf("legacy 2026-W39 = %v", got)
	}
	if got := names(chosen, w38); len(got) != 2 {
		t.Errorf("a household that chose Monday changed: %v", got)
	}
	if h, _ := hhStore.GetHousehold(ctx, chosen); h.WeekStartsOn != "mon" {
		t.Errorf("chosen household WeekStartsOn = %q", h.WeekStartsOn)
	}

	if err := run(ctx, []string{"-household", legacy, "-all"}, env, &out); err == nil {
		t.Error("-household with -all must fail")
	}
	if err := run(ctx, []string{"-all", "-to", "sunday"}, env, &out); err == nil {
		t.Error("-to sunday must fail")
	}
}
