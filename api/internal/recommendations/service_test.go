package recommendations

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
)

func smokerProfile() ProfileUpdate {
	return ProfileUpdate{
		Schedule:  &Schedule{PlanDays: []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}, Weeknights: []string{"mon", "tue", "wed", "thu"}, MealsPerWeek: 5},
		Equipment: &[]string{"smoker"},
		WeekdayRules: &[]WeekdayRule{{
			Day: "sun", Label: "Sunday smoker night", Proteins: []string{"chicken", "pork"}, Methods: []string{"smoker"},
			TimeBand: "long", Frequency: "at_most_once",
		}},
	}
}

func mustWeek(t *testing.T, s string) planning.Week {
	t.Helper()
	w, err := planning.ParseWeek(s)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func TestProfileSectionsAndHistory(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	svc := env.svc

	p, err := svc.Profile(ctx, hhA)
	if err != nil || p.Configured() || p.Schedule.MealsPerWeek != DefaultMealsPerWeek || len(p.Sections) != 0 {
		t.Fatalf("default profile = %+v, %v", p, err)
	}

	onboarding := smokerProfile()
	onboarding.Taste = &Taste{Likes: Choices{Cuisines: []string{" Mexican", "thai"}, Proteins: []string{"pork"}}}
	p, err = svc.UpdateProfile(ctx, hhA, userAda, onboarding, true)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Configured() || p.Version != 1 || len(p.Sections) != len(Sections) || p.CreatedBy != userAda || !slices.Equal(p.Taste.Likes.Cuisines, []string{"mexican", "thai"}) {
		t.Fatalf("onboarded profile = %+v", p)
	}
	if p.CookTime.QuickMaxMinutes != 20 || p.Novelty != DefaultNovelty {
		t.Errorf("sections the onboarding left out should be defaults: %+v", p)
	}
	recorded := env.events.ofType(events.TypeAutopilotPreferencesUpdated)
	if len(recorded) != 1 || len(recorded[0].Payload.(events.AutopilotPreferencesUpdated).Sections) != len(Sections) {
		t.Fatalf("onboarding events = %+v", recorded)
	}

	// Another member changes one section.
	env.svc.now = func() func() time.Time { later := testNow.Add(time.Hour); return func() time.Time { return later } }()
	p, err = svc.UpdateProfile(ctx, hhA, userAlan, ProfileUpdate{Novelty: ptr("adventurous")}, false)
	if err != nil {
		t.Fatal(err)
	}
	if p.Sections[SectionNovelty].UpdatedBy != userAlan || p.Sections[SectionTaste].UpdatedBy != userAda || p.UpdatedBy != userAlan || p.CreatedBy != userAda || p.Version != 2 {
		t.Errorf("section tracking = %+v", p.Sections)
	}
	recorded = env.events.ofType(events.TypeAutopilotPreferencesUpdated)
	change := recorded[1].Payload.(events.AutopilotPreferencesUpdated)
	if !slices.Equal(change.Sections, []string{"novelty"}) || len(change.Changes) != 1 || change.Changes[0].From != "balanced" || change.Changes[0].To != "adventurous" {
		t.Errorf("novelty change = %+v", change)
	}

	// An update that changes nothing writes nothing.
	if same, err := svc.UpdateProfile(ctx, hhA, userAda, ProfileUpdate{Novelty: ptr("adventurous")}, false); err != nil || same.Version != 2 {
		t.Errorf("no-op update = %+v, %v", same, err)
	}
	if n := len(env.events.ofType(events.TypeAutopilotPreferencesUpdated)); n != 2 {
		t.Errorf("preference events = %d, want 2", n)
	}

	for name, u := range map[string]ProfileUpdate{
		"rule method without equipment": {Equipment: &[]string{}},
		"nothing to change":             {},
	} {
		if _, err := svc.UpdateProfile(ctx, hhA, userAda, u, false); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: error = %v", name, err)
		}
	}

	history, err := svc.History(ctx, hhA, 10)
	if err != nil || len(history) != 2 || history[0].UserID != userAlan || history[1].UserID != userAda || !slices.Equal(history[0].Sections, []string{"novelty"}) {
		t.Errorf("history = %+v, %v", history, err)
	}
	if servings, err := svc.DefaultServings(ctx, p); err != nil || servings != 2 {
		t.Errorf("DefaultServings() = %d, %v", servings, err)
	}
}

