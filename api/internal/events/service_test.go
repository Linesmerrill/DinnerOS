package events

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
)

const (
	hhA     = "66e5a1f2c3b4a5d6e7f80a01"
	hhB     = "66e5a1f2c3b4a5d6e7f80b01"
	recipeA = "66e5a1f2c3b4a5d6e7f80a11" // belongs to hhA
	recipeB = "66e5a1f2c3b4a5d6e7f80b11" // belongs to hhB
	userA   = "66e5a1f2c3b4a5d6e7f80a21"
	userA2  = "66e5a1f2c3b4a5d6e7f80a22"
)

var testNow = time.Date(2026, 9, 15, 18, 0, 0, 0, time.UTC)

// fakeRecipes knows which recipes belong to which household.
type fakeRecipes struct {
	byHousehold map[string][]string
	err         error
	calls       int
}

func (f *fakeRecipes) ExistingRecipeIDs(_ context.Context, householdID string, ids []string) ([]string, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	var out []string
	for _, id := range ids {
		if slices.Contains(f.byHousehold[householdID], id) {
			out = append(out, id)
		}
	}
	return out, nil
}

func newTestService(store Store) (*Service, *fakeRecipes) {
	recipes := &fakeRecipes{byHousehold: map[string][]string{hhA: {recipeA}, hhB: {recipeB}}}
	return NewService(ServiceOptions{Store: store, Recipes: recipes, Now: func() time.Time { return testNow }}), recipes
}

func memberOf(householdID, userID string) households.Membership {
	return households.Membership{HouseholdID: householdID, UserID: userID, Role: households.RoleMember}
}

func TestRecordFillsDefaults(t *testing.T) {
	store := &memoryStore{}
	svc, _ := newTestService(store)
	ctx := context.Background()

	rated := Event{HouseholdID: hhA, UserID: userA, Type: TypeRecipeRated, RecipeID: recipeA, Payload: RecipeRated{Score: 4, Tags: []string{"make-again"}}}
	if err := svc.Record(ctx, rated); err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	occurred := testNow.Add(-time.Hour)
	if err := svc.Record(ctx, Event{HouseholdID: hhA, Type: TypeImportCompleted, OccurredAt: occurred, Payload: ImportCompleted{Source: "hellofresh", Created: 3}}); err != nil {
		t.Fatalf("Record(import) error = %v", err)
	}
	if err := svc.Record(ctx, Event{HouseholdID: hhA, UserID: userA, Type: TypeRecipeViewed, RecipeID: recipeA}); err != nil {
		t.Fatalf("Record(viewed without payload) error = %v", err)
	}

	got := store.all()
	if len(got) != 3 {
		t.Fatalf("stored %d events, want 3", len(got))
	}
	if e := got[0]; e.Source != SourceAPI || !e.OccurredAt.Equal(testNow) || !e.RecordedAt.Equal(testNow) || e.ID == "" ||
		!reflect.DeepEqual(e.Payload, RecipeRated{Score: 4, Tags: []string{"make-again"}}) {
		t.Errorf("rated event = %+v", e)
	}
	if e := got[1]; !e.OccurredAt.Equal(occurred) || !e.RecordedAt.Equal(testNow) || e.UserID != "" {
		t.Errorf("import event = %+v", e)
	}
	if e := got[2]; e.Payload != (RecipeViewed{}) {
		t.Errorf("nil payload stored as %#v, want the zero RecipeViewed", e.Payload)
	}
}

