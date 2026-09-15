package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
	"github.com/Linesmerrill/DinnerOS/api/internal/substitutes"
)

func TestSeedSpecialties(t *testing.T) {
	ctx := context.Background()
	client := mongotest.Client(t)
	db := client.Database()
	now := time.Date(2026, 9, 15, 18, 30, 0, 0, time.UTC)
	if _, err := recipes.NewMongoStore(db).UpsertIngredients(ctx, []recipes.Ingredient{
		{Key: "tex mex paste", Name: "Tex-Mex Paste", Category: "condiments", CategoryConfident: true, CreatedAt: now, UpdatedAt: now},
		{Key: "sichuan paste", Name: "Sichuan Paste", Category: "condiments", CategoryConfident: true, CreatedAt: now, UpdatedAt: now},
	}); err != nil {
		t.Fatal(err)
	}
	seed, err := substitutes.LoadSeed()
	if err != nil {
		t.Fatal(err)
	}
	n := len(seed.Specialties)
	count := func() int64 {
		c, err := db.Collection(substitutes.SpecialtiesCollection).CountDocuments(ctx, bson.D{})
		if err != nil {
			t.Fatal(err)
		}
		return c
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
	var out bytes.Buffer
	if err := run(ctx, nil, env, &out); err != nil {
		t.Fatalf("dry run error = %v", err)
	}
	if count() != 0 || !strings.Contains(out.String(), "would create") || !strings.Contains(out.String(), "2 of ") {
		t.Fatalf("dry run wrote %d or printed:\n%s", count(), out.String())
	}
	for _, want := range []string{"tex-mex-paste                     catalog: Tex-Mex Paste", "szechuan-paste                    catalog: Sichuan Paste"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}

	out.Reset()
	if err := run(ctx, []string{"-apply"}, env, &out); err != nil {
		t.Fatalf("apply error = %v", err)
	}
	if int(count()) != n {
		t.Fatalf("applied %d specialties, want %d:\n%s", count(), n, out.String())
	}
	rep, err := sync(ctx, db, true, now.Add(time.Hour))
	if err != nil || len(rep.res.Unchanged) != n || len(rep.res.Created)+len(rep.res.Updated)+len(rep.res.Retired) != 0 {
		t.Errorf("second apply = %+v, %v", rep.res, err)
	}
	if err := run(ctx, []string{"-bogus"}, env, &out); err == nil {
		t.Error("unknown flag: error = nil")
	}
}
