package planning

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"
)

const nextWeek = "2026-W39"

// A meal that didn't get made moves to next week with everything about it:
// servings, note, customizations, origin, and its add-on. It keeps its night
// when next week has that night free, and the old week no longer has it
// (decision 645). Nothing is recorded, so Autopilot doesn't read the move as
// turning the meal down.
func TestMoveEntryToWeekBringsTheMealAndItsAddOn(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t, newMemoryStore())
	rec := &fakeRecorder{}
	svc.WithEvents(rec, nil)
	_, soup := mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeSoup, Servings: 2, Day: "thu", Note: "meat in the freezer", Origin: OriginAutopilot, ProposalID: "p1"})
	_, bread := mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeBread, Servings: 2, Day: "thu"})
	_, tacos := mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeTacos, Servings: 2, Day: "fri"})
	custom := []Customization{{IngredientKey: "name:chicken", ChoiceID: "double", Label: "2x Chicken"}}
	if _, err := svc.SetEntryCustomizations(ctx, hhAda, testWeek, soup.ID, custom); err != nil {
		t.Fatal(err)
	}
	before := len(rec.recorded())

	m, err := svc.MoveEntryToWeek(ctx, hhAda, testWeek, soup.ID, nextWeek, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.From.Entries) != 1 || m.From.Entries[0].ID != tacos.ID {
		t.Errorf("old week = %+v, want only the tacos", m.From.Entries)
	}
	if len(m.To.Entries) != 2 {
		t.Fatalf("next week = %+v, want the soup and its bread", m.To.Entries)
	}
	got := m.Entry
	if got.ID == soup.ID || got.RecipeID != recipeSoup || got.Day != Thursday || got.Servings != 2 ||
		got.Note != "meat in the freezer" || got.Origin != OriginAutopilot || got.ProposalID != "p1" ||
		!reflect.DeepEqual(got.Customizations, custom) {
		t.Errorf("moved entry = %+v", got)
	}
	if b := m.To.Entries[1]; b.RecipeID != bread.RecipeID || b.Day != Thursday {
		t.Errorf("add-on = %+v, want it on Thursday with the soup", b)
	}
	if m.To.FirstDay == "" || m.From.FirstDay == "" {
		t.Errorf("first days = %q, %q", m.From.FirstDay, m.To.FirstDay)
	}
	if n := len(rec.recorded()); n != before {
		t.Errorf("recorded %d events for a move", n-before)
	}
}

// A taken night isn't taken from the meal there: the moved meal waits for a
// day instead. A day asked for takes the night (decision 644), and an empty
// one leaves the meal without a day.
func TestMoveEntryToWeekDays(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t, newMemoryStore())
	_, there := mustAdd(t, svc, hhAda, userAda, nextWeek, NewEntry{RecipeID: recipeTacos, Servings: 2, Day: "thu"})
	_, soup := mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeSoup, Servings: 2, Day: "thu"})
	_, salad := mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeSalad, Servings: 2, Day: "mon"})

	m, err := svc.MoveEntryToWeek(ctx, hhAda, testWeek, soup.ID, nextWeek, nil)
	if err != nil {
		t.Fatal(err)
	}
	if m.Entry.Day != "" || dayOf(t, m.To, there.ID) != Thursday {
		t.Errorf("next week = %+v; the soup should wait for a day", m.To.Entries)
	}

	thu := Thursday
	if m, err = svc.MoveEntryToWeek(ctx, hhAda, testWeek, salad.ID, nextWeek, &thu); err != nil {
		t.Fatal(err)
	}
	if m.Entry.Day != Thursday || dayOf(t, m.To, there.ID) != "" {
		t.Errorf("next week = %+v; the salad should take Thursday", m.To.Entries)
	}

	none := Day("")
	if m, err = svc.MoveEntryToWeek(ctx, hhAda, nextWeek, m.Entry.ID, testWeek, &none); err != nil {
		t.Fatal(err)
	}
	if m.Entry.Day != "" {
		t.Errorf("moved back on %q, want no day", m.Entry.Day)
	}
}

