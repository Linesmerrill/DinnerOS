package mealkit

import (
	"context"
	"slices"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// wholeSource returns complete recipes, so the real import pipeline accepts
// them.
type wholeSource struct{ fakeSource }

func (w *wholeSource) Recipe(ctx context.Context, o OrderedRecipe) (recipes.ImportRecipe, []recipes.ImportReviewItem, error) {
	return wholeRecipe(o.SourceRecipeID, o.Name, o.Weeks...), nil, nil
}

func wholeRecipe(id, name string, weeks ...string) recipes.ImportRecipe {
	return recipes.ImportRecipe{
		Source: SourceHelloFresh, SourceRecipeID: id, SourceURL: "https://recipes.example.com/" + id,
		Name: name, Servings: []int{2}, TotalMinutes: 30,
		Ingredients: []recipes.ImportIngredient{{
			SourceIngredientID: "ing-salt", Name: "Salt", PantryStaple: true,
			Amounts: []recipes.ImportAmount{{Servings: 2, RawText: "Salt"}},
		}},
		Steps:      []recipes.ImportStep{{Index: 1, Text: "Cook everything."}},
		OrderWeeks: weeks,
	}
}

// The production case end to end, against the real recipe pipeline and
// MongoDB: one dish on the order history under two HelloFresh ids that the
// library already knows are one recipe. The second is merged — its delivery
// week lands on the stored recipe — and nothing is listed as a failure.
func TestIntegrationTheSameDishUnderTwoIdsLandsOnOneRecipe(t *testing.T) {
	mstore, ctx := newMongoStore(t)
	db := mstore.jobs.Database()
	library := recipes.NewService(recipes.NewMongoStore(db))

	// An earlier import stored the dish once, knowing both ids as aliases.
	stored := wholeRecipe("canon-1", "Garlic Bread", "2026-W01")
	stored.SourceAliases = []string{"menu-1", "menu-2"}
	if _, err := library.Import(ctx, hhAda, recipes.ImportFile{
		Version: recipes.ImportVersion, Source: SourceHelloFresh, Recipes: []recipes.ImportRecipe{stored},
	}); err != nil {
		t.Fatalf("seed Import() error = %v", err)
	}

	src := &wholeSource{}
	service := NewService(ServiceOptions{Store: mstore, Enabled: true, Sources: map[string]Source{SourceHelloFresh: src}})
	job, err := service.StartImport(ctx, ImportRequest{
		HouseholdID: hhAda, UserID: userAda, Source: SourceHelloFresh,
		Orders: []OrderedRecipe{
			{SourceRecipeID: "menu-1", Name: "Garlic Bread", Weeks: []string{"2026-W03"}},
			{SourceRecipeID: "menu-2", Name: "Garlic Bread", Weeks: []string{"2026-W04"}},
		},
	})
	if err != nil {
		t.Fatalf("StartImport() error = %v", err)
	}
	worker := NewWorker(WorkerOptions{
		Store: mstore, Service: service, Publisher: ServicePublisher{Service: library}, Owner: "worker-test",
	})
	if report, err := worker.Run(ctx); err != nil || report.Succeeded != 1 {
		t.Fatalf("Run() = %+v, %v", report, err)
	}

	got, err := mstore.GetJob(ctx, hhAda, job.ID)
	if err != nil {
		t.Fatalf("GetJob() error = %v", err)
	}
	if len(got.Checkpoint.Failures) != 0 {
		t.Fatalf("failures = %+v; one dish under two ids is not a failure", got.Checkpoint.Failures)
	}
	if got.RecipesDone() != 2 || got.Checkpoint.Imported != 0 || got.Checkpoint.Updated+got.Checkpoint.Unchanged != 2 {
		t.Errorf("counts = done %d imported %d updated %d unchanged %d",
			got.RecipesDone(), got.Checkpoint.Imported, got.Checkpoint.Updated, got.Checkpoint.Unchanged)
	}
	found, err := recipes.NewMongoStore(db).FindRecipesBySourceIDs(ctx, hhAda, SourceHelloFresh, []string{"canon-1", "menu-1", "menu-2"})
	if err != nil {
		t.Fatalf("FindRecipesBySourceIDs() error = %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("the dish is stored %d times, want once", len(found))
	}
	if weeks := found[0].OrderWeeks; !slices.Contains(weeks, "2026-W03") || !slices.Contains(weeks, "2026-W04") {
		t.Errorf("stored weeks = %v, want both deliveries", weeks)
	}
}
