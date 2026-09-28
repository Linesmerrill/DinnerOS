package liveactivity

import (
	"context"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/mealkit"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
	"github.com/Linesmerrill/DinnerOS/api/internal/push"
)

func TestIntegrationActivityTokenLivesOnTheJobAndGoesWithIt(t *testing.T) {
	client := mongotest.Client(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db := client.Database()
	jobs := mealkit.NewMongoStore(db)
	store := NewMongoStore(db)
	now := time.Now().UTC().Truncate(time.Millisecond)

	job, err := jobs.InsertJob(ctx, mealkit.Job{
		HouseholdID: hhAda, UserID: userAda, Source: mealkit.SourceHelloFresh, Status: mealkit.JobQueued,
		MaxAttempts: 5, AvailableAt: now, CreatedAt: now, UpdatedAt: now,
		Checkpoint: mealkit.Checkpoint{Phase: mealkit.PhaseRecipes, Orders: []mealkit.OrderedRecipe{{SourceRecipeID: "r1"}}},
	})
	if err != nil {
		t.Fatalf("InsertJob() error = %v", err)
	}

	if _, err := store.Get(ctx, job.ID); err != ErrNotFound {
		t.Fatalf("Get() before registering = %v, want ErrNotFound", err)
	}
	if err := store.Register(ctx, hhBob, mealkit.SourceHelloFresh, job.ID, token1, push.EnvironmentSandbox, now); err != ErrNotFound {
		t.Fatalf("Register() for another household = %v, want ErrNotFound", err)
	}
	if err := store.Register(ctx, hhAda, mealkit.SourceHelloFresh, job.ID, token1, push.EnvironmentSandbox, now); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if err := store.MarkSent(ctx, job.ID, ContentState{Phase: PhaseImporting, Done: 10, Total: 40}, now); err != nil {
		t.Fatalf("MarkSent() error = %v", err)
	}
	// A rotated token replaces the old one and keeps the rate-limit history.
	if err := store.Register(ctx, hhAda, mealkit.SourceHelloFresh, job.ID, token2, push.EnvironmentSandbox, now); err != nil {
		t.Fatalf("Register() rotation error = %v", err)
	}
	reg, err := store.Get(ctx, job.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if reg.Token != token2 || reg.Environment != push.EnvironmentSandbox || reg.HouseholdID != hhAda ||
		reg.LastSent != (ContentState{Phase: PhaseImporting, Done: 10, Total: 40}) || !reg.LastSentAt.Equal(now) {
		t.Fatalf("Get() = %+v", reg)
	}

	// The job itself still reads as it did: the mealkit package never sees the
	// subdocument.
	read, err := jobs.GetJob(ctx, hhAda, job.ID)
	if err != nil || read.Status != mealkit.JobQueued || read.RecipesFound() != 1 {
		t.Fatalf("GetJob() = %+v, %v", read, err)
	}

	if err := store.Clear(ctx, job.ID); err != nil {
		t.Fatalf("Clear() error = %v", err)
	}
	if _, err := store.Get(ctx, job.ID); err != ErrNotFound {
		t.Fatalf("Get() after Clear = %v", err)
	}
	// MarkSent on a cleared job does not bring the subdocument back.
	if err := store.MarkSent(ctx, job.ID, ContentState{Phase: PhaseWaiting}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, job.ID); err != ErrNotFound {
		t.Fatalf("MarkSent resurrected a cleared registration: %v", err)
	}

	// A finished job takes no token.
	if _, err := jobs.CancelJobs(ctx, hhAda, mealkit.SourceHelloFresh, "stopped", now); err != nil {
		t.Fatal(err)
	}
	if err := store.Register(ctx, hhAda, mealkit.SourceHelloFresh, job.ID, token1, push.EnvironmentSandbox, now); err != ErrNotFound {
		t.Fatalf("Register() on a canceled job = %v, want ErrNotFound", err)
	}
	if err := store.Unregister(ctx, hhAda, mealkit.SourceHelloFresh, job.ID); err != nil {
		t.Fatalf("Unregister() error = %v", err)
	}
}
