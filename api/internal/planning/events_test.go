package planning

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/events"
)

// fakeRecorder keeps recorded events, or fails every Record when err is set.
type fakeRecorder struct {
	mu     sync.Mutex
	events []events.Event
	err    error
}

func (f *fakeRecorder) Record(_ context.Context, e events.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.events = append(f.events, e)
	return nil
}

func (f *fakeRecorder) recorded() []events.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.events)
}

func TestEntryChangesRecordEvents(t *testing.T) {
	recorder := &fakeRecorder{}
	svc, _ := newTestService(t, newMemoryStore())
	if got := svc.WithEvents(recorder, nil); got != svc {
		t.Fatal("WithEvents must return the service it configures")
	}
	ctx := context.Background()

	_, tacos := mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeTacos, Day: "tue", Servings: 2})
	_, soup := mustAdd(t, svc, hhAda, userViewer, testWeek, NewEntry{RecipeID: recipeSoup, Servings: 4})

	// Failed changes record nothing.
	if _, _, err := svc.AddEntry(ctx, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeTacos, Servings: 3}); !errors.Is(err, ErrInvalidEntry) {
		t.Fatalf("AddEntry(bad servings) error = %v", err)
	}
	if _, err := svc.DeleteEntry(ctx, hhAda, userAda, testWeek, "ffffffffffffffffffffffff"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteEntry(unknown) error = %v", err)
	}
	if _, err := svc.DeleteEntry(ctx, hhAda, "", testWeek, tacos.ID); err == nil {
		t.Fatal("DeleteEntry(no user) error = nil")
	}

	if _, err := svc.DeleteEntry(ctx, hhAda, userViewer, testWeek, tacos.ID); err != nil {
		t.Fatalf("DeleteEntry() error = %v", err)
	}
	if _, err := svc.SetStatus(ctx, hhAda, testWeek, "finalized"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DeleteEntry(ctx, hhAda, userAda, testWeek, soup.ID); !errors.Is(err, ErrFinalized) {
		t.Fatalf("DeleteEntry(finalized) error = %v", err)
	}

	want := []events.Event{
		{
			HouseholdID: hhAda, UserID: userAda, Type: events.TypeRecipePlanned, RecipeID: recipeTacos, Week: testWeek, OccurredAt: testNow,
			Payload: events.RecipePlanned{EntryID: tacos.ID, Day: "tue", Date: "2026-09-15", Servings: 2, Origin: "manual"},
		},
		{
			HouseholdID: hhAda, UserID: userViewer, Type: events.TypeRecipePlanned, RecipeID: recipeSoup, Week: testWeek, OccurredAt: testNow,
			Payload: events.RecipePlanned{EntryID: soup.ID, Servings: 4, Origin: "manual"},
		},
		{
			HouseholdID: hhAda, UserID: userViewer, Type: events.TypeRecipeUnplanned, RecipeID: recipeTacos, Week: testWeek, OccurredAt: testNow,
			Payload: events.RecipeUnplanned{EntryID: tacos.ID, Day: "tue", Date: "2026-09-15", Origin: "manual"},
		},
	}
	if got := recorder.recorded(); !reflect.DeepEqual(got, want) {
		t.Errorf("recorded events =\n %+v\nwant\n %+v", got, want)
	}
}

func TestRecorderFailureDoesNotFailEntryChanges(t *testing.T) {
	var logs bytes.Buffer
	recorder := &fakeRecorder{err: errors.New("events collection unavailable")}
	svc, _ := newTestService(t, newMemoryStore())
	svc.WithEvents(recorder, slog.New(slog.NewTextHandler(&logs, nil)))

	_, e := mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeTacos, Servings: 2})
	p, err := svc.DeleteEntry(context.Background(), hhAda, userAda, testWeek, e.ID)
	if err != nil || len(p.Entries) != 0 {
		t.Fatalf("DeleteEntry() = %+v, %v; a recording failure must not fail the change", p, err)
	}
	if n := strings.Count(logs.String(), "record event failed"); n != 2 {
		t.Errorf("logged %d recording failures, want 2:\n%s", n, logs.String())
	}
}

// eventCapture is an events.Store that keeps what the real events service
// validated and inserted.
type eventCapture struct {
	mu     sync.Mutex
	events []events.Event
}

func (c *eventCapture) Insert(_ context.Context, list []events.Event) (int, int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, list...)
	return len(list), 0, nil
}

func (c *eventCapture) List(context.Context, events.Query) ([]events.Event, error) { return nil, nil }

// TestPlanEventsPassEventValidation records through events.Service, so a
// payload the events module would reject fails here rather than only in logs.
func TestPlanEventsPassEventValidation(t *testing.T) {
	capture := &eventCapture{}
	var logs bytes.Buffer
	recorder := events.NewService(events.ServiceOptions{Store: capture, Now: func() time.Time { return testNow }})
	svc, _ := newTestService(t, newMemoryStore())
	svc.WithEvents(recorder, slog.New(slog.NewTextHandler(&logs, nil)))

	_, scheduled := mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeTacos, Day: "sun", Servings: 4})
	_, unscheduled := mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeSalad, Servings: 2})
	for _, id := range []string{scheduled.ID, unscheduled.ID} {
		if _, err := svc.DeleteEntry(context.Background(), hhAda, userAda, testWeek, id); err != nil {
			t.Fatal(err)
		}
	}
	if logs.Len() != 0 {
		t.Fatalf("events were rejected:\n%s", logs.String())
	}
	if len(capture.events) != 4 || capture.events[0].Payload.(events.RecipePlanned).Date != "2026-09-20" {
		t.Errorf("stored events = %+v", capture.events)
	}
}