func TestRecordRejectsInvalidEvents(t *testing.T) {
	valid := func() Event {
		return Event{HouseholdID: hhA, UserID: userA, Type: TypeRecipeCooked, RecipeID: recipeA, Payload: RecipeCooked{Date: "2026-09-14", Servings: 2}}
	}
	tests := []struct {
		name   string
		mutate func(*Event)
		want   string
	}{
		{"no household", func(e *Event) { e.HouseholdID = "" }, "householdId is required"},
		{"unknown type", func(e *Event) { e.Type = "recipe.eaten" }, "not a known event type"},
		{"bad source", func(e *Event) { e.Source = "partner" }, "source must be api or client"},
		{"recipe event without recipe", func(e *Event) { e.RecipeID = "" }, "recipeId is required"},
		{"non-recipe event with recipe", func(e *Event) { e.Type, e.Payload = TypeGroceryItemChecked, GroceryItemChecked{Name: "Limes"} }, "recipeId is not allowed"},
		{"payload of another type", func(e *Event) { e.Payload = RecipeSkipped{} }, "wrong type"},
		{"bad week", func(e *Event) { e.Week = "2026-38" }, "ISO week"},
		{"week 54", func(e *Event) { e.Week = "2026-W54" }, "ISO week"},
		{"bad date", func(e *Event) { e.Payload = RecipeCooked{Date: "14/09/2026"} }, "date must be YYYY-MM-DD"},
		{"too many servings", func(e *Event) { e.Payload = RecipeCooked{Servings: 13} }, "servings"},
		{"joined payload problems", func(e *Event) { e.Payload = RecipeCooked{Date: "x", Servings: -1} }, "date must be YYYY-MM-DD; servings"},
		{"rating out of range", func(e *Event) { e.Type, e.Payload = TypeRecipeRated, RecipeRated{Score: 6} }, "score must be between 1 and 5"},
		{"unrated without previous score", func(e *Event) { e.Type, e.Payload = TypeRecipeUnrated, nil }, "previousScore"},
		{"bad skip reason", func(e *Event) { e.Type, e.Payload = TypeRecipeSkipped, RecipeSkipped{Reason: "bored"} }, "reason must be one of"},
		{"bad plan origin", func(e *Event) { e.Type, e.Payload = TypeRecipePlanned, RecipePlanned{Origin: "robot"} }, "origin must be one of"},
		{"bad plan day", func(e *Event) { e.Type, e.Payload = TypeRecipePlanned, RecipePlanned{Day: "monday"} }, "day must be one of mon, tue"},
		{"long entry id", func(e *Event) { e.Payload = RecipeCooked{EntryID: strings.Repeat("x", 65)} }, "entryId must be at most 64"},
		{"import without source", func(e *Event) { e.Type, e.RecipeID, e.Payload = TypeImportCompleted, "", ImportCompleted{} }, "source is required"},
		{"long client event id", func(e *Event) { e.ClientEventID = strings.Repeat("x", 65) }, "clientEventId"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &memoryStore{}
			svc, _ := newTestService(store)
			e := valid()
			tt.mutate(&e)
			err := svc.Record(context.Background(), e)
			if !errors.Is(err, ErrInvalidEvent) || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Record() error = %v, want ErrInvalidEvent containing %q", err, tt.want)
			}
			if len(store.all()) != 0 {
				t.Error("invalid event was stored")
			}
		})
	}
}

// TestPlanPayloadsMatchThePlanner records the events the planning module will
// send for an entry: its ID, day, date, and servings, with the plan's week.
func TestPlanPayloadsMatchThePlanner(t *testing.T) {
	store := &memoryStore{}
	svc, _ := newTestService(store)
	ctx := context.Background()
	planned := Event{HouseholdID: hhA, UserID: userA, Type: TypeRecipePlanned, RecipeID: recipeA, Week: "2026-W38",
		Payload: RecipePlanned{EntryID: "66e5a1f2c3b4a5d6e7f80c01", Day: "tue", Date: "2026-09-15", Servings: 2, Origin: "manual"}}
	unscheduled := Event{HouseholdID: hhA, UserID: userA, Type: TypeRecipeUnplanned, RecipeID: recipeA, Week: "2026-W38",
		Payload: RecipeUnplanned{EntryID: "66e5a1f2c3b4a5d6e7f80c02"}}
	for _, e := range []Event{planned, unscheduled} {
		if err := svc.Record(ctx, e); err != nil {
			t.Fatalf("Record(%s) error = %v", e.Type, err)
		}
	}
	if got := store.all(); len(got) != 2 || got[0].Payload != planned.Payload || got[1].Week != "2026-W38" {
		t.Errorf("stored = %+v", got)
	}
}

