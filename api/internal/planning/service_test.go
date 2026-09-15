package planning

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestGetReturnsEmptyDraftForUnplannedWeek(t *testing.T) {
	svc, _ := newTestService(t, newMemoryStore())
	ctx := context.Background()

	p, err := svc.Get(ctx, hhAda, testWeek)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if p.HouseholdID != hhAda || p.Week.String() != testWeek || p.Status != StatusDraft || p.Entries != nil || !p.CreatedAt.IsZero() {
		t.Errorf("empty plan = %+v", p)
	}
	if _, err := svc.Get(ctx, hhAda, "2026-W54"); !errors.Is(err, ErrInvalidWeek) {
		t.Errorf("Get(bad week) error = %v, want ErrInvalidWeek", err)
	}
	if _, err := svc.Get(ctx, "", testWeek); err == nil {
		t.Error("Get(no household) error = nil")
	}
}

func TestAddEntryValidation(t *testing.T) {
	store := newMemoryStore()
	svc, _ := newTestService(t, store)
	ctx := context.Background()

	tests := []struct {
		name string
		week string
		in   NewEntry
		want error
	}{
		{"malformed week", "2026-38", NewEntry{RecipeID: recipeTacos, Servings: 2}, ErrInvalidWeek},
		{"week 53 in a 52-week year", "2025-W53", NewEntry{RecipeID: recipeTacos, Servings: 2}, ErrInvalidWeek},
		{"missing recipe", testWeek, NewEntry{Servings: 2}, ErrInvalidEntry},
		{"unknown day", testWeek, NewEntry{RecipeID: recipeTacos, Servings: 2, Day: "monday"}, ErrInvalidEntry},
		{"note too long", testWeek, NewEntry{RecipeID: recipeTacos, Servings: 2, Note: strings.Repeat("é", MaxNoteLength+1)}, ErrInvalidEntry},
		{"another household's recipe", testWeek, NewEntry{RecipeID: recipeBobs, Servings: 2}, ErrRecipeNotFound},
		{"unknown recipe", testWeek, NewEntry{RecipeID: "66e5a1f2c3b4a5d6e7f8ffff", Servings: 2}, ErrRecipeNotFound},
		{"servings the recipe doesn't offer", testWeek, NewEntry{RecipeID: recipeSalad, Servings: 4}, ErrInvalidEntry},
		{"servings missing", testWeek, NewEntry{RecipeID: recipeTacos}, ErrInvalidEntry},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := svc.AddEntry(ctx, hhAda, userAda, tt.week, tt.in); !errors.Is(err, tt.want) {
				t.Errorf("AddEntry() error = %v, want %v", err, tt.want)
			}
		})
	}
	valid := NewEntry{RecipeID: recipeTacos, Servings: 2}
	if _, _, err := svc.AddEntry(ctx, "", userAda, testWeek, valid); err == nil {
		t.Error("AddEntry(no household) error = nil")
	}
	if _, _, err := svc.AddEntry(ctx, hhAda, "", testWeek, valid); err == nil {
		t.Error("AddEntry(no user) error = nil")
	}
	if len(store.plans) != 0 {
		t.Errorf("rejected entries created plans: %+v", store.plans)
	}
}

