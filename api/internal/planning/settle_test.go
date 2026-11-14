package planning

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

// eventLog records events and lists them back, like events.Service.
type eventLog struct {
	mu   sync.Mutex
	list []events.Event
}

func (l *eventLog) Record(_ context.Context, e events.Event) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if e.OccurredAt.IsZero() {
		e.OccurredAt = testNow
	}
	l.list = append(l.list, e)
	return nil
}

func (l *eventLog) List(_ context.Context, q events.Query) ([]events.Event, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []events.Event
	for _, e := range l.list {
		if e.HouseholdID == q.HouseholdID && (len(q.Types) == 0 || slices.Contains(q.Types, e.Type)) && !e.OccurredAt.Before(q.Since) {
			out = append(out, e)
		}
	}
	slices.SortStableFunc(out, func(a, b events.Event) int { return a.OccurredAt.Compare(b.OccurredAt) })
	if q.Newest {
		slices.Reverse(out)
	}
	return out, nil
}

func (l *eventLog) cooks() []events.Event {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []events.Event
	for _, e := range l.list {
		if e.Type == events.TypeRecipeCooked {
			out = append(out, e)
		}
	}
	return out
}

func (l *eventLog) answer(entryID string, cooked bool, at time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e := events.Event{HouseholdID: hhAda, RecipeID: recipeTacos, OccurredAt: at, Type: events.TypeRecipeSkipped,
		Payload: events.RecipeSkipped{EntryID: entryID}}
	if cooked {
		e.Type, e.Payload = events.TypeRecipeCooked, events.RecipeCooked{EntryID: entryID, Servings: 2}
	}
	l.list = append(l.list, e)
}

// settleNow is a Tuesday in 2026-W47: W45 and W46 have ended.
var settleNow = time.Date(2026, 11, 17, 15, 0, 0, 0, time.UTC)

func settleService(t *testing.T) (*Service, *eventLog) {
	t.Helper()
	svc, _ := newTestService(t, newMemoryStore())
	log := &eventLog{}
	svc.WithEvents(log, nil).WithOutcomes(log)
	svc.now = func() time.Time { return settleNow }
	return svc, log
}

// A week that ended with meals nobody answered for counts them as made
// (decision 662): one implicit cook each, on its night, never twice, and
// never for a meal someone answered or a week before the change.
func TestSettleEndedWeeksAssumesUnansweredMealsWereMade(t *testing.T) {
	ctx := context.Background()
	svc, log := settleService(t)
	_, tacos := mustAdd(t, svc, hhAda, userAda, "2026-W46", NewEntry{RecipeID: recipeTacos, Day: "tue", Servings: 2})
	_, soup := mustAdd(t, svc, hhAda, userAda, "2026-W46", NewEntry{RecipeID: recipeSoup, Day: "wed", Servings: 4})
	_, salad := mustAdd(t, svc, hhAda, userAda, "2026-W46", NewEntry{RecipeID: recipeSalad, Day: "thu", Servings: 2})
	_, later := mustAdd(t, svc, hhAda, userAda, "2026-W46", NewEntry{RecipeID: recipeTacos, Servings: 2}) // no day
	_, early := mustAdd(t, svc, hhAda, userAda, "2026-W44", NewEntry{RecipeID: recipeTacos, Day: "mon", Servings: 2})
	_, thisWeek := mustAdd(t, svc, hhAda, userAda, "2026-W47", NewEntry{RecipeID: recipeTacos, Day: "mon", Servings: 2})
	log.answer(soup.ID, false, time.Date(2026, 11, 11, 20, 0, 0, 0, time.UTC))
	log.answer(salad.ID, true, time.Date(2026, 11, 12, 20, 0, 0, 0, time.UTC))

	if err := svc.SettleEndedWeeks(ctx, hhAda); err != nil {
		t.Fatal(err)
	}
	byEntry := map[string]events.Event{}
	for _, e := range log.cooks() {
		if p := e.Payload.(events.RecipeCooked); p.Implicit {
			byEntry[p.EntryID] = e
		}
	}
	if len(byEntry) != 2 {
		t.Fatalf("implicit cooks = %+v, want tacos and the unscheduled meal", byEntry)
	}
	e := byEntry[tacos.ID]
	p := e.Payload.(events.RecipeCooked)
	if e.RecipeID != recipeTacos || e.Week != "2026-W46" || p.Date != "2026-11-10" || p.Servings != 2 ||
		!e.OccurredAt.Equal(time.Date(2026, 11, 10, 19, 0, 0, 0, time.UTC)) || e.UserID != "" ||
		e.ClientEventID != "implicit-cook-"+tacos.ID {
		t.Errorf("tacos implicit cook = %+v", e)
	}
	// An unscheduled meal is counted on the week's last day.
	if p := byEntry[later.ID].Payload.(events.RecipeCooked); p.Date != "2026-11-15" {
		t.Errorf("unscheduled meal date = %s, want 2026-11-15", p.Date)
	}
	for _, id := range []string{soup.ID, salad.ID, early.ID, thisWeek.ID} {
		if _, ok := byEntry[id]; ok {
			t.Errorf("entry %s was assumed made", id)
		}
	}

	// Again, in this process or another: nothing new.
	before := len(log.cooks())
	if err := svc.SettleEndedWeeks(ctx, hhAda); err != nil {
		t.Fatal(err)
	}
	other := NewService(svc.store, svc.recipes)
	other.WithEvents(log, nil).WithOutcomes(log)
	other.now = svc.now
	if err := other.SettleEndedWeeks(ctx, hhAda); err != nil {
		t.Fatal(err)
	}
	if got := len(log.cooks()); got != before {
		t.Errorf("settling again added %d cooks", got-before)
	}
}