func TestMoveEntryToWeekErrors(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	svc, _ := newTestService(t, store)
	_, soup := mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeSoup, Servings: 2, Day: "thu"})

	for name, tc := range map[string]struct {
		week, entry, to string
		day             *Day
		want            error
	}{
		"same week":   {testWeek, soup.ID, testWeek, nil, ErrInvalidEntry},
		"bad week":    {testWeek, soup.ID, "soon", nil, ErrInvalidWeek},
		"bad day":     {testWeek, soup.ID, nextWeek, ptr(Day("someday")), ErrInvalidEntry},
		"no entry":    {testWeek, "66e5a1f2c3b4a5d6e7f8ffff", nextWeek, nil, ErrNotFound},
		"no plan":     {"2026-W30", soup.ID, nextWeek, nil, ErrNotFound},
		"other house": {testWeek, soup.ID, nextWeek, nil, ErrNotFound},
	} {
		t.Run(name, func(t *testing.T) {
			hh := hhAda
			if name == "other house" {
				hh = hhBob
			}
			if _, err := svc.MoveEntryToWeek(ctx, hh, tc.week, tc.entry, tc.to, tc.day); !errors.Is(err, tc.want) {
				t.Errorf("error = %v, want %v", err, tc.want)
			}
		})
	}

	// A finalized week on either side refuses, and nothing changes.
	if _, err := svc.SetStatus(ctx, hhAda, nextWeek, string(StatusFinalized)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.MoveEntryToWeek(ctx, hhAda, testWeek, soup.ID, nextWeek, nil); !errors.Is(err, ErrFinalized) {
		t.Errorf("into a finalized week: error = %v", err)
	}
	if _, err := svc.SetStatus(ctx, hhAda, testWeek, string(StatusFinalized)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.MoveEntryToWeek(ctx, hhAda, testWeek, soup.ID, "2026-W40", nil); !errors.Is(err, ErrFinalized) {
		t.Errorf("out of a finalized week: error = %v", err)
	}
	if p, _ := svc.Get(ctx, hhAda, testWeek); len(p.Entries) != 1 {
		t.Errorf("old week = %+v", p.Entries)
	}
	for _, w := range []string{nextWeek, "2026-W40"} {
		if p, _ := svc.Get(ctx, hhAda, w); len(p.Entries) != 0 {
			t.Errorf("%s = %+v", w, p.Entries)
		}
	}
}

// A full week takes nothing, and the meal stays where it was.
func TestMoveEntryToWeekFull(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t, newMemoryStore())
	_, soup := mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeSoup, Servings: 2})
	for range MaxEntriesPerWeek {
		mustAdd(t, svc, hhAda, userAda, nextWeek, NewEntry{RecipeID: recipeBread, Servings: 2})
	}
	if _, err := svc.MoveEntryToWeek(ctx, hhAda, testWeek, soup.ID, nextWeek, nil); !errors.Is(err, ErrPlanFull) {
		t.Fatalf("error = %v, want ErrPlanFull", err)
	}
	if p, _ := svc.Get(ctx, hhAda, testWeek); len(p.Entries) != 1 {
		t.Errorf("old week = %+v", p.Entries)
	}
}

func TestMoveEntryHandler(t *testing.T) {
	router := newPlanTestRouter(t)
	plan := plansPath(hhAda) + "/" + testWeek
	rec := do(t, router, http.MethodPost, plan+"/entries", `{"recipeId":"`+recipeSoup+`","day":"thu","servings":2}`, userAda)
	wantStatus(t, rec, http.StatusCreated)
	soup := decodeBody[AddEntryResponse](t, rec).Entry
	move := plan + "/entries/" + soup.ID + "/move"

	wantError(t, do(t, router, http.MethodPost, move, `{"week":"`+nextWeek+`"}`, userViewer), http.StatusForbidden, "forbidden")
	wantError(t, do(t, router, http.MethodPost, move, `{}`, userAda), http.StatusBadRequest, "validation_failed")
	wantError(t, do(t, router, http.MethodPost, move, `{"week":"`+nextWeek+`","day":""}`, userAda), http.StatusBadRequest, "validation_failed")
	wantError(t, do(t, router, http.MethodPost, move, `{"week":"`+nextWeek+`","day":4}`, userAda), http.StatusBadRequest, "invalid_request")

	rec = do(t, router, http.MethodPost, move, `{"week":"`+nextWeek+`","day":"sat"}`, userAda)
	wantStatus(t, rec, http.StatusOK)
	got := decodeBody[MoveEntryResponse](t, rec)
	if got.From.Week != testWeek || len(got.From.Entries) != 0 || got.To.Week != nextWeek || len(got.To.Entries) != 1 ||
		got.Entry.Day == nil || *got.Entry.Day != Saturday || got.Entry.Date == nil || *got.Entry.Date != "2026-09-26" {
		t.Errorf("move = %s", rec.Body.String())
	}
	// The old entry is gone.
	wantError(t, do(t, router, http.MethodPost, move, `{"week":"`+nextWeek+`"}`, userAda), http.StatusNotFound, "not_found")

	// null leaves the meal without a day.
	back := plansPath(hhAda) + "/" + nextWeek + "/entries/" + got.Entry.ID + "/move"
	rec = do(t, router, http.MethodPost, back, `{"week":"`+testWeek+`","day":null}`, userAda)
	wantStatus(t, rec, http.StatusOK)
	if e := decodeBody[MoveEntryResponse](t, rec).Entry; e.Day != nil {
		t.Errorf("moved back = %+v", e)
	}
}
