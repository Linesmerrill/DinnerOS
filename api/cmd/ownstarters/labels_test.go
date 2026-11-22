package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/Linesmerrill/DinnerOS/api/internal/catalog"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
)

func TestIntegrationLabels(t *testing.T) {
	client := mongotest.Client(t)
	ctx := t.Context()
	db := client.Database()
	labeled := "Web Description: Fun fact: “Rosa” means “pink” in Italian."
	clean := "Fun fact: “Rosa” means “pink” in Italian."
	own := "Description of my own: kept as written."
	if _, err := db.Collection("recipes").InsertMany(ctx, []any{
		bson.D{{Key: "_id", Value: bson.NewObjectID()}, {Key: "name", Value: "Rosa"}, {Key: "description", Value: labeled}},
		bson.D{{Key: "_id", Value: bson.NewObjectID()}, {Key: "name", Value: "Mine"}, {Key: "description", Value: own}},
		bson.D{{Key: "_id", Value: bson.NewObjectID()}, {Key: "name", Value: "None"}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Collection(catalog.RecipesCollection).InsertOne(ctx,
		bson.D{{Key: "_id", Value: bson.NewObjectID()}, {Key: "description", Value: labeled}}); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"MONGODB_URI": os.Getenv("MONGODB_TEST_URI"), "MONGODB_DATABASE": db.Name()}
	getenv := func(k string) string { return env[k] }
	var out bytes.Buffer
	// A dry run changes nothing and needs no starter household.
	if err := run(ctx, []string{"-labels"}, getenv, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "would update the description of 1 recipes and 1 Shared Recipes entries") {
		t.Errorf("dry run = %q", out.String())
	}
	out.Reset()
	if err := run(ctx, []string{"-labels", "-apply"}, getenv, &out); err != nil {
		t.Fatal(err)
	}
	var r struct {
		Description string `bson:"description"`
	}
	for _, c := range []string{"recipes", catalog.RecipesCollection} {
		if err := db.Collection(c).FindOne(ctx, bson.D{{Key: "description", Value: bson.D{{Key: "$regex", Value: "Rosa"}}}}).Decode(&r); err != nil {
			t.Fatal(err)
		}
		if r.Description != clean {
			t.Errorf("%s description = %q", c, r.Description)
		}
	}
	if err := db.Collection("recipes").FindOne(ctx, bson.D{{Key: "name", Value: "Mine"}}).Decode(&r); err != nil || r.Description != own {
		t.Errorf("a description without a label changed: %q %v", r.Description, err)
	}
	// Running it again changes nothing.
	out.Reset()
	if err := run(ctx, []string{"-labels"}, getenv, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "would update the description of 0 recipes and 0 Shared Recipes entries") {
		t.Errorf("second run = %q", out.String())
	}
}
