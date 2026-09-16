package substitutes

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

// Synthetic IDs, valid ObjectID hex so the fixtures work against MongoDB.
const (
	testHousehold  = "66e5a1f2c3b4a5d6e7f80a01"
	otherHousehold = "66e5a1f2c3b4a5d6e7f80b01"
	testUser       = "66e5a1f2c3b4a5d6e7f80c01"
)

var testNow = time.Date(2026, 9, 15, 18, 30, 0, 0, time.UTC)

func testSpecialty(name string, options ...Option) Specialty {
	sp := Specialty{ID: Slug(name), Name: name, Category: "spices", DefaultOptionID: Slug(name) + ".store"}
	sp.Key = ingredients.NormalizeName(name)
	sp.UnitSizes = []UnitSize{{Per: "count", Quantity: "1", Unit: "tbsp"}}
	if len(options) == 0 {
		options = []Option{{
			ID: sp.ID + ".store", SpecialtyID: sp.ID, Source: SourceCurated, Type: TypeStoreAlternative, Name: "Store mix",
			Per: &Measure{Quantity: "1", Unit: "tbsp"}, Ingredients: []Component{{Name: "Chili Powder", Quantity: "3/2", Unit: "tsp"}},
		}}
	}
	sp.Options = options
	sp.ContentHash = contentHash(sp)
	sp.CreatedAt, sp.UpdatedAt = testNow, testNow
	return sp
}

