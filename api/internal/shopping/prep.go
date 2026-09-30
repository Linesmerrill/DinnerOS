package shopping

import (
	"context"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/pantry"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/providers"
)

// This file holds the prep plan: the guided "you just got the groceries"
// checklist that turns a week's bulk packs into portions
// (docs/shopping-providers.md#the-prep-plan).
//
// Bulk packs (bulkpack.go) already say "you bought four pounds and the week
// needs ten ounces", and the freezer (pantry/freezer.go) already records a
// remainder. What was missing is the half-hour between the two: standing at
// the counter on Sunday with a 4 lb pork loin and a knife, deciding how many
// pieces to cut it into.
//
// The prep session answers that with one card per pack:
//
//   - what the week's meals actually need, and which meals, by name and day;
//   - what is left over once they have it;
//   - how many portions to cut the rest into, and how big each one is;
//   - how long one of those portions takes to thaw — which is the number the
//     old "Freeze the Rest" button got wrong, because it recorded the whole
//     surplus as a single portion and then quoted a thaw time for the bag.
//
// Finishing a card does the bookkeeping: the reserved amount is simply not
// frozen, so it stays in the fridge for the meal that needs it, and the rest
// becomes frozen pantry stock with the portion count the member chose. The
// freeze is the existing endpoint's, so it carries the existing endpoint's
// idempotency: the handoff line is named, and sealing it twice changes
// nothing.
//
// Only the card's answer is stored (prep_card_states). Everything else —
// which lines are packs, what the week needs, the suggested portions — is
// derived on read from the handoff and the plan, like the order reminder and
// the thaw reminder before it. A member who never opens the session loses
// nothing, and one who opens it on Wednesday sees the same cards, with the
// meals that have already been and gone marked as past.

// MaxPrepPortions bounds the portion counts a card suggests and offers in
// its stepper. Twelve is well past any sensible number of dinners from one
// pack; a member who really wants more can freeze by hand, up to
// pantry.MaxPortions.
const MaxPrepPortions = 12

// PrepKind says what a card asks the member to do. Only bulk packs exist
// today; the field is here so a card for something else (herbs to wash, a
// batch to make) can join the same checklist without changing its shape.
type PrepKind string

// Prep card kinds.
const (
	// PrepBulkPack: a pack far bigger than the week needs, to portion.
	PrepBulkPack PrepKind = "bulk_pack"
)

// PrepStatus is one card's answer.
type PrepStatus string

// Prep card statuses.
const (
	PrepPending PrepStatus = "pending"
	PrepDone    PrepStatus = "done"
	// PrepSkipped: the member said "not this one". It stays in the list and
	// can still be done later.
	PrepSkipped PrepStatus = "skipped"
)

// PrepState summarizes a session.
type PrepState string

// Prep session states.
const (
	// PrepNothingToDo: the week bought nothing that needs prepping. This is
	// an ordinary, good outcome, not an error or an empty screen.
	PrepNothingToDo PrepState = "nothing_to_prep"
	// PrepReady: at least one card is still unanswered.
	PrepReady PrepState = "ready"
	// PrepFinished: every card is done or skipped.
	PrepFinished PrepState = "finished"
)

// PortionBasis says where one portion's size came from.
type PortionBasis string

// Portion bases.
const (
	// BasisMeal: a portion is one meal's worth — the week's need for this
	// ingredient divided by the meals that need it. This is the usual case,
	// and the reason the count is not a guess: the household's own recipes
	// say how much of this ingredient a dinner takes.
	BasisMeal PortionBasis = "meal"
	// BasisWeek: the line records no recipes (an extra a member typed, or a
	// handoff stored before recipes were kept), so the week's whole need
	// stands in for one meal.
	BasisWeek PortionBasis = "week"
)

// PrepReminder is what the app may honestly promise when a card is finished.
type PrepReminder string

// Prep reminders. Each value names a reminder that exists; there is no value
// for "we'll remember", because nothing in the system does that in general.
const (
	// PrepReminderNone: nothing is being frozen, so nothing is promised.
	PrepReminderNone PrepReminder = "none"
	// PrepReminderThaw: a planned meal that needs it is still ahead, so the
	// hourly thaw sweep will remind the household that morning
	// (docs/pantry-usage.md#thaw-reminders).
	PrepReminderThaw PrepReminder = "thaw"
	// PrepReminderList: nothing is planned for it yet. The freezer keeps it
	// on the list as "Grab from the freezer", and the thaw reminder starts
	// the day a meal is planned for it.
	PrepReminderList PrepReminder = "list"
)