func TestWeekContextLifecycle(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	svc := env.svc

	c, err := svc.WeekContext(ctx, hhA, testWeek)
	if err != nil || c.Configured() || c.Week != testWeek {
		t.Fatalf("empty context = %+v, %v", c, err)
	}
	in := WeekContext{Busy: true, MaxMinutes: 20, Note: "soccer every night", Days: []DayOverride{{Day: "fri", Servings: 6}, {Day: "sat", Skip: true}}}
	c, err = svc.SaveWeekContext(ctx, hhA, userAda, testWeek, in)
	if err != nil || !c.Configured() || c.UpdatedBy != userAda || len(c.Days) != 2 {
		t.Fatalf("saved context = %+v, %v", c, err)
	}
	if again, err := svc.SaveWeekContext(ctx, hhA, userAlan, testWeek, in); err != nil || again.Version != c.Version || again.UpdatedBy != userAda {
		t.Errorf("unchanged save = %+v, %v", again, err)
	}
	recorded := env.events.ofType(events.TypeAutopilotWeekContextUpdated)
	if len(recorded) != 1 || recorded[0].Week != testWeek || len(recorded[0].Payload.(events.AutopilotWeekContextUpdated).Changes) != 5 {
		t.Fatalf("context events = %+v", recorded)
	}
	for name, bad := range map[string]WeekContext{"cap": {MaxMinutes: 1}, "day": {Days: []DayOverride{{Day: "x", Skip: true}}}} {
		if _, err := svc.SaveWeekContext(ctx, hhA, userAda, testWeek, bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: error = %v", name, err)
		}
	}
	if _, err := svc.SaveWeekContext(ctx, hhA, userAda, "2026-W99", in); !errors.Is(err, planning.ErrInvalidWeek) {
		t.Errorf("bad week error = %v", err)
	}

	if err := svc.ClearWeekContext(ctx, hhA, userAda, testWeek); err != nil {
		t.Fatal(err)
	}
	if err := svc.ClearWeekContext(ctx, hhA, userAda, testWeek); err != nil {
		t.Errorf("clearing twice = %v", err)
	}
	recorded = env.events.ofType(events.TypeAutopilotWeekContextUpdated)
	if len(recorded) != 2 || !recorded[1].Payload.(events.AutopilotWeekContextUpdated).Cleared {
		t.Errorf("clear events = %+v", recorded)
	}
	if c, _ := svc.WeekContext(ctx, hhA, testWeek); c.Configured() {
		t.Error("context still configured after clearing")
	}
}

func slotRecipes(p Proposal) map[string]string {
	out := map[string]string{}
	for _, s := range p.Slots {
		out[s.Day] = s.RecipeID
	}
	return out
}