func TestRecordReportsStoreFailure(t *testing.T) {
	svc, _ := newTestService(&memoryStore{insertErr: errors.New("mongo down")})
	err := svc.Record(context.Background(), Event{HouseholdID: hhA, Type: TypeImportCompleted, Payload: ImportCompleted{Source: "hellofresh"}})
	if err == nil || errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("Record() error = %v, want the store error", err)
	}
}

func clientCooked(offset time.Duration) ClientEvent {
	return ClientEvent{Type: string(TypeRecipeCooked), RecipeID: recipeA, OccurredAt: testNow.Add(offset).Format(time.RFC3339), Payload: []byte(`{"date":"2026-09-14","servings":4}`)}
}

func TestIngestBatchLimits(t *testing.T) {
	svc, _ := newTestService(&memoryStore{})
	actor := memberOf(hhA, userA)
	for _, n := range []int{0, MaxBatchSize + 1} {
		batch := make([]ClientEvent, n)
		for i := range batch {
			batch[i] = clientCooked(-time.Minute)
		}
		if _, err := svc.Ingest(context.Background(), actor, batch); !errors.Is(err, ErrInvalidBatch) {
			t.Errorf("Ingest(%d events) error = %v, want ErrInvalidBatch", n, err)
		}
	}
	batch := make([]ClientEvent, MaxBatchSize)
	for i := range batch {
		batch[i] = clientCooked(-time.Duration(i) * time.Second)
	}
	res, err := svc.Ingest(context.Background(), actor, batch)
	if err != nil || res.Accepted != MaxBatchSize || len(res.Rejected) != 0 {
		t.Errorf("Ingest(%d events) = %+v, %v; want all accepted", MaxBatchSize, res, err)
	}
}

