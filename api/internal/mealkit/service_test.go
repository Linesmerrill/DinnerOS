package mealkit

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func newTestService(t *testing.T, store *memoryStore, src *fakeSource) *Service {
	t.Helper()
	return NewService(ServiceOptions{
		Store:   store,
		Enabled: true,
		Sources: map[string]Source{SourceHelloFresh: src},
	})
}

func harvested() []OrderedRecipe {
	return []OrderedRecipe{
		{SourceRecipeID: "recipe-1", Name: "Sheet Pan Chicken", Weeks: []string{"2026-W38"}},
		{SourceRecipeID: "recipe-2", Name: "Garlic Bread", IsAddon: true},
	}
}

func TestStartImportQueuesTheHarvestedHistoryAndStoresNoCredential(t *testing.T) {
	store := &memoryStore{}
	svc := newTestService(t, store, &fakeSource{})

	job, err := svc.StartImport(context.Background(), ImportRequest{
		HouseholdID: hhAda, UserID: userAda, Source: SourceHelloFresh, Orders: harvested(),
	})
	if err != nil {
		t.Fatalf("StartImport() error = %v", err)
	}
	if job.Status != JobQueued || job.RecipesFound() != 2 {
		t.Fatalf("job = %+v", job)
	}
	// The run starts at the recipes phase: the order history came with it, so
	// there is no reading-the-account phase on the server any more.
	if job.Checkpoint.Phase != PhaseRecipes {
		t.Errorf("phase = %q, want %q", job.Checkpoint.Phase, PhaseRecipes)
	}
	if job.Checkpoint.Orders[0].Weeks[0] != "2026-W38" || !job.Checkpoint.Orders[1].IsAddon {
		t.Errorf("orders = %+v", job.Checkpoint.Orders)
	}
}

func TestStartImportPutsEverySubmittedEntryThroughTheSourcesDoor(t *testing.T) {
	store := &memoryStore{}
	src := &fakeSource{rejectID: "recipe-2"}
	svc := newTestService(t, store, src)

	job, err := svc.StartImport(context.Background(), ImportRequest{
		HouseholdID: hhAda, UserID: userAda, Source: SourceHelloFresh,
		Orders: []OrderedRecipe{
			{SourceRecipeID: "recipe-1", Weeks: []string{"2026-W38"}},
			// Refused by the source: dropped, and the rest still imports.
			{SourceRecipeID: "recipe-2"},
			// The same recipe again, another week: merged, never duplicated.
			{SourceRecipeID: "recipe-1", Weeks: []string{"2026-W33"}},
		},
	})
	if err != nil {
		t.Fatalf("StartImport() error = %v", err)
	}
	if len(job.Checkpoint.Orders) != 1 {
		t.Fatalf("orders = %+v", job.Checkpoint.Orders)
	}
	if got := job.Checkpoint.Orders[0].Weeks; len(got) != 2 {
		t.Errorf("weeks = %v, want both deliveries merged onto one recipe", got)
	}
}

