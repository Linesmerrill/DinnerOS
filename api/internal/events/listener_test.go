package events

import (
	"context"
	"sync"
	"testing"
	"time"
)

type recordingListener struct {
	mu     sync.Mutex
	calls  [][]Event
	hasDDL bool
}

func (l *recordingListener) EventsStored(ctx context.Context, list []Event) {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, l.hasDDL = ctx.Deadline()
	l.calls = append(l.calls, list)
}

func TestListenersSeeStoredEvents(t *testing.T) {
	listener := &recordingListener{}
	store := &memoryStore{}
	svc := NewService(ServiceOptions{
		Store: store, Recipes: &fakeRecipes{byHousehold: map[string][]string{hhA: {recipeA}}},
		Now: func() time.Time { return testNow }, Listeners: []Listener{listener},
	})
	// A canceled request still reaches listeners, with a deadline.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	first := clientCooked(0)
	first.ClientEventID = "a"
	bad := clientCooked(0)
	bad.Type = "recipe.rated" // not a client type: rejected
	if _, err := svc.Ingest(ctx, memberOf(hhA, userA), []ClientEvent{first, bad}); err != nil {
		t.Fatal(err)
	}
	if len(listener.calls) != 1 || len(listener.calls[0]) != 1 || listener.calls[0][0].Type != TypeRecipeCooked ||
		listener.calls[0][0].UserID != userA || listener.calls[0][0].ClientEventID != "a" || !listener.hasDDL {
		t.Fatalf("calls = %+v (deadline %v)", listener.calls, listener.hasDDL)
	}

	// A retry of a stored event reaches listeners again; they're idempotent.
	res, err := svc.Ingest(context.Background(), memberOf(hhA, userA), []ClientEvent{first})
	if err != nil || res.Duplicates != 1 || len(listener.calls) != 2 {
		t.Errorf("retry = %+v, %v, calls %d", res, err, len(listener.calls))
	}
	// A batch with nothing valid calls nobody.
	if _, err := svc.Ingest(context.Background(), memberOf(hhA, userA), []ClientEvent{bad}); err != nil || len(listener.calls) != 2 {
		t.Errorf("rejected batch: %v, calls %d", err, len(listener.calls))
	}

	if err := svc.Record(context.Background(), Event{HouseholdID: hhA, Type: TypeRecipeRated, RecipeID: recipeA, Payload: RecipeRated{Score: 4}}); err != nil {
		t.Fatal(err)
	}
	if len(listener.calls) != 3 || listener.calls[2][0].Type != TypeRecipeRated {
		t.Errorf("Record did not reach listeners: %+v", listener.calls)
	}
}