func TestGenerateSwapAccept(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	svc := env.svc
	if _, err := svc.UpdateProfile(ctx, hhA, userAda, smokerProfile(), true); err != nil {
		t.Fatal(err)
	}
	w := mustWeek(t, testWeek)
	env.planner.set(planning.Plan{HouseholdID: hhA, Week: w, Status: planning.StatusDraft, Entries: []planning.Entry{
		{ID: "e-manual", RecipeID: rBurger, Day: planning.Monday, Servings: 2, Origin: planning.OriginManual},
	}})

	p, err := svc.Generate(ctx, hhA, userAda, testWeek, GenerateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	days := slotRecipes(p)
	if p.Status != StatusProposed || p.Planned != 4 || p.Requested != 5 || days["mon"] != "" || p.Attempt != 1 || p.Version != 1 {
		t.Fatalf("proposal = %+v", p)
	}
	if slices.Contains(maps2values(days), rBurger) || slices.Contains(maps2values(days), rBread) {
		t.Errorf("proposal repeats the manual meal or plans an add-on: %v", days)
	}
	sun := p.Slots[slices.IndexFunc(p.Slots, func(s Slot) bool { return s.Day == "sun" })]
	if sun.RecipeID != rTenderloin && sun.RecipeID != rThighs {
		t.Errorf("sunday = %s; want a smoker-friendly cut", sun.RecipeName)
	}
	if !strings.HasPrefix(sun.Reasons[0].Text, "Sunday smoker night · ") || !strings.HasSuffix(sun.Reasons[0].Text, " · Long cook OK") || sun.TimeBand != "long" {
		t.Errorf("sunday reasons = %+v (%s)", sun.Reasons, sun.TimeBand)
	}
	smokerMeals := 0
	for _, s := range p.Slots {
		if s.RecipeID == rTenderloin || s.RecipeID == rThighs {
			smokerMeals++
		}
		if len(s.Reasons) == 0 || s.Signals["rating"] != 0 || s.RecipeName == "" || s.Servings != 2 {
			t.Errorf("slot = %+v", s)
		}
	}
	if smokerMeals != 1 {
		t.Errorf("planned %d smoker meals; the rule allows one a week", smokerMeals)
	}
	if generated := env.events.ofType(events.TypeWeekGenerated); len(generated) != 1 || generated[0].Payload.(events.WeekGenerated).Planned != 4 {
		t.Errorf("week.generated = %+v", generated)
	}
	if got, err := svc.Proposal(ctx, hhA, testWeek); err != nil || got.ID != p.ID {
		t.Errorf("Proposal() = %+v, %v", got, err)
	}

	// Swap Tuesday.
	oldTue := days["tue"]
	swapped, err := svc.Swap(ctx, hhA, userAlan, testWeek, "tue", p.Version)
	if err != nil {
		t.Fatal(err)
	}
	tue := swapped.Slots[slices.IndexFunc(swapped.Slots, func(s Slot) bool { return s.Day == "tue" })]
	if tue.RecipeID == oldTue || !slices.Equal(tue.RejectedRecipeIDs, []string{oldTue}) || tue.SwapCount != 1 || swapped.SwapCount != 1 || swapped.Version != p.Version+1 {
		t.Errorf("swapped tuesday = %+v", tue)
	}
	if slices.Contains(maps2values(slotRecipes(swapped)), rBurger) {
		t.Error("a swap offered the week's manual meal")
	}
	if ev := env.events.ofType(events.TypeMealSwapped); len(ev) != 1 || ev[0].RecipeID != tue.RecipeID || ev[0].Payload.(events.MealSwapped).PreviousRecipeID != oldTue {
		t.Errorf("meal.swapped = %+v", ev)
	}
	if _, err := svc.Swap(ctx, hhA, userAda, testWeek, "tue", p.Version); !errors.Is(err, ErrProposalChanged) {
		t.Errorf("stale swap error = %v", err)
	}
	if _, err := svc.Swap(ctx, hhA, userAda, testWeek, "mon", swapped.Version); !errors.Is(err, ErrNotFound) {
		t.Errorf("swap of a day without a meal error = %v", err)
	}

	// Accept, leaving Wednesday out.
	res, err := svc.Accept(ctx, hhA, userAda, testWeek, swapped.Version, []string{"wed", "wed"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Proposal.Status != StatusAccepted || res.Proposal.DecidedBy != userAda || len(res.Added) != 3 || len(res.Plan.Entries) != 4 || len(res.Skipped) != 0 {
		t.Fatalf("accept = %+v", res)
	}
	for _, e := range res.Added {
		if e.Origin != planning.OriginAutopilot || e.ProposalID != p.ID || e.Day == planning.Wednesday {
			t.Errorf("added entry = %+v", e)
		}
	}
	accepted := env.events.ofType(events.TypeWeekAccepted)
	if len(accepted) != 1 || accepted[0].Payload != (events.WeekAccepted{ProposalID: p.ID, ModelVersion: p.ModelVersion, Planned: 4, Added: 3, Excluded: 1, Swaps: 1}) {
		t.Errorf("week.accepted = %+v", accepted)
	}
	if rejected := env.events.ofType(events.TypeMealRejected); len(rejected) != 1 || rejected[0].RecipeID != days["wed"] {
		t.Errorf("meal.rejected = %+v", rejected)
	}
	if _, err := svc.Accept(ctx, hhA, userAda, testWeek, res.Proposal.Version, nil); !errors.Is(err, ErrProposalNotPending) {
		t.Errorf("second accept error = %v", err)
	}

	// Generating again fills only what's left and doesn't reject an accepted
	// proposal.
	again, err := svc.Generate(ctx, hhA, userAda, testWeek, GenerateOptions{})
	if err != nil || again.Attempt != 2 || again.Planned != 1 || again.ID == p.ID {
		t.Fatalf("second generation = %+v, %v", again, err)
	}
	if n := len(env.events.ofType(events.TypeWeekRejected)); n != 0 {
		t.Errorf("week.rejected = %d after generating over an accepted proposal", n)
	}
}

func maps2values(m map[string]string) []string {
	var out []string
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

func TestRegenerateAndReject(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	first, err := env.svc.Generate(ctx, hhA, userAda, testWeek, GenerateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := env.svc.Generate(ctx, hhA, userAda, testWeek, GenerateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if second.Attempt != 2 || second.Version != first.Version+1 || slices.Equal(maps2values(slotRecipes(first)), maps2values(slotRecipes(second))) {
		t.Errorf("regenerated = %v, first %v", slotRecipes(second), slotRecipes(first))
	}
	for _, id := range maps2values(slotRecipes(second)) {
		if slices.Contains(maps2values(slotRecipes(first)), id) {
			t.Errorf("regenerating repeated %s although alternatives exist", id)
		}
	}
	rejected := env.events.ofType(events.TypeWeekRejected)
	if len(rejected) != 1 || rejected[0].Payload.(events.WeekRejected).Reason != "regenerated" || rejected[0].Payload.(events.WeekRejected).ProposalID != first.ID {
		t.Errorf("week.rejected = %+v", rejected)
	}
	if g := env.events.ofType(events.TypeWeekGenerated); g[1].Payload.(events.WeekGenerated).ReplacedProposalID != first.ID {
		t.Errorf("week.generated = %+v", g[1])
	}
	if p, err := env.svc.Generate(ctx, hhA, userAda, testWeek, GenerateOptions{AvoidPrevious: ptr(false)}); err != nil || p.Attempt != 3 {
		t.Errorf("generate without avoiding = %+v, %v", p, err)
	}

	p, _ := env.svc.Proposal(ctx, hhA, testWeek)
	dismissed, err := env.svc.Reject(ctx, hhA, userAlan, testWeek, p.Version)
	if err != nil || dismissed.Status != StatusRejected || dismissed.DecidedBy != userAlan {
		t.Fatalf("Reject() = %+v, %v", dismissed, err)
	}
	if r := env.events.ofType(events.TypeWeekRejected); r[len(r)-1].Payload.(events.WeekRejected).Reason != "dismissed" {
		t.Errorf("dismissal event = %+v", r[len(r)-1])
	}
	if _, err := env.svc.Swap(ctx, hhA, userAda, testWeek, p.Slots[0].ID, dismissed.Version); !errors.Is(err, ErrProposalNotPending) {
		t.Errorf("swap after reject error = %v", err)
	}
	if _, err := env.svc.Proposal(ctx, hhA, "2026-W40"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing proposal error = %v", err)
	}
}

func TestDeterministicProposals(t *testing.T) {
	ctx := context.Background()
	a, b := newTestEnv(t), newTestEnv(t)
	pa, err := a.svc.Generate(ctx, hhA, userAda, testWeek, GenerateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	pb, err := b.svc.Generate(ctx, hhA, userAda, testWeek, GenerateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if pa.InputsHash == "" || pa.InputsHash != pb.InputsHash || !slices.EqualFunc(pa.Slots, pb.Slots, func(x, y Slot) bool {
		return x.Day == y.Day && x.RecipeID == y.RecipeID && x.Score == y.Score && slices.Equal(reasonCodes(x), reasonCodes(y))
	}) {
		t.Errorf("same inputs gave different weeks:\n%+v\n%+v", pa.Slots, pb.Slots)
	}
}

func reasonCodes(s Slot) []string {
	var out []string
	for _, r := range s.Reasons {
		out = append(out, r.Text)
	}
	return out
}

func TestWeekConstraints(t *testing.T) {
	ctx := context.Background()

	t.Run("finalized plan", func(t *testing.T) {
		env := newTestEnv(t)
		env.planner.set(planning.Plan{HouseholdID: hhA, Week: mustWeek(t, testWeek), Status: planning.StatusFinalized})
		if _, err := env.svc.Generate(ctx, hhA, userAda, testWeek, GenerateOptions{}); !errors.Is(err, ErrPlanFinalized) {
			t.Errorf("Generate(finalized) error = %v", err)
		}
	})

	t.Run("skipped week", func(t *testing.T) {
		env := newTestEnv(t)
		if _, err := env.svc.SaveWeekContext(ctx, hhA, userAda, testWeek, WeekContext{Skip: true}); err != nil {
			t.Fatal(err)
		}
		p, err := env.svc.Generate(ctx, hhA, userAda, testWeek, GenerateOptions{})
		if err != nil || p.Planned != 0 || len(p.Messages) == 0 || p.Messages[0].Code != "week_skipped" {
			t.Fatalf("skipped week = %+v, %v", p, err)
		}
		if _, err := env.svc.Accept(ctx, hhA, userAda, testWeek, p.Version, nil); !errors.Is(err, ErrNothingToAccept) {
			t.Errorf("accept of an empty week error = %v", err)
		}
	})

	t.Run("busy week uses effective cook time", func(t *testing.T) {
		env := newTestEnv(t)
		if _, err := env.svc.SaveWeekContext(ctx, hhA, userAda, testWeek, WeekContext{MaxMinutes: 20, MealsPerWeek: 5}); err != nil {
			t.Fatal(err)
		}
		p, err := env.svc.Generate(ctx, hhA, userAda, testWeek, GenerateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		// Rigatoni (prep 20, total 5), salmon (20), stir fry (prep 15 only),
		// and the omelet (15) are ready within 20 minutes; garlic bread is an
		// add-on.
		want := "Only 4 quick recipes (≤20 min) match; planned 4 of 5 nights."
		if p.Planned != 4 || p.Candidates != 4 || p.Messages[0].Text != want || len(p.Unfilled) != 1 {
			t.Fatalf("busy week = %+v", p)
		}
		for _, s := range p.Slots {
			if s.CookMinutes > 20 || s.CookMinutes == 0 || s.Reasons[0].Code != "busyWeek" {
				t.Errorf("busy slot = %+v", s)
			}
			if s.RecipeID == rRigatoni && s.CookMinutes != 20 {
				t.Errorf("rigatoni cook minutes = %d, want max(prep, total) = 20", s.CookMinutes)
			}
		}
	})

	t.Run("accept never replaces planned days or duplicates recipes", func(t *testing.T) {
		env := newTestEnv(t)
		p, err := env.svc.Generate(ctx, hhA, userAda, testWeek, GenerateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		days := slotRecipes(p)
		// Since generating, a member planned Thursday's meal on Tuesday.
		if _, _, err := env.planner.AddEntries(ctx, hhA, userAlan, testWeek, []planning.NewEntry{{RecipeID: days["thu"], Day: "tue", Servings: 2}}); err != nil {
			t.Fatal(err)
		}
		res, err := env.svc.Accept(ctx, hhA, userAda, testWeek, p.Version, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Added) != len(p.Slots)-2 || len(res.Skipped) != 2 ||
			res.Skipped[0] != (SkippedSlot{SlotID: "tue", Day: "tue", Reason: SkipDayTaken}) ||
			res.Skipped[1] != (SkippedSlot{SlotID: "thu", Day: "thu", Reason: SkipAlreadyPlanned}) {
			t.Errorf("accept = added %+v, skipped %+v", res.Added, res.Skipped)
		}
		if _, err := env.svc.Accept(ctx, hhA, userAda, testWeek, p.Version, []string{"sun"}); !errors.Is(err, ErrProposalChanged) {
			t.Errorf("stale accept error = %v", err)
		}
	})

	t.Run("excluding an unknown slot", func(t *testing.T) {
		env := newTestEnv(t)
		p, _ := env.svc.Generate(ctx, hhA, userAda, testWeek, GenerateOptions{})
		if _, err := env.svc.Accept(ctx, hhA, userAda, testWeek, p.Version, []string{"sun"}); !errors.Is(err, ErrInvalid) {
			t.Errorf("error = %v", err)
		}
	})
}

func TestRecipeOverrides(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	svc := env.svc
	method := func(a RecipeAttributes, name string) MethodAttribute {
		return a.Methods[slices.IndexFunc(a.Methods, func(m MethodAttribute) bool { return m.Method == name })]
	}

	a, err := svc.RecipeAttributes(ctx, hhA, rTenderloin)
	if err != nil {
		t.Fatal(err)
	}
	if m := method(a, "smoker"); !m.Suits || m.Source != SourceHeuristic || m.Evidence != "Pork Tenderloin" || a.CookMinutes != 90 || a.TimeBand != "long" {
		t.Fatalf("tenderloin attributes = %+v", a)
	}

	a, err = svc.SetRecipeOverride(ctx, hhA, userAda, rTenderloin, map[string]*bool{"smoker": ptr(false)})
	if err != nil {
		t.Fatal(err)
	}
	if m := method(a, "smoker"); m.Suits || !m.HeuristicSuits || m.Source != SourceOverride || a.Override == nil || a.Override.UpdatedBy != userAda {
		t.Errorf("overridden = %+v", a)
	}
	a, err = svc.SetRecipeOverride(ctx, hhA, userAlan, rTenderloin, map[string]*bool{"smoker": nil, "grill": ptr(true)})
	if err != nil {
		t.Fatal(err)
	}
	if m := method(a, "smoker"); !m.Suits || m.Source != SourceHeuristic {
		t.Errorf("cleared smoker override = %+v", m)
	}
	if m := method(a, "grill"); !m.Suits || m.Source != SourceOverride {
		t.Errorf("grill override = %+v", m)
	}
	updates := env.events.ofType(events.TypeAutopilotRecipeOverrideUpdated)
	if len(updates) != 3 || updates[0].Payload != (events.AutopilotRecipeOverrideUpdated{Method: "smoker", Value: "no", Previous: "auto"}) ||
		updates[2].Payload != (events.AutopilotRecipeOverrideUpdated{Method: "smoker", Value: "auto", Previous: "no"}) {
		t.Errorf("override events = %+v", updates)
	}
	if list, err := svc.RecipeOverrides(ctx, hhA); err != nil || len(list) != 1 || !list[0].Methods["grill"] {
		t.Errorf("RecipeOverrides() = %+v, %v", list, err)
	}
	if _, err := svc.SetRecipeOverride(ctx, hhA, userAda, rTenderloin, map[string]*bool{"grill": ptr(true)}); err != nil ||
		len(env.events.ofType(events.TypeAutopilotRecipeOverrideUpdated)) != 3 {
		t.Errorf("an unchanged override recorded an event: %v", err)
	}

	for name, call := range map[string]func() error{
		"unknown method": func() error {
			_, err := svc.SetRecipeOverride(ctx, hhA, userAda, rTenderloin, map[string]*bool{"oven": ptr(true)})
			return err
		},
		"no methods": func() error { _, err := svc.SetRecipeOverride(ctx, hhA, userAda, rTenderloin, nil); return err },
	} {
		if err := call(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: error = %v", name, err)
		}
	}
	if _, err := svc.SetRecipeOverride(ctx, hhA, userAda, rBobs, map[string]*bool{"grill": ptr(true)}); !errors.Is(err, ErrNotFound) {
		t.Errorf("another household's recipe error = %v", err)
	}
	if _, err := svc.RecipeAttributes(ctx, hhA, "ffffffffffffffffffffffff"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown recipe error = %v", err)
	}

	history, err := svc.History(ctx, hhA, 0)
	if err != nil || len(history) != 3 || history[0].Method != "grill" && history[0].Method != "smoker" {
		t.Errorf("history = %+v, %v", history, err)
	}

	v, err := svc.Vocabulary(ctx, hhA)
	if err != nil || v.CatalogRecipes != 10 {
		t.Errorf("Vocabulary() = %+v, %v", v, err)
	}
}
