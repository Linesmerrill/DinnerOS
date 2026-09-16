package shopping

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strings"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/pantry"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
)

// This file computes what a week of groceries really cost
// (docs/grocery-engine.md#weekly-cost).
//
// A $130 cart and a $130 meal kit aren't the same spend: the cart's sour
// cream, soy sauce, and spices outlast the week. So a week's cost splits what
// was bought into what the week's meals used and what is stocked for later:
//
//   - Spent: the order total a member entered (fees, tax, and tip included),
//     else the sum of the item prices that are known.
//   - Used this week, per priced item:
//   - not tracked in the pantry (fresh, used up): the whole price;
//   - tracked: price × the week's need ÷ what was bought, measured the way
//     package counts are; or the whole price when that can't be measured,
//     so savings are never overstated;
//   - then, when the pantry's estimate says less is left than the plan
//     implies (more was cooked, or other use), the difference moves from
//     stocked to used.
//   - plus pantry stock bought before this week that this week's cooked
//     meals used (the cook deductions × the purchase's price per unit);
//   - plus, when an order total was entered, what it holds beyond the item
//     prices (fees, tax, tip, and items without a price).
//   - Stocked for later: what's left of this week's priced items.
//   - Cost per meal: used ÷ the week's planned meals (add-ons don't count).
//   - vs meal kit: the household's meal kit price per meal × meals − used.
//
// Every figure is based only on the prices members entered, and the summary
// says how many items that is.

// ItemUsage says how an item's used share was decided.
type ItemUsage string

// Item usages.
const (
	// UsageMeasured: the week's need measured against the package.
	UsageMeasured ItemUsage = "measured"
	// UsageWholePackage: fresh for the week, used up.
	UsageWholePackage ItemUsage = "whole_package"
	// UsageUnknown: tracked, but the need can't be measured against the
	// package, so the whole price counts as used.
	UsageUnknown ItemUsage = "unknown"
)

// Spent sources.
const (
	SpentOrderTotal = "order_total"
	SpentItemPrices = "item_prices"
)

// Limits for the savings history.
const (
	DefaultSavingsWeeks = 8
	MaxSavingsWeeks     = 26
)

// CostItem is one bought handoff line in a week's cost.
type CostItem struct {
	HandoffID     string
	LineID        string
	IngredientKey string
	Name          string
	// PriceCents, UsedCents, and StockedCents are nil without a price.
	PriceCents   *int64
	UsedCents    *int64
	StockedCents *int64
	Pantry       PantryTracking
	Usage        ItemUsage
}

// WeekCost is a week's grocery cost.
type WeekCost struct {
	Week            string
	OrderTotalCents *int64
	// SpentCents is nil when there's neither an order total nor a price.
	SpentCents  *int64
	SpentSource string
	ItemsBought int
	ItemsPriced int
	// UsedCents and StockedCents are nil when nothing is priced.
	UsedCents             *int64
	StockedCents          *int64
	EarlierStockUsedCents int64
	FeesAndUnpricedCents  int64
	UnmeasuredItems       int
	Meals                 int
	CostPerMealCents      *int64
	MealKit               *households.MealKit
	SavedCents            *int64
	Items                 []CostItem
	Summary               string
}

// Partial reports whether some bought item has no price.
func (c WeekCost) Partial() bool { return c.ItemsPriced < c.ItemsBought }

// WeekSavings is the savings history.
type WeekSavings struct {
	Weeks           []WeekCost
	TotalSavedCents *int64
	WeeksCounted    int
	MealKit         *households.MealKit
}

// WeekCost computes a week's grocery cost.
func (s *Service) WeekCost(ctx context.Context, householdID, week string) (WeekCost, error) {
	if householdID == "" {
		return WeekCost{}, errHouseholdRequired
	}
	w, err := planning.ParseWeek(week)
	if err != nil {
		return WeekCost{}, err
	}
	kit, err := s.mealKit(ctx, householdID)
	if err != nil {
		return WeekCost{}, err
	}
	return s.weekCost(ctx, householdID, w, kit)
}

func (s *Service) mealKit(ctx context.Context, householdID string) (*households.MealKit, error) {
	if s.households == nil {
		return nil, nil
	}
	hh, err := s.households.GetHousehold(ctx, householdID)
	if err != nil {
		return nil, fmt.Errorf("get household: %w", err)
	}
	return hh.MealKit, nil
}