// PrepMeal is one planned meal a card's ingredient was bought for.
type PrepMeal struct {
	RecipeID   string
	RecipeName string
	// Day is the plan's weekday code ("thu"), empty for "this week, not
	// scheduled"; Date is its calendar date, empty for the same reason.
	Day  string
	Date string
	// Past is true when Date is before today in the household's time zone:
	// the member came back to the session mid-week, and the copy must not
	// promise a Thursday that has been and gone.
	Past bool
}

// PortionPlan is the card's portioning advice: what to keep out, what to cut
// the rest into, and how long one of those pieces takes to thaw.
type PortionPlan struct {
	// Unit is the pack's unit; every amount here is in it.
	Unit string
	// Reserved is what this week's meals need and what therefore stays out
	// of the freezer. Exact ("21/2").
	Reserved string
	// Surplus is what goes in the freezer: bought less reserved.
	Surplus string
	// Meals is how many planned meals Reserved is for.
	Meals int
	// TypicalMeal is one meal's worth of this ingredient, the number the
	// portion count comes from, and Basis says how it was arrived at.
	TypicalMeal string
	Basis       PortionBasis
	// Portions is the suggested count, and PortionSize one portion's size —
	// always one dinner's worth (TypicalMeal), never the surplus split evenly:
	// a 14 oz bag thawed for a 10 oz dinner is 4 oz thrown out later.
	Portions    int
	PortionSize string
	// Frozen is what goes in the freezer (Portions × PortionSize) and Leftover
	// what is left of the surplus after whole portions — less than a dinner,
	// which no recipe in the library calls for, so the card says to toss it.
	Frozen   string
	Leftover string
	// Thaw is the estimate for one portion of PortionSize. It follows the
	// count, which is the whole point: four portions of a four-pound pack
	// thaw in a fifth of the time the pack does.
	Thaw pantry.ThawEstimate
	// Options are the counts the app offers, 1..MaxPrepPortions, each with
	// its portion size and thaw estimate, so changing the count on the card
	// never shows a thaw time that belongs to another count.
	Options []PortionOption
}

// PortionOption is one choice of portion count.
type PortionOption struct {
	Portions int
	Size     string
	// Leftover is what is left of the surplus after this many bags — what
	// the card tells the member to toss or cook in when they pick it.
	Leftover string
	Thaw     pantry.ThawEstimate
}

// PrepCard is one item to prep.
type PrepCard struct {
	// ID is "<handoffId>:<lineId>" — the same reference the freezer records
	// as FrozenFrom, which is what makes finishing a card twice a no-op.
	ID        string
	Kind      PrepKind
	HandoffID string
	Provider  providers.Key
	LineID    string
	Status    PrepStatus
	// ImageURL is the ingredient's photo — the one the recipe screen shows on
	// this ingredient's row — or empty when there is none (ingredientImages).
	ImageURL string
	// Pack is the measured pack, with its second-meal suggestions.
	Pack BulkPack
	// Meals are the planned meals the reserved amount is for.
	Meals []PrepMeal
	// Portions is the portioning advice. It is empty for a card that isn't
	// freezable: there is nothing to cut up and put away.
	Portions PortionPlan
	// Reminder is the promise the card may make, and ReminderText is it in
	// English.
	Reminder     PrepReminder
	ReminderText string
	// Instruction is the card's one line of guidance, ready to show.
	Instruction string
	// BagLabel is what to write on each bag, one line each: "Ground Pork,
	// 10 oz", "Frozen Sep 27", "Best by Dec 27". Empty when nothing is
	// being frozen or the card is already answered.
	BagLabel []string
	// Wrap is how the household freezes meat, which sets the label's best-by
	// date and the frozen item's.
	Wrap string
	// FrozenItemID, FrozenPortions, and FrozenAt describe what finishing it
	// recorded, when it froze something.
	FrozenItemID   string
	FrozenPortions int
	AnsweredBy     string
	AnsweredAt     time.Time
}