// Without events nothing is assumed: an older setup keeps working.
func TestSettleEndedWeeksNeedsEvents(t *testing.T) {
	svc, _ := newTestService(t, newMemoryStore())
	svc.now = func() time.Time { return settleNow }
	mustAdd(t, svc, hhAda, userAda, "2026-W46", NewEntry{RecipeID: recipeTacos, Day: "tue", Servings: 2})
	if err := svc.SettleEndedWeeks(context.Background(), hhAda); err != nil {
		t.Fatal(err)
	}
}

// Next week's list counts on this week's meals being made: the sour cream
// this week's tacos will use isn't there for next week's (decision 662).
func TestGroceryListCountsThisWeeksMealsAsMade(t *testing.T) {
	ctx := context.Background()
	svc, log := settleService(t)
	tbsp, err := ingredients.LookupUnit("tbsp")
	if err != nil {
		t.Fatal(err)
	}
	svc.WithPantry(&fakePantry{stock: grocery.PantryStock{
		InStock: map[string]bool{ingSourCream: true}, OutOfStock: map[string]bool{},
		Amounts: map[string]grocery.Amount{ingSourCream: {Quantity: ingredients.NewQuantity(3, 1), Unit: tbsp}},
	}})
	_, thisWeek := mustAdd(t, svc, hhAda, userAda, "2026-W47", NewEntry{RecipeID: recipeTacos, Day: "thu", Servings: 2})
	mustAdd(t, svc, hhAda, userAda, "2026-W48", NewEntry{RecipeID: recipeTacos, Day: "tue", Servings: 2})

	sourCream := func(week string) grocery.Item {
		t.Helper()
		g, err := svc.GroceryList(ctx, hhAda, week)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range g.Categories {
			for _, it := range c.Items {
				if it.Name == "Sour Cream" {
					return it
				}
			}
		}
		t.Fatalf("no sour cream on %s", week)
		return grocery.Item{}
	}
	// This week's list: 3 Tbsp at home covers its 2.
	if it := sourCream("2026-W47"); it.Status != grocery.StatusInPantry {
		t.Errorf("this week's sour cream = %s, want inPantry", it.Status)
	}
	// Next week: this week's tacos leave 1 Tbsp, so 1 more is bought.
	it := sourCream("2026-W48")
	if it.Status != grocery.StatusToBuy || it.OnHand == nil || !it.OnHand.Quantity.Equal(ingredients.NewQuantity(1, 1)) {
		t.Errorf("next week's sour cream = %+v, want 1 Tbsp on hand", it)
	}
	// Didn't make it: the tacos leave the sour cream for next week.
	log.answer(thisWeek.ID, false, settleNow)
	if it := sourCream("2026-W48"); it.Status != grocery.StatusInPantry {
		t.Errorf("after didn't make it = %s, want inPantry", it.Status)
	}
	// Made it after all: already taken out of the pantry when marked, so not
	// counted a second time here.
	log.answer(thisWeek.ID, true, settleNow.Add(time.Hour))
	if it := sourCream("2026-W48"); it.Status != grocery.StatusInPantry {
		t.Errorf("after made it = %s, want inPantry", it.Status)
	}
}

// A household that stayed away for weeks still has those meals counted as
// made when it comes back, so its pantry and grocery list don't keep their
// ingredients on hand.
func TestSettleEndedWeeksCatchesUpAfterWeeksAway(t *testing.T) {
	ctx := context.Background()
	svc, log := settleService(t)
	_, old := mustAdd(t, svc, hhAda, userAda, "2026-W46", NewEntry{RecipeID: recipeTacos, Day: "tue", Servings: 2})
	_, skipped := mustAdd(t, svc, hhAda, userAda, "2026-W47", NewEntry{RecipeID: recipeSoup, Day: "wed", Servings: 2})
	log.answer(skipped.ID, false, time.Date(2026, 11, 18, 20, 0, 0, 0, time.UTC))
	// Five weeks later (2026-W51).
	svc.now = func() time.Time { return time.Date(2026, 12, 15, 15, 0, 0, 0, time.UTC) }
	if err := svc.SettleEndedWeeks(ctx, hhAda); err != nil {
		t.Fatal(err)
	}
	var implicit []string
	for _, e := range log.cooks() {
		if p := e.Payload.(events.RecipeCooked); p.Implicit {
			implicit = append(implicit, p.EntryID)
		}
	}
	if len(implicit) != 1 || implicit[0] != old.ID {
		t.Errorf("implicit cooks = %v, want only the unanswered meal from five weeks ago", implicit)
	}
}
