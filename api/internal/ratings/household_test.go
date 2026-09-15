package ratings

import (
	"context"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestHouseholdRatings(t *testing.T) {
	runHouseholdRatingsContract(t, &memoryStore{})
}

func TestIntegrationHouseholdRatings(t *testing.T) {
	store, _ := newTestMongoStore(t)
	runHouseholdRatingsContract(t, store)
}

func runHouseholdRatingsContract(t *testing.T, store Store) {
	t.Helper()
	ctx := context.Background()
	hh, other := bson.NewObjectID().Hex(), bson.NewObjectID().Hex()
	recipe1, recipe2 := "66e5a1f2c3b4a5d6e7f80c01", "66e5a1f2c3b4a5d6e7f80c02"
	user1, user2 := "66e5a1f2c3b4a5d6e7f80d01", "66e5a1f2c3b4a5d6e7f80d02"
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	for _, r := range []Rating{
		{HouseholdID: hh, RecipeID: recipe2, UserID: user1, Score: 2, Tags: []Tag{TagNeverAgain}, Comment: "no"},
		{HouseholdID: hh, RecipeID: recipe1, UserID: user2, Score: 5, Tags: []Tag{TagMakeAgain}},
		{HouseholdID: hh, RecipeID: recipe1, UserID: user1, Score: 4},
		{HouseholdID: other, RecipeID: recipe1, UserID: user1, Score: 1},
	} {
		r.CreatedAt, r.UpdatedAt = now, now
		if _, _, err := store.Upsert(ctx, r); err != nil {
			t.Fatalf("Upsert() error = %v", err)
		}
	}
	svc := NewService(ServiceOptions{Store: store})
	got, err := svc.HouseholdRatings(ctx, hh)
	if err != nil || len(got) != 3 {
		t.Fatalf("HouseholdRatings() = %+v, %v; want the household's 3 ratings", got, err)
	}
	order := [][2]string{{recipe1, user1}, {recipe1, user2}, {recipe2, user1}}
	for i, want := range order {
		if got[i].RecipeID != want[0] || got[i].UserID != want[1] {
			t.Errorf("rating %d = %s/%s, want %s/%s", i, got[i].RecipeID, got[i].UserID, want[0], want[1])
		}
	}
	if got[2].Comment != "" || len(got[2].Tags) != 1 || got[2].Tags[0] != TagNeverAgain || got[1].Score != 5 {
		t.Errorf("rating fields = %+v; want tags and scores without comments", got)
	}
	if _, err := svc.HouseholdRatings(ctx, ""); err == nil {
		t.Error("HouseholdRatings() accepted an empty household")
	}
}