// PrepSession is a week's prep checklist.
type PrepSession struct {
	HouseholdID string
	Week        string
	State       PrepState
	// Cards are in the order the week's handoffs bought them, newest handoff
	// first. Never nil.
	Cards   []PrepCard
	Pending int
	Done    int
	Skipped int
	// Headline is ready to show, including when there is nothing to do.
	Headline  string
	UpdatedAt time.Time
}

// PrepCardState is the stored half of a card: the member's answer, and what
// it recorded. Everything else about a card is derived on read.
type PrepCardState struct {
	HouseholdID string
	Week        string
	// CardID is "<handoffId>:<lineId>".
	CardID       string
	Status       PrepStatus
	Portions     int
	FrozenItemID string
	AnsweredBy   string
	AnsweredAt   time.Time
}

// PrepInput is the body of finishing a card.
type PrepInput struct {
	// Portions overrides the suggested count when positive.
	Portions int
}

// Freezer seals a bulk pack's remainder. *pantry.Service implements it.
// Without one, a prep card is still shown and still answerable; it just
// records nothing in the freezer, which is the same bargain the rest of the
// optional dependencies here make.
type Freezer interface {
	Freeze(ctx context.Context, actor households.Membership, in pantry.FreezeInput) (pantry.FreezeResult, error)
}

// PrepSession returns the week's prep checklist.
func (s *Service) PrepSession(ctx context.Context, householdID, week string) (PrepSession, error) {
	if householdID == "" {
		return PrepSession{}, errHouseholdRequired
	}
	w, err := planning.ParseWeek(week)
	if err != nil {
		return PrepSession{}, err
	}
	return s.prepSession(ctx, householdID, w.String())
}

func (s *Service) prepSession(ctx context.Context, householdID, week string) (PrepSession, error) {
	out := PrepSession{HouseholdID: householdID, Week: week, Cards: []PrepCard{}}
	list, err := s.store.ListHandoffs(ctx, householdID, HandoffFilter{Week: week, Limit: MaxHandoffList})
	if err != nil {
		return PrepSession{}, fmt.Errorf("list the week's handoffs: %w", err)
	}
	packs, err := s.weekPacks(ctx, householdID, list)
	if err != nil {
		return PrepSession{}, err
	}
	states, err := s.prepStates(ctx, householdID, week)
	if err != nil {
		return PrepSession{}, err
	}
	meals, today, bagOz, wrap, err := s.weekMeals(ctx, householdID, week)
	if err != nil {
		return PrepSession{}, err
	}
	images := s.ingredientImages(ctx, packs)
	for _, p := range packs {
		card := prepCardFor(p.handoffID, p.provider, p.pack, meals, today, bagOz)
		card.Wrap = wrap
		card.ImageURL = images[p.pack.IngredientID()]
		if st, ok := states[card.ID]; ok {
			card.Status, card.FrozenItemID = st.Status, st.FrozenItemID
			card.AnsweredBy, card.AnsweredAt = st.AnsweredBy, st.AnsweredAt
			card.FrozenPortions = st.Portions
			if st.Status == PrepDone && st.Portions > 0 {
				card.Portions = applyPortions(card.Portions, p.pack, st.Portions)
			}
		}
		// A remainder that is already sealed is finished, however it got
		// there: the old bulk-pack sheet, another member's phone, or a
		// session this household never opened. Saying "still to do" about
		// something already in the freezer would be the one thing a
		// checklist must not do.
		if p.pack.Frozen && card.Status == PrepPending {
			card.Status = PrepDone
		}
		if card.Status == PrepPending {
			card.BagLabel = bagLabel(card, today)
		}
		if card.AnsweredAt.After(out.UpdatedAt) {
			out.UpdatedAt = card.AnsweredAt
		}
		out.Cards = append(out.Cards, card)
	}
	out.count()
	return out, nil
}

// count fills the session's tallies, state, and headline.
func (p *PrepSession) count() {
	for _, c := range p.Cards {
		switch c.Status {
		case PrepDone:
			p.Done++
		case PrepSkipped:
			p.Skipped++
		default:
			p.Pending++
		}
	}
	switch {
	case len(p.Cards) == 0:
		p.State = PrepNothingToDo
		p.Headline = "Nothing to prep this week — every pack was about the size the week needs."
	case p.Pending == 0:
		p.State = PrepFinished
		p.Headline = "Everything's put away."
	case p.Pending == 1:
		p.State = PrepReady
		p.Headline = "One thing to put away."
	default:
		p.State = PrepReady
		p.Headline = fmt.Sprintf("%d things to put away.", p.Pending)
	}
}

