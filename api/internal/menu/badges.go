package menu

// Badge codes form a closed set, so the app can map each to an icon.
const (
	BadgeQuick          = "quick"
	BadgeMakeAgain      = "make_again"
	BadgeKidFavorite    = "kid_favorite"
	BadgeNew            = "new"
	BadgeTopRated       = "top_rated"
	BadgeAutopilotPick  = "autopilot_pick"
	BadgeSmokerFriendly = "smoker_friendly"
	BadgeOftenOrdered   = "often_ordered"
)

// Badge rules.
const (
	maxBadges         = 2
	topRatedAverage   = 4.5
	oftenOrderedTimes = 5
)

// Badge is a short label on a card.
type Badge struct {
	Code string
	Text string
}

// badgeOrder is the priority order: what the household said first, then
// Autopilot and equipment, then history and facts.
var badgeOrder = []string{
	BadgeMakeAgain, BadgeTopRated, BadgeKidFavorite, BadgeAutopilotPick, BadgeSmokerFriendly, BadgeOftenOrdered, BadgeQuick, BadgeNew,
}

var badgeTexts = map[string]string{
	BadgeQuick: "Quick", BadgeMakeAgain: "Make Again", BadgeKidFavorite: "Kid Favorite", BadgeNew: "New",
	BadgeTopRated: "Top Rated", BadgeAutopilotPick: "Autopilot Pick", BadgeSmokerFriendly: "Smoker Friendly", BadgeOftenOrdered: "Often Ordered",
}

// hasBadge reports whether a recipe earns a badge:
//   - make_again, kid_favorite: any member's rating has the tag;
//   - top_rated: household average ≥ 4.5;
//   - autopilot_pick: in the week's pending proposal, or added to the week's
//     plan from a proposal;
//   - smoker_friendly: the household has a smoker and the recipe suits it
//     (heuristic or override);
//   - often_ordered: ordered 5+ times;
//   - quick: known cook time within the quick limit;
//   - new: never ordered or cooked.
func (s *snapshot) hasBadge(it *item, code string) bool {
	switch code {
	case BadgeMakeAgain:
		return it.makeAgain
	case BadgeTopRated:
		avg, ok := it.rating.Average()
		return ok && avg >= topRatedAverage
	case BadgeKidFavorite:
		return it.kidFavorite
	case BadgeAutopilotPick:
		return s.autopilotPicks[it.recipe.ID]
	case BadgeSmokerFriendly:
		return s.hasSmoker && it.attrs.Suits("smoker")
	case BadgeOftenOrdered:
		return it.recipe.TimesOrdered >= oftenOrderedTimes
	case BadgeQuick:
		return it.cook > 0 && it.cook <= s.quick
	case BadgeNew:
		return it.isNew()
	}
	return false
}

// badges returns at most maxBadges badges in priority order. suppress is a
// code that would repeat the section title ("quick" in Quick & Easy), and
// promote a code that leads when earned (smoker_friendly in a smoker rule's
// section).
func (s *snapshot) badges(it *item, suppress, promote string) []Badge {
	out := []Badge{}
	if promote != "" && promote != suppress && s.hasBadge(it, promote) {
		out = append(out, Badge{Code: promote, Text: badgeTexts[promote]})
	}
	for _, code := range badgeOrder {
		if len(out) == maxBadges {
			break
		}
		if code == promote || code == suppress || !s.hasBadge(it, code) {
			continue
		}
		out = append(out, Badge{Code: code, Text: badgeTexts[code]})
	}
	return out
}
