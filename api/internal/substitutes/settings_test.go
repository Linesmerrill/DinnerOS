package substitutes

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
)

// fakeRecorder collects the events a service records.
type fakeRecorder struct {
	mu       sync.Mutex
	recorded []events.Event
}

func (r *fakeRecorder) Record(_ context.Context, e events.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recorded = append(r.recorded, e)
	return nil
}

func (r *fakeRecorder) types() []events.Type {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]events.Type, 0, len(r.recorded))
	for _, e := range r.recorded {
		out = append(out, e.Type)
	}
	return out
}

// curatedSpecialty reads one curated specialty ingredient from the seed the
// fixture synced.
func curatedSpecialty(t *testing.T, f *fixture, id string) Specialty {
	t.Helper()
	sp, err := f.store.GetSpecialty(f.ctx, id)
	if err != nil {
		t.Fatalf("GetSpecialty(%s) error = %v", id, err)
	}
	return sp
}

// TestStrategyResolution covers the rule the whole feature rests on: an
// explicit choice always wins, each strategy prefers its kind of option and
// falls back to the other, and ask applies nothing.
func TestStrategyResolution(t *testing.T) {
	f := newFixture(t)
	// tex-mex-paste has both kinds and defaults to its store alternative;
	// southwest-spice-blend has both and defaults to its batch; the stock
	// concentrate has only a store alternative.
	tex := curatedSpecialty(t, f, "tex-mex-paste")
	southwest := curatedSpecialty(t, f, "southwest-spice-blend")
	stock := curatedSpecialty(t, f, "chicken-stock-concentrate")

	for _, tc := range []struct {
		name       string
		sp         Specialty
		choice     *Choice
		strategy   Strategy
		wantSource ChoiceSource
		wantOption string
		wantAsIs   bool
	}{
		{name: "similar prefers the store alternative", sp: tex, strategy: StrategySimilar,
			wantSource: ChoiceSourceStrategy, wantOption: "tex-mex-paste.store"},
		{name: "closest prefers the batch", sp: tex, strategy: StrategyClosest,
			wantSource: ChoiceSourceStrategy, wantOption: "tex-mex-paste.batch"},
		// The curated default is a batch, but similar still wants the store.
		{name: "similar overrides a batch default", sp: southwest, strategy: StrategySimilar,
			wantSource: ChoiceSourceStrategy, wantOption: "southwest-spice-blend.store"},
		{name: "closest takes the batch default", sp: southwest, strategy: StrategyClosest,
			wantSource: ChoiceSourceStrategy, wantOption: "southwest-spice-blend.batch"},
		{name: "ask applies nothing", sp: tex, strategy: StrategyAsk, wantSource: ChoiceSourceNone},
		{name: "similar uses the only store option", sp: stock, strategy: StrategySimilar,
			wantSource: ChoiceSourceStrategy, wantOption: "chicken-stock-concentrate.store"},
		{name: "closest falls back when there is no batch", sp: stock, strategy: StrategyClosest,
			wantSource: ChoiceSourceStrategy, wantOption: "chicken-stock-concentrate.store"},
		{name: "an explicit choice beats the strategy", sp: tex, strategy: StrategySimilar,
			choice:     &Choice{SpecialtyID: tex.ID, OptionID: "tex-mex-paste.batch"},
			wantSource: ChoiceSourceHousehold, wantOption: "tex-mex-paste.batch"},
		{name: "as_is beats the strategy", sp: tex, strategy: StrategyClosest,
			choice:     &Choice{SpecialtyID: tex.ID, OptionID: OptionAsIs},
			wantSource: ChoiceSourceHousehold, wantAsIs: true},
		{name: "a deleted option falls back to the strategy", sp: tex, strategy: StrategySimilar,
			choice:     &Choice{SpecialtyID: tex.ID, OptionID: "66e5a1f2c3b4a5d6e7f8ffff"},
			wantSource: ChoiceSourceStrategy, wantOption: "tex-mex-paste.store"},
		{name: "a deleted option under ask stays unchosen", sp: tex, strategy: StrategyAsk,
			choice:     &Choice{SpecialtyID: tex.ID, OptionID: "66e5a1f2c3b4a5d6e7f8ffff"},
			wantSource: ChoiceSourceNone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := resolve(tc.sp, tc.choice, nil, tc.strategy)
			if r.Source != tc.wantSource || r.AsIs != tc.wantAsIs {
				t.Fatalf("resolve() = %+v, want source %q asIs %v", r, tc.wantSource, tc.wantAsIs)
			}
			switch {
			case tc.wantOption == "":
				if r.Option != nil {
					t.Errorf("option = %+v, want none", r.Option)
				}
			case r.Option == nil || r.Option.ID != tc.wantOption:
				t.Errorf("option = %+v, want %s", r.Option, tc.wantOption)
			}
			if tc.wantSource == ChoiceSourceStrategy && r.Strategy != tc.strategy {
				t.Errorf("strategy = %q, want %q", r.Strategy, tc.strategy)
			}
			if tc.wantSource != ChoiceSourceStrategy && r.Strategy != "" {
				t.Errorf("strategy = %q, want empty", r.Strategy)
			}
		})
	}
}

