package recommendations

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot"
	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
)

func record(t *testing.T, env *testEnv, e events.Event) {
	t.Helper()
	e.HouseholdID = hhA
	if e.UserID == "" {
		e.UserID = userAda
	}
	if err := env.svc.events.Record(context.Background(), e); err != nil {
		t.Fatalf("Record(%s) error = %v", e.Type, err)
	}
}

// TestFeedbackBecomesInteractions checks how the adapter reads feedback
// events: the swapped-out meal (not its replacement), accepted and removed
// autopilot entries only, views off the plan, ratings with their score, and
// skips on busy weeks.
func TestFeedbackBecomesInteractions(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	at := testNow.Add(-72 * time.Hour)
	for _, e := range []events.Event{
		{Type: events.TypeMealSwapped, RecipeID: rCurry, Week: "2026-W37", OccurredAt: at, Payload: events.MealSwapped{ProposalID: "p", SlotID: "tue", Day: "tue", PreviousRecipeID: rTacos, ModelVersion: "m", SwapNumber: 1}},
		{Type: events.TypeMealRejected, RecipeID: rSoup, Week: "2026-W37", OccurredAt: at, Payload: events.MealRejected{ProposalID: "p", SlotID: "wed", Day: "wed", ModelVersion: "m"}},
		{Type: events.TypeRecipePlanned, RecipeID: rSalmon, Week: "2026-W37", OccurredAt: at, Payload: events.RecipePlanned{Day: "mon", Origin: "autopilot"}},
		{Type: events.TypeRecipePlanned, RecipeID: rBurger, Week: "2026-W37", OccurredAt: at, Payload: events.RecipePlanned{Day: "mon", Origin: "manual"}},
		{Type: events.TypeRecipeUnplanned, RecipeID: rOmelet, Week: "2026-W37", OccurredAt: at, Payload: events.RecipeUnplanned{Origin: "autopilot"}},
		{Type: events.TypeRecipeViewed, RecipeID: rCurry, OccurredAt: at, Payload: events.RecipeViewed{Surface: "detail"}},
		{Type: events.TypeRecipeViewed, RecipeID: rBurger, OccurredAt: at, Payload: events.RecipeViewed{Surface: "plan"}},
		{Type: events.TypeRecipeRated, RecipeID: rStirFry, OccurredAt: at, Payload: events.RecipeRated{Score: 2}},
		{Type: events.TypeRecipeSkipped, RecipeID: rThighs, Week: "2026-W36", OccurredAt: at.Add(-7 * 24 * time.Hour), Payload: events.RecipeSkipped{Reason: "other"}},
	} {
		record(t, env, e)
	}
	if _, err := env.svc.SaveWeekContext(ctx, hhA, userAda, "2026-W36", WeekContext{Busy: true}); err != nil {
		t.Fatal(err)
	}
	profile, _ := env.svc.Profile(ctx, hhA)
	in, _, err := env.svc.buildInput(ctx, hhA, mustWeek(t, testWeek), profile, WeekContext{}, planning.Plan{}, DeviceSignals{})
	if err != nil {
		t.Fatal(err)
	}
	type key struct {
		id   string
		kind autopilot.InteractionKind
	}
	got := map[key]autopilot.Interaction{}
	for _, h := range in.History {
		if h.Kind != autopilot.KindOrdered && h.Kind != autopilot.KindPlanned {
			got[key{h.ItemID, h.Kind}] = h
		}
	}
	for _, want := range []key{
		{rTacos, autopilot.KindSwappedOut}, {rSoup, autopilot.KindRejected}, {rSalmon, autopilot.KindAccepted},
		{rOmelet, autopilot.KindRejected}, {rCurry, autopilot.KindViewed}, {rStirFry, autopilot.KindRated}, {rThighs, autopilot.KindSkipped},
	} {
		h, ok := got[want]
		if !ok {
			t.Errorf("missing %+v in %+v", want, got)
			continue
		}
		if h.At.IsZero() {
			t.Errorf("%+v has no time", want)
		}
	}
	if len(got) != 7 {
		t.Errorf("interactions = %+v; the swapped-in meal, manual plans, and plan views aren't feedback", got)
	}
	if h := got[key{rStirFry, autopilot.KindRated}]; h.Score != 2 {
		t.Errorf("rated = %+v", h)
	}
	if h := got[key{rThighs, autopilot.KindSkipped}]; !h.Busy {
		t.Errorf("a skip in a busy week = %+v", h)
	}
	if !in.LearningSince.IsZero() {
		t.Errorf("LearningSince = %v without a reset", in.LearningSince)
	}
}