func TestIngestValidatesEachEvent(t *testing.T) {
	store := &memoryStore{}
	svc, recipes := newTestService(store)
	actor := memberOf(hhA, userA)
	at := testNow.Add(-time.Hour).Format(time.RFC3339)

	batch := []ClientEvent{
		0:  clientCooked(-time.Hour),
		1:  {Type: string(TypeRecipeViewed), RecipeID: recipeA, OccurredAt: "2026-09-15T11:59:30.250-06:00", Payload: []byte(`{"surface":"detail"}`)},
		2:  {Type: string(TypeRecipeRated), RecipeID: recipeA, OccurredAt: at, Payload: []byte(`{"score":5}`)},
		3:  {Type: "recipe.eaten", RecipeID: recipeA, OccurredAt: at},
		4:  {Type: string(TypeRecipeCooked), RecipeID: recipeA},
		5:  {Type: string(TypeRecipeCooked), RecipeID: recipeA, OccurredAt: "yesterday"},
		6:  clientCooked(10 * time.Minute),
		7:  clientCooked(-31 * 24 * time.Hour),
		8:  {Type: string(TypeRecipeSkipped), RecipeID: recipeA, OccurredAt: at, Payload: []byte(`{"reason":"no-time","mood":"meh"}`)},
		9:  {Type: string(TypeRecipeSkipped), RecipeID: recipeA, OccurredAt: at, Payload: []byte(`{"reason":"bored"}`)},
		10: {Type: string(TypeGroceryItemChecked), OccurredAt: at, Week: "2026-W38", Payload: []byte(`{"name":"Limes","checked":true}`)},
		11: {Type: string(TypeGroceryItemChecked), OccurredAt: at, Payload: []byte(`{"checked":true}`)},
		12: {Type: string(TypeGroceryItemChecked), RecipeID: recipeA, OccurredAt: at, Payload: []byte(`{"name":"Limes"}`)},
		13: {Type: string(TypeRecipeCooked), RecipeID: recipeB, OccurredAt: at}, // another household's recipe
		14: {Type: string(TypeRecipeCooked), RecipeID: "not-an-id", OccurredAt: at},
		15: {Type: string(TypeRecipeViewed), RecipeID: recipeA, OccurredAt: at, Payload: []byte(`{"surface":"` + strings.Repeat("x", MaxPayloadBytes) + `"}`)},
		16: {Type: string(TypeRecipeViewed), RecipeID: recipeA, OccurredAt: at, Payload: []byte(`null`)},
		17: {Type: string(TypeRecipeViewed), RecipeID: recipeA, OccurredAt: at, Payload: []byte(`{"surface":1}`)},
	}
	res, err := svc.Ingest(context.Background(), actor, batch)
	if err != nil {
		t.Fatalf("Ingest() error = %v", err)
	}
	wantRejected := map[int]string{
		2:  "type must be one of grocery.item_checked, recipe.cooked, recipe.skipped, recipe.viewed",
		3:  "type must be one of",
		4:  "occurredAt is required",
		5:  "occurredAt must be an RFC 3339 timestamp",
		6:  "must not be in the future",
		7:  "within the last 30 days",
		8:  `payload is not valid for recipe.skipped: unknown field "mood"`,
		9:  "payload: reason must be one of",
		11: "ingredientId or name is required",
		12: "recipeId is not allowed",
		13: "does not match a recipe in this household",
		14: "does not match a recipe in this household",
		15: "payload must be at most 1024 bytes",
		17: "payload is not valid for recipe.viewed",
	}
	if res.Accepted != 4 || res.Duplicates != 0 || len(res.Rejected) != len(wantRejected) {
		t.Fatalf("result = %+v", res)
	}
	for i, rej := range res.Rejected {
		if i > 0 && rej.Index <= res.Rejected[i-1].Index {
			t.Errorf("rejections not in batch order: %+v", res.Rejected)
		}
		if want, ok := wantRejected[rej.Index]; !ok || !strings.Contains(rej.Message, want) {
			t.Errorf("rejected[%d] = %q, want containing %q", rej.Index, rej.Message, want)
		}
		if strings.HasPrefix(rej.Message, "events:") {
			t.Errorf("rejection leaks the error prefix: %q", rej.Message)
		}
	}
	if recipes.calls != 1 {
		t.Errorf("recipe checks = %d, want one per batch", recipes.calls)
	}

	stored := store.all()
	if len(stored) != 4 {
		t.Fatalf("stored = %+v", stored)
	}
	for _, e := range stored {
		if e.Source != SourceClient || e.UserID != userA || e.HouseholdID != hhA || !e.RecordedAt.Equal(testNow) {
			t.Errorf("stored event = %+v; source, user, and household must come from the server", e)
		}
	}
	if p := stored[0].Payload; p != (RecipeCooked{Date: "2026-09-14", Servings: 4}) {
		t.Errorf("cooked payload = %#v", p)
	}
	if e := stored[1]; e.OccurredAt.Location() != time.UTC || !e.OccurredAt.Equal(time.Date(2026, 9, 15, 17, 59, 30, 250e6, time.UTC)) {
		t.Errorf("viewed occurredAt = %v, want normalized to UTC", e.OccurredAt)
	}
	if e := stored[2]; e.Week != "2026-W38" || e.Payload != (GroceryItemChecked{Name: "Limes", Checked: true}) {
		t.Errorf("grocery event = %+v", e)
	}
}