func TestEntryLifecycle(t *testing.T) {
	svc, reader := newTestService(t, newMemoryStore())
	ctx := context.Background()

	p, tacos := mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeTacos, Day: "tue", Servings: 2, Note: "  extra lime  "})
	if tacos.ID == "" || tacos.RecipeID != recipeTacos || tacos.RecipeName != "Beef Tacos" || tacos.RecipeImageURL != "https://img.example.com/tacos.jpg" ||
		tacos.Day != Tuesday || tacos.Servings != 2 || tacos.Note != "extra lime" || tacos.AddedBy != userAda || !tacos.AddedAt.Equal(testNow) {
		t.Errorf("added entry = %+v", tacos)
	}
	if p.Status != StatusDraft || !p.CreatedAt.Equal(testNow) || !p.UpdatedAt.Equal(testNow) || len(p.Entries) != 1 {
		t.Errorf("plan after first add = %+v", p)
	}
	longNote := strings.Repeat("é", MaxNoteLength)
	_, soup := mustAdd(t, svc, hhAda, userViewer, testWeek, NewEntry{RecipeID: recipeSoup, Servings: 4, Note: longNote})
	if soup.Day != "" || soup.Note != longNote {
		t.Errorf("unscheduled entry = %+v", soup)
	}

	// The snapshot keeps the name it was added with; the recipe is live elsewhere.
	renamed := tacosRecipe()
	renamed.Name = "Crispy Beef Tacos"
	reader.put(renamed)
	p, err := svc.Get(ctx, hhAda, testWeek)
	if err != nil || len(p.Entries) != 2 || p.Entries[0].ID != tacos.ID || p.Entries[1].ID != soup.ID || p.Entries[0].RecipeName != "Beef Tacos" {
		t.Fatalf("Get() = %+v, %v", p, err)
	}

	p, err = svc.UpdateEntry(ctx, hhAda, testWeek, tacos.ID, EntryChanges{Day: ptr(Day("")), Servings: ptr(4), Note: ptr("")})
	if err != nil {
		t.Fatalf("UpdateEntry() error = %v", err)
	}
	if e := p.Entries[0]; e.Day != "" || e.Servings != 4 || e.Note != "" || e.RecipeID != recipeTacos {
		t.Errorf("updated tacos = %+v", e)
	}
	p, err = svc.UpdateEntry(ctx, hhAda, testWeek, soup.ID, EntryChanges{Day: ptr(Friday)})
	if err != nil || p.Entries[1].Day != Friday || p.Entries[1].Servings != 4 || p.Entries[1].Note != longNote {
		t.Errorf("partial update = %+v, %v", p.Entries[1], err)
	}

	updateErrors := []struct {
		name    string
		week    string
		hh      string
		entryID string
		changes EntryChanges
		want    error
	}{
		{"no changes", testWeek, hhAda, tacos.ID, EntryChanges{}, ErrInvalidEntry},
		{"servings not offered", testWeek, hhAda, tacos.ID, EntryChanges{Servings: ptr(3)}, ErrInvalidEntry},
		{"unknown day", testWeek, hhAda, tacos.ID, EntryChanges{Day: ptr(Day("someday"))}, ErrInvalidEntry},
		{"note too long", testWeek, hhAda, tacos.ID, EntryChanges{Note: ptr(longNote + "!")}, ErrInvalidEntry},
		{"bad week", "2026-W99", hhAda, tacos.ID, EntryChanges{Note: ptr("x")}, ErrInvalidWeek},
		{"unknown entry", testWeek, hhAda, "nope", EntryChanges{Note: ptr("x")}, ErrNotFound},
		{"unknown entry with servings", testWeek, hhAda, "nope", EntryChanges{Servings: ptr(2)}, ErrNotFound},
		{"entry from another week", "2026-W39", hhAda, tacos.ID, EntryChanges{Servings: ptr(2)}, ErrNotFound},
		{"entry from another household", testWeek, hhBob, tacos.ID, EntryChanges{Note: ptr("x")}, ErrNotFound},
	}
	for _, tt := range updateErrors {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := svc.UpdateEntry(ctx, tt.hh, tt.week, tt.entryID, tt.changes); !errors.Is(err, tt.want) {
				t.Errorf("UpdateEntry() error = %v, want %v", err, tt.want)
			}
		})
	}

	// Servings are checked against the live recipe.
	reader.remove(recipeSoup)
	if _, err := svc.UpdateEntry(ctx, hhAda, testWeek, soup.ID, EntryChanges{Servings: ptr(2)}); !errors.Is(err, ErrRecipeNotFound) {
		t.Errorf("UpdateEntry(servings, recipe gone) error = %v, want ErrRecipeNotFound", err)
	}

	p, err = svc.DeleteEntry(ctx, hhAda, testWeek, tacos.ID)
	if err != nil || len(p.Entries) != 1 || p.Entries[0].ID != soup.ID {
		t.Fatalf("DeleteEntry() = %+v, %v", p, err)
	}
	if _, err := svc.DeleteEntry(ctx, hhAda, testWeek, tacos.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("second DeleteEntry() error = %v, want ErrNotFound", err)
	}
	if _, err := svc.DeleteEntry(ctx, hhAda, "2026-W39", soup.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteEntry(other week) error = %v, want ErrNotFound", err)
	}
}

func TestFinalizedPlanLocksEntries(t *testing.T) {
	svc, _ := newTestService(t, newMemoryStore())
	ctx := context.Background()
	_, e := mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeTacos, Servings: 2})

	p, err := svc.SetStatus(ctx, hhAda, testWeek, "finalized")
	if err != nil || p.Status != StatusFinalized || len(p.Entries) != 1 {
		t.Fatalf("SetStatus(finalized) = %+v, %v", p, err)
	}
	if _, _, err := svc.AddEntry(ctx, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeSoup, Servings: 2}); !errors.Is(err, ErrFinalized) {
		t.Errorf("AddEntry(finalized) error = %v, want ErrFinalized", err)
	}
	if _, err := svc.UpdateEntry(ctx, hhAda, testWeek, e.ID, EntryChanges{Note: ptr("x")}); !errors.Is(err, ErrFinalized) {
		t.Errorf("UpdateEntry(finalized) error = %v, want ErrFinalized", err)
	}
	if _, err := svc.UpdateEntry(ctx, hhAda, testWeek, e.ID, EntryChanges{Servings: ptr(4)}); !errors.Is(err, ErrFinalized) {
		t.Errorf("UpdateEntry(finalized, servings) error = %v, want ErrFinalized", err)
	}
	if _, err := svc.DeleteEntry(ctx, hhAda, testWeek, e.ID); !errors.Is(err, ErrFinalized) {
		t.Errorf("DeleteEntry(finalized) error = %v, want ErrFinalized", err)
	}
	if _, err := svc.UpdateEntry(ctx, hhAda, testWeek, "nope", EntryChanges{Note: ptr("x")}); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateEntry(finalized, unknown entry) error = %v, want ErrNotFound", err)
	}
	if _, err := svc.SetStatus(ctx, hhAda, testWeek, "cooked"); !errors.Is(err, ErrInvalidStatus) {
		t.Errorf("SetStatus(cooked) error = %v, want ErrInvalidStatus", err)
	}

	if _, err := svc.SetStatus(ctx, hhAda, testWeek, "draft"); err != nil {
		t.Fatal(err)
	}
	mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeSoup, Servings: 2})

	// Setting the status of an unplanned week creates the plan.
	p, err = svc.SetStatus(ctx, hhAda, "2026-W40", "finalized")
	if err != nil || p.Status != StatusFinalized || !p.CreatedAt.Equal(testNow) || p.Entries != nil {
		t.Errorf("SetStatus(new week) = %+v, %v", p, err)
	}
}