func TestLearningAndReset(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	for i, week := range []string{"2026-W36", "2026-W37"} {
		record(t, env, events.Event{
			Type: events.TypeMealSwapped, RecipeID: rCurry, Week: week, OccurredAt: testNow.Add(-time.Duration(8+i) * 24 * time.Hour),
			Payload: events.MealSwapped{ProposalID: "p", SlotID: "tue", Day: "tue", PreviousRecipeID: rTacos, ModelVersion: "m", SwapNumber: 1},
		})
	}
	l, err := env.svc.Learning(ctx, hhA)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(l.Adjustments, func(a LearnedAdjustment) bool { return a.Kind == "item" && a.RecipeID == rTacos })
	if i < 0 || l.Adjustments[i].Label != "Beef Tacos" || l.Adjustments[i].Text != "You swapped this out twice recently" || l.Adjustments[i].Value >= 0 {
		t.Fatalf("Learning() = %+v", l)
	}
	if l.Interactions != 2 || !l.ResetAt.IsZero() {
		t.Errorf("Learning() = %+v", l)
	}

	// The next proposal explains it.
	p, err := env.svc.Generate(ctx, hhA, userAda, testWeek, GenerateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range p.Slots {
		if s.RecipeID == rTacos && !slices.ContainsFunc(s.Reasons, func(r Reason) bool { return r.Text == "You swapped this out twice recently" }) {
			t.Errorf("tacos picked without the learned reason: %+v", s.Reasons)
		}
	}

	reset, err := env.svc.ResetLearning(ctx, hhA, userAda)
	if err != nil {
		t.Fatal(err)
	}
	if len(reset.Adjustments) != 0 || !reset.ResetAt.Equal(testNow) || reset.ResetBy != userAda {
		t.Errorf("ResetLearning() = %+v", reset)
	}
	recorded := env.events.ofType(events.TypeAutopilotLearningReset)
	if len(recorded) != 1 || recorded[0].Payload.(events.AutopilotLearningReset).Adjustments != len(l.Adjustments) {
		t.Errorf("reset events = %+v", recorded)
	}
	// Ratings and history still work; only learning starts over.
	profile, _ := env.svc.Profile(ctx, hhA)
	in, _, err := env.svc.buildInput(ctx, hhA, mustWeek(t, testWeek), profile, WeekContext{}, planning.Plan{}, DeviceSignals{})
	if err != nil || !in.LearningSince.Equal(testNow) {
		t.Errorf("LearningSince = %v, %v", in.LearningSince, err)
	}
	if _, err := env.svc.ResetLearning(ctx, "", userAda); err == nil {
		t.Error("ResetLearning without a household succeeded")
	}
}

type plainProvider struct {
	autopilot.RecommendationProvider
}

func TestLearningUnsupportedProvider(t *testing.T) {
	env := newTestEnv(t)
	env.svc.provider = plainProvider{env.svc.provider}
	if _, err := env.svc.Learning(context.Background(), hhA); !errors.Is(err, ErrLearningUnsupported) {
		t.Errorf("Learning() error = %v", err)
	}
}

func TestLearningEndpoints(t *testing.T) {
	router, env := newTestRouter(t)
	record(t, env, events.Event{
		Type: events.TypeMealRejected, RecipeID: rSoup, Week: "2026-W37", OccurredAt: testNow.Add(-48 * time.Hour),
		Payload: events.MealRejected{ProposalID: "p", SlotID: "wed", Day: "wed", ModelVersion: "m"},
	})
	rec := do(t, router, http.MethodGet, "/learning", "", userView)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET learning = %d %s", rec.Code, rec.Body)
	}
	body := decodeBody[LearningResponse](t, rec)
	if len(body.Adjustments) != 1 || body.Adjustments[0].Direction != "away" || body.Adjustments[0].Label != "Onion Soup" ||
		body.Adjustments[0].RecipeID == nil || body.ResetAt != nil || body.ModelVersion == "" {
		t.Errorf("GET learning = %+v", body)
	}
	if rec := do(t, router, http.MethodDelete, "/learning", "", userView); rec.Code != http.StatusForbidden {
		t.Errorf("DELETE learning without plan.edit = %d", rec.Code)
	}
	rec = do(t, router, http.MethodDelete, "/learning", "", userAda)
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE learning = %d %s", rec.Code, rec.Body)
	}
	if body := decodeBody[LearningResponse](t, rec); len(body.Adjustments) != 0 || body.ResetAt == nil || body.ResetBy == nil || *body.ResetBy != userAda {
		t.Errorf("DELETE learning = %+v", body)
	}

	env.svc.provider = plainProvider{env.svc.provider}
	if rec := do(t, router, http.MethodGet, "/learning", "", userAda); rec.Code != http.StatusNotImplemented {
		t.Errorf("GET learning without a reporter = %d %s", rec.Code, rec.Body)
	}
}
