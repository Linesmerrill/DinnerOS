package planning

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
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
	// One plate of the meal being cooked, and every meal after it in the week
	// that's still to come. Only later meals: two meals sharing a lemon each
	// said to save some for the other (build 122, Hark's review item 4), so
	// the week's first meal keeps some back and the later one just uses it.
	// A meal planned twice is cooked once at a time.
	place := map[string]int{}
	for i, m := range groceryMeals(p) {
		place[m.RecipeID] = i
	}
	cooking := false
	kept := make([]Entry, 0, len(p.Entries))
	for _, e := range p.Entries {
		if e.RecipeID == recipeID {
			if !cooking {
				kept, cooking = append(kept, e), true
			}
			continue
		}
		if _, done := answered[e.ID]; !done && place[e.RecipeID] > place[recipeID] {
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
		// Not bought this week is at home, and still has to last. Left out of
		// a recipe isn't used there, and left out of every dish isn't used at
		// all: neither is anything to save (build 123, Merrill's iPad review).
		if item.SkipScope == grocery.SkipThisWeek {
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
	groups := byRecipe(others)
	slices.SortStableFunc(groups, func(a, b []grocery.Share) int {
		return meals[a[0].RecipeID].order - meals[b[0].RecipeID].order
	})
	// A meal kit's "1 unit Crispy Fried Onions" is a pack of them: "Use 1 of
	// the 2 packs", not "Use 1 Crispy Fried Onions" (build 123, Merrill's
	// iPad review).
	packs := packCounts(mine, groups, item.Name)
	use := sharesText(mine, item.Name)
	if packs != nil {
		use = packs.use()
	}
	if use == "" {
		// "Salt to taste" says nothing about how much to keep.
		return recipes.SaveForLater{}, false
	}
	var parts []string
	var short string
	for i, group := range groups {
		amount := sharesText(group, item.Name)
		if packs != nil {
			amount = packs.save(i)
		}
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
			text += " " + countName(a.Quantity, name)
		}
		texts = append(texts, text)
	}
	return strings.Join(texts, " + ")
}

var one = ingredients.NewQuantity(1, 1)
var half = ingredients.NewQuantity(1, 2)

// countName is name after a count: "½ Yellow Onion", "1 Scallion", "2
// Lemons".
func countName(q ingredients.Quantity, name string) string {
	head, last := splitLast(name)
	if q.Cmp(one) > 0 {
		if pluralWord(last) {
			return name
		}
		return head + pluralOf(last)
	}
	if pluralWord(last) {
		return head + singularOf(last)
	}
	return name
}

// mealPacks are the packs of an item each meal uses, when every meal counts
// it in packs: this meal's first, then each later meal's in order.
type mealPacks struct {
	word   string
	counts []ingredients.Quantity
}

// packCounts reads the shares as packs, when the item is bought by the pack
// and every share is a plain count of them; nil otherwise.
func packCounts(mine []grocery.Share, groups [][]grocery.Share, name string) *mealPacks {
	word, ok := packWord(name)
	if !ok {
		return nil
	}
	out := &mealPacks{word: word}
	for _, shares := range append([][]grocery.Share{mine}, groups...) {
		var total ingredients.Quantity
		for i, sh := range shares {
			if sh.Unquantified || len(sh.Amounts) != 1 {
				return nil
			}
			a := sh.Amounts[0]
			if a.Unit.Label(a.Quantity) != "" || a.Quantity.IsZero() {
				return nil
			}
			if i == 0 {
				total = a.Quantity
			} else {
				total = total.Add(a.Quantity)
			}
		}
		out.counts = append(out.counts, total)
	}
	return out
}

func (p *mealPacks) total() ingredients.Quantity {
	sum := p.counts[0]
	for _, q := range p.counts[1:] {
		sum = sum.Add(q)
	}
	return sum
}

// use is this meal's packs: "1 of the 2 packs", "half the bag", "1 ½ packs".
func (p *mealPacks) use() string {
	mine, total := p.counts[0], p.total()
	switch {
	case whole(mine) && whole(total):
		return mine.Format() + " of the " + total.Format() + " " + p.plural()
	case total.Equal(one) && mine.Equal(half):
		return "half the " + p.word
	case total.Equal(one):
		return mine.Format() + " of the " + p.word
	}
	return p.amount(mine)
}

// save is the i-th later meal's packs: "1 pack", "half a bag", or "the other
// half" when two meals split one pack.
func (p *mealPacks) save(i int) string {
	q := p.counts[i+1]
	if len(p.counts) == 2 && q.Equal(half) && p.counts[0].Equal(half) {
		return "the other half"
	}
	return p.amount(q)
}

func (p *mealPacks) amount(q ingredients.Quantity) string {
	switch {
	case q.Equal(half):
		return "half a " + p.word
	case q.Cmp(one) < 0:
		return q.Format() + " of a " + p.word
	case q.Equal(one):
		return "1 " + p.word
	}
	return q.Format() + " " + p.plural()
}

func (p *mealPacks) plural() string { return pluralOf(p.word) }

func whole(q ingredients.Quantity) bool {
	_, den := q.Fraction()
	return den == 1
}

// packTails are last words of a name that come in a pack when counted: "1
// Apricot Jam" is a packet of it, and "1 Marinara Cup" is a cup.
var packTails = map[string]string{
	"cup": "cup", "can": "can", "jar": "jar", "tub": "tub", "bag": "bag", "box": "box", "pouch": "pouch",
	"packet": "packet", "pack": "pack", "container": "container", "bottle": "bottle",
	"jam": "pack", "dressing": "pack", "crema": "pack", "glaze": "pack", "mayonnaise": "pack", "mayo": "pack",
	"mustard": "pack", "honey": "pack", "syrup": "pack", "spread": "pack", "vinaigrette": "pack",
	"sauce": "pack", "paste": "pack", "seasoning": "pack", "mix": "pack", "cream": "pack",
}

// packWord is the word a counted item comes in ("pack", "cup"), when a count
// of it means packs rather than pieces: a name ending in its packaging, or a
// plural pantry name like Crispy Fried Onions. Lemons, Scallions, Eggs and
// Tortillas are counted one by one.
func packWord(name string) (string, bool) {
	_, last := splitLast(name)
	last = strings.ToLower(last)
	if word, ok := packTails[last]; ok {
		return word, true
	}
	if !pluralWord(last) {
		return "", false
	}
	switch category, _ := ingredients.Categorize(name); category {
	case ingredients.CategoryPantry, ingredients.CategorySpices, ingredients.CategoryCondiments,
		ingredients.CategoryFrozen, ingredients.CategoryDeli:
		return "pack", true
	}
	return "", false
}

// splitLast is name's last word and what comes before it, space included.
func splitLast(name string) (head, last string) {
	i := strings.LastIndexByte(name, ' ')
	return name[:i+1], name[i+1:]
}

// pluralWord reports whether a word reads as a plural: "Onions", not
// "Asparagus", "Hummus" or "Swiss".
func pluralWord(w string) bool {
	lower := strings.ToLower(w)
	if !strings.HasSuffix(lower, "s") {
		return false
	}
	for _, end := range []string{"ss", "us", "is"} {
		if strings.HasSuffix(lower, end) {
			return false
		}
	}
	return true
}

func pluralOf(w string) string {
	lower := strings.ToLower(w)
	switch {
	case lower == "tomato" || lower == "potato":
		return w + "es"
	case strings.HasSuffix(lower, "x") || strings.HasSuffix(lower, "ch") || strings.HasSuffix(lower, "sh") ||
		strings.HasSuffix(lower, "s"):
		return w + "es"
	case strings.HasSuffix(lower, "y") && len(lower) > 1 && !strings.ContainsRune("aeiou", rune(lower[len(lower)-2])):
		return w[:len(w)-1] + "ies"
	}
	return w + "s"
}

func singularOf(w string) string {
	lower := strings.ToLower(w)
	switch {
	case strings.HasSuffix(lower, "toes"):
		return w[:len(w)-2]
	case strings.HasSuffix(lower, "ies") && len(lower) > 4:
		return w[:len(w)-3] + "y"
	case strings.HasSuffix(lower, "ches") || strings.HasSuffix(lower, "shes") || strings.HasSuffix(lower, "xes"):
		return w[:len(w)-2]
	}
	return w[:len(w)-1]
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