func TestAddEntryLimit(t *testing.T) {
	svc, _ := newTestService(t, newMemoryStore())
	for range MaxEntriesPerWeek {
		mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeTacos, Servings: 2})
	}
	if _, _, err := svc.AddEntry(context.Background(), hhAda, userAda, testWeek, NewEntry{RecipeID: recipeTacos, Servings: 2}); !errors.Is(err, ErrPlanFull) {
		t.Errorf("AddEntry(full) error = %v, want ErrPlanFull", err)
	}
}

func TestListSummaries(t *testing.T) {
	svc, _ := newTestService(t, newMemoryStore())
	ctx := context.Background()
	mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeTacos, Servings: 2})
	mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeSoup, Servings: 2})
	mustAdd(t, svc, hhAda, userAda, "2026-W40", NewEntry{RecipeID: recipeSalad, Servings: 2})
	if _, err := svc.SetStatus(ctx, hhAda, "2026-W40", "finalized"); err != nil {
		t.Fatal(err)
	}
	mustAdd(t, svc, hhBob, userBob, "2026-W39", NewEntry{RecipeID: recipeBobs, Servings: 2})

	items, err := svc.List(ctx, hhAda, "2026-W37", "2026-W40")
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	want := []struct {
		week    string
		status  Status
		count   int
		planned bool
	}{
		{"2026-W37", StatusDraft, 0, false},
		{testWeek, StatusDraft, 2, true},
		{"2026-W39", StatusDraft, 0, false}, // Bob's plan is not Ada's
		{"2026-W40", StatusFinalized, 1, true},
	}
	if len(items) != len(want) {
		t.Fatalf("List() = %+v, want %d items", items, len(want))
	}
	for i, w := range want {
		got := items[i]
		if got.Week.String() != w.week || got.Status != w.status || got.EntryCount != w.count || got.UpdatedAt.IsZero() == w.planned {
			t.Errorf("item %d = %+v, want %+v", i, got, w)
		}
	}

	weeks := func(items []Summary) []string {
		var out []string
		for _, s := range items {
			out = append(out, s.Week.String())
		}
		return out
	}
	if items, err := svc.List(ctx, hhAda, "2026-W52", "2027-W02"); err != nil || strings.Join(weeks(items), ",") != "2026-W52,2026-W53,2027-W01,2027-W02" {
		t.Errorf("List(year boundary) = %v, %v", weeks(items), err)
	}
	if items, err := svc.List(ctx, hhAda, testWeek, testWeek); err != nil || len(items) != 1 {
		t.Errorf("List(one week) = %v, %v", items, err)
	}
	for _, r := range [][2]string{{"2026-W01", "2026-W26"}, {"2026-W40", "2027-W12"}} {
		if items, err := svc.List(ctx, hhAda, r[0], r[1]); err != nil || len(items) != MaxRangeWeeks {
			t.Errorf("List(%s..%s) = %d items, %v; want %d", r[0], r[1], len(items), err, MaxRangeWeeks)
		}
	}

	rangeErrors := []struct {
		from, to string
		want     error
	}{
		{"", "2026-W40", ErrInvalidRange},
		{"2026-W38", "", ErrInvalidRange},
		{"2026-W40", "2026-W38", ErrInvalidRange},
		{"2026-W01", "2026-W27", ErrInvalidRange},
		{"2026-W40", "2027-W13", ErrInvalidRange},
		{"2026-W40", "2026-W99", ErrInvalidWeek},
		{"soon", "2026-W40", ErrInvalidWeek},
	}
	for _, tt := range rangeErrors {
		if _, err := svc.List(ctx, hhAda, tt.from, tt.to); !errors.Is(err, tt.want) {
			t.Errorf("List(%q, %q) error = %v, want %v", tt.from, tt.to, err, tt.want)
		}
	}
}
