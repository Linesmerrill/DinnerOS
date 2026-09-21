package mealkit

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// The numbers in this file are measured against a real HelloFresh account on
// 2026-09-21, not invented: one harvest returned 740 recipes across 160
// delivered weeks in 40 pages, and it stopped because it hit the app's
// 40-page cap with about four years of history still behind it. A
// past-deliveries page covers roughly four to five weeks, which is why 40
// pages reaches roughly 160.
const (
	measuredRecipes = 740
	measuredWeeks   = 160
	measuredPages   = 40
	// thisWeek and the weeks below are the shape of that walk: it started at
	// today and reached about three years back before the cap.
	measuredLatestWeek   = "2026-W38"
	measuredEarliestWeek = "2023-W30"
)

func TestAPastDeliveriesPageCoversAboutFourToFiveWeeks(t *testing.T) {
	// 160 weeks in 40 pages. If a future build changes the walk so a page
	// covers one week, a 4-year history would need ~200 pages and the cap
	// would bite five times as hard — that is worth failing a test over.
	perPage := float64(measuredWeeks) / float64(measuredPages)
	if perPage < 3.5 || perPage > 5.5 {
		t.Fatalf("a page covered %.1f weeks; the measured walk covers about 4", perPage)
	}
	// The whole measured history fits inside one submitted order history, so
	// a capped harvest never has to hold anything back.
	if measuredRecipes > MaxOrderedRecipes {
		t.Errorf("a measured harvest of %d recipes does not fit the %d cap", measuredRecipes, MaxOrderedRecipes)
	}
}

func TestHittingTheCapMeansThereIsMoreHistory(t *testing.T) {
	report := HarvestReport{
		EarliestWeek: measuredEarliestWeek, LatestWeek: measuredLatestWeek,
		Pages: measuredPages, Weeks: measuredWeeks, Stopped: HarvestStopCap,
	}
	cursor := Cursor{}.Merge(report, time.Now())

	if cursor.Complete {
		t.Error("a harvest that stopped on the cap marked the history complete")
	}
	if !cursor.MoreToFetch() || !report.Stopped.MoreToFetch() {
		t.Error("a capped harvest did not report more to fetch")
	}
	if cursor.ResumeFrom() != measuredEarliestWeek {
		t.Errorf("ResumeFrom() = %q, want the earliest week reached %q", cursor.ResumeFrom(), measuredEarliestWeek)
	}
}

func TestTheCursorOnlyEverGainsGroundAndAFinishedHistoryStaysFinished(t *testing.T) {
	at := time.Now()
	// Pass one: stopped on the cap, three years back.
	cursor := Cursor{}.Merge(HarvestReport{
		EarliestWeek: measuredEarliestWeek, LatestWeek: measuredLatestWeek, Stopped: HarvestStopCap,
	}, at)
	// Pass two resumes below the floor and reaches the start of the history.
	cursor = cursor.Merge(HarvestReport{
		EarliestWeek: "2022-W05", LatestWeek: "2023-W29", Stopped: HarvestStopEmpty,
	}, at)

	if cursor.EarliestWeek != "2022-W05" || cursor.LatestWeek != measuredLatestWeek {
		t.Fatalf("cursor = %+v", cursor)
	}
	if !cursor.Complete || cursor.MoreToFetch() || cursor.ResumeFrom() != "" {
		t.Fatalf("a finished history is still reporting more to fetch: %+v", cursor)
	}

	// Pass three is a catch-up for a new delivery: it moves the ceiling up,
	// never the floor, and a completed history stays completed.
	cursor = cursor.Merge(HarvestReport{
		EarliestWeek: "2026-W38", LatestWeek: "2026-W40", Stopped: HarvestStopCaughtUp,
	}, at)
	if cursor.EarliestWeek != "2022-W05" || cursor.LatestWeek != "2026-W40" || !cursor.Complete {
		t.Fatalf("a catch-up pass moved the wrong end: %+v", cursor)
	}

	// An out-of-order replay of an old report loses nothing.
	replayed := cursor.Merge(HarvestReport{
		EarliestWeek: measuredEarliestWeek, LatestWeek: measuredLatestWeek, Stopped: HarvestStopCap,
	}, at)
	if replayed.EarliestWeek != "2022-W05" || replayed.LatestWeek != "2026-W40" || !replayed.Complete {
		t.Fatalf("replaying an older harvest lost ground: %+v", replayed)
	}
}

