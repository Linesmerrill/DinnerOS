package recommendations

import (
	"context"
	"slices"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot/baseline"
	"github.com/Linesmerrill/DinnerOS/api/internal/companions"
	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
	"github.com/Linesmerrill/DinnerOS/api/internal/ratings"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

func pairingSummary(v WeekPairingsView) []string {
	var out []string
	for _, m := range v.Meals {
		for _, p := range m.Pairings {
			out = append(out, m.Entry.RecipeID+" "+p.Key+" "+p.Source+" "+p.Frequency+" "+p.Reason)
		}
	}
	return out
}

func groceryNames(t *testing.T, list planning.GroceryList) []string {
	t.Helper()
	var out []string
	for _, c := range list.Categories {
		for _, it := range c.Items {
			out = append(out, it.Name)
		}
	}
	slices.Sort(out)
	return out
}

// TestIntegrationCompanionsKeepAddonsWorking is a household like the ones
// that had add-ons before companions: an add-on recipe, a rule that pairs it
// with pasta, and a grocery rule. Turning companions on makes a companion for
// the add-on and changes nothing the household sees: the same suggestions,
// the same planned add-on, the same grocery list, and the recipe untouched.
func TestIntegrationCompanionsKeepAddonsWorking(t *testing.T) {
	client := mongotest.Client(t)
	ctx := context.Background()
	if err := client.EnsureIndexes(ctx, slices.Concat(recipes.Indexes(), ratings.Indexes(), events.Indexes(), planning.Indexes(), Indexes(), companions.Indexes())...); err != nil {
		t.Fatal(err)
	}
	db := client.Database()
	hh := bson.NewObjectID().Hex()

	recipeService := recipes.NewService(recipes.NewMongoStore(db))
	eventService := events.NewService(events.ServiceOptions{Store: events.NewMongoStore(db), Recipes: recipeService})
	ratingService := ratings.NewService(ratings.ServiceOptions{Store: ratings.NewMongoStore(db), Recipes: recipeService, Events: eventService})
	planService := planning.NewService(planning.NewMongoStore(db), recipeService).WithEvents(eventService, nil)
	svc := NewService(ServiceOptions{
		Store: NewMongoStore(db), Provider: baseline.New(baseline.Options{}),
		Households: fakeHouseholds{hh: {ID: hh, DefaultServings: 2, TimeZone: "America/Denver"}},
		Recipes:    recipeService, Ratings: ratingService, Events: eventService, Plans: planService,
		Now: func() time.Time { return testNow },
	})
	planService.WithExtras(svc)

	if _, err := recipeService.Import(ctx, hh, pairingImport()); err != nil {
		t.Fatal(err)
	}
	page, err := recipeService.List(ctx, hh, recipes.ListQuery{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	for _, r := range page.Items {
		ids[r.Name] = r.ID
	}
	pastaID, soupID, breadID := ids["Creamy Garlic Spaghetti"], ids["Chicken & Potato Mushroom Soup"], ids["Garlic Bread"]
	breadBefore, err := recipeService.Get(ctx, hh, breadID)
	if err != nil {
		t.Fatal(err)
	}
	rules := []PairingRule{
		{When: PairingWhen{MealCategories: []string{"pasta"}}, Add: PairingTarget{RecipeID: breadID}, Frequency: PairingSuggest},
		{When: PairingWhen{MealCategories: []string{"soup"}}, Add: PairingTarget{GroceryItem: &GroceryItem{Name: "Club crackers", Quantity: "1", Unit: "package"}}},
	}
	if _, err := svc.UpdateProfile(ctx, hh, userAda, ProfileUpdate{Pairings: &rules}, false); err != nil {
		t.Fatal(err)
	}
	_, added, err := planService.AddEntries(ctx, hh, userAda, testWeek, []planning.NewEntry{
		{RecipeID: pastaID, Day: "mon", Servings: 2}, {RecipeID: soupID, Day: "tue", Servings: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	pastaEntry, soupEntry := added[0], added[1]
	before, err := svc.WeekPairings(ctx, hh, userAda, testWeek, "")
	if err != nil {
		t.Fatal(err)
	}
	listBefore, err := planService.GroceryList(ctx, hh, testWeek)
	if err != nil {
		t.Fatal(err)
	}

	// Companions on.
	companionService := companions.NewService(companions.Options{
		Store: companions.NewMongoStore(db), Recipes: recipeService, Hints: svc,
		CanonicalCuisine: CanonicalCuisine, CuisineLabel: CuisineLabel, MealCategory: IsMealCategory, Now: func() time.Time { return testNow },
	})
	svc.WithCompanions(companionService)

	after, err := svc.WeekPairings(ctx, hh, userAda, testWeek, "")
	if err != nil {
		t.Fatal(err)
	}
	if b, a := pairingSummary(before), pairingSummary(after); !slices.Equal(b, a) {
		t.Fatalf("suggestions changed:\nbefore %q\nafter  %q", b, a)
	}
	list, err := companionService.List(ctx, hh)
	if err != nil || len(list) != 1 {
		t.Fatalf("companions = %+v, %v", list, err)
	}
	bread := list[0]
	if bread.Name != "Garlic Bread" || bread.LinkedRecipe == nil || bread.LinkedRecipe.ID != breadID || bread.Mode != companions.ModeSuggest ||
		!slices.Equal(bread.MealCategories, []string{"pasta"}) || bread.Frequency != companions.FrequencyNone {
		t.Errorf("migrated bread = %+v", bread)
	}
	if after, _ := planService.GroceryList(ctx, hh, testWeek); !slices.Equal(groceryNames(t, listBefore), groceryNames(t, after)) {
		t.Errorf("grocery list changed: %v, then %v", groceryNames(t, listBefore), groceryNames(t, after))
	}

	// The add-on still plans from its rule, and the companion records the week.
	res, err := svc.AcceptPairing(ctx, hh, userAda, testWeek, pastaEntry.ID, "recipe:"+breadID)
	if err != nil || res.Entry == nil || res.Entry.RecipeID != breadID || res.Pairing.Source != PairingSourceRule {
		t.Fatalf("accept = %+v, %v", res, err)
	}
	if got, _ := companionService.Get(ctx, hh, bread.ID); got.LastListedWeek != testWeek {
		t.Errorf("last listed = %q", got.LastListedWeek)
	}
	breadAfter, err := recipeService.Get(ctx, hh, breadID)
	if err != nil || breadAfter.Name != breadBefore.Name || !breadAfter.IsAddon || len(breadAfter.Ingredients) != len(breadBefore.Ingredients) ||
		!slices.Equal(breadAfter.Servings, breadBefore.Servings) {
		t.Errorf("the add-on recipe changed: %+v", breadAfter)
	}

	// A new companion set to Add It for Me reaches the list on its own, with
	// no member behind it, through Mongo.
	chips, err := companionService.Create(ctx, hh, userAda, companions.Input{
		Name: "Chips", StoreItem: "Tortilla chips", RecipeIDs: []string{soupID}, Mode: companions.ModeAuto,
	})
	if err != nil {
		t.Fatal(err)
	}
	grocer, err := planService.GroceryList(ctx, hh, testWeek)
	if err != nil {
		t.Fatal(err)
	}
	var found grocery.Item
	for _, c := range grocer.Categories {
		for _, it := range c.Items {
			if it.Name == "Tortilla chips" {
				found = it
			}
		}
	}
	if len(found.Extras) != 1 || found.Extras[0].Text != "Tortilla chips for Chicken & Potato Mushroom Soup" {
		t.Fatalf("chips on the list = %+v", found)
	}
	stored, err := NewMongoStore(db).GetWeekPairings(ctx, hh, testWeek)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(stored.GroceryItems, func(g WeekGroceryItem) bool { return g.CompanionID == chips.ID })
	if i < 0 || stored.GroceryItems[i].AddedBy != "" || stored.GroceryItems[i].ForEntryID != soupEntry.ID {
		t.Errorf("stored items = %+v", stored.GroceryItems)
	}
}