// costInput is everything computeWeekCost reads, loaded by weekCost.
type costInput struct {
	week     string
	handoffs []Handoff
	// purchases and stock are keyed by purchase ID.
	purchases map[string]pantry.Purchase
	stock     map[string]pantry.StockValue
	spend     *WeekSpend
	meals     int
	earlier   int64
	kit       *households.MealKit
}

func (s *Service) weekCost(ctx context.Context, householdID string, w planning.Week, kit *households.MealKit) (WeekCost, error) {
	in := costInput{week: w.String(), kit: kit, purchases: map[string]pantry.Purchase{}, stock: map[string]pantry.StockValue{}}
	var err error
	if in.handoffs, err = s.store.ListHandoffs(ctx, householdID, HandoffFilter{Week: in.week, Limit: MaxHandoffList}); err != nil {
		return WeekCost{}, fmt.Errorf("list the week's handoffs: %w", err)
	}
	spend, err := s.store.GetWeekSpend(ctx, householdID, in.week)
	switch {
	case err == nil:
		in.spend = &spend
	case !errors.Is(err, ErrNotFound):
		return WeekCost{}, fmt.Errorf("get week spend: %w", err)
	}

	var purchaseIDs []string
	handoffIDs := map[string]bool{}
	for _, h := range in.handoffs {
		handoffIDs[h.ID] = true
		for _, l := range h.Lines {
			if l.Status == LineConfirmed && l.PurchaseID != "" {
				purchaseIDs = append(purchaseIDs, l.PurchaseID)
			}
		}
	}
	if s.pantry != nil && len(purchaseIDs) > 0 {
		list, err := s.pantry.PurchasesByIDs(ctx, householdID, purchaseIDs)
		if err != nil {
			return WeekCost{}, err
		}
		for _, p := range list {
			in.purchases[p.ID] = p
			stock, err := s.pantry.PurchaseStock(ctx, p)
			if err != nil && !isNotFound(err) {
				return WeekCost{}, fmt.Errorf("estimate pantry stock: %w", err)
			}
			in.stock[p.ID] = stock
		}
	}

	if s.plans != nil {
		plan, err := s.plans.Get(ctx, householdID, in.week)
		if err != nil {
			return WeekCost{}, fmt.Errorf("get the week's plan: %w", err)
		}
		var entryIDs []string
		for _, e := range plan.Entries {
			if !e.RecipeIsAddon {
				in.meals++
			}
			entryIDs = append(entryIDs, e.ID)
		}
		if s.pantry != nil && len(entryIDs) > 0 {
			if in.earlier, err = s.earlierStockUsed(ctx, householdID, in.week, handoffIDs, entryIDs); err != nil {
				return WeekCost{}, err
			}
		}
	}
	return computeWeekCost(in), nil
}

// earlierStockUsed values what this week's cooked meals took from pantry
// purchases made for other weeks: each deduction × its purchase's price per
// unit. Purchases from this week's handoffs are already counted by the week's
// items.
func (s *Service) earlierStockUsed(ctx context.Context, householdID, week string, handoffIDs map[string]bool, entryIDs []string) (int64, error) {
	cooked, err := s.pantry.CookUsageForEntries(ctx, householdID, entryIDs)
	if err != nil {
		return 0, err
	}
	type deduction struct {
		amount *big.Rat
		unit   string
	}
	byCycle := map[string][]deduction{}
	for _, u := range cooked {
		for _, l := range u.Lines {
			if l.Deducted != "" && l.CycleID != "" {
				r, ok := new(big.Rat).SetString(l.Deducted)
				if ok {
					byCycle[l.CycleID] = append(byCycle[l.CycleID], deduction{amount: r, unit: l.TrackingUnit})
				}
			}
		}
	}
	if len(byCycle) == 0 {
		return 0, nil
	}
	ids := make([]string, 0, len(byCycle))
	for id := range byCycle {
		ids = append(ids, id)
	}
	purchases, err := s.pantry.PurchasesByIDs(ctx, householdID, ids)
	if err != nil {
		return 0, err
	}
	total := new(big.Rat)
	for _, p := range purchases {
		if p.PriceCents == nil || p.Week == week || (p.Provider != nil && handoffIDs[p.Provider.HandoffID]) {
			continue
		}
		for _, d := range byCycle[p.ID] {
			bought, ok := pantry.PurchaseAmount(p, d.unit)
			if !ok || bought.Sign() <= 0 {
				continue
			}
			share := new(big.Rat).Quo(d.amount, bought)
			if share.Cmp(big.NewRat(1, 1)) > 0 {
				share.SetInt64(1)
			}
			total.Add(total, share.Mul(share, big.NewRat(*p.PriceCents, 1)))
		}
	}
	return roundCents(total), nil
}

