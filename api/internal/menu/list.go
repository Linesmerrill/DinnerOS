package menu

import (
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot"
	"github.com/Linesmerrill/DinnerOS/api/internal/recommendations"
)

// Sort orders the All Meals list. Ties always break by name
// (case-insensitive), then ID.
type Sort string

// Sorts.
const (
	// SortRecommended is favorite score + taste fit, best first; recipes that
	// break a hard restriction are lowered.
	SortRecommended Sort = "recommended"
	// SortPopular is most ordered first.
	SortPopular Sort = "popular"
	// SortRecent is most recently ordered first; never-ordered last.
	SortRecent Sort = "recent"
	// SortQuick is shortest cook time first; unknown times last.
	SortQuick Sort = "quick"
	// SortName is alphabetical.
	SortName Sort = "name"
)

// List limits.
const (
	DefaultListLimit = 20
	MaxListLimit     = 50
	maxTextLength    = 100
	unknownMinutes   = math.MaxInt32
)

// ListQuery selects a page of the All Meals list.
type ListQuery struct {
	// Search matches name or headline text, case-insensitively.
	Search string
	// Protein is an Autopilot protein value ("chicken").
	Protein string
	// Cuisine matches a recipe's canonical cuisines and their regions.
	Cuisine string
	// Tag matches canonical tags.
	Tag string
	// MaxMinutes, when positive, keeps recipes with a known cook time at most
	// this long.
	MaxMinutes int
	// Addons lists add-ons instead of main meals.
	Addons bool
	Sort   Sort
	// Week is the week inPlan refers to; "" is the current week.
	Week string
	// Limit defaults to DefaultListLimit and is capped at MaxListLimit.
	Limit int
	// Cursor is the previous page's NextCursor.
	Cursor string
}

// ListPage is one page of cards.
type ListPage struct {
	Cards []Card
	// NextCursor is "" on the last page.
	NextCursor string
	Bands      autopilot.TimeBands
}

// Recipes returns a page of the All Meals list. Every page reads the catalog
// again and sorts in memory; the cursor is the last item's sort key, so pages
// stay in order even when recipes change between requests.
func (s *Service) Recipes(ctx context.Context, householdID, userID string, q ListQuery) (ListPage, error) {
	q, after, err := q.validate()
	if err != nil {
		return ListPage{}, err
	}
	current, loc, first, err := s.clock(ctx, householdID)
	if err != nil {
		return ListPage{}, err
	}
	w, err := parseWeek(q.Week, current)
	if err != nil {
		return ListPage{}, err
	}
	in, err := s.load(ctx, householdID, userID, w, current, loc, first)
	if err != nil {
		return ListPage{}, err
	}
	return newSnapshot(in).list(q, after), nil
}

func (q ListQuery) validate() (ListQuery, *listKey, error) {
	if q.Sort == "" {
		q.Sort = SortRecommended
	}
	switch q.Sort {
	case SortRecommended, SortPopular, SortRecent, SortQuick, SortName:
	default:
		return q, nil, invalidf("sort must be recommended, popular, recent, quick, or name")
	}
	switch {
	case q.Limit < 0:
		return q, nil, invalidf("limit must be a positive integer")
	case q.Limit == 0:
		q.Limit = DefaultListLimit
	case q.Limit > MaxListLimit:
		q.Limit = MaxListLimit
	}
	if q.MaxMinutes < 0 {
		return q, nil, invalidf("maxMinutes must be a positive integer")
	}
	q.Search, q.Cuisine, q.Tag = strings.TrimSpace(q.Search), strings.TrimSpace(q.Cuisine), strings.TrimSpace(q.Tag)
	q.Protein = strings.ToLower(strings.TrimSpace(q.Protein))
	for _, v := range []string{q.Search, q.Protein, q.Cuisine, q.Tag} {
		if utf8.RuneCountInString(v) > maxTextLength {
			return q, nil, invalidf("q, protein, cuisine, and tag must be at most %d characters", maxTextLength)
		}
	}
	if q.Protein != "" && !slices.ContainsFunc(recommendations.ProteinOptions, func(o recommendations.Option) bool { return o.Value == q.Protein }) {
		values := make([]string, 0, len(recommendations.ProteinOptions))
		for _, o := range recommendations.ProteinOptions {
			values = append(values, o.Value)
		}
		return q, nil, invalidf("protein must be one of %s", strings.Join(values, ", "))
	}
	if q.Cursor == "" {
		return q, nil, nil
	}
	key, err := decodeCursor(q.Cursor, q.Sort)
	if err != nil {
		return q, nil, err
	}
	return q, &key, nil
}

