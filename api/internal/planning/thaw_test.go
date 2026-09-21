package planning

import (
	"context"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/notifications"
	"github.com/Linesmerrill/DinnerOS/api/internal/pantry"
)

// testNow is 2026-09-15 18:30 UTC, which in Denver is 12:30 on Tuesday the
// 15th — inside week 2026-W38 for a Monday-start household.
const thawDate = "2026-09-15"

type fakeHouseholds struct{ hh households.Household }

func (f fakeHouseholds) GetHousehold(context.Context, string) (households.Household, error) {
	return f.hh, nil
}

type fakeFreezer struct{ items []pantry.FrozenItem }

func (f fakeFreezer) FrozenStock(context.Context, string) ([]pantry.FrozenItem, error) {
	return f.items, nil
}

type fakeThawNotifier struct{ created []notifications.New }

func (f *fakeThawNotifier) Create(_ context.Context, in notifications.New) (notifications.Notification, bool, error) {
	f.created = append(f.created, in)
	return notifications.Notification{ID: "n" + in.DedupeKey, HouseholdID: in.HouseholdID}, true, nil
}

func frozenChicken() pantry.FrozenItem {
	item := pantry.Item{
		ID: "66e5a1f2c3b4a5d6e7f84001", HouseholdID: hhAda, IngredientID: ingChicken,
		DisplayName: "Chicken Thighs", Category: "meat-seafood",
		Quantity: "32", Unit: "oz", Status: pantry.StatusInStock, Storage: pantry.StorageFreezer, Portions: 2,
	}
	return pantry.FrozenItem{Item: item, Keys: []string{ingChicken}, Thaw: pantry.ThawFor(item)}
}

func thawService(t *testing.T, hour *int, stock ...pantry.FrozenItem) (*Service, *fakeThawNotifier) {
	t.Helper()
	svc, reader := newTestService(t, newMemoryStore())
	reader.put(newChickenRecipe())
	notifier := &fakeThawNotifier{}
	hh := households.Household{
		ID: hhAda, Name: "Ada's", TimeZone: "America/Denver", WeekStartsOn: "mon", ThawReminderHour: hour,
	}
	svc.WithFreezer(fakeFreezer{items: stock}, fakeHouseholds{hh: hh}, notifier)
	return svc, notifier
}

func TestThawDueFindsTodaysFrozenIngredient(t *testing.T) {
	ctx := context.Background()
	svc, _ := thawService(t, nil, frozenChicken())
	// Tuesday is today in Denver.
	mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeChicken, Day: "tue", Servings: 2})

	due, err := svc.ThawDue(ctx, hhAda)
	if err != nil {
		t.Fatalf("ThawDue: %v", err)
	}
	if due.Date != thawDate {
		t.Fatalf("Date = %q, want %q", due.Date, thawDate)
	}
	if due.Hour != households.DefaultThawReminderHour {
		t.Errorf("Hour = %d, want the default %d", due.Hour, households.DefaultThawReminderHour)
	}
	if len(due.Items) != 1 {
		t.Fatalf("%d items, want the chicken", len(due.Items))
	}
	item := due.Items[0]
	// 32 oz in two portions is a pound a portion: five hours in the fridge.
	if item.Hours != 5 || !item.Measured {
		t.Errorf("thaw = %d hours (measured %v), want 5", item.Hours, item.Measured)
	}
	if item.MoveBy != "13:00" {
		t.Errorf("MoveBy = %q, want 13:00: five hours before the %d:00 dinner", item.MoveBy, DinnerHour)
	}
	if item.Overnight {
		t.Error("Overnight = true, want false: it is 12:30 and the deadline is 13:00")
	}
	if len(item.Recipes) != 1 || item.Recipes[0] != "Roast Chicken Thighs" {
		t.Errorf("Recipes = %v, want the meal that needs it", item.Recipes)
	}
	if item.Summary == "" {
		t.Error("Summary is empty; it is the notification body")
	}
}

