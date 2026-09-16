package shopping

import (
	"strconv"
	"strings"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
	"github.com/Linesmerrill/DinnerOS/api/internal/providers"
)

// This file holds the pure matching of a week's grocery list to saved
// products: which lines go in the cart, their package counts, and the cart
// links. It has no I/O.

// unnamedKeyPrefix keys grocery lines without a catalog ingredient ID
// (planning's and the pantry's "name:" prefix).
const unnamedKeyPrefix = "name:"

// catalogID returns the catalog ingredient ID a line key names, or "".
func catalogID(key string) string {
	if strings.HasPrefix(key, unnamedKeyPrefix) {
		return ""
	}
	return key
}

// normalizeIngredientKey validates a grocery line key: a catalog ingredient
// ID, or "name:" and a normalized ingredient name.
func normalizeIngredientKey(key string) (string, error) {
	key = strings.TrimSpace(key)
	const msg = "ingredientKey must be a catalog ingredient ID or name: followed by a normalized ingredient name, as on the grocery list"
	if key == "" || len(key) > maxIngredientKeyLength {
		return "", invalid(msg)
	}
	if name, ok := strings.CutPrefix(key, unnamedKeyPrefix); ok {
		if name == "" || ingredients.NormalizeName(name) != name {
			return "", invalid(msg)
		}
		return key, nil
	}
	key = strings.ToLower(key)
	if _, err := mongodb.ParseID(key); err != nil {
		return "", invalid(msg)
	}
	return key, nil
}

// amountFromSize parses a stored package size.
func amountFromSize(s *PackageSize) *ingredients.Amount {
	if s == nil {
		return nil
	}
	q, err1 := ingredients.ParseQuantity(s.Quantity)
	u, err2 := ingredients.LookupUnit(s.Unit)
	if err1 != nil || err2 != nil || q.IsZero() {
		return nil
	}
	return &ingredients.Amount{Quantity: q, Unit: u}
}

// needsOf parses a line's stored amounts, skipping unusable ones.
func needsOf(amounts []Amount) []ingredients.Amount {
	out := make([]ingredients.Amount, 0, len(amounts))
	for _, a := range amounts {
		q, err1 := ingredients.ParseQuantity(a.Quantity)
		u, err2 := ingredients.LookupUnit(a.Unit)
		if err1 == nil && err2 == nil && !q.IsZero() {
			out = append(out, ingredients.Amount{Quantity: q, Unit: u})
		}
	}
	return out
}

// PackageCount recomputes a line's package count from its stored amounts,
// package size, and coverage rule.
func (l HandoffLine) PackageCount() providers.PackageCount {
	return providers.CountPackagesFor(needsOf(l.Amounts), amountFromSize(l.PackageSize), l.Coverage)
}

func validateMatchInput(in MatchInput) (MatchInput, error) {
	out := MatchInput{}
	if len(in.Lines) > MaxSelectionKeys || len(in.CheckedOffKeys) > MaxSelectionKeys || len(in.ExcludeKeys) > MaxSelectionKeys {
		return MatchInput{}, invalid("each key list holds at most %d keys", MaxSelectionKeys)
	}
	if in.Lines != nil {
		out.Lines = make([]LineSelection, 0, len(in.Lines))
		seen := map[string]bool{}
		for _, sel := range in.Lines {
			key, err := normalizeIngredientKey(sel.IngredientKey)
			if err != nil {
				return MatchInput{}, err
			}
			if seen[key] {
				return MatchInput{}, invalid("lines lists %s more than once", key)
			}
			if sel.Packages < 0 || sel.Packages > providers.MaxPackages {
				return MatchInput{}, invalid("packages must be between 1 and %d", providers.MaxPackages)
			}
			seen[key] = true
			out.Lines = append(out.Lines, LineSelection{IngredientKey: key, Packages: sel.Packages})
		}
	}
	for _, list := range []struct {
		in  []string
		out *[]string
	}{{in.CheckedOffKeys, &out.CheckedOffKeys}, {in.ExcludeKeys, &out.ExcludeKeys}} {
		for _, k := range list.in {
			key, err := normalizeIngredientKey(k)
			if err != nil {
				return MatchInput{}, err
			}
			*list.out = append(*list.out, key)
		}
	}
	return out, nil
}

func keySet(keys []string) map[string]bool {
	out := make(map[string]bool, len(keys))
	for _, k := range keys {
		out[k] = true
	}
	return out
}