func (s *snapshot) list(q ListQuery, after *listKey) ListPage {
	search := strings.ToLower(q.Search)
	var cuisine, tag string
	if q.Cuisine != "" {
		cuisine = recommendations.CanonicalCuisine(q.Cuisine)
	}
	if q.Tag != "" {
		tag = recommendations.CanonicalTag(q.Tag)
	}
	var matches []*item
	keys := map[*item]listKey{}
	for _, it := range s.items {
		switch {
		case it.recipe.IsAddon != q.Addons,
			search != "" && !strings.Contains(strings.ToLower(it.recipe.Name), search) && !strings.Contains(strings.ToLower(it.recipe.Headline), search),
			q.Protein != "" && !slices.Contains(it.attrs.Proteins, q.Protein),
			cuisine != "" && !slices.Contains(it.withRegions, cuisine),
			tag != "" && !slices.Contains(it.attrs.Tags, tag),
			q.MaxMinutes > 0 && (it.cook <= 0 || it.cook > q.MaxMinutes):
			continue
		}
		matches = append(matches, it)
		keys[it] = s.keyOf(it)
	}
	slices.SortFunc(matches, func(a, b *item) int { return compareKeys(q.Sort, keys[a], keys[b]) })

	start := 0
	if after != nil {
		start = len(matches)
		for i, it := range matches {
			if compareKeys(q.Sort, keys[it], *after) > 0 {
				start = i
				break
			}
		}
	}
	end := min(start+q.Limit, len(matches))
	page := ListPage{Cards: []Card{}, Bands: s.bands}
	for _, it := range matches[start:end] {
		page.Cards = append(page.Cards, s.card(it, s.listReason(it), "", ""))
	}
	if end < len(matches) {
		page.NextCursor = encodeCursor(q.Sort, keys[matches[end-1]])
	}
	return page
}

// recommendedScore is the favorite score plus taste fit, lowered for recipes
// that break a hard restriction.
func (s *snapshot) recommendedScore(it *item) float64 {
	score := it.favorite + it.fit
	if it.restricted {
		score -= restrictedPenalty
	}
	return round6(score)
}

func (s *snapshot) listReason(it *item) string {
	if r := favoriteReason(it); r != "" {
		return r
	}
	if it.cook > 0 && it.cook <= s.quick {
		return fmt.Sprintf("Ready in %d min", it.cook)
	}
	return it.fitReason
}

// listKey is an item's position in every list order.
type listKey struct {
	Score   float64 `json:"v,omitempty"`
	Times   int     `json:"t,omitempty"`
	Week    string  `json:"w,omitempty"`
	Minutes int     `json:"m,omitempty"`
	// Name is lowercased.
	Name string `json:"n"`
	ID   string `json:"i"`
}

func (s *snapshot) keyOf(it *item) listKey {
	k := listKey{
		Score: s.recommendedScore(it), Times: it.recipe.TimesOrdered, Week: it.recipe.LastOrderedWeek,
		Minutes: it.cook, Name: strings.ToLower(it.recipe.Name), ID: it.recipe.ID,
	}
	if k.Minutes <= 0 {
		k.Minutes = unknownMinutes
	}
	return k
}

func compareKeys(sort Sort, a, b listKey) int {
	var c int
	switch sort {
	case SortRecommended:
		c = cmp.Compare(b.Score, a.Score)
	case SortPopular:
		c = cmp.Compare(b.Times, a.Times)
	case SortRecent:
		c = cmp.Compare(b.Week, a.Week)
	case SortQuick:
		c = cmp.Compare(a.Minutes, b.Minutes)
	}
	if c != 0 {
		return c
	}
	if c = cmp.Compare(a.Name, b.Name); c != 0 {
		return c
	}
	return cmp.Compare(a.ID, b.ID)
}

type cursorPayload struct {
	Sort Sort    `json:"s"`
	Key  listKey `json:"k"`
}

func encodeCursor(sort Sort, key listKey) string {
	b, _ := json.Marshal(cursorPayload{Sort: sort, Key: key})
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(s string, sort Sort) (listKey, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return listKey{}, invalidf("cursor is invalid")
	}
	var c cursorPayload
	if err := json.Unmarshal(b, &c); err != nil || c.Key.ID == "" {
		return listKey{}, invalidf("cursor is invalid")
	}
	if c.Sort != sort {
		return listKey{}, invalidf("cursor belongs to a different sort")
	}
	return c.Key, nil
}