// TestStrategyPrefersAHouseholdOption checks that a household option the
// members chose still wins over the strategy, and that the strategy itself
// only ever picks curated options.
func TestStrategyPrefersAHouseholdOption(t *testing.T) {
	f := newFixture(t)
	tex := curatedSpecialty(t, f, "tex-mex-paste")
	own := Option{ID: "66e5a1f2c3b4a5d6e7f80e01", SpecialtyID: tex.ID, Source: SourceHousehold, Type: TypeStoreAlternative, Name: "Ours"}
	choice := &Choice{SpecialtyID: tex.ID, OptionID: own.ID}
	r := resolve(tex, choice, []Option{own}, StrategyClosest)
	if r.Source != ChoiceSourceHousehold || r.Option == nil || r.Option.ID != own.ID {
		t.Errorf("a chosen household option = %+v", r)
	}
	// Without a choice, the strategy ignores the household's own option.
	if r := resolve(tex, nil, []Option{own}, StrategySimilar); r.Option == nil || r.Option.Source != SourceCurated {
		t.Errorf("strategy picked %+v, want a curated option", r.Option)
	}
}

func TestSettingsAndStrategy(t *testing.T) {
	f := newFixture(t)
	rec := &fakeRecorder{}
	f.svc.WithEvents(rec)

	// A household that never set one gets the default, with no author.
	f.store.settings = map[string]Settings{}
	set, err := f.svc.Settings(f.ctx, testHousehold)
	if err != nil || set.Strategy != StrategySimilar || set.UpdatedBy != "" || !set.UpdatedAt.IsZero() {
		t.Fatalf("default settings = %+v, %v", set, err)
	}

	saved, err := f.svc.SetStrategy(f.ctx, f.actor, "closest")
	if err != nil || saved.Strategy != StrategyClosest || saved.UpdatedBy != testUser || !saved.UpdatedAt.Equal(testNow) {
		t.Fatalf("SetStrategy() = %+v, %v", saved, err)
	}
	if got, _ := f.svc.Settings(f.ctx, testHousehold); got.Strategy != StrategyClosest {
		t.Errorf("after set = %+v", got)
	}
	if _, err := f.svc.SetStrategy(f.ctx, f.actor, " Similar "); err != nil {
		t.Fatal(err)
	}
	if got := rec.types(); !slices.Equal(got, []events.Type{events.TypeSpecialtyStrategyUpdated, events.TypeSpecialtyStrategyUpdated}) {
		t.Fatalf("events = %v", got)
	}
	first, ok := rec.recorded[0].Payload.(events.SpecialtyStrategyUpdated)
	if !ok || first.Strategy != "closest" || first.Previous != "" {
		t.Errorf("first event = %+v", rec.recorded[0].Payload)
	}
	second, ok := rec.recorded[1].Payload.(events.SpecialtyStrategyUpdated)
	if !ok || second.Strategy != "similar" || second.Previous != "closest" {
		t.Errorf("second event = %+v", rec.recorded[1].Payload)
	}
	if rec.recorded[0].HouseholdID != testHousehold || rec.recorded[0].UserID != testUser {
		t.Errorf("event actor = %+v", rec.recorded[0])
	}

	for _, bad := range []string{"", "maybe", "similar-ish", "SIMILARISH"} {
		if _, err := f.svc.SetStrategy(f.ctx, f.actor, bad); !isValidation(err) {
			t.Errorf("SetStrategy(%q) error = %v", bad, err)
		}
	}
	noEdit := households.Membership{HouseholdID: testHousehold, UserID: testUser}
	if _, err := f.svc.SetStrategy(f.ctx, noEdit, "ask"); !errors.Is(err, ErrForbidden) {
		t.Errorf("SetStrategy() without pantry.edit error = %v", err)
	}
	if _, err := f.svc.Settings(f.ctx, ""); err == nil {
		t.Error("Settings() without a household: error = nil")
	}
	// A stored strategy that is no longer known reads as the default.
	if _, err := f.store.PutSettings(f.ctx, Settings{HouseholdID: testHousehold, Strategy: "retired", UpdatedBy: testUser, UpdatedAt: testNow}); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.svc.Settings(f.ctx, testHousehold); got.Strategy != DefaultStrategy {
		t.Errorf("unknown stored strategy = %+v", got)
	}
}