// computeWeekCost is the arithmetic, with no I/O.
func computeWeekCost(in costInput) WeekCost {
	out := WeekCost{Week: in.week, Meals: in.meals, MealKit: in.kit, EarlierStockUsedCents: in.earlier, Items: []CostItem{}}
	var pricedSum, itemUsed, itemStocked int64
	for _, h := range in.handoffs {
		for _, l := range h.Lines {
			if l.Status != LineConfirmed {
				continue
			}
			out.ItemsBought++
			item := CostItem{HandoffID: h.ID, LineID: l.ID, IngredientKey: l.IngredientKey, Name: l.Name, Pantry: l.Pantry}
			purchase, hasPurchase := in.purchases[l.PurchaseID]
			if item.Pantry == "" {
				// Confirmed before the leftovers rule: a purchase means tracked.
				item.Pantry = PantryNotTracked
				if l.PurchaseID != "" {
					item.Pantry = PantryTracked
				}
			}
			price := l.PriceCents
			if price == nil && hasPurchase {
				price = purchase.PriceCents
			}
			var usedFraction *big.Rat
			switch left := leftoverOf(l, l.ConfirmedPackages); {
			case item.Pantry == PantryNotTracked:
				item.Usage, usedFraction = UsageWholePackage, big.NewRat(1, 1)
			case left.Measured:
				item.Usage, usedFraction = UsageMeasured, left.UsedFraction()
			default:
				item.Usage, usedFraction = UsageUnknown, big.NewRat(1, 1)
				out.UnmeasuredItems++
			}
			if price != nil {
				out.ItemsPriced++
				p := *price
				used := roundCents(new(big.Rat).Mul(usedFraction, big.NewRat(p, 1)))
				stocked := p - used
				// The pantry knows better than the plan when more is gone.
				if st, ok := in.stock[l.PurchaseID]; ok && st.Tracked && item.Usage == UsageMeasured && st.Bought.Sign() > 0 {
					left := roundCents(new(big.Rat).Mul(new(big.Rat).Quo(st.Remaining, st.Bought), big.NewRat(p, 1)))
					if left < stocked {
						stocked, used = left, p-left
					}
				}
				item.PriceCents, item.UsedCents, item.StockedCents = &p, &used, &stocked
				pricedSum += p
				itemUsed += used
				itemStocked += stocked
			}
			out.Items = append(out.Items, item)
		}
	}

	switch {
	case in.spend != nil:
		total := in.spend.OrderTotalCents
		out.OrderTotalCents = &total
		out.SpentCents, out.SpentSource = &total, SpentOrderTotal
		out.FeesAndUnpricedCents = max(0, total-pricedSum)
	case out.ItemsPriced > 0:
		out.SpentCents, out.SpentSource = &pricedSum, SpentItemPrices
	}
	if in.spend != nil || out.ItemsPriced > 0 || in.earlier > 0 {
		used := itemUsed + in.earlier + out.FeesAndUnpricedCents
		stocked := itemStocked
		out.UsedCents, out.StockedCents = &used, &stocked
		if out.Meals > 0 {
			perMeal := (used + int64(out.Meals)/2) / int64(out.Meals)
			out.CostPerMealCents = &perMeal
			if in.kit != nil {
				saved := in.kit.PerMealCents()*int64(out.Meals) - used
				out.SavedCents = &saved
			}
		}
	}
	out.Summary = costSummary(out)
	return out
}