// Card returns the card with id.
func (p PrepSession) Card(id string) (PrepCard, bool) {
	for _, c := range p.Cards {
		if c.ID == id {
			return c, true
		}
	}
	return PrepCard{}, false
}

// weekPack is one bulk pack with the handoff it came from.
type weekPack struct {
	handoffID string
	provider  providers.Key
	pack      BulkPack
}

// weekPacks collects the week's bulk packs across every handoff, newest
// first, one per ingredient. A week that sent twice has the same pork loin
// on two handoffs; the newest one is the one whose packages were actually
// counted, and two cards for one loin would be two chances to freeze it.
func (s *Service) weekPacks(ctx context.Context, householdID string, list []Handoff) ([]weekPack, error) {
	seen := map[string]bool{}
	var out []weekPack
	for _, h := range list {
		frozen, err := s.frozenLines(ctx, householdID, h.ID)
		if err != nil {
			return nil, err
		}
		for _, l := range h.Lines {
			pack, ok := bulkPackOf(l)
			if !ok || seen[l.IngredientKey] {
				continue
			}
			seen[l.IngredientKey] = true
			pack.Frozen = frozen[l.ID]
			pack.Suggestions = s.leftoverPicks(ctx, householdID, h.Week, pack.LineSource)
			out = append(out, weekPack{handoffID: h.ID, provider: h.Provider, pack: pack})
		}
	}
	return out, nil
}

// ingredientImages maps each pack's catalog ingredient ID to its photo, with
// one catalog read for the whole session.
//
// The photo a card shows is the one the recipe screen shows on that
// ingredient's row. A recipe ingredient's image is not stored on the recipe:
// recipes.Service fills it from the catalog ingredient the line references
// (fillCategories), and a grocery line's key is that same catalog ID. So the
// catalog is the recipe ingredient's image — the same for every meal the line
// is for, which makes "first match across its meals" the catalog's one value.
// A line keyed by name (an extra a member typed) has no catalog ingredient
// and no photo, and the app shows a category glyph instead.
//
// A picture is decoration: when the catalog can't be read the cards still
// come back, without photos, rather than failing the checklist.
func (s *Service) ingredientImages(ctx context.Context, packs []weekPack) map[string]string {
	out := map[string]string{}
	if s.catalog == nil {
		return out
	}
	var ids []string
	for _, p := range packs {
		if id := p.pack.IngredientID(); id != "" && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return out
	}
	found, err := s.catalog.IngredientsByID(ctx, ids)
	if err != nil {
		s.logger.WarnContext(ctx, "prep card photos unavailable", "error", err)
		return out
	}
	for _, ing := range found {
		if url := strings.TrimSpace(ing.ImageURL); url != "" {
			out[ing.ID] = url
		}
	}
	return out
}

// prepStates reads the week's stored answers, keyed by card ID.
func (s *Service) prepStates(ctx context.Context, householdID, week string) (map[string]PrepCardState, error) {
	states, err := s.store.ListPrepCardStates(ctx, householdID, week)
	if err != nil {
		return nil, fmt.Errorf("list prep card states: %w", err)
	}
	out := make(map[string]PrepCardState, len(states))
	for _, st := range states {
		out[st.CardID] = st
	}
	return out, nil
}

// weekMeals maps a recipe ID to the planned meals that use it, and returns
// today's date in the household's time zone and the household's frozen bag
// size in ounces (nil for one dinner's worth). Without a planner the meals
// are simply unknown, and a card then reserves the week's whole need as one
// meal's worth.
func (s *Service) weekMeals(
	ctx context.Context, householdID, week string,
) (map[string][]PrepMeal, string, *int, string, error) {
	loc := time.UTC
	var bagOz *int
	wrap := households.FreezerWrapVacuum
	if s.households != nil {
		hh, err := s.orderHousehold(ctx, householdID)
		if err != nil {
			return nil, "", nil, "", err
		}
		loc, bagOz, wrap = orderLocation(hh.TimeZone), hh.FreezeMinOunces, hh.Wrap()
	}
	today := s.now().In(loc).Format(time.DateOnly)
	if s.plans == nil {
		return map[string][]PrepMeal{}, today, bagOz, wrap, nil
	}
	plan, err := s.plans.Get(ctx, householdID, week)
	if err != nil {
		return nil, "", nil, "", fmt.Errorf("load plan: %w", err)
	}
	out := map[string][]PrepMeal{}
	for _, e := range plan.Entries {
		date := plan.DateOf(e.Day)
		out[e.RecipeID] = append(out[e.RecipeID], PrepMeal{
			RecipeID: e.RecipeID, RecipeName: e.RecipeName, Day: string(e.Day), Date: date,
			Past: date != "" && date < today,
		})
	}
	return out, today, bagOz, wrap, nil
}