func TestIngestDeduplicatesClientEventIDs(t *testing.T) {
	store := &memoryStore{}
	svc, _ := newTestService(store)
	ctx := context.Background()
	withKey := func(key string) ClientEvent {
		e := clientCooked(-time.Minute)
		e.ClientEventID = key
		return e
	}

	res, err := svc.Ingest(ctx, memberOf(hhA, userA), []ClientEvent{withKey("k1"), withKey("k1"), withKey("k2"), clientCooked(0), clientCooked(0)})
	if err != nil || res.Accepted != 4 || res.Duplicates != 1 {
		t.Fatalf("first batch = %+v, %v; want 4 accepted (events without a key are never deduplicated) and 1 duplicate", res, err)
	}
	res, err = svc.Ingest(ctx, memberOf(hhA, userA), []ClientEvent{withKey("k1"), withKey("k3")})
	if err != nil || res.Accepted != 1 || res.Duplicates != 1 {
		t.Fatalf("retry = %+v, %v; want k1 duplicate and k3 accepted", res, err)
	}
	// Keys are per user.
	res, err = svc.Ingest(ctx, memberOf(hhA, userA2), []ClientEvent{withKey("k1")})
	if err != nil || res.Accepted != 1 {
		t.Fatalf("other user's k1 = %+v, %v; want accepted", res, err)
	}
	if n := len(store.all()); n != 6 {
		t.Errorf("stored %d events, want 6", n)
	}
}

func TestIngestAuthorizationAndFailures(t *testing.T) {
	svc, recipes := newTestService(&memoryStore{})
	ctx := context.Background()
	stranger := households.Membership{HouseholdID: hhA, UserID: userA, Role: "guest"}
	if _, err := svc.Ingest(ctx, stranger, []ClientEvent{clientCooked(0)}); !errors.Is(err, households.ErrForbidden) {
		t.Errorf("unknown role error = %v, want ErrForbidden", err)
	}

	recipes.err = errors.New("mongo down")
	if _, err := svc.Ingest(ctx, memberOf(hhA, userA), []ClientEvent{clientCooked(0)}); err == nil || errors.Is(err, ErrInvalidBatch) {
		t.Errorf("recipe check failure error = %v, want an internal error", err)
	}

	failing, _ := newTestService(&memoryStore{insertErr: errors.New("mongo down")})
	if _, err := failing.Ingest(ctx, memberOf(hhA, userA), []ClientEvent{clientCooked(0)}); err == nil {
		t.Error("store failure was not reported")
	}
}

type recorderFunc func(ctx context.Context, e Event) error

func (f recorderFunc) Record(ctx context.Context, e Event) error { return f(ctx, e) }

func TestRecordOrLog(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	e := Event{HouseholdID: hhA, Type: TypeRecipeRated, RecipeID: recipeA}

	// A canceled request still records, with a deadline of its own.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var sawErr error
	var sawDeadline bool
	RecordOrLog(ctx, recorderFunc(func(ctx context.Context, _ Event) error {
		sawErr = ctx.Err()
		_, sawDeadline = ctx.Deadline()
		return nil
	}), logger, e)
	if sawErr != nil || !sawDeadline {
		t.Errorf("record context err = %v, deadline = %v; want live context with a deadline", sawErr, sawDeadline)
	}
	if logs.Len() != 0 {
		t.Errorf("success logged: %s", logs.String())
	}

	RecordOrLog(context.Background(), recorderFunc(func(context.Context, Event) error { return fmt.Errorf("mongo down") }), logger, e)
	if !strings.Contains(logs.String(), "record event failed") || !strings.Contains(logs.String(), "recipe.rated") {
		t.Errorf("failure not logged: %s", logs.String())
	}

	RecordOrLog(context.Background(), nil, logger, e) // no recorder: no-op, no panic
}

func TestTypeLists(t *testing.T) {
	if got := ClientTypes(); !slices.Equal(got, []Type{TypeGroceryItemChecked, TypeRecipeCooked, TypeRecipeSkipped, TypeRecipeViewed}) {
		t.Errorf("ClientTypes() = %v", got)
	}
	for _, typ := range Types() {
		p, err := typeSpecs[typ].decode(nil, nil)
		if err != nil || p.EventType() != typ {
			t.Errorf("%s decodes to %#v, %v", typ, p, err)
		}
	}
	if Type("recipe.eaten").Valid() || !TypeRecipeCooked.Valid() {
		t.Error("Type.Valid is wrong")
	}
}
