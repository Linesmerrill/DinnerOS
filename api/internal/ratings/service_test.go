package ratings

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
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/users"
)

const (
	hhA      = "66e5a1f2c3b4a5d6e7f80a01"
	hhB      = "66e5a1f2c3b4a5d6e7f80b01"
	tacos    = "66e5a1f2c3b4a5d6e7f80a11" // hhA
	curry    = "66e5a1f2c3b4a5d6e7f80a12" // hhA
	bobsStew = "66e5a1f2c3b4a5d6e7f80b11" // hhB
	userAda  = "66e5a1f2c3b4a5d6e7f80a21"
	userAlan = "66e5a1f2c3b4a5d6e7f80a22"
	userBob  = "66e5a1f2c3b4a5d6e7f80b21"
	userGone = "66e5a1f2c3b4a5d6e7f80a29" // rated, then the account disappeared
)

type fakeRecipes map[string][]string

func (f fakeRecipes) ExistingRecipeIDs(_ context.Context, householdID string, ids []string) ([]string, error) {
	var out []string
	for _, id := range ids {
		if slices.Contains(f[householdID], id) {
			out = append(out, id)
		}
	}
	return out, nil
}

var testRecipes = fakeRecipes{hhA: {tacos, curry}, hhB: {bobsStew}}

type fakeUsers struct {
	names map[string]string
	err   error
}

func (f fakeUsers) GetUser(_ context.Context, id string) (users.User, error) {
	if f.err != nil {
		return users.User{}, f.err
	}
	name, ok := f.names[id]
	if !ok {
		return users.User{}, users.ErrNotFound
	}
	return users.User{ID: id, DisplayName: name}, nil
}

var testUsers = fakeUsers{names: map[string]string{userAda: "Ada", userAlan: "Alan", userBob: "Bob"}}

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

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

type testEnv struct {
	svc      *Service
	store    *memoryStore
	recorder *fakeRecorder
	clock    *clock
	logs     *bytes.Buffer
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	env := &testEnv{
		store:    &memoryStore{},
		recorder: &fakeRecorder{},
		clock:    &clock{t: time.Date(2026, 9, 15, 18, 0, 0, 0, time.UTC)},
		logs:     &bytes.Buffer{},
	}
	env.svc = NewService(ServiceOptions{
		Store: env.store, Recipes: testRecipes, Users: testUsers, Events: env.recorder,
		Logger: slog.New(slog.NewTextHandler(env.logs, nil)), Now: env.clock.now,
	})
	return env
}

func member(householdID, userID string) households.Membership {
	return households.Membership{HouseholdID: householdID, UserID: userID, Role: households.RoleMember}
}

func mustRate(t *testing.T, env *testEnv, actor households.Membership, recipeID string, in RateInput) Rating {
	t.Helper()
	r, err := env.svc.Rate(context.Background(), actor, recipeID, in)
	if err != nil {
		t.Fatalf("Rate(%s, %s) error = %v", actor.UserID, recipeID, err)
	}
	return r
}

func TestRateUpsertsOneRatingPerMember(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	ada, alan := member(hhA, userAda), member(hhA, userAlan)
	created := env.clock.t

	first := mustRate(t, env, ada, tacos, RateInput{Score: 3})
	if first.ID == "" || first.Score != 3 || first.Comment != "" || first.Tags != nil || !first.CreatedAt.Equal(created) || first.UserID != userAda || first.HouseholdID != hhA {
		t.Fatalf("first rating = %+v", first)
	}

	env.clock.t = created.Add(time.Hour)
	second := mustRate(t, env, ada, tacos, RateInput{Score: 5, Comment: "  Better with extra lime.  ", Tags: []string{"kid-favorite", "make-again", "kid-favorite"}})
	want := Rating{
		ID: first.ID, HouseholdID: hhA, RecipeID: tacos, UserID: userAda, Score: 5, Comment: "Better with extra lime.",
		Tags: []Tag{TagMakeAgain, TagKidFavorite}, CreatedAt: created, UpdatedAt: created.Add(time.Hour),
	}
	if !reflect.DeepEqual(second, want) {
		t.Fatalf("re-rating =\n %+v\nwant\n %+v", second, want)
	}
	mustRate(t, env, alan, tacos, RateInput{Score: 4})
	if n := env.store.count(); n != 2 {
		t.Fatalf("stored %d ratings, want one per member", n)
	}

	got := env.recorder.recorded()
	if len(got) != 3 {
		t.Fatalf("recorded %d events, want 3", len(got))
	}
	wantSecond := events.Event{
		HouseholdID: hhA, UserID: userAda, Type: events.TypeRecipeRated, RecipeID: tacos, OccurredAt: created.Add(time.Hour),
		Payload: events.RecipeRated{Score: 5, PreviousScore: 3, Tags: []string{"make-again", "kid-favorite"}},
	}
	if !reflect.DeepEqual(got[1], wantSecond) {
		t.Errorf("re-rating event =\n %+v\nwant\n %+v", got[1], wantSecond)
	}
	if p := got[0].Payload.(events.RecipeRated); p.PreviousScore != 0 || p.Tags != nil {
		t.Errorf("first rating payload = %+v", p)
	}

	sums, err := env.svc.Summaries(ctx, hhA, userAda, []string{tacos, curry})
	if err != nil {
		t.Fatal(err)
	}
	if s := sums[tacos]; s.Count != 2 || s.Sum != 9 || s.Mine == nil || s.Mine.Score != 5 {
		t.Errorf("tacos summary = %+v", s)
	}
	if avg, ok := sums[tacos].Average(); !ok || avg != 4.5 {
		t.Errorf("tacos average = %v, %v", avg, ok)
	}
	if s, ok := sums[curry]; !ok || s.Count != 0 || s.Mine != nil {
		t.Errorf("unrated curry summary = %+v, present %v; want a zero summary", s, ok)
	}
	if sums, _ := env.svc.Summaries(ctx, hhA, userBob, []string{tacos}); sums[tacos].Mine != nil || sums[tacos].Count != 2 {
		t.Errorf("summary for a non-rater = %+v", sums[tacos])
	}
}