// prepCardFor builds one card from a measured pack.
func prepCardFor(
	handoffID string, provider providers.Key, pack BulkPack, meals map[string][]PrepMeal, today string, bagOz *int,
) PrepCard {
	card := PrepCard{
		ID: handoffID + ":" + pack.LineID, Kind: PrepBulkPack, HandoffID: handoffID, Provider: provider,
		LineID: pack.LineID, Status: PrepPending, Pack: pack, Meals: prepMeals(pack, meals),
	}
	if pack.Freezable {
		card.Portions = portionPlanFor(pack, len(card.Meals), bagOz)
	}
	card.Reminder, card.ReminderText = prepReminder(card, today)
	card.Instruction = prepInstruction(card)
	return card
}

// prepMeals are the planned meals the line's recipes are, in the week's day
// order. A recipe the plan no longer holds is left out: the card names meals
// the household can still see.
func prepMeals(pack BulkPack, byRecipe map[string][]PrepMeal) []PrepMeal {
	var out []PrepMeal
	for _, r := range pack.Recipes {
		found := byRecipe[r.ID]
		if len(found) == 0 {
			continue
		}
		out = append(out, found...)
	}
	slices.SortStableFunc(out, func(a, b PrepMeal) int {
		switch {
		// An unscheduled meal ("this week, no day yet") sorts last: it has
		// no date to compare and no morning to be reminded on.
		case a.Date == "" && b.Date != "":
			return 1
		case a.Date != "" && b.Date == "":
			return -1
		}
		return strings.Compare(a.Date, b.Date)
	})
	return out
}

// portionPlanFor is the portion-size heuristic
// (docs/shopping-providers.md#the-portion-size-heuristic).
//
// One portion is one meal's worth. The household's own recipes already say
// how much of this ingredient a dinner takes — it is the week's need for the
// line divided by the meals that need it — so the suggested count is the
// surplus measured in dinners, rounded down to whole ones. Nothing is
// invented: a four-pound loin bought for a ten-ounce Thursday becomes bags of
// ten ounces, because that is what this household cooks.
//
// bagOz, when set, is the household's own idea of a future dinner
// (households.Household.FreezeMinOunces): bags are that size instead, and
// anything less is thrown in or tossed. It only applies to a pack measured by
// weight; a count or a volume keeps the recipe's dinner.
func portionPlanFor(pack BulkPack, meals int, bagOz *int) PortionPlan {
	plan := PortionPlan{
		Unit: pack.Unit, Reserved: pack.Needed, Surplus: pack.Surplus, Meals: meals,
		Basis: BasisMeal,
	}
	mealCount := meals
	if mealCount < 1 {
		mealCount, plan.Basis = 1, BasisWeek
	}
	surplus, needed := ratOfExact(pack.Surplus), ratOfExact(pack.Needed)
	typical := new(big.Rat).Quo(needed, big.NewRat(int64(mealCount), 1))
	plan.TypicalMeal = typical.RatString()
	bag := typical
	if b, ok := ouncesIn(bagOz, pack.Unit); ok {
		bag = b
	}
	plan.PortionSize = bag.RatString()
	whole := wholePortions(surplus, bag)
	for n := 1; n <= whole && n <= MaxPrepPortions; n++ {
		frozen := new(big.Rat).Mul(bag, big.NewRat(int64(n), 1))
		plan.Options = append(plan.Options, PortionOption{
			Portions: n, Size: plan.PortionSize, Leftover: new(big.Rat).Sub(surplus, frozen).RatString(),
			Thaw: thawForPortion(pack, bag),
		})
	}
	return applyPortions(plan, pack, whole)
}