// A meal on another day isn't today's problem.
func TestThawDueIgnoresOtherDays(t *testing.T) {
	svc, _ := thawService(t, nil, frozenChicken())
	mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeChicken, Day: "fri", Servings: 2})
	due, err := svc.ThawDue(context.Background(), hhAda)
	if err != nil {
		t.Fatalf("ThawDue: %v", err)
	}
	if len(due.Items) != 0 {
		t.Errorf("%d items, want none: Friday's meal is not today's errand", len(due.Items))
	}
}

// Nothing in the freezer means nothing to take out, whatever is planned.
func TestThawDueWithAnEmptyFreezer(t *testing.T) {
	svc, _ := thawService(t, nil)
	mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeChicken, Day: "tue", Servings: 2})
	due, err := svc.ThawDue(context.Background(), hhAda)
	if err != nil {
		t.Fatalf("ThawDue: %v", err)
	}
	if len(due.Items) != 0 {
		t.Errorf("%d items, want none", len(due.Items))
	}
}

func TestRefreshWaitsForTheHouseholdsHour(t *testing.T) {
	ctx := context.Background()
	// 22:00 local is after 12:30, so the reminder hasn't come round yet.
	svc, notifier := thawService(t, ptr(22), frozenChicken())
	mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeChicken, Day: "tue", Servings: 2})
	if err := svc.Refresh(ctx, hhAda); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if len(notifier.created) != 0 {
		t.Fatalf("%d notifications before the household's hour, want none", len(notifier.created))
	}

	// 6am has long passed by 12:30, so the reminder is due.
	svc, notifier = thawService(t, ptr(6), frozenChicken())
	mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeChicken, Day: "tue", Servings: 2})
	if err := svc.Refresh(ctx, hhAda); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if len(notifier.created) != 1 {
		t.Fatalf("%d notifications, want 1", len(notifier.created))
	}
	n := notifier.created[0]
	switch {
	case n.Type != notifications.TypePantryThaw:
		t.Errorf("Type = %q, want %q", n.Type, notifications.TypePantryThaw)
	case n.Subject.Kind != notifications.SubjectPantryItem:
		t.Errorf("Subject.Kind = %q, want %q", n.Subject.Kind, notifications.SubjectPantryItem)
	case n.DedupeKey != "pantry.thaw:"+thawDate+":"+frozenChicken().Item.ID:
		t.Errorf("DedupeKey = %q, want one per item per day", n.DedupeKey)
	}
}

func TestStillRelevantOnlyJudgesThawReminders(t *testing.T) {
	ctx := context.Background()
	svc, _ := thawService(t, ptr(6), frozenChicken())
	mustAdd(t, svc, hhAda, userAda, testWeek, NewEntry{RecipeID: recipeChicken, Day: "tue", Servings: 2})

	other := notifications.Notification{HouseholdID: hhAda, Type: notifications.TypeShoppingOrderDue}
	if ok, err := svc.StillRelevant(ctx, other); err != nil || !ok {
		t.Errorf("StillRelevant(order due) = %v, %v; other types are not the planner's to judge", ok, err)
	}
	mine := notifications.Notification{
		HouseholdID: hhAda, Type: notifications.TypePantryThaw,
		Subject: notifications.Subject{Kind: notifications.SubjectPantryItem, ID: frozenChicken().Item.ID},
	}
	if ok, err := svc.StillRelevant(ctx, mine); err != nil || !ok {
		t.Errorf("StillRelevant(today's item) = %v, %v; want true", ok, err)
	}
	gone := mine
	gone.Subject.ID = "66e5a1f2c3b4a5d6e7f84099"
	if ok, err := svc.StillRelevant(ctx, gone); err != nil || ok {
		t.Errorf("StillRelevant(item no longer needed) = %v, %v; want false", ok, err)
	}
}

func TestClockText(t *testing.T) {
	for in, want := range map[string]string{"14:00": "2 PM", "06:30": "6:30 AM", "nope": "nope"} {
		if got := clockText(in); got != want {
			t.Errorf("clockText(%q) = %q, want %q", in, got, want)
		}
	}
}