func TestRateValidation(t *testing.T) {
	tests := []struct {
		name string
		in   RateInput
		want string
	}{
		{"missing score", RateInput{}, "score must be between 1 and 5"},
		{"score too high", RateInput{Score: 6}, "score must be between 1 and 5"},
		{"comment too long", RateInput{Score: 3, Comment: strings.Repeat("é", 501)}, "comment must be at most 500 characters"},
		{"unknown tag", RateInput{Score: 3, Tags: []string{"yummy"}}, `tag "yummy" is not allowed; tags must be from: make-again, never-again`},
		{"tag case matters", RateInput{Score: 3, Tags: []string{"Make-Again"}}, "is not allowed"},
		{"contradictory tags", RateInput{Score: 3, Tags: []string{"never-again", "make-again"}}, "can't be used together"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestEnv(t)
			_, err := env.svc.Rate(context.Background(), member(hhA, userAda), tacos, tt.in)
			var ve *ValidationError
			if !errors.As(err, &ve) || !strings.Contains(ve.Message, tt.want) {
				t.Fatalf("Rate() error = %v, want ValidationError containing %q", err, tt.want)
			}
			if env.store.count() != 0 || len(env.recorder.recorded()) != 0 {
				t.Error("invalid rating was stored or recorded")
			}
		})
	}

	env := newTestEnv(t)
	long := mustRate(t, env, member(hhA, userAda), tacos, RateInput{Score: 1, Comment: strings.Repeat("é", 500), Tags: []string{"too-spicy", "never-again"}})
	if !slices.Equal(long.Tags, []Tag{TagNeverAgain, TagTooSpicy}) {
		t.Errorf("tags = %v, want canonical order", long.Tags)
	}
}

func TestRatingsRequireAHouseholdRecipe(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	ada := member(hhA, userAda)
	for _, recipeID := range []string{bobsStew, "not-an-id", "ffffffffffffffffffffffff"} {
		if _, err := env.svc.Rate(ctx, ada, recipeID, RateInput{Score: 4}); !errors.Is(err, ErrNotFound) {
			t.Errorf("Rate(%s) error = %v, want ErrNotFound", recipeID, err)
		}
		if err := env.svc.Remove(ctx, ada, recipeID); !errors.Is(err, ErrNotFound) {
			t.Errorf("Remove(%s) error = %v, want ErrNotFound", recipeID, err)
		}
		if _, _, err := env.svc.List(ctx, ada, recipeID); !errors.Is(err, ErrNotFound) {
			t.Errorf("List(%s) error = %v, want ErrNotFound", recipeID, err)
		}
	}
	if env.store.count() != 0 || len(env.recorder.recorded()) != 0 {
		t.Error("rating stored or recorded for another household's recipe")
	}
}

func TestRecorderFailureDoesNotFailRatingChanges(t *testing.T) {
	env := newTestEnv(t)
	env.recorder.err = errors.New("events collection unavailable")
	ctx := context.Background()
	ada := member(hhA, userAda)

	saved, err := env.svc.Rate(ctx, ada, tacos, RateInput{Score: 4})
	if err != nil || saved.Score != 4 {
		t.Fatalf("Rate() = %+v, %v; a recording failure must not fail the rating", saved, err)
	}
	if env.store.count() != 1 {
		t.Fatal("rating not stored")
	}
	if err := env.svc.Remove(ctx, ada, tacos); err != nil {
		t.Fatalf("Remove() error = %v; a recording failure must not fail the removal", err)
	}
	if env.store.count() != 0 {
		t.Fatal("rating not removed")
	}
	if n := strings.Count(env.logs.String(), "record event failed"); n != 2 {
		t.Errorf("logged %d recording failures, want 2:\n%s", n, env.logs.String())
	}
}