// wholePortions is how many dinners of typical the surplus holds, rounded
// down: only whole ones are worth sealing.
func wholePortions(surplus, typical *big.Rat) int {
	if typical.Sign() <= 0 || surplus.Sign() <= 0 {
		return 0
	}
	q := new(big.Rat).Quo(surplus, typical)
	n := new(big.Int).Quo(q.Num(), q.Denom()).Int64()
	if n > MaxPrepPortions {
		return MaxPrepPortions
	}
	return int(n)
}

// applyPortions sets the chosen count and what follows from it: each portion
// is one bag of PortionSize, the freezer gets count × bag, and the rest is
// leftover. A count above what the surplus holds is capped; zero means
// nothing to seal.
func applyPortions(plan PortionPlan, pack BulkPack, portions int) PortionPlan {
	surplus, bag := ratOfExact(pack.Surplus), ratOfExact(plan.PortionSize)
	if most := wholePortions(surplus, bag); portions > most {
		portions = most
	}
	if portions < 0 {
		portions = 0
	}
	plan.Portions = portions
	frozen := new(big.Rat).Mul(bag, big.NewRat(int64(portions), 1))
	plan.Frozen = frozen.RatString()
	plan.Leftover = new(big.Rat).Sub(surplus, frozen).RatString()
	plan.Thaw = thawForPortion(pack, bag)
	return plan
}

// ouncesIn converts a household's bag size in ounces to the pack's unit. ok
// is false when there is no bag size or the pack isn't measured by weight.
func ouncesIn(oz *int, unitCode string) (*big.Rat, bool) {
	if oz == nil || *oz <= 0 {
		return nil, false
	}
	to, err := ingredients.LookupUnit(unitCode)
	if err != nil || to.Kind != ingredients.KindMass {
		return nil, false
	}
	from, err := ingredients.LookupUnit("oz")
	if err != nil {
		return nil, false
	}
	q, err := ingredients.ParseQuantity(fmt.Sprint(*oz))
	if err != nil {
		return nil, false
	}
	in, err := ingredients.Convert(q, from, to)
	if err != nil {
		return nil, false
	}
	r := in.Rat()
	return r, r.Sign() > 0
}

func thawForPortion(pack BulkPack, size *big.Rat) pantry.ThawEstimate {
	return pantry.ThawFor(pantry.Item{
		DisplayName: pack.Name, Key: ingredients.NormalizeName(pack.Name), Category: pack.Category,
		Quantity: size.RatString(), Unit: pack.Unit, Portions: 1,
	})
}

func ratOfExact(exact string) *big.Rat {
	r, ok := new(big.Rat).SetString(exact)
	if !ok {
		return new(big.Rat)
	}
	return r
}

// prepReminder is the promise the card is allowed to make. There is no
// branch that says "we'll remember for you": the only reminder the system
// actually has is the thaw sweep, so the copy either names it or says what
// the list will do instead.
func prepReminder(card PrepCard, today string) (PrepReminder, string) {
	if !card.Pack.Freezable {
		return PrepReminderNone, ""
	}
	hours := card.Portions.Thaw.Summary
	for _, m := range card.Meals {
		if m.Date == "" || m.Past {
			continue
		}
		if m.Date >= today {
			return PrepReminderThaw, fmt.Sprintf(
				"We'll remind you %s morning to move a bag to the fridge. It takes %s to thaw.",
				dayName(m.Day), hours)
		}
	}
	return PrepReminderList, fmt.Sprintf(
		"When you plan it again, we'll remind you that morning to move a bag to the fridge. It takes %s to thaw.",
		hours)
}

// prepInstruction is the card's one line of guidance: what to do with the
// knife, in the amounts this household's own week produced.
func prepInstruction(card PrepCard) string {
	reserved := amountText(card.Pack.Needed, card.Pack.Unit)
	if !card.Pack.Freezable {
		if len(card.Meals) == 0 {
			return fmt.Sprintf("This week uses %s. Cook the rest again this week — it doesn't freeze back into itself.", reserved)
		}
		return fmt.Sprintf("Keep %s out for %s. Cook the rest again this week — it doesn't freeze back into itself.",
			reserved, mealsText(card.Meals))
	}
	each := amountText(card.Portions.PortionSize, card.Pack.Unit)
	keep := fmt.Sprintf("Keep %s out", reserved)
	if len(card.Meals) > 0 {
		keep = fmt.Sprintf("Keep %s out for %s", reserved, mealsText(card.Meals))
	}
	leftover := ""
	if ratOfExact(card.Portions.Leftover).Sign() > 0 {
		leftover = fmt.Sprintf(" %s left over. %s",
			amountText(card.Portions.Leftover, card.Pack.Unit), leftoverAdvice(card.Pack.Category))
	}
	switch n := card.Portions.Portions; {
	case n == 0:
		return fmt.Sprintf("%s.%s", keep, leftover)
	case n == 1:
		return fmt.Sprintf("%s, then freeze another %s in one bag for a future dinner.%s", keep, each, leftover)
	default:
		return fmt.Sprintf("%s, then freeze %d bags of %s, one per future dinner.%s", keep, n, each, leftover)
	}
}

