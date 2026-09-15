package recipes

import (
	"context"
	"slices"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestExistingRecipeIDs(t *testing.T) {
	svc := NewService(newMemoryStore())
	runExistingRecipeIDsContract(t, svc, "66e5a1f2c3b4a5d6e7f80a01", "66e5a1f2c3b4a5d6e7f80b01")
}

func TestIntegrationExistingRecipeIDs(t *testing.T) {
	svc, _, _ := newTestMongoService(t)
	runExistingRecipeIDsContract(t, svc, bson.NewObjectID().Hex(), bson.NewObjectID().Hex())
}

func runExistingRecipeIDsContract(t *testing.T, svc *Service, hh, other string) {
	t.Helper()
	ctx := context.Background()
	mustImport(t, svc, hh, testFile(testRecipe("r1", "Tacos"), testRecipe("r2", "Bowls")))
	mustImport(t, svc, other, testFile(testRecipe("r3", "Stew")))
	mine, err := svc.List(ctx, hh, ListQuery{})
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := svc.List(ctx, other, ListQuery{})
	if err != nil {
		t.Fatal(err)
	}

	ids := []string{mine.Items[0].ID, theirs.Items[0].ID, "not-an-id", mine.Items[1].ID}
	got, err := svc.ExistingRecipeIDs(ctx, hh, ids)
	want := []string{mine.Items[0].ID, mine.Items[1].ID}
	slices.Sort(got)
	slices.Sort(want)
	if err != nil || !slices.Equal(got, want) {
		t.Errorf("ExistingRecipeIDs() = %v, %v; want only the household's recipes %v", got, err, want)
	}
	if got, err := svc.ExistingRecipeIDs(ctx, hh, nil); err != nil || got != nil {
		t.Errorf("no ids = %v, %v", got, err)
	}
	if _, err := svc.ExistingRecipeIDs(ctx, "", ids); err == nil {
		t.Error("empty household was accepted")
	}
}