// TestGrocerySpecialtiesUnderStrategies builds the same week's specialty lines
// under each strategy: store alternatives, batches, or nothing applied.
func TestGrocerySpecialtiesUnderStrategies(t *testing.T) {
	f := newFixture(t)
	texID, southwestID := f.catalog.id("Tex-Mex Paste"), f.catalog.id("Southwest Spice Blend")
	lines := []grocery.Line{
		{IngredientKey: texID, Name: "Tex-Mex Paste"},
		{IngredientKey: southwestID, Name: "Southwest Spice Blend"},
	}
	for _, tc := range []struct {
		strategy       Strategy
		kind           grocery.ChoiceType
		texOpt, swOpt  string
		wantPantryKeys bool
	}{
		{strategy: StrategySimilar, kind: grocery.ChoiceStoreAlternative,
			texOpt: "tex-mex-paste.store", swOpt: "southwest-spice-blend.store"},
		{strategy: StrategyClosest, kind: grocery.ChoiceHouseMadeBatch,
			texOpt: "tex-mex-paste.batch", swOpt: "southwest-spice-blend.batch", wantPantryKeys: true},
	} {
		t.Run(string(tc.strategy), func(t *testing.T) {
			if _, err := f.svc.SetStrategy(f.ctx, f.actor, string(tc.strategy)); err != nil {
				t.Fatal(err)
			}
			specs, err := f.svc.GrocerySpecialties(f.ctx, testHousehold, lines)
			if err != nil {
				t.Fatal(err)
			}
			for key, wantOpt := range map[string]string{texID: tc.texOpt, southwestID: tc.swOpt} {
				c := specs[key].Choice
				if c == nil || c.Type != tc.kind || c.OptionID != wantOpt || c.Strategy != string(tc.strategy) {
					t.Fatalf("%s = %+v, want %s from %s", key, c, wantOpt, tc.strategy)
				}
				if len(c.Components) == 0 {
					t.Errorf("%s has no components to buy", key)
				}
				if got := c.PantryKey != ""; got != tc.wantPantryKeys {
					t.Errorf("%s pantryKey = %q", key, c.PantryKey)
				}
			}
		})
	}

	// ask applies nothing, so both lines stay with their suggestions.
	if _, err := f.svc.SetStrategy(f.ctx, f.actor, string(StrategyAsk)); err != nil {
		t.Fatal(err)
	}
	specs, err := f.svc.GrocerySpecialties(f.ctx, testHousehold, lines)
	if err != nil {
		t.Fatal(err)
	}
	for key, spec := range specs {
		if spec.Choice != nil || len(spec.Suggestions) == 0 {
			t.Errorf("%s under ask = %+v, suggestions %d", key, spec.Choice, len(spec.Suggestions))
		}
	}

	// An explicit choice still wins, and carries no strategy.
	if _, err := f.svc.SetStrategy(f.ctx, f.actor, string(StrategyClosest)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.SetChoice(f.ctx, f.actor, "tex-mex-paste", "tex-mex-paste.store"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.SetChoice(f.ctx, f.actor, "southwest-spice-blend", OptionAsIs); err != nil {
		t.Fatal(err)
	}
	specs, err = f.svc.GrocerySpecialties(f.ctx, testHousehold, lines)
	if err != nil {
		t.Fatal(err)
	}
	if c := specs[texID].Choice; c == nil || c.OptionID != "tex-mex-paste.store" || c.Strategy != "" {
		t.Errorf("explicit choice under closest = %+v", c)
	}
	if c := specs[southwestID].Choice; c == nil || c.Type != grocery.ChoiceAsIs {
		t.Errorf("as_is under closest = %+v", c)
	}
}

// TestListReportsChoiceSource covers the reporting the app shows: whether a
// specialty ingredient's plan is a member's decision or the default.
func TestListReportsChoiceSource(t *testing.T) {
	f := newFixture(t)
	if _, err := f.svc.SetStrategy(f.ctx, f.actor, string(StrategySimilar)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.SetChoice(f.ctx, f.actor, "tex-mex-paste", OptionAsIs); err != nil {
		t.Fatal(err)
	}
	byID := func() map[string]View {
		views, err := f.svc.List(f.ctx, testHousehold, false)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]View{}
		for _, v := range views {
			out[v.Specialty.ID] = v
		}
		return out
	}
	views := byID()
	if r := views["tex-mex-paste"].Resolution; r.Source != ChoiceSourceHousehold || !r.AsIs {
		t.Errorf("an explicit as_is = %+v", r)
	}
	if r := views["southwest-spice-blend"].Resolution; r.Source != ChoiceSourceStrategy || r.Strategy != StrategySimilar ||
		r.Option == nil || r.Option.ID != "southwest-spice-blend.store" {
		t.Errorf("a strategy pick = %+v", r)
	}

	if _, err := f.svc.SetStrategy(f.ctx, f.actor, string(StrategyAsk)); err != nil {
		t.Fatal(err)
	}
	views = byID()
	if r := views["tex-mex-paste"].Resolution; r.Source != ChoiceSourceHousehold || !r.AsIs {
		t.Errorf("as_is must survive a strategy change = %+v", r)
	}
	if r := views["southwest-spice-blend"].Resolution; r.Source != ChoiceSourceNone || r.Option != nil {
		t.Errorf("under ask = %+v", r)
	}
}
