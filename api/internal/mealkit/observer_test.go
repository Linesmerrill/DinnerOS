package mealkit

import (
	"context"
	"strconv"
	"sync"
	"testing"
)

// recordingObserver keeps every call, as "progress:<status>:<done>" or
// "end:<status>".
type recordingObserver struct {
	mu    sync.Mutex
	calls []string
}

func (o *recordingObserver) ImportProgressed(_ context.Context, job Job) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.calls = append(o.calls, "progress:"+string(job.Status)+":"+strconv.Itoa(job.RecipesDone()))
}

func (o *recordingObserver) ImportEnded(_ context.Context, job Job) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.calls = append(o.calls, "end:"+string(job.Status))
}

func TestWorkerTellsTheObserverAfterEachCheckpointAndOnSuccess(t *testing.T) {
	f := newWorkerFixture(t, orders(25))
	obs := &recordingObserver{}
	if _, err := f.worker(t, func(o *WorkerOptions) { o.Observer = obs }).Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	want := []string{"progress:running:10", "progress:running:20", "progress:running:25", "end:succeeded"}
	if len(obs.calls) != len(want) {
		t.Fatalf("calls = %v, want %v", obs.calls, want)
	}
	for i := range want {
		if obs.calls[i] != want[i] {
			t.Fatalf("calls = %v, want %v", obs.calls, want)
		}
	}
}

func TestWorkerTellsTheObserverWhenARunPutsTheJobDown(t *testing.T) {
	f := newWorkerFixture(t, orders(25))
	obs := &recordingObserver{}
	w := f.worker(t, func(o *WorkerOptions) { o.Observer = obs; o.RecipesPerRun = 10 })
	if _, err := w.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	last := obs.calls[len(obs.calls)-1]
	if last != "progress:queued:10" {
		t.Fatalf("calls = %v, want the last to be the requeue", obs.calls)
	}
}

func TestStoppingAnImportTellsTheObserverWhichRunEnded(t *testing.T) {
	store := &memoryStore{}
	obs := &recordingObserver{}
	svc := NewService(ServiceOptions{
		Store: store, Enabled: true, Sources: map[string]Source{SourceHelloFresh: &fakeSource{}}, Observer: obs,
	})
	if _, err := svc.StartImport(context.Background(), ImportRequest{
		HouseholdID: hhAda, UserID: userAda, Source: SourceHelloFresh, Orders: orders(3),
	}); err != nil {
		t.Fatalf("StartImport() error = %v", err)
	}
	if _, err := svc.StopImports(context.Background(), hhAda, SourceHelloFresh); err != nil {
		t.Fatalf("StopImports() error = %v", err)
	}
	if len(obs.calls) != 1 || obs.calls[0] != "end:canceled" {
		t.Fatalf("calls = %v", obs.calls)
	}
	// Nothing in flight: stopping again tells nobody anything.
	if _, err := svc.StopImports(context.Background(), hhAda, SourceHelloFresh); err != nil {
		t.Fatalf("StopImports() error = %v", err)
	}
	if len(obs.calls) != 1 {
		t.Fatalf("calls = %v", obs.calls)
	}
}
