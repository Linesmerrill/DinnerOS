package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
)

// A synthetic import file; never real order history.
const testImportFile = `{
  "version": 1,
  "source": "hellofresh",
  "generatedAt": "2026-09-01T00:00:00Z",
  "recipes": [
    {
      "source": "hellofresh", "sourceRecipeId": "r1", "sourceUrl": "https://recipes.example.com/r1",
      "name": "Test Tacos", "isAddon": false, "servings": [2],
      "ingredients": [
        {"sourceIngredientId": "ing-garlic", "name": "Garlic", "pantryStaple": false,
         "amounts": [{"servings": 2, "quantity": 0.5, "unit": "clove", "sourceUnit": "clove", "rawText": "½ clove Garlic"}]}
      ],
      "steps": [{"index": 1, "text": "Cook."}],
      "orderWeeks": ["2026-W10"]
    },
    {
      "source": "hellofresh", "sourceRecipeId": "r2", "sourceUrl": "", "name": "Bad Unit", "isAddon": false, "servings": [2],
      "ingredients": [{"sourceIngredientId": "ing-x", "name": "Paste", "pantryStaple": false,
        "amounts": [{"servings": 2, "quantity": 1, "unit": "dollop", "sourceUnit": "dollop", "rawText": "1 dollop Paste"}]}],
      "steps": [], "orderWeeks": []
    }
  ],
  "review": [{"sourceRecipeId": "r2", "recipeName": "Bad Unit", "field": "ingredients.Paste.unit", "value": "dollop", "reason": "unknown unit"}]
}`

func writeFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "recipes.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunValidatesArguments(t *testing.T) {
	noEnv := func(string) string { return "" }
	valid := writeFile(t, testImportFile)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"missing flags", nil, "-file and -household are required"},
		{"bad household", []string{"-file", valid, "-household", "home"}, "24-character hex"},
		{"missing file", []string{"-file", filepath.Join(t.TempDir(), "nope.json"), "-household", bson.NewObjectID().Hex()}, "no such file"},
		{"unknown field", []string{"-file", writeFile(t, `{"version":1,"households":[]}`), "-household", bson.NewObjectID().Hex()}, "unknown field"},
		{"trailing data", []string{"-file", writeFile(t, `{"version":1} {}`), "-household", bson.NewObjectID().Hex()}, "trailing data"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			err := run(context.Background(), tt.args, noEnv, &out)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("run() error = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestIntegrationRunImportsIntoExistingHousehold(t *testing.T) {
	client := mongotest.Client(t)
	ctx := context.Background()
	uri := os.Getenv("MONGODB_TEST_URI")
	getenv := func(key string) string {
		switch key {
		case "MONGODB_URI":
			return uri
		case "MONGODB_DATABASE":
			return client.Database().Name()
		}
		return ""
	}
	now := time.Now().UTC()
	h, err := households.NewMongoStore(client.Database()).CreateHousehold(ctx, households.Household{
		Name: "Test Home", DefaultServings: 2, TimeZone: "America/Denver", CreatedBy: bson.NewObjectID().Hex(), CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	path := writeFile(t, testImportFile)

	var out bytes.Buffer
	if err := run(ctx, []string{"-file", path, "-household", h.ID}, getenv, &out); err != nil {
		t.Fatalf("run() error = %v\n%s", err, out.String())
	}
	for _, want := range []string{`household "Test Home"`, "1 created, 0 updated, 0 unchanged, 1 rejected", "ingredients created: 1", "review items: 1", `rejected recipes[1] r2 "Bad Unit"`} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), uri) {
		t.Errorf("output contains the MongoDB URI:\n%s", out.String())
	}

	out.Reset()
	if err := run(ctx, []string{"-file", path, "-household", h.ID}, getenv, &out); err != nil {
		t.Fatalf("second run() error = %v", err)
	}
	if !strings.Contains(out.String(), "0 created, 0 updated, 1 unchanged") {
		t.Errorf("second run output:\n%s", out.String())
	}

	err = run(ctx, []string{"-file", path, "-household", bson.NewObjectID().Hex()}, getenv, &out)
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("unknown household error = %v", err)
	}
}
