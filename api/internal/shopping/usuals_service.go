package shopping

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
)

// Words under an added item on the list.
const (
	addedText      = "You added this"
	fromUsualsText = "From your usuals"
)

// AddItems adds items to a week's grocery list (Add Item, or "Add your
// usuals" with fromUsuals). Each name resolves to a catalog ingredient when
// one matches, so it merges with a recipe's line and its saved product.
// Adding an item the week already has sets its count. Answering the week's
// prompt with fromUsuals records it as answered.
func (s *Service) AddItems(ctx context.Context, actor households.Membership, week string, in []AddItemInput, fromUsuals bool) ([]AddedItem, error) {
	if err := authorize(actor, households.PermPlanEdit); err != nil {
		return nil, err
	}
	w, err := planning.ParseWeek(week)
	if err != nil {
		return nil, err
	}
	if len(in) == 0 || len(in) > MaxAddedItemsPerRequest {
		return nil, invalid("add between 1 and %d items", MaxAddedItemsPerRequest)
	}
	names := make([]string, 0, len(in))
	for i, item := range in {
		name := strings.Join(strings.Fields(item.Name), " ")
		switch {
		case ingredients.NormalizeName(name) == "":
			return nil, invalid("Name each item.")
		case utf8.RuneCountInString(name) > MaxAddedItemName:
			return nil, invalid("Item names must be %d characters or fewer.", MaxAddedItemName)
		case item.Count < 0 || item.Count > MaxAddedItemCount:
			return nil, invalid("Add between 1 and %d of an item.", MaxAddedItemCount)
		}
		in[i].Name, in[i].Count = name, max(item.Count, 1)
		names = append(names, name)
	}
	resolved, err := s.resolveItems(ctx, names)
	if err != nil {
		return nil, err
	}
	existing, err := s.store.ListAddedItems(ctx, actor.HouseholdID, w.String())
	if err != nil {
		return nil, fmt.Errorf("shopping: load added items: %w", err)
	}
	have := map[string]bool{}
	for _, a := range existing {
		have[a.IngredientKey] = true
	}
	// The last of two names for the same item wins.
	order := []string{}
	byKey := map[string]AddedItem{}
	now := s.timestamp()
	for i, item := range in {
		r := resolved[i]
		if _, seen := byKey[r.key]; !seen {
			order = append(order, r.key)
		}
		byKey[r.key] = AddedItem{
			HouseholdID: actor.HouseholdID, Week: w.String(), IngredientKey: r.key, Name: r.name, Count: item.Count,
			FromUsuals: fromUsuals, AddedBy: actor.UserID, AddedAt: now,
		}
	}
	fresh := 0
	for _, k := range order {
		if !have[k] {
			fresh++
		}
	}
	if len(existing)+fresh > MaxAddedItemsPerWeek {
		return nil, invalid("A week holds at most %d added items.", MaxAddedItemsPerWeek)
	}
	out := make([]AddedItem, 0, len(order))
	for _, k := range order {
		saved, _, err := s.store.UpsertAddedItem(ctx, byKey[k])
		if err != nil {
			return nil, fmt.Errorf("shopping: add item: %w", err)
		}
		out = append(out, saved)
	}
	if fromUsuals {
		if err := s.store.AnswerUsualsPrompt(ctx, UsualsPrompt{HouseholdID: actor.HouseholdID, Week: w.String(), AnsweredBy: actor.UserID, AnsweredAt: now}); err != nil {
			return nil, fmt.Errorf("shopping: answer usuals prompt: %w", err)
		}
	}
	return out, nil
}

// RemoveAddedItem takes an added item back off its week's list.
func (s *Service) RemoveAddedItem(ctx context.Context, actor households.Membership, id string) error {
	if err := authorize(actor, households.PermPlanEdit); err != nil {
		return err
	}
	return s.store.DeleteAddedItem(ctx, actor.HouseholdID, id)
}