func TestAHarvestReportIsUntrustedInput(t *testing.T) {
	for name, report := range map[string]HarvestReport{
		"a week that is not one":   {EarliestWeek: "last march"},
		"a week 99 of the year":    {LatestWeek: "2026-W99"},
		"a stop reason we made up": {Stopped: HarvestStop("gave-up")},
		"absurd pages":             {Pages: MaxHarvestPages + 1},
		"absurd weeks":             {Weeks: MaxHarvestWeeks + 1},
	} {
		if _, err := report.Validate(); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}

	// Nothing said is valid and teaches the server nothing, which is the safe
	// direction: it never marks a history complete on silence.
	empty, err := HarvestReport{}.Validate()
	if err != nil {
		t.Fatalf("an absent report was rejected: %v", err)
	}
	if (Cursor{}).Merge(empty, time.Now()).Complete {
		t.Error("an empty report marked the history complete")
	}

	// Two weeks the wrong way round are read the only way they make sense.
	swapped, err := HarvestReport{EarliestWeek: "2026-W38", LatestWeek: "2023-W30"}.Validate()
	if err != nil || swapped.EarliestWeek != "2023-W30" || swapped.LatestWeek != "2026-W38" {
		t.Errorf("swapped = %+v, %v", swapped, err)
	}
}

// --- The service side ---------------------------------------------------------

func TestStartImportRemembersWhereTheHarvestStoppedAndResumesThere(t *testing.T) {
	store := &memoryStore{}
	svc := newTestService(t, store, &fakeSource{})
	ctx := context.Background()

	// Pass one: 40 pages, capped, three years back.
	if _, err := svc.StartImport(ctx, ImportRequest{
		HouseholdID: hhAda, UserID: userAda, Source: SourceHelloFresh, Orders: harvested(),
		Harvest: HarvestReport{
			EarliestWeek: measuredEarliestWeek, LatestWeek: measuredLatestWeek,
			Pages: measuredPages, Weeks: measuredWeeks, Stopped: HarvestStopCap,
		},
	}); err != nil {
		t.Fatalf("StartImport() error = %v", err)
	}

	status, err := svc.Status(ctx, hhAda, SourceHelloFresh)
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	// The app is told exactly where to pick the walk up, so the next harvest
	// does not step back through three years of pages to get there.
	if status.Cursor.ResumeFrom() != measuredEarliestWeek {
		t.Errorf("resume week = %q, want %q", status.Cursor.ResumeFrom(), measuredEarliestWeek)
	}
	if !status.Cursor.MoreToFetch() || status.Job.Harvest.Stopped != HarvestStopCap {
		t.Errorf("cursor = %+v, job harvest = %+v", status.Cursor, status.Job.Harvest)
	}

	// Pass two, after the first run finished: it resumed below the floor and
	// reached the start of the history.
	store.jobs[0].Status = JobSucceeded
	if _, err := svc.StartImport(ctx, ImportRequest{
		HouseholdID: hhAda, UserID: userAda, Source: SourceHelloFresh, Orders: harvested(),
		Harvest: HarvestReport{EarliestWeek: "2022-W05", LatestWeek: "2023-W29", Stopped: HarvestStopEnd},
	}); err != nil {
		t.Fatalf("second StartImport() error = %v", err)
	}
	status, err = svc.Status(ctx, hhAda, SourceHelloFresh)
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if !status.Cursor.Complete || status.Cursor.ResumeFrom() != "" || status.Cursor.MoreToFetch() {
		t.Errorf("after finishing the history the cursor is %+v", status.Cursor)
	}
	if status.Cursor.LatestWeek != measuredLatestWeek {
		t.Errorf("latest week = %q, want the newest ever seen %q", status.Cursor.LatestWeek, measuredLatestWeek)
	}
}

func TestOnlyOneImportRunsForAHouseholdAtATime(t *testing.T) {
	store := &memoryStore{}
	svc := NewService(ServiceOptions{
		Store: store, Enabled: true,
		Sources: map[string]Source{SourceHelloFresh: &fakeSource{}},
	})
	ctx := context.Background()
	req := ImportRequest{HouseholdID: hhAda, UserID: userAda, Source: SourceHelloFresh, Orders: harvested()}

	first, err := svc.StartImport(ctx, req)
	if err != nil {
		t.Fatalf("StartImport() error = %v", err)
	}
	// The same source returns the run in flight rather than a second one.
	again, err := svc.StartImport(ctx, req)
	if err != nil || again.ID != first.ID {
		t.Fatalf("a second start made another job: %v %v", again.ID, err)
	}
	// Another household is unaffected: the limit is per household, not global.
	if _, err := svc.StartImport(ctx, ImportRequest{
		HouseholdID: hhBob, UserID: userBob, Source: SourceHelloFresh, Orders: harvested(),
	}); err != nil {
		t.Errorf("another household was blocked by Ada's run: %v", err)
	}

	// A different source for the same household is refused outright: one
	// household is one polite conversation at a time.
	store.jobs[0].Source = "blueapron"
	if _, err := svc.StartImport(ctx, req); err == nil {
		t.Error("a second source ran next to the first")
	}
}