// costSummary says what the figures are based on.
func costSummary(c WeekCost) string {
	var parts []string
	switch {
	case c.ItemsBought == 0 && c.OrderTotalCents != nil:
		parts = append(parts, "Based on the order total; add item prices to see what's stocked for later.")
	case c.ItemsBought == 0:
	case c.ItemsPriced == 0 && c.OrderTotalCents != nil:
		parts = append(parts, fmt.Sprintf("No prices yet for the %s; the whole order total counts as used this week.", countWord(c.ItemsBought, "item", "items")))
	case c.ItemsPriced == 0:
		parts = append(parts, "Add prices to see what this week cost.")
	case c.ItemsPriced == c.ItemsBought:
		parts = append(parts, fmt.Sprintf("Based on prices for all %s.", countWord(c.ItemsBought, "item", "items")))
	default:
		parts = append(parts, fmt.Sprintf("Based on %d of %d items with prices.", c.ItemsPriced, c.ItemsBought))
	}
	if c.FeesAndUnpricedCents > 0 && c.ItemsPriced > 0 {
		parts = append(parts, fmt.Sprintf("Fees, tax, and unpriced items (%s) count as used.", dollars(c.FeesAndUnpricedCents)))
	}
	if c.EarlierStockUsedCents > 0 {
		parts = append(parts, fmt.Sprintf("Includes %s of pantry stock bought earlier.", dollars(c.EarlierStockUsedCents)))
	}
	if n := c.UnmeasuredItems; n > 0 && c.ItemsPriced > 0 {
		parts = append(parts, fmt.Sprintf("%s couldn't be measured and count as used.", countWord(n, "item", "items")))
	}
	return strings.Join(parts, " ")
}

func countWord(n int, singular, plural string) string {
	if n == 1 {
		return "1 " + singular
	}
	return fmt.Sprintf("%d %s", n, plural)
}

func dollars(cents int64) string {
	return fmt.Sprintf("$%d.%02d", cents/100, cents%100)
}

// roundCents rounds a non-negative amount of cents to the nearest cent.
func roundCents(r *big.Rat) int64 {
	scaled := new(big.Rat).Add(r, big.NewRat(1, 2))
	return new(big.Int).Quo(scaled.Num(), scaled.Denom()).Int64()
}

// Savings returns recent weeks' costs, newest first, and what they saved
// against the household's meal kit. A week is included when it has a
// confirmed order or an order total.
func (s *Service) Savings(ctx context.Context, householdID string, limit int) (WeekSavings, error) {
	if householdID == "" {
		return WeekSavings{}, errHouseholdRequired
	}
	switch {
	case limit <= 0:
		limit = DefaultSavingsWeeks
	case limit > MaxSavingsWeeks:
		limit = MaxSavingsWeeks
	}
	kit, err := s.mealKit(ctx, householdID)
	if err != nil {
		return WeekSavings{}, err
	}
	weeks := map[string]bool{}
	handoffs, err := s.store.ListHandoffs(ctx, householdID, HandoffFilter{Limit: MaxHandoffList})
	if err != nil {
		return WeekSavings{}, fmt.Errorf("list handoffs: %w", err)
	}
	for _, h := range handoffs {
		for _, l := range h.Lines {
			if l.Status == LineConfirmed {
				weeks[h.Week] = true
				break
			}
		}
	}
	spends, err := s.store.ListWeekSpend(ctx, householdID, limit)
	if err != nil {
		return WeekSavings{}, fmt.Errorf("list week spend: %w", err)
	}
	for _, sp := range spends {
		weeks[sp.Week] = true
	}
	ordered := make([]string, 0, len(weeks))
	for w := range weeks {
		ordered = append(ordered, w)
	}
	slices.Sort(ordered)
	slices.Reverse(ordered)
	if len(ordered) > limit {
		ordered = ordered[:limit]
	}

	out := WeekSavings{MealKit: kit, Weeks: []WeekCost{}}
	var total int64
	for _, week := range ordered {
		w, err := planning.ParseWeek(week)
		if err != nil {
			continue
		}
		c, err := s.weekCost(ctx, householdID, w, kit)
		if err != nil {
			return WeekSavings{}, err
		}
		out.Weeks = append(out.Weeks, c)
		if c.SavedCents != nil {
			total += *c.SavedCents
			out.WeeksCounted++
		}
	}
	if out.WeeksCounted > 0 {
		out.TotalSavedCents = &total
	}
	return out, nil
}