// RemoveAddedItemByKey takes the item added for an ingredient off a week's
// list.
func (s *Service) RemoveAddedItemByKey(ctx context.Context, actor households.Membership, week, ingredientKey string) error {
	if err := authorize(actor, households.PermPlanEdit); err != nil {
		return err
	}
	w, err := planning.ParseWeek(week)
	if err != nil {
		return err
	}
	key, err := normalizeIngredientKey(ingredientKey)
	if err != nil {
		return err
	}
	return s.store.DeleteAddedItemByKey(ctx, actor.HouseholdID, w.String(), key)
}

// GroceryExtras implements planning.ExtrasSource: the week's added items as
// grocery lines for no meal, counted in whole items.
func (s *Service) GroceryExtras(ctx context.Context, plan planning.Plan) ([]grocery.Line, error) {
	added, err := s.store.ListAddedItems(ctx, plan.HouseholdID, plan.Week.String())
	if err != nil {
		return nil, err
	}
	lines := make([]grocery.Line, 0, len(added))
	for _, a := range added {
		q := ingredients.NewQuantity(int64(max(a.Count, 1)), 1)
		text := addedText
		if a.FromUsuals {
			text = fromUsualsText
		}
		line := grocery.Line{
			IngredientKey: a.IngredientKey, Name: a.Name, Quantity: &q, UnitCode: "count",
			Extra: &grocery.Extra{ID: a.ID, Origin: grocery.OriginAdded, Text: text},
		}
		line.Category, _ = ingredients.Categorize(a.Name)
		lines = append(lines, line)
	}
	return lines, nil
}

// Usuals returns the household's usuals for a week, marked against the
// week's list, and whether the week should ask "Add your usuals?".
func (s *Service) Usuals(ctx context.Context, householdID, week string) (UsualsView, error) {
	w, err := planning.ParseWeek(week)
	if err != nil {
		return UsualsView{}, err
	}
	added, err := s.store.ListAddedItems(ctx, householdID, "")
	if err != nil {
		return UsualsView{}, fmt.Errorf("shopping: load added items: %w", err)
	}
	sent, err := s.store.SentWeeks(ctx, householdID, w.String())
	if err != nil {
		return UsualsView{}, fmt.Errorf("shopping: load sent weeks: %w", err)
	}
	prefs, err := s.store.ListUsualPrefs(ctx, householdID)
	if err != nil {
		return UsualsView{}, fmt.Errorf("shopping: load usuals: %w", err)
	}
	onList := map[string]bool{}
	for _, a := range added {
		if a.Week == w.String() {
			onList[a.IngredientKey] = true
		}
	}
	if s.grocery != nil {
		list, err := s.grocery.GroceryList(ctx, householdID, w.String())
		if err != nil {
			return UsualsView{}, fmt.Errorf("shopping: load grocery list: %w", err)
		}
		for _, c := range list.Categories {
			for _, it := range c.Items {
				if it.Status == grocery.StatusToBuy {
					onList[it.IngredientKey] = true
				}
			}
		}
	}
	view := findUsuals(w.String(), added, sent, prefs, onList)
	if len(view.Offered()) > 0 {
		_, err := s.store.GetUsualsPrompt(ctx, householdID, w.String())
		switch {
		case errors.Is(err, ErrNotFound):
			view.Prompt = true
		case err != nil:
			return UsualsView{}, fmt.Errorf("shopping: load usuals prompt: %w", err)
		}
	}
	return view, nil
}

// UsualInput changes how one item is treated. With neither Pinned nor
// NotUsual, the item goes back to being counted like any other.
type UsualInput struct {
	// IngredientKey names the item; when empty, Name is resolved to one.
	IngredientKey string
	Name          string
	// Count is the amount to offer a pinned item; 0 uses the usual one.
	Count    int
	Pinned   bool
	NotUsual bool
}