// runStoreContract checks behavior both stores must share.
func runStoreContract(t *testing.T, store Store) {
	t.Helper()
	ctx := context.Background()

	// --- Specialties.
	blend := testSpecialty("Southwest Spice Blend")
	blend.Aliases, blend.AliasKeys = []string{"Southwestern Spice Blend"}, []string{"southwestern spice blend"}
	blend.Options = append(blend.Options, Option{
		ID: "southwest-spice-blend.batch", SpecialtyID: blend.ID, Source: SourceCurated, Type: TypeHouseMadeBatch, Name: "House blend",
		Notes: "Keep dry.", Ingredients: []Component{{Name: "Chili Powder", Quantity: "3", Unit: "tbsp"}, {Name: "Salt", Category: "spices"}},
		Steps: []string{"Mix.", "Store."}, Yield: &Measure{Quantity: "12", Unit: "tbsp"}, ShelfLifeDays: 180,
	})
	paste := testSpecialty("Tex-Mex Paste")
	for _, sp := range []Specialty{paste, blend} {
		if err := store.UpsertSpecialty(ctx, sp); err != nil {
			t.Fatalf("UpsertSpecialty(%s) error = %v", sp.ID, err)
		}
	}
	list, err := store.ListSpecialties(ctx, false)
	if err != nil || len(list) != 2 || list[0].ID != blend.ID || list[1].ID != paste.ID {
		t.Fatalf("ListSpecialties() = %+v, %v", list, err)
	}
	if !reflect.DeepEqual(list[0], blend) {
		t.Errorf("stored specialty =\n%+v\nwant\n%+v", list[0], blend)
	}
	// A later upsert keeps CreatedAt.
	renamed := blend
	renamed.Name, renamed.UpdatedAt, renamed.CreatedAt = "Southwest Blend", testNow.Add(time.Hour), testNow.Add(time.Hour)
	if err := store.UpsertSpecialty(ctx, renamed); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetSpecialty(ctx, blend.ID)
	if err != nil || got.Name != "Southwest Blend" || !got.CreatedAt.Equal(testNow) || !got.UpdatedAt.Equal(testNow.Add(time.Hour)) {
		t.Errorf("after rename = %+v, %v", got, err)
	}
	if _, err := store.GetSpecialty(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetSpecialty(missing) error = %v", err)
	}
	if err := store.RetireSpecialties(ctx, []string{paste.ID}, testNow.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if active, _ := store.ListSpecialties(ctx, false); len(active) != 1 || active[0].ID != blend.ID {
		t.Errorf("active after retiring = %+v", active)
	}
	if all, _ := store.ListSpecialties(ctx, true); len(all) != 2 || !all[1].Retired {
		t.Errorf("all after retiring = %+v", all)
	}
	if err := store.UpsertSpecialty(ctx, paste); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.GetSpecialty(ctx, paste.ID); got.Retired {
		t.Error("an upsert must un-retire")
	}

	// --- Household options.
	o := Option{
		HouseholdID: testHousehold, SpecialtyID: blend.ID, Type: TypeHouseMadeBatch, Name: "Mild blend", Notes: "No cayenne.",
		Ingredients:   []Component{{Name: "Chili Powder", Quantity: "2", Unit: "tbsp"}},
		Steps:         []string{"Mix."},
		Yield:         &Measure{Quantity: "8", Unit: "tbsp"},
		ShelfLifeDays: 90, BasedOnOptionID: "southwest-spice-blend.batch",
		CreatedBy: testUser, UpdatedBy: testUser, CreatedAt: testNow, UpdatedAt: testNow,
	}
	saved, err := store.InsertOption(ctx, o)
	if err != nil || saved.ID == "" || saved.Source != SourceHousehold {
		t.Fatalf("InsertOption() = %+v, %v", saved, err)
	}
	o.ID, o.Source = saved.ID, SourceHousehold
	if !reflect.DeepEqual(saved, o) {
		t.Errorf("inserted =\n%+v\nwant\n%+v", saved, o)
	}
	second := o
	second.ID, second.Name, second.CreatedAt = "", "Second", testNow.Add(time.Minute)
	second, _ = store.InsertOption(ctx, second)
	other := o
	other.ID, other.HouseholdID = "", otherHousehold
	if _, err := store.InsertOption(ctx, other); err != nil {
		t.Fatal(err)
	}
	if got, err := store.GetOption(ctx, testHousehold, o.ID); err != nil || !reflect.DeepEqual(got, o) {
		t.Errorf("GetOption() = %+v, %v", got, err)
	}
	for _, tc := range []struct{ hh, id string }{{otherHousehold, o.ID}, {testHousehold, "not-an-id"}, {testHousehold, "66e5a1f2c3b4a5d6e7f8ffff"}} {
		if _, err := store.GetOption(ctx, tc.hh, tc.id); !errors.Is(err, ErrNotFound) {
			t.Errorf("GetOption(%s, %s) error = %v", tc.hh, tc.id, err)
		}
	}
	if opts, err := store.ListOptions(ctx, testHousehold, blend.ID); err != nil || len(opts) != 2 || opts[0].ID != o.ID || opts[1].ID != second.ID {
		t.Errorf("ListOptions() = %+v, %v", opts, err)
	}
	if opts, _ := store.ListOptions(ctx, testHousehold, paste.ID); len(opts) != 0 {
		t.Errorf("ListOptions(other specialty) = %+v", opts)
	}
	if n, err := store.CountOptions(ctx, testHousehold, blend.ID); err != nil || n != 2 {
		t.Errorf("CountOptions() = %d, %v", n, err)
	}
	// Replace switches type, clears batch fields and notes, and keeps authorship.
	replaced := Option{
		ID: o.ID, HouseholdID: testHousehold, SpecialtyID: "ignored", Type: TypeStoreAlternative, Name: "Rack mix",
		Per: &Measure{Quantity: "1", Unit: "tbsp"}, Ingredients: []Component{{Name: "Paprika", Quantity: "1", Unit: "tsp"}},
		CreatedBy: "ignored", UpdatedBy: testUser, UpdatedAt: testNow.Add(time.Hour),
	}
	got2, err := store.ReplaceOption(ctx, replaced)
	want := replaced
	want.SpecialtyID, want.Source, want.CreatedBy, want.CreatedAt = blend.ID, SourceHousehold, testUser, testNow
	if err != nil || !reflect.DeepEqual(got2, want) {
		t.Errorf("ReplaceOption() =\n%+v, %v\nwant\n%+v", got2, err, want)
	}
	if again, _ := store.GetOption(ctx, testHousehold, o.ID); !reflect.DeepEqual(again, want) {
		t.Errorf("after replace = %+v", again)
	}
	wrongHH := replaced
	wrongHH.HouseholdID = otherHousehold
	if _, err := store.ReplaceOption(ctx, wrongHH); !errors.Is(err, ErrNotFound) {
		t.Errorf("ReplaceOption(other household) error = %v", err)
	}
	if err := store.DeleteOption(ctx, testHousehold, second.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteOption(ctx, testHousehold, second.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second DeleteOption() error = %v", err)
	}

	// --- Choices.
	c := Choice{HouseholdID: testHousehold, SpecialtyID: blend.ID, OptionID: o.ID, ChosenBy: testUser, ChosenAt: testNow}
	if got, err := store.PutChoice(ctx, c); err != nil || got != c {
		t.Fatalf("PutChoice() = %+v, %v", got, err)
	}
	c.OptionID, c.ChosenAt = OptionAsIs, testNow.Add(time.Minute)
	if got, err := store.PutChoice(ctx, c); err != nil || got != c {
		t.Errorf("replacing PutChoice() = %+v, %v", got, err)
	}
	if ok, err := store.InsertChoice(ctx, Choice{HouseholdID: testHousehold, SpecialtyID: blend.ID, OptionID: "x", ChosenBy: testUser, ChosenAt: testNow}); err != nil || ok {
		t.Errorf("InsertChoice(existing) = %v, %v", ok, err)
	}
	pasteChoice := Choice{HouseholdID: testHousehold, SpecialtyID: paste.ID, OptionID: o.ID, ChosenBy: testUser, ChosenAt: testNow}
	if ok, err := store.InsertChoice(ctx, pasteChoice); err != nil || !ok {
		t.Errorf("InsertChoice(new) = %v, %v", ok, err)
	}
	if _, err := store.PutChoice(ctx, Choice{HouseholdID: otherHousehold, SpecialtyID: blend.ID, OptionID: o.ID, ChosenBy: testUser, ChosenAt: testNow}); err != nil {
		t.Fatal(err)
	}
	choices, err := store.ListChoices(ctx, testHousehold)
	if err != nil || !slices.Equal(choices, []Choice{c, pasteChoice}) {
		t.Errorf("ListChoices() = %+v, %v", choices, err)
	}
	if err := store.DeleteChoicesForOption(ctx, testHousehold, o.ID); err != nil {
		t.Fatal(err)
	}
	if choices, _ := store.ListChoices(ctx, testHousehold); !slices.Equal(choices, []Choice{c}) {
		t.Errorf("after DeleteChoicesForOption = %+v", choices)
	}
	for range 2 {
		if err := store.DeleteChoice(ctx, testHousehold, blend.ID); err != nil {
			t.Errorf("DeleteChoice() error = %v", err)
		}
	}
	if choices, _ := store.ListChoices(ctx, testHousehold); len(choices) != 0 {
		t.Errorf("after DeleteChoice = %+v", choices)
	}
	if choices, _ := store.ListChoices(ctx, otherHousehold); len(choices) != 1 {
		t.Errorf("other household's choices = %+v", choices)
	}

	// --- Settings.
	if _, err := store.GetSettings(ctx, testHousehold); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetSettings() before any error = %v, want ErrNotFound", err)
	}
	set := Settings{HouseholdID: testHousehold, Strategy: StrategySimilar, UpdatedBy: testUser, UpdatedAt: testNow}
	if got, err := store.PutSettings(ctx, set); err != nil || got != set {
		t.Fatalf("PutSettings() = %+v, %v", got, err)
	}
	set.Strategy, set.UpdatedAt = StrategyClosest, testNow.Add(time.Minute)
	if got, err := store.PutSettings(ctx, set); err != nil || got != set {
		t.Errorf("replacing PutSettings() = %+v, %v", got, err)
	}
	if got, err := store.GetSettings(ctx, testHousehold); err != nil || got != set {
		t.Errorf("GetSettings() = %+v, %v", got, err)
	}
	if _, err := store.GetSettings(ctx, otherHousehold); !errors.Is(err, ErrNotFound) {
		t.Errorf("another household's settings error = %v, want ErrNotFound", err)
	}
}

// runSeedSync checks that syncing the embedded seed is idempotent, that a
// dry run writes nothing, and that removed specialties are retired.
func runSeedSync(t *testing.T, store Store, upserts func() int) {
	t.Helper()
	ctx := context.Background()
	seed, err := LoadSeed()
	if err != nil {
		t.Fatal(err)
	}
	n := len(seed.Specialties)

	dry, err := SyncSeed(ctx, store, seed, false, testNow)
	if err != nil || len(dry.Created) != n {
		t.Fatalf("dry run = %+v, %v", dry, err)
	}
	if list, _ := store.ListSpecialties(ctx, true); len(list) != 0 {
		t.Fatalf("a dry run wrote %d specialties", len(list))
	}
	res, err := SyncSeed(ctx, store, seed, true, testNow)
	if err != nil || len(res.Created) != n || len(res.Updated)+len(res.Unchanged)+len(res.Retired) != 0 {
		t.Fatalf("first sync = %+v, %v", res, err)
	}
	stored, _ := store.ListSpecialties(ctx, false)
	if len(stored) != n {
		t.Fatalf("stored %d specialties, want %d", len(stored), n)
	}
	for _, sp := range seed.Specialties {
		got, err := store.GetSpecialty(ctx, sp.ID)
		want := sp
		want.CreatedAt, want.UpdatedAt = testNow, testNow
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("stored %s =\n%+v\nwant\n%+v (%v)", sp.ID, got, want, err)
		}
	}
	before := upserts()
	again, err := SyncSeed(ctx, store, seed, true, testNow.Add(time.Hour))
	if err != nil || len(again.Unchanged) != n || len(again.Created)+len(again.Updated)+len(again.Retired) != 0 || upserts() != before {
		t.Fatalf("second sync = %+v, %v, %d writes", again, err, upserts()-before)
	}
	if got, _ := store.GetSpecialty(ctx, seed.Specialties[0].ID); !got.UpdatedAt.Equal(testNow) {
		t.Errorf("an unchanged sync touched updatedAt: %v", got.UpdatedAt)
	}

	// A new seed changes one specialty's content and drops the last one.
	changed := Seed{Version: seed.Version + 1, Specialties: slices.Clone(seed.Specialties[:n-1])}
	first := cloneSpecialty(changed.Specialties[0])
	first.Options[0].Notes = "A clearer note."
	first.ContentHash = contentHash(first)
	changed.Specialties[0] = first
	dropped := seed.Specialties[n-1].ID
	res, err = SyncSeed(ctx, store, changed, true, testNow.Add(2*time.Hour))
	if err != nil || !slices.Equal(res.Updated, []string{first.ID}) || !slices.Equal(res.Retired, []string{dropped}) || len(res.Unchanged) != n-2 {
		t.Fatalf("changed sync = %+v, %v", res, err)
	}
	if got, _ := store.GetSpecialty(ctx, dropped); !got.Retired {
		t.Errorf("%s isn't retired", dropped)
	}
	if got, _ := store.GetSpecialty(ctx, first.ID); got.Options[0].Notes != "A clearer note." || got.SeedVersion != seed.Version+1 || !got.CreatedAt.Equal(testNow) {
		t.Errorf("updated %s = %+v", first.ID, got)
	}
	// Syncing the original seed again restores both.
	res, err = SyncSeed(ctx, store, seed, true, testNow.Add(3*time.Hour))
	if err != nil || len(res.Updated) != 2 || len(res.Retired) != 0 {
		t.Errorf("restoring sync = %+v, %v", res, err)
	}
	if active, _ := store.ListSpecialties(ctx, false); len(active) != n {
		t.Errorf("active after restoring = %d, want %d", len(active), n)
	}
}

func TestMemoryStoreContract(t *testing.T) {
	runStoreContract(t, newMemoryStore())
}

func TestSeedSyncMemory(t *testing.T) {
	store := newMemoryStore()
	runSeedSync(t, store, func() int { return store.upserts })
}
