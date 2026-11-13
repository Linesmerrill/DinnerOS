package planning

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// saveForLaterWeeks are the weeks, relative to the household's current one,
// searched for the meal being cooked: this week first, then last week's meal
// cooked late, then next week's cooked early.
var saveForLaterWeeks = []int{0, -1, 1}

// dayNames are the days as a person says them.
var dayNames = map[Day]string{
	Monday: "Monday", Tuesday: "Tuesday", Wednesday: "Wednesday", Thursday: "Thursday",
	Friday: "Friday", Saturday: "Saturday", Sunday: "Sunday",
}

// SaveForLater lists the ingredients the household's planned meal of recipeID
// shares with the other meals of its week that nobody has said were made or
// skipped: how much this meal uses, and how much to keep for each of the
// others (decision 663). It is empty when the recipe isn't planned near now or
// shares nothing. Implements recipes.SaveForLaterSource.
func (s *Service) SaveForLater(ctx context.Context, householdID, recipeID string) ([]recipes.SaveForLater, error) {
	now, first, err := s.householdClock(ctx, householdID)
	if err != nil {
		return nil, err
	}
	current := WeekOfOn(now, first)
	for _, offset := range saveForLaterWeeks {
		p, err := s.store.GetPlan(ctx, householdID, current.AddWeeks(offset))
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("planning: save for later: %w", err)
		}
		p.FirstDay = first
		if !slices.ContainsFunc(p.Entries, func(e Entry) bool { return e.RecipeID == recipeID }) {
			continue
		}
		return s.saveForLaterIn(ctx, householdID, p, recipeID)
	}
	return nil, nil
}

func (s *Service) saveForLaterIn(ctx context.Context, householdID string, p Plan, recipeID string) ([]recipes.SaveForLater, error) {
	answered := map[string]bool{}
	if s.outcomes != nil {
		got, err := s.entryAnswers(ctx, householdID, p)
		if err != nil {
			return nil, err
		}
		answered = got
	}
	// One plate of the meal being cooked, and every other meal still to come.
	// A meal planned twice is cooked once at a time.
	cooking := false
	kept := make([]Entry, 0, len(p.Entries))
	for _, e := range p.Entries {
		if e.RecipeID == recipeID {
			if !cooking {
				kept, cooking = append(kept, e), true
			}
			continue
		}
		if _, done := answered[e.ID]; !done {
			kept = append(kept, e)
		}
	}
	later := p
	later.Entries = kept
	needs, err := s.weekNeeds(ctx, householdID, later, false)
	if err != nil {
		return nil, err
	}
	list, err := grocery.AggregateWith(needs.selections, grocery.PantrySet{}, needs.skips)
	if err != nil {
		return nil, err
	}
	meals := mealOrder{}
	for i, m := range groceryMeals(later) {
		meals[m.RecipeID] = mealAt{day: m.Day, order: i}
	}
	items := list.Items
	for _, item := range list.SkippedItems {
		// Left out of a recipe isn't used there; skipped for the week or for
		// good is at home, and still has to last.
		if item.SkipScope != grocery.SkipRecipe {
			items = append(items, item)
		}
	}
	var out []recipes.SaveForLater
	for _, item := range items {
		if item.Status == grocery.StatusPantryHint {
			// Salt and oil come from a box that lasts; nobody runs out of
			// them between Monday and Thursday.
			continue
		}
		if note, ok := saveForLaterNote(item, recipeID, meals, needs.alsoCalled[item.IngredientKey]); ok {
			out = append(out, note)
		}
	}
	return out, nil
}

// saveForLaterNote is item's note for the meal of recipeID, when another meal
// needs some too.
func saveForLaterNote(item grocery.Item, recipeID string, meals mealOrder, also []grocery.AlsoCalled) (recipes.SaveForLater, bool) {
	var mine []grocery.Share
	var others []grocery.Share
	for _, sh := range item.Shares {
		switch {
		case sh.Extra || sh.Combined:
			// A pairing isn't cooked into anything; a house-made batch is
			// made once for every meal it's for.
			continue
		case sh.RecipeID == recipeID:
			mine = append(mine, sh)
		default:
			others = append(others, sh)
		}
	}
	if len(mine) == 0 || len(others) == 0 {
		return recipes.SaveForLater{}, false
	}
	keys := []string{item.IngredientKey}
	var partOf []string
	for _, sh := range mine {
		if c := sh.Component; c != nil && c.LineKey != "" && !slices.Contains(keys, c.LineKey) {
			keys = append(keys, c.LineKey)
			partOf = append(partOf, c.LineKey)
		}
	}
	for _, a := range also {
		if a.Key != "" && !slices.Contains(keys, a.Key) {
			keys = append(keys, a.Key)
		}
	}
	use := sharesText(mine, item.Name)
	if use == "" {
		// "Salt to taste" says nothing about how much to keep.
		return recipes.SaveForLater{}, false
	}
	var parts []string
	var short string
	groups := byRecipe(others)
	slices.SortStableFunc(groups, func(a, b []grocery.Share) int {
		return meals[a[0].RecipeID].order - meals[b[0].RecipeID].order
	})
	for i, group := range groups {
		amount := sharesText(group, item.Name)
		name := group[0].RecipeName
		part := "some for " + name
		if amount != "" {
			part = amount + " for " + name
		}
		if i == 0 {
			short = "Save " + part
		}
		if day := dayNames[meals[group[0].RecipeID].day]; day != "" {
			part += " on " + day
		}
		parts = append(parts, part)
	}
	text := "Use " + use + ". Save " + joinAnd(parts) + "."
	if len(parts) > 1 {
		short = "Save some for later meals"
	}
	return recipes.SaveForLater{Keys: keys, PartOf: partOf, Name: item.Name, Use: use, Text: text, Short: short}, true
}

// mealOrder is each planned recipe's first day and place in the week.
type mealOrder map[string]mealAt

type mealAt struct {
	day   Day
	order int
}

// byRecipe groups shares by recipe, in their order.
func byRecipe(shares []grocery.Share) [][]grocery.Share {
	var out [][]grocery.Share
	index := map[string]int{}
	for _, sh := range shares {
		i, ok := index[sh.RecipeID]
		if !ok {
			i = len(out)
			index[sh.RecipeID] = i
			out = append(out, nil)
		}
		out[i] = append(out[i], sh)
	}
	return out
}

// sharesText is one meal's amount of an item across its shares ("4 oz",
// "½ Yellow Onion" for a count), empty when any share has no amount: "some"
// reads better than half a number.
func sharesText(shares []grocery.Share, name string) string {
	var total []grocery.Amount
	for _, sh := range shares {
		if sh.Unquantified || len(sh.Amounts) == 0 {
			return ""
		}
	next:
		for _, a := range sh.Amounts {
			for i, t := range total {
				if t.Unit.Code == a.Unit.Code {
					total[i].Quantity = t.Quantity.Add(a.Quantity)
					continue next
				}
			}
			total = append(total, a)
		}
	}
	texts := make([]string, 0, len(total))
	for _, a := range total {
		text := amountText(a)
		if a.Unit.Label(a.Quantity) == "" {
			text += " " + name
		}
		texts = append(texts, text)
	}
	return strings.Join(texts, " + ")
}

// joinAnd joins "a", "a and b", "a, b, and c".
func joinAnd(parts []string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	case 2:
		return parts[0] + " and " + parts[1]
	}
	return strings.Join(parts[:len(parts)-1], ", ") + ", and " + parts[len(parts)-1]
}