// SetUsual pins an item, marks it "not a usual", or clears either.
func (s *Service) SetUsual(ctx context.Context, actor households.Membership, in UsualInput) (UsualPref, error) {
	if err := authorize(actor, households.PermPlanEdit); err != nil {
		return UsualPref{}, err
	}
	if in.Pinned && in.NotUsual {
		return UsualPref{}, invalid("An item can't be pinned and not a usual.")
	}
	if in.Count < 0 || in.Count > MaxAddedItemCount {
		return UsualPref{}, invalid("Offer between 1 and %d of an item.", MaxAddedItemCount)
	}
	name := strings.Join(strings.Fields(in.Name), " ")
	if utf8.RuneCountInString(name) > MaxAddedItemName {
		return UsualPref{}, invalid("Item names must be %d characters or fewer.", MaxAddedItemName)
	}
	key := strings.TrimSpace(in.IngredientKey)
	if key != "" {
		var err error
		if key, err = normalizeIngredientKey(key); err != nil {
			return UsualPref{}, err
		}
	}
	if key == "" || name == "" {
		if ingredients.NormalizeName(name) == "" {
			return UsualPref{}, invalid("Name the item.")
		}
		r, err := s.resolveItems(ctx, []string{name})
		if err != nil {
			return UsualPref{}, err
		}
		if key == "" {
			key = r[0].key
		}
		name = r[0].name
	}
	if !in.Pinned && !in.NotUsual {
		return UsualPref{HouseholdID: actor.HouseholdID, IngredientKey: key, Name: name}, s.store.DeleteUsualPref(ctx, actor.HouseholdID, key)
	}
	prefs, err := s.store.ListUsualPrefs(ctx, actor.HouseholdID)
	if err != nil {
		return UsualPref{}, fmt.Errorf("shopping: load usuals: %w", err)
	}
	known := false
	for _, p := range prefs {
		known = known || p.IngredientKey == key
	}
	if !known && len(prefs) >= MaxUsualPrefs {
		return UsualPref{}, invalid("A household keeps at most %d of these.", MaxUsualPrefs)
	}
	p := UsualPref{
		HouseholdID: actor.HouseholdID, IngredientKey: key, Name: name, Pinned: in.Pinned, NotUsual: in.NotUsual,
		UpdatedBy: actor.UserID, UpdatedAt: s.timestamp(),
	}
	if in.Pinned {
		p.Count = in.Count
	}
	return p, s.store.PutUsualPref(ctx, p)
}

// DismissUsuals answers a week's "Add your usuals?" with Not Now.
func (s *Service) DismissUsuals(ctx context.Context, actor households.Membership, week string) error {
	if err := authorize(actor, households.PermPlanEdit); err != nil {
		return err
	}
	w, err := planning.ParseWeek(week)
	if err != nil {
		return err
	}
	return s.store.AnswerUsualsPrompt(ctx, UsualsPrompt{HouseholdID: actor.HouseholdID, Week: w.String(), AnsweredBy: actor.UserID, AnsweredAt: s.timestamp()})
}

type resolvedItem struct{ key, name string }

// resolveItems maps typed names to grocery keys: the catalog ingredient
// whose key matches the normalized name, else "name:" and that name.
func (s *Service) resolveItems(ctx context.Context, names []string) ([]resolvedItem, error) {
	norms := make([]string, len(names))
	var lookup []string
	for i, n := range names {
		norms[i] = ingredients.NormalizeName(n)
		lookup = append(lookup, norms[i])
	}
	catalog := map[string]resolvedItem{}
	if s.catalog != nil {
		found, err := s.catalog.IngredientsByKey(ctx, lookup)
		if err != nil {
			return nil, fmt.Errorf("shopping: resolve items: %w", err)
		}
		for _, ing := range found {
			catalog[ing.Key] = resolvedItem{key: ing.ID, name: ing.Name}
		}
	}
	out := make([]resolvedItem, len(names))
	for i, n := range names {
		if r, ok := catalog[norms[i]]; ok {
			out[i] = r
			continue
		}
		out[i] = resolvedItem{key: unnamedKeyPrefix + norms[i], name: displayItemName(n)}
	}
	return out, nil
}

// displayItemName capitalizes each word of a name typed all in lower case
// ("paper towels" becomes "Paper Towels"). A name with any capital is the
// member's own and left alone ("iPhone case", "HEB tortillas").
func displayItemName(name string) string {
	if strings.ToLower(name) != name {
		return name
	}
	words := strings.Fields(name)
	for i, w := range words {
		runes := []rune(w)
		runes[0] = unicode.ToUpper(runes[0])
		words[i] = string(runes)
	}
	return strings.Join(words, " ")
}