// bagLabel is what to write on each bag frozen today, so a freezer with
// three kinds of meat in it says which is which, and the thaw reminder's
// "the Ground Pork bag dated Sep 27" matches what's on the bag.
func bagLabel(card PrepCard, today string) []string {
	if card.Portions.Portions <= 0 {
		return nil
	}
	out := []string{
		fmt.Sprintf("%s, %s", card.Pack.Name, amountText(card.Portions.PortionSize, card.Pack.Unit)),
	}
	if frozen, err := time.Parse(time.DateOnly, today); err == nil {
		out = append(out, "Frozen "+frozen.Format("Jan 2"))
	}
	if best := pantry.BestByWrapped(today, card.Pack.Name, card.Pack.Category, card.Wrap); best != "" {
		if t, err := time.Parse(time.DateOnly, best); err == nil {
			out = append(out, "Best by "+t.Format("Jan 2, 2006"))
		}
	}
	return out
}

// leftoverAdvice is what to do with less than a bag.
func leftoverAdvice(category string) string {
	if category == ingredients.CategoryMeatSeafood {
		return "Throw it in for a little more protein, or toss it."
	}
	return "Cook it in or toss it."
}

// mealsText names the meals a reserve is for, with their days: "Thursday's
// Tuscan Chicken".
func mealsText(meals []PrepMeal) string {
	parts := make([]string, 0, len(meals))
	for _, m := range meals {
		if day := dayName(m.Day); day != "" {
			parts = append(parts, day+"'s "+m.RecipeName)
			continue
		}
		parts = append(parts, m.RecipeName)
	}
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

func dayName(code string) string { return weekdayNames[code] }

// --- Answering a card ---------------------------------------------------------

// CompletePrepCard finishes a card: the week's need stays out of the freezer
// for the meals that want it, and the surplus is sealed in portions.
//
// It is idempotent in the way the freeze endpoint is: the handoff line is
// named, so doing the same card twice seals nothing twice. A member who
// redoes a card that already froze something keeps the portion count that is
// actually in the freezer — changing it here would say one thing while the
// bags in the drawer say another.
func (s *Service) CompletePrepCard(
	ctx context.Context, actor households.Membership, week, cardID string, in PrepInput,
) (PrepSession, PrepCard, error) {
	if err := authorize(actor, households.PermPantryEdit); err != nil {
		return PrepSession{}, PrepCard{}, err
	}
	if in.Portions < 0 || in.Portions > pantry.MaxPortions {
		return PrepSession{}, PrepCard{}, invalid("portions must be between 1 and %d", pantry.MaxPortions)
	}
	session, card, err := s.prepCard(ctx, actor.HouseholdID, week, cardID)
	if err != nil {
		return PrepSession{}, PrepCard{}, err
	}
	state := PrepCardState{
		HouseholdID: actor.HouseholdID, Week: session.Week, CardID: card.ID, Status: PrepDone,
		AnsweredBy: actor.UserID, AnsweredAt: s.timestamp(),
	}
	if card.Pack.Freezable {
		portions := card.Portions.Portions
		if in.Portions > 0 {
			// Never more bags than the surplus holds whole dinners of.
			portions = applyPortions(card.Portions, card.Pack, in.Portions).Portions
		}
		// Less than a dinner left over: nothing to seal, and the card is done.
		if portions > 0 {
			res, err := s.freeze(ctx, actor, card, portions)
			if err != nil {
				return PrepSession{}, PrepCard{}, err
			}
			state.FrozenItemID, state.Portions = res.Item.ID, portions
			if res.AlreadyFrozen {
				state.Portions = max(res.Item.Portions, 1)
			}
		}
	}
	return s.answer(ctx, session, state)
}

// SkipPrepCard records "not this one". Nothing is written to the pantry, and
// the card stays in the list so it can be done later — a household that
// skipped the whole session on Sunday and comes back on Wednesday finds it
// where it left it.
func (s *Service) SkipPrepCard(
	ctx context.Context, actor households.Membership, week, cardID string,
) (PrepSession, PrepCard, error) {
	if err := authorize(actor, households.PermPantryEdit); err != nil {
		return PrepSession{}, PrepCard{}, err
	}
	session, card, err := s.prepCard(ctx, actor.HouseholdID, week, cardID)
	if err != nil {
		return PrepSession{}, PrepCard{}, err
	}
	return s.answer(ctx, session, PrepCardState{
		HouseholdID: actor.HouseholdID, Week: session.Week, CardID: card.ID, Status: PrepSkipped,
		AnsweredBy: actor.UserID, AnsweredAt: s.timestamp(),
	})
}

// prepCard loads the week's session and finds one card in it.
func (s *Service) prepCard(ctx context.Context, householdID, week, cardID string) (PrepSession, PrepCard, error) {
	w, err := planning.ParseWeek(week)
	if err != nil {
		return PrepSession{}, PrepCard{}, err
	}
	if _, _, ok := splitFrozenFrom(cardID); !ok {
		return PrepSession{}, PrepCard{}, invalid("cardId must be a handoff and a line")
	}
	session, err := s.prepSession(ctx, householdID, w.String())
	if err != nil {
		return PrepSession{}, PrepCard{}, err
	}
	card, ok := session.Card(cardID)
	if !ok {
		return PrepSession{}, PrepCard{}, ErrNotFound
	}
	return session, card, nil
}

// answer stores one card's answer and returns the session as it now stands.
func (s *Service) answer(ctx context.Context, session PrepSession, state PrepCardState) (PrepSession, PrepCard, error) {
	if err := s.store.PutPrepCardState(ctx, state); err != nil {
		return PrepSession{}, PrepCard{}, fmt.Errorf("store prep card state: %w", err)
	}
	s.logger.InfoContext(ctx, "prep card answered",
		"householdId", state.HouseholdID, "week", state.Week, "cardId", state.CardID,
		"status", string(state.Status), "portions", state.Portions)
	updated, err := s.prepSession(ctx, state.HouseholdID, session.Week)
	if err != nil {
		return PrepSession{}, PrepCard{}, err
	}
	card, _ := updated.Card(state.CardID)
	return updated, card, nil
}

// freeze seals the card's surplus through the pantry's own freeze, so the
// record, the usage cycle, and the idempotency are all the existing ones.
func (s *Service) freeze(
	ctx context.Context, actor households.Membership, card PrepCard, portions int,
) (pantry.FreezeResult, error) {
	if s.freezer == nil {
		return pantry.FreezeResult{}, nil
	}
	res, err := s.freezer.Freeze(ctx, actor, pantry.FreezeInput{
		IngredientID: card.Pack.IngredientID(), Name: card.Pack.Name,
		// Only the whole portions go in: the leftover is less than a dinner.
		Quantity: frozenAmount(card.Portions, portions), Unit: card.Pack.Unit, Portions: portions,
		Source: &pantry.FreezeSource{
			Provider: string(card.Provider), HandoffID: card.HandoffID, LineID: card.LineID,
		},
		KeptOutThrough: keptOutThrough(card.Meals, s.timestamp()),
		Wrap:           card.Wrap,
	})
	if err != nil {
		return pantry.FreezeResult{}, fmt.Errorf("freeze the remainder: %w", err)
	}
	return res, nil
}

// keptOutThrough is the last day of the meals the card kept a fresh portion
// out for: the latest planned date, or a week from now when a meal has no
// day. Until then the frozen bag is for later weeks.
func keptOutThrough(meals []PrepMeal, now time.Time) string {
	last := ""
	for _, m := range meals {
		if m.Date == "" {
			return now.AddDate(0, 0, 6).Format(time.DateOnly)
		}
		last = max(last, m.Date)
	}
	return last
}

// frozenAmount is what sealing count portions puts in the freezer.
func frozenAmount(plan PortionPlan, count int) string {
	return new(big.Rat).Mul(ratOfExact(plan.PortionSize), big.NewRat(int64(count), 1)).RatString()
}