func TestRemove(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	ada := member(hhA, userAda)
	mustRate(t, env, ada, tacos, RateInput{Score: 2})
	mustRate(t, env, member(hhA, userAlan), tacos, RateInput{Score: 5})

	if err := env.svc.Remove(ctx, ada, tacos); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if err := env.svc.Remove(ctx, ada, tacos); err != nil {
		t.Fatalf("second Remove() error = %v; removal is idempotent", err)
	}
	got := env.recorder.recorded()
	if len(got) != 3 || got[2].Type != events.TypeRecipeUnrated || got[2].Payload != (events.RecipeUnrated{PreviousScore: 2}) || got[2].UserID != userAda {
		t.Errorf("events = %+v; want exactly one recipe.unrated", got)
	}
	sums, _ := env.svc.Summaries(ctx, hhA, userAda, []string{tacos})
	if s := sums[tacos]; s.Count != 1 || s.Mine != nil {
		t.Errorf("summary after removal = %+v; only Ada's own rating goes", s)
	}

	env.store.upsertErr = errStoreDown
	if _, err := env.svc.Rate(ctx, ada, tacos, RateInput{Score: 3}); !errors.Is(err, errStoreDown) {
		t.Errorf("Rate() with failing store error = %v", err)
	}
}

func TestListRatings(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	start := env.clock.t
	mustRate(t, env, member(hhA, userAda), tacos, RateInput{Score: 5, Comment: "Great"})
	env.clock.t = start.Add(time.Minute)
	mustRate(t, env, member(hhA, userGone), tacos, RateInput{Score: 2, Tags: []string{"too-spicy"}})
	env.clock.t = start.Add(2 * time.Minute)
	mustRate(t, env, member(hhA, userAlan), tacos, RateInput{Score: 4})
	mustRate(t, env, member(hhA, userAlan), curry, RateInput{Score: 1})

	list, summary, err := env.svc.List(ctx, member(hhA, userAda), tacos)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, mr := range list {
		names = append(names, mr.DisplayName+"="+string(rune('0'+mr.Score)))
	}
	if !slices.Equal(names, []string{"Alan=4", "=2", "Ada=5"}) {
		t.Errorf("list = %v; want most recent first, with an empty name for a missing user", names)
	}
	if avg, _ := summary.Average(); summary.Count != 3 || avg != 3.67 || summary.Mine == nil || summary.Mine.Comment != "Great" {
		t.Errorf("summary = %+v (average %v)", summary, avg)
	}

	failing := NewService(ServiceOptions{Store: env.store, Recipes: testRecipes, Users: fakeUsers{err: errors.New("users down")}})
	if _, _, err := failing.List(ctx, member(hhA, userAda), tacos); err == nil {
		t.Error("user lookup failure was not reported")
	}
}

func TestRatingsRequireHouseholdView(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	guest := households.Membership{HouseholdID: hhA, UserID: userAda, Role: "guest"}
	if _, err := env.svc.Rate(ctx, guest, tacos, RateInput{Score: 3}); !errors.Is(err, households.ErrForbidden) {
		t.Errorf("Rate() error = %v, want ErrForbidden", err)
	}
	if err := env.svc.Remove(ctx, guest, tacos); !errors.Is(err, households.ErrForbidden) {
		t.Errorf("Remove() error = %v, want ErrForbidden", err)
	}
	if _, _, err := env.svc.List(ctx, guest, tacos); !errors.Is(err, households.ErrForbidden) {
		t.Errorf("List() error = %v, want ErrForbidden", err)
	}
}

func TestSummaryAverage(t *testing.T) {
	tests := []struct {
		s    Summary
		want float64
		ok   bool
	}{
		{Summary{}, 0, false},
		{Summary{Count: 1, Sum: 5}, 5, true},
		{Summary{Count: 3, Sum: 13}, 4.33, true},
		{Summary{Count: 3, Sum: 11}, 3.67, true},
	}
	for _, tt := range tests {
		if got, ok := tt.s.Average(); got != tt.want || ok != tt.ok {
			t.Errorf("%+v.Average() = %v, %v; want %v, %v", tt.s, got, ok, tt.want, tt.ok)
		}
	}
	if got := Tags(); len(got) != 8 || got[0] != TagMakeAgain {
		t.Errorf("Tags() = %v", got)
	}
}