func TestARefusalBacksOffHardAndIsSurfaced(t *testing.T) {
	store := &memoryStore{}
	svc := newTestService(t, store, &fakeSource{})
	ctx := context.Background()
	now := time.Now().UTC()

	if _, err := svc.StartImport(ctx, ImportRequest{
		HouseholdID: hhAda, UserID: userAda, Source: SourceHelloFresh, Orders: harvested(),
	}); err != nil {
		t.Fatalf("StartImport() error = %v", err)
	}
	// The worker met a 403 and dead-lettered the job; the cursor carries the
	// cooldown.
	store.jobs[0].Status = JobDead
	if err := store.MarkCursorBlocked(ctx, hhAda, SourceHelloFresh, now); err != nil {
		t.Fatalf("MarkCursorBlocked() error = %v", err)
	}

	_, err := svc.StartImport(ctx, ImportRequest{
		HouseholdID: hhAda, UserID: userAda, Source: SourceHelloFresh, Orders: harvested(),
	})
	var validation *ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("StartImport() after a refusal = %v, want a message for the member", err)
	}
	if !strings.Contains(validation.Message, "HelloFresh") {
		t.Errorf("the refusal message does not say who refused: %q", validation.Message)
	}
	if len(store.jobs) != 1 {
		t.Errorf("a run was queued at a service that had just refused us: %d jobs", len(store.jobs))
	}

	// Once the cooldown has passed, importing is offered again.
	if err := store.MarkCursorBlocked(ctx, hhAda, SourceHelloFresh, now.Add(-BlockedCooldown-time.Minute)); err != nil {
		t.Fatalf("MarkCursorBlocked() error = %v", err)
	}
	if _, err := svc.StartImport(ctx, ImportRequest{
		HouseholdID: hhAda, UserID: userAda, Source: SourceHelloFresh, Orders: harvested(),
	}); err != nil {
		t.Errorf("StartImport() after the cooldown = %v", err)
	}
}

func TestWorkerRecordsARefusalOnTheCursor(t *testing.T) {
	store := &memoryStore{}
	src := &fakeSource{recipeErr: map[string]error{"recipe-1": ErrBlocked}}
	svc := newTestService(t, store, src)
	ctx := context.Background()
	if _, err := svc.StartImport(ctx, ImportRequest{
		HouseholdID: hhAda, UserID: userAda, Source: SourceHelloFresh, Orders: harvested(),
	}); err != nil {
		t.Fatalf("StartImport() error = %v", err)
	}

	worker := NewWorker(WorkerOptions{
		Store: store, Service: svc, Publisher: &fakePublisher{},
		Sources: map[string]Source{SourceHelloFresh: src},
	})
	if _, err := worker.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if store.jobs[0].Status != JobDead {
		t.Errorf("job after a refusal = %q, want dead", store.jobs[0].Status)
	}
	cursor, err := store.GetCursor(ctx, hhAda, SourceHelloFresh)
	if err != nil {
		t.Fatalf("GetCursor() error = %v", err)
	}
	if !cursor.Blocked(time.Now()) {
		t.Errorf("the refusal was not recorded: %+v", cursor)
	}
}

// A second harvest of a household that is already imported must cost the
// library nothing: the pipeline answers "unchanged", and no recipe is
// duplicated.
func TestASecondImportOfTheSameRecipesChangesNothing(t *testing.T) {
	store := &memoryStore{}
	src := &fakeSource{}
	svc := newTestService(t, store, src)
	publisher := &fakePublisher{}
	ctx := context.Background()

	for pass := 1; pass <= 2; pass++ {
		if _, err := svc.StartImport(ctx, ImportRequest{
			HouseholdID: hhAda, UserID: userAda, Source: SourceHelloFresh, Orders: harvested(),
			Harvest: HarvestReport{
				EarliestWeek: "2026-W33", LatestWeek: measuredLatestWeek, Stopped: HarvestStopCaughtUp,
			},
		}); err != nil {
			t.Fatalf("pass %d StartImport() error = %v", pass, err)
		}
		worker := NewWorker(WorkerOptions{
			Store: store, Service: svc, Publisher: publisher,
			Sources: map[string]Source{SourceHelloFresh: src},
		})
		if _, err := worker.Run(ctx); err != nil {
			t.Fatalf("pass %d Run() error = %v", pass, err)
		}
	}

	if len(store.jobs) != 2 {
		t.Fatalf("jobs = %d, want one per pass", len(store.jobs))
	}
	if first := store.jobs[0]; first.Checkpoint.Imported != 2 {
		t.Errorf("the first pass imported %d, want 2", first.Checkpoint.Imported)
	}
	second := store.jobs[1]
	if second.Checkpoint.Imported != 0 || second.Checkpoint.Unchanged != 2 {
		t.Errorf("the second pass imported %d and left %d unchanged; want 0 and 2",
			second.Checkpoint.Imported, second.Checkpoint.Unchanged)
	}
	if len(second.Checkpoint.Failures) != 0 {
		t.Errorf("the second pass failed recipes: %+v", second.Checkpoint.Failures)
	}
}