// buildProposal matches the week's grocery list g to saved products. in
// must be validated. Lines keep the list's aisle order and are numbered l1,
// l2, …; the links come from p.
func buildProposal(p providers.GroceryProvider, settings Settings, g planning.GroceryList, prefs []Preference, in MatchInput) (Proposal, error) {
	out := Proposal{
		HouseholdID: settings.HouseholdID, Week: g.Week.String(), Provider: p.Key(), AffiliateTracked: p.AffiliateTracked(),
		Lines: []HandoffLine{}, Excluded: []Excluded{}, Links: []CartLink{},
	}
	if settings.Provider == p.Key() {
		out.StoreID = settings.StoreID
	}
	byKey := make(map[string]Preference, len(prefs))
	for _, pref := range prefs {
		byKey[pref.IngredientKey] = pref
	}
	var selected map[string]LineSelection
	if in.Lines != nil {
		selected = make(map[string]LineSelection, len(in.Lines))
		for _, sel := range in.Lines {
			selected[sel.IngredientKey] = sel
		}
	}
	checkedOff, excluded := keySet(in.CheckedOffKeys), keySet(in.ExcludeKeys)
	onList := map[string]bool{}

	for _, category := range g.Categories {
		for _, item := range category.Items {
			onList[item.IngredientKey] = true
			src := lineSource(category.Category, item)
			sel, isSelected := selected[item.IngredientKey]
			pref, hasPref := byKey[item.IngredientKey]
			reason := ExclusionReason("")
			switch {
			case checkedOff[item.IngredientKey]:
				reason = ExcludedCheckedOff
			case excluded[item.IngredientKey]:
				reason = ExcludedByMember
			case item.Specialty != nil && item.Specialty.HouseMade:
				reason = ExcludedHouseMade
			case selected != nil && !isSelected:
				reason = ExcludedNotSelected
			case selected == nil && item.Status == grocery.StatusInPantry:
				reason = ExcludedInPantry
			case selected == nil && item.Status == grocery.StatusPantryHint:
				reason = ExcludedPantryHint
			case !hasPref:
				reason = ExcludedNoProduct
			}
			if reason != "" {
				out.Excluded = append(out.Excluded, Excluded{LineSource: src, Reason: reason})
				continue
			}
			line := HandoffLine{
				ID: "l" + strconv.Itoa(len(out.Lines)+1), LineSource: src,
				ProductID: pref.ProductID, ProductName: pref.DisplayName, PackageSize: pref.PackageSize, Status: LinePending,
				// Resolved here, and stored on the line, so a handoff read
				// back later recomputes the count it was created with even
				// if the household changes the rule afterwards.
				Coverage: coverageFor(pref.Coverage, src.Category),
			}
			count := line.PackageCount()
			line.ComputedPackages, line.Packages = count.Packages, count.Packages
			line.Reason, line.CoversWeek = count.Reason, count.CoversWeek
			if sel.Packages > 0 {
				line.Packages = sel.Packages
			}
			out.Lines = append(out.Lines, line)
		}
	}
	for _, sel := range in.Lines {
		if !onList[sel.IngredientKey] {
			out.Excluded = append(out.Excluded, Excluded{LineSource: LineSource{IngredientKey: sel.IngredientKey}, Reason: ExcludedNotOnList})
		}
	}

	items := make([]providers.CartItem, 0, len(out.Lines))
	for _, l := range out.Lines {
		items = append(items, providers.CartItem{ProductID: l.ProductID, Quantity: l.Packages, LineIDs: []string{l.ID}})
	}
	links, err := p.BuildCartLinks(items, providers.StoreRef{StoreID: out.StoreID})
	if err != nil {
		return Proposal{}, err
	}
	for _, link := range links {
		cl := CartLink{URL: link.URL, ItemCount: len(link.Items)}
		for _, it := range link.Items {
			cl.LineIDs = append(cl.LineIDs, it.LineIDs...)
		}
		out.Links = append(out.Links, cl)
	}
	return out, nil
}

func lineSource(category string, item grocery.Item) LineSource {
	src := LineSource{
		IngredientKey: item.IngredientKey, Name: item.Name, Category: category, Unquantified: item.Unquantified,
		GroceryStatus: item.Status, Amounts: make([]Amount, 0, len(item.Amounts)),
	}
	for _, a := range item.Amounts {
		src.Amounts = append(src.Amounts, Amount{Quantity: a.Quantity.String(), Unit: a.Unit.Code})
	}
	return src
}