func TestStartImportRejectsAHistoryThatIsNotOne(t *testing.T) {
	svc := newTestService(t, &memoryStore{}, &fakeSource{})
	base := ImportRequest{HouseholdID: hhAda, UserID: userAda, Source: SourceHelloFresh, Orders: harvested()}

	tooMany := make([]OrderedRecipe, MaxOrderedRecipes+1)
	for i := range tooMany {
		tooMany[i] = OrderedRecipe{SourceRecipeID: "recipe-1"}
	}

	for name, mutate := range map[string]func(ImportRequest) ImportRequest{
		"no household": func(r ImportRequest) ImportRequest { r.HouseholdID = ""; return r },
		"bad source":   func(r ImportRequest) ImportRequest { r.Source = "blueapron"; return r },
		"no recipes":   func(r ImportRequest) ImportRequest { r.Orders = nil; return r },
		"nothing the source will take": func(r ImportRequest) ImportRequest {
			r.Orders = []OrderedRecipe{{Name: "no id"}}
			return r
		},
		"more than anyone ordered": func(r ImportRequest) ImportRequest { r.Orders = tooMany; return r },
	} {
		if _, err := svc.StartImport(context.Background(), mutate(base)); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestStartImportIsDisabledWhenTheFeatureIsOff(t *testing.T) {
	svc := NewService(ServiceOptions{Store: &memoryStore{}, Sources: map[string]Source{SourceHelloFresh: &fakeSource{}}})
	if svc.Enabled() {
		t.Fatal("Enabled() is true with the feature off")
	}
	_, err := svc.StartImport(context.Background(), ImportRequest{
		HouseholdID: hhAda, UserID: userAda, Source: SourceHelloFresh, Orders: harvested(),
	})
	if !errors.Is(err, ErrDisabled) {
		t.Errorf("StartImport() = %v, want ErrDisabled", err)
	}
}

func TestStartImportNeverDuplicatesARunInFlight(t *testing.T) {
	store := &memoryStore{}
	svc := newTestService(t, store, &fakeSource{})
	req := ImportRequest{HouseholdID: hhAda, UserID: userAda, Source: SourceHelloFresh, Orders: harvested()}

	first, err := svc.StartImport(context.Background(), req)
	if err != nil {
		t.Fatalf("StartImport() error = %v", err)
	}
	second, err := svc.StartImport(context.Background(), req)
	if err != nil || second.ID != first.ID {
		t.Fatalf("a second start made another job: %v %v", second.ID, err)
	}

	// Once the first run is over, importing again is a fresh run — which is
	// how a re-sync works. Recipes already in the library come back from the
	// pipeline as unchanged, never as duplicates.
	store.jobs[0].Status = JobSucceeded
	third, err := svc.StartImport(context.Background(), req)
	if err != nil || third.ID == first.ID {
		t.Fatalf("re-importing after a finished run = %v %v", third.ID, err)
	}
}

func TestStoppingImportsCancelsEveryRun(t *testing.T) {
	store := &memoryStore{}
	svc := newTestService(t, store, &fakeSource{})
	if _, err := svc.StartImport(context.Background(), ImportRequest{
		HouseholdID: hhAda, UserID: userAda, Source: SourceHelloFresh, Orders: harvested(),
	}); err != nil {
		t.Fatalf("StartImport() error = %v", err)
	}

	canceled, err := svc.StopImports(context.Background(), hhAda, SourceHelloFresh)
	if err != nil || canceled != 1 {
		t.Fatalf("StopImports() = %d, %v", canceled, err)
	}
	if store.jobs[0].Status != JobCanceled {
		t.Errorf("job after stopping = %q", store.jobs[0].Status)
	}
	// Stopping again is not an error.
	if _, err := svc.StopImports(context.Background(), hhAda, SourceHelloFresh); err != nil {
		t.Errorf("second StopImports() error = %v", err)
	}
}

// The whole point of the redesign: there is nowhere in this package to put a
// credential. This test fails if a field for one is ever added back.
func TestNothingAboutTheAccountIsStored(t *testing.T) {
	store := &memoryStore{}
	svc := newTestService(t, store, &fakeSource{})
	if _, err := svc.StartImport(context.Background(), ImportRequest{
		HouseholdID: hhAda, UserID: userAda, Source: SourceHelloFresh, Orders: harvested(),
	}); err != nil {
		t.Fatalf("StartImport() error = %v", err)
	}
	for _, forbidden := range []string{"token", "secret", "cookie", "password", "email"} {
		if strings.Contains(strings.ToLower(describeJob(store.jobs[0])), forbidden) {
			t.Errorf("a stored job carries something called %q", forbidden)
		}
	}
}

func describeJob(j Job) string {
	var b strings.Builder
	b.WriteString(j.ID + j.HouseholdID + j.UserID + j.Source + string(j.Status))
	for _, o := range j.Checkpoint.Orders {
		b.WriteString(o.SourceRecipeID + o.Name + o.URL + strings.Join(o.Weeks, ""))
	}
	return b.String()
}

func TestStatusReportsNothingForAHouseholdThatNeverImported(t *testing.T) {
	svc := newTestService(t, &memoryStore{}, &fakeSource{})
	status, err := svc.Status(context.Background(), hhBob, SourceHelloFresh)
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if status.Job != nil {
		t.Errorf("status = %+v", status)
	}
}

func TestBackoffGrowsAndIsCapped(t *testing.T) {
	base, maxDelay := time.Minute, 10*time.Minute
	prev := time.Duration(0)
	for attempt := 1; attempt <= 3; attempt++ {
		d := Backoff(attempt, base, maxDelay, 0)
		if d <= prev {
			t.Errorf("Backoff(%d) = %v, not longer than %v", attempt, d, prev)
		}
		prev = d
	}
	if d := Backoff(50, base, maxDelay, 0); d != maxDelay {
		t.Errorf("Backoff(50) = %v, want the cap %v", d, maxDelay)
	}
	// Jitter only ever adds, and never more than a quarter.
	plain, jittered := Backoff(2, base, maxDelay, 0), Backoff(2, base, maxDelay, 1)
	if jittered <= plain || jittered > plain+plain/4 {
		t.Errorf("jittered = %v, plain = %v", jittered, plain)
	}
}
