package planning

import (
	"context"
	"testing"
)

func dayOf(t *testing.T, p Plan, id string) Day {
	t.Helper()
	e, ok := p.entry(id)
	if !ok {
		t.Fatalf("entry %s missing", id)
	}
	return e.Day
}

// Moving a meal onto a day that has one puts it there and takes the day from
// the meal that was there, which then waits for a day ("Pick a Day"). Two
// dinners never share a night; an add-on stays with whatever's there
// (decision 644).
func TestMovingAMealOntoATakenDayFreesTheOtherMeal(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestService(t, newMemoryStore())
	_, thursday := mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeSoup, Servings: 2, Day: "thu"})
	_, bread := mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeBread, Servings: 2, Day: "thu"})
	_, saturday := mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeTacos, Servings: 2, Day: "sat"})

	thu := Thursday
	p, err := svc.UpdateEntry(ctx, hhAda, testWeek, saturday.ID, EntryChanges{Day: &thu})
	if err != nil {
		t.Fatal(err)
	}
	if got := dayOf(t, p, saturday.ID); got != Thursday {
		t.Errorf("moved meal is on %q, want thu", got)
	}
	if got := dayOf(t, p, thursday.ID); got != "" {
		t.Errorf("Thursday's first meal is on %q; it should wait for a day", got)
	}
	if got := dayOf(t, p, bread.ID); got != Thursday {
		t.Errorf("the add-on moved to %q; it stays on Thursday", got)
	}

	// Moving to an open day, or unscheduling, touches nothing else.
	fri := Friday
	if p, err = svc.UpdateEntry(ctx, hhAda, testWeek, thursday.ID, EntryChanges{Day: &fri}); err != nil {
		t.Fatal(err)
	}
	if dayOf(t, p, thursday.ID) != Friday || dayOf(t, p, saturday.ID) != Thursday {
		t.Errorf("days = %+v", p.Entries)
	}
}

// Adding a meal on a taken day works the same way; adding an add-on there
// doesn't move the meal.
func TestAddingAMealOnATakenDayFreesTheOtherMeal(t *testing.T) {
	svc, _ := newTestService(t, newMemoryStore())
	_, first := mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeSoup, Servings: 2, Day: "mon"})
	p, _ := mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeBread, Servings: 2, Day: "mon"})
	if got := dayOf(t, p, first.ID); got != Monday {
		t.Fatalf("an add-on took the meal's day: %q", got)
	}
	p, second := mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeTacos, Servings: 2, Day: "mon"})
	if dayOf(t, p, second.ID) != Monday || dayOf(t, p, first.ID) != "" {
		t.Errorf("days = %+v", p.Entries)
	}
	// A meal added without a day leaves the week alone.
	p, _ = mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeSalad, Servings: 2})
	if dayOf(t, p, second.ID) != Monday {
		t.Errorf("an unscheduled meal moved Monday's: %+v", p.Entries)
	}
}

// An add-on stored before add-ons were marked is still recognized by its
// recipe, so it never loses its day to a meal.
func TestAnUnmarkedOldAddOnKeepsItsDay(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	svc, _ := newTestService(t, store)
	p, bread := mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeBread, Servings: 2, Day: "wed"})
	// As stored before the mark existed.
	w := mustWeek(t, testWeek)
	stored := store.plans[planKey(hhAda, w)]
	for i := range stored.Entries {
		stored.Entries[i].RecipeIsAddon = false
	}
	store.plans[planKey(hhAda, w)] = stored
	if len(stored.Entries) != 1 || p.Entries[0].ID != bread.ID {
		t.Fatalf("stored = %+v", stored.Entries)
	}
	_, meal := mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeSoup, Servings: 2, Day: "wed"})
	got, err := svc.Get(ctx, hhAda, testWeek)
	if err != nil {
		t.Fatal(err)
	}
	if dayOf(t, got, bread.ID) != Wednesday || dayOf(t, got, meal.ID) != Wednesday {
		t.Errorf("days = %+v", got.Entries)
	}
}
