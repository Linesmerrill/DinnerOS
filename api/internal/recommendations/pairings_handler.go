package recommendations

import (
	"fmt"
	"math"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

// mountPairings registers the pairing routes inside Mount's authenticated
// group.
func (h *Handler) mountPairings(r chi.Router, view, edit func(http.Handler) http.Handler) {
	const week = "/households/{householdId}/autopilot/weeks/{week}/pairings"
	r.With(view).Get(week, h.weekPairings)
	r.With(edit).Post(week+"/accept", h.acceptPairing)
	r.With(edit).Post(week+"/dismiss", h.dismissPairing)
	r.With(edit).Post(week+"/rules", h.makePairingRule)
	r.With(edit).Delete(week+"/grocery-items/{itemId}", h.removePairingGroceryItem)
	r.With(view).Get("/households/{householdId}/recipes/{recipeId}/pairings", h.recipePairings)
}

// --- wire types -------------------------------------------------------------------

// GroceryItemJSON is a grocery item that isn't a recipe.
type GroceryItemJSON struct {
	Name string `json:"name"`
	// Quantity and Unit are null when the item has no amount.
	Quantity *float64 `json:"quantity"`
	Unit     *string  `json:"unit"`
}

// PairingWhenJSON is which meals a pairing rule applies to.
type PairingWhenJSON struct {
	MealCategories []string `json:"mealCategories"`
	Cuisines       []string `json:"cuisines"`
	Tags           []string `json:"tags"`
	Proteins       []string `json:"proteins"`
}

// PairingRuleTargetJSON is what a rule adds: recipeId or groceryItem.
type PairingRuleTargetJSON struct {
	// Kind is recipe or grocery_item; ignored in requests.
	Kind     string  `json:"kind"`
	RecipeID *string `json:"recipeId"`
	// RecipeName is read-only.
	RecipeName  *string          `json:"recipeName"`
	GroceryItem *GroceryItemJSON `json:"groceryItem"`
}

// PairingRuleJSON is a household pairing rule.
type PairingRuleJSON struct {
	// ID is null (or absent) for a new rule in a request.
	ID        *string               `json:"id"`
	Label     string                `json:"label"`
	When      PairingWhenJSON       `json:"when"`
	Add       PairingRuleTargetJSON `json:"add"`
	Frequency string                `json:"frequency"`
}

// PairingRecipeJSON is an add-on recipe offered as a pairing: a recipe
// summary without ratings.
type PairingRecipeJSON struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Headline        string   `json:"headline,omitempty"`
	ImageURL        string   `json:"imageUrl,omitempty"`
	CookMinutes     *int     `json:"cookMinutes,omitempty"`
	TimesOrdered    int      `json:"timesOrdered"`
	LastOrderedWeek string   `json:"lastOrderedWeek,omitempty"`
	IsAddon         bool     `json:"isAddon"`
	Tags            []string `json:"tags"`
}

// PairingTargetJSON is what a pairing adds.
type PairingTargetJSON struct {
	Kind        string             `json:"kind"`
	Recipe      *PairingRecipeJSON `json:"recipe,omitempty"`
	GroceryItem *GroceryItemJSON   `json:"groceryItem,omitempty"`
}

// LearnedPairingJSON is the history behind a learned pairing.
type LearnedPairingJSON struct {
	WeeksTogether     int     `json:"weeksTogether"`
	MealCategoryWeeks int     `json:"mealCategoryWeeks"`
	OtherWeeks        int     `json:"otherWeeks"`
	OtherWeeksRate    float64 `json:"otherWeeksRate"`
}

// PairingJSON is a pairing suggested with a meal.
type PairingJSON struct {
	Key    string            `json:"key"`
	Target PairingTargetJSON `json:"target"`
	// Servings is the add-on's serving size; null for a grocery item.
	Servings     *int                `json:"servings"`
	Source       string              `json:"source"`
	Frequency    string              `json:"frequency"`
	MealCategory *string             `json:"mealCategory"`
	Confidence   *float64            `json:"confidence"`
	Learned      *LearnedPairingJSON `json:"learned"`
	Reason       string              `json:"reason"`
	RuleID       *string             `json:"ruleId"`
	InPlan       bool                `json:"inPlan"`
	CanMakeRule  bool                `json:"canMakeRule"`
}

// ProposalPairingJSON is a pairing on a proposal slot.
type ProposalPairingJSON struct {
	ID string `json:"id"`
	PairingJSON
	Included bool `json:"included"`
}

// MealPairingsJSON is the pairings for one planned meal.
type MealPairingsJSON struct {
	EntryID        string             `json:"entryId"`
	Day            *planning.Day      `json:"day"`
	Date           *string            `json:"date"`
	Recipe         ProposalRecipeJSON `json:"recipe"`
	Servings       int                `json:"servings"`
	MealCategories []string           `json:"mealCategories"`
	Pairings       []PairingJSON      `json:"pairings"`
}

// PairingGroceryItemJSON is a paired grocery item on the week's list.
type PairingGroceryItemJSON struct {
	ID          string             `json:"id"`
	Key         string             `json:"key"`
	GroceryItem GroceryItemJSON    `json:"groceryItem"`
	EntryID     string             `json:"entryId"`
	Recipe      ProposalRecipeJSON `json:"recipe"`
	Source      string             `json:"source"`
	RuleID      *string            `json:"ruleId"`
	Text        string             `json:"text"`
	AddedBy     string             `json:"addedBy"`
	AddedAt     time.Time          `json:"addedAt"`
}

// WeekPairingsResponse is a week's pairings.
type WeekPairingsResponse struct {
	Week         string                   `json:"week"`
	StartDate    string                   `json:"startDate"`
	EndDate      string                   `json:"endDate"`
	Meals        []MealPairingsJSON       `json:"meals"`
	GroceryItems []PairingGroceryItemJSON `json:"groceryItems"`
}

// PairingAcceptResponse is returned by accepting a pairing.
type PairingAcceptResponse struct {
	Status      string                  `json:"status"`
	Pairing     PairingJSON             `json:"pairing"`
	Entry       *planning.EntryResponse `json:"entry"`
	GroceryItem *PairingGroceryItemJSON `json:"groceryItem"`
	Plan        planning.PlanResponse   `json:"plan"`
}

// MakePairingRuleResponse is returned by turning a pairing into a rule.
type MakePairingRuleResponse struct {
	Status  string          `json:"status"`
	Rule    PairingRuleJSON `json:"rule"`
	Profile ProfileResponse `json:"profile"`
}

// RecipePairingsResponse is what goes well with a recipe.
type RecipePairingsResponse struct {
	RecipeID       string        `json:"recipeId"`
	Week           *string       `json:"week"`
	EntryID        *string       `json:"entryId"`
	MealCategories []string      `json:"mealCategories"`
	Items          []PairingJSON `json:"items"`
}

// AddedPairingJSON is a pairing accepting a proposal added.
type AddedPairingJSON struct {
	ID          string                  `json:"id"`
	SlotID      string                  `json:"slotId"`
	Pairing     PairingJSON             `json:"pairing"`
	Entry       *planning.EntryResponse `json:"entry"`
	GroceryItem *PairingGroceryItemJSON `json:"groceryItem"`
}

// SkippedPairingJSON is a chosen pairing accepting didn't add.
type SkippedPairingJSON struct {
	ID     string `json:"id"`
	SlotID string `json:"slotId"`
	Key    string `json:"key"`
	Reason string `json:"reason"`
}

// MealCategoryJSON says whether a recipe is in a meal category.
type MealCategoryJSON struct {
	Category       string `json:"category"`
	Label          string `json:"label"`
	Suits          bool   `json:"suits"`
	Source         string `json:"source"`
	HeuristicSuits bool   `json:"heuristicSuits"`
	Evidence       string `json:"evidence,omitempty"`
}

type pairingActionRequest struct {
	EntryID string `json:"entryId"`
	Key     string `json:"key"`
}

type makePairingRuleRequest struct {
	EntryID   *string `json:"entryId"`
	SlotID    *string `json:"slotId"`
	Key       string  `json:"key"`
	Frequency *string `json:"frequency"`
}

// --- conversions ------------------------------------------------------------------

func round2(v float64) float64 { return math.Round(v*100) / 100 }

func groceryItemJSON(g GroceryItem) GroceryItemJSON {
	out := GroceryItemJSON{Name: g.Name}
	if q, err := ingredients.ParseQuantity(g.Quantity); g.Quantity != "" && err == nil {
		f, unit := q.Float64(), g.Unit
		out.Quantity, out.Unit = &f, &unit
	}
	return out
}

func (g GroceryItemJSON) item(field string) (*GroceryItem, error) {
	item := &GroceryItem{Name: g.Name}
	if g.Quantity != nil {
		q, err := ingredients.QuantityFromFloat(*g.Quantity)
		if err != nil {
			return nil, invalidf("%s.quantity must be more than 0 and at most %d", field, MaxGroceryItemQuantity)
		}
		item.Quantity = q.String()
	}
	if g.Unit != nil {
		item.Unit = *g.Unit
	}
	return item, nil
}

func pairingRulesJSON(rules []PairingRule) []PairingRuleJSON {
	out := make([]PairingRuleJSON, 0, len(rules))
	for _, r := range rules {
		out = append(out, pairingRuleJSON(r))
	}
	return out
}

func pairingRuleJSON(r PairingRule) PairingRuleJSON {
	id := r.ID
	rj := PairingRuleJSON{
		ID: &id, Label: r.Label, Frequency: r.Frequency, Add: PairingRuleTargetJSON{Kind: r.Add.Kind()},
		When: PairingWhenJSON{
			MealCategories: orEmptyStrings(r.When.MealCategories), Cuisines: orEmptyStrings(r.When.Cuisines),
			Tags: orEmptyStrings(r.When.Tags), Proteins: orEmptyStrings(r.When.Proteins),
		},
	}
	if g := r.Add.GroceryItem; g != nil {
		item := groceryItemJSON(*g)
		rj.Add.GroceryItem = &item
	} else {
		recipeID, name := r.Add.RecipeID, r.Add.RecipeName
		rj.Add.RecipeID, rj.Add.RecipeName = &recipeID, &name
	}
	return rj
}

func pairingRulesFromJSON(in []PairingRuleJSON) ([]PairingRule, error) {
	rules := make([]PairingRule, 0, len(in))
	for i, rj := range in {
		r := PairingRule{
			Label: rj.Label, Frequency: rj.Frequency,
			When: PairingWhen{MealCategories: rj.When.MealCategories, Cuisines: rj.When.Cuisines, Tags: rj.When.Tags, Proteins: rj.When.Proteins},
		}
		if rj.ID != nil {
			r.ID = *rj.ID
		}
		if rj.Add.RecipeID != nil {
			r.Add.RecipeID = *rj.Add.RecipeID
		}
		if rj.Add.GroceryItem != nil {
			item, err := rj.Add.GroceryItem.item(fmt.Sprintf("pairings[%d].add.groceryItem", i))
			if err != nil {
				return nil, err
			}
			r.Add.GroceryItem = item
		}
		rules = append(rules, r)
	}
	return rules, nil
}

func pairingJSON(p Pairing) PairingJSON {
	pj := PairingJSON{
		Key: p.Key, Target: PairingTargetJSON{Kind: p.Kind}, Source: p.Source, Frequency: p.Frequency,
		MealCategory: optionalString(p.MealCategory), Reason: p.Reason, RuleID: optionalString(p.RuleID), InPlan: p.InPlan,
		CanMakeRule: p.Source == PairingSourceLearned && p.Recipe != nil && p.MealCategory != "",
	}
	if r := p.Recipe; r != nil {
		pj.Target.Recipe = &PairingRecipeJSON{
			ID: r.ID, Name: r.Name, Headline: r.Headline, ImageURL: r.ImageURL, CookMinutes: optionalInt(r.CookMinutes),
			TimesOrdered: r.TimesOrdered, LastOrderedWeek: r.LastOrderedWeek, IsAddon: true, Tags: orEmptyStrings(r.Tags),
		}
		pj.Servings = optionalInt(p.Servings)
	}
	if p.GroceryItem != nil {
		g := groceryItemJSON(*p.GroceryItem)
		pj.Target.GroceryItem = &g
	}
	if l := p.Learned; l != nil {
		c := round2(p.Confidence)
		pj.Confidence = &c
		pj.Learned = &LearnedPairingJSON{WeeksTogether: l.WeeksTogether, MealCategoryWeeks: l.CategoryWeeks, OtherWeeks: l.OtherWeeks, OtherWeeksRate: round2(l.OtherRate)}
	}
	return pj
}

func pairingsJSON(pairings []Pairing) []PairingJSON {
	out := make([]PairingJSON, 0, len(pairings))
	for _, p := range pairings {
		out = append(out, pairingJSON(p))
	}
	return out
}

func proposalPairingsJSON(slotID string, pairings []Pairing) []ProposalPairingJSON {
	out := make([]ProposalPairingJSON, 0, len(pairings))
	for _, p := range pairings {
		out = append(out, ProposalPairingJSON{ID: ProposalPairingID(slotID, p.Key), PairingJSON: pairingJSON(p), Included: p.Included})
	}
	return out
}

func pairingGroceryItemJSON(g WeekGroceryItem) PairingGroceryItemJSON {
	return PairingGroceryItemJSON{
		ID: g.ID, Key: g.Key, GroceryItem: groceryItemJSON(g.GroceryItem), EntryID: g.ForEntryID,
		Recipe: ProposalRecipeJSON{ID: g.ForRecipeID, Name: g.ForRecipeName}, Source: g.Source, RuleID: optionalString(g.RuleID),
		Text: g.Text(), AddedBy: g.AddedBy, AddedAt: g.AddedAt,
	}
}

func newWeekPairingsResponse(v WeekPairingsView) WeekPairingsResponse {
	resp := WeekPairingsResponse{
		Week: v.Week.String(), StartDate: v.Week.Date(planning.Monday), EndDate: v.Week.Date(planning.Sunday),
		Meals: make([]MealPairingsJSON, 0, len(v.Meals)), GroceryItems: make([]PairingGroceryItemJSON, 0, len(v.GroceryItems)),
	}
	for _, m := range v.Meals {
		e := planning.NewEntryResponse(v.Week, m.Entry)
		resp.Meals = append(resp.Meals, MealPairingsJSON{
			EntryID: e.ID, Day: e.Day, Date: e.Date, Recipe: ProposalRecipeJSON{ID: e.Recipe.ID, Name: e.Recipe.Name, ImageURL: e.Recipe.ImageURL},
			Servings: e.Servings, MealCategories: orEmptyStrings(m.MealCategories), Pairings: pairingsJSON(m.Pairings),
		})
	}
	for _, g := range v.GroceryItems {
		resp.GroceryItems = append(resp.GroceryItems, pairingGroceryItemJSON(g))
	}
	return resp
}

func newMealCategoriesJSON(attrs []MealCategoryAttribute) []MealCategoryJSON {
	out := make([]MealCategoryJSON, 0, len(attrs))
	for _, a := range attrs {
		out = append(out, MealCategoryJSON{
			Category: a.Category, Label: optionLabel(MealCategoryOptions, a.Category), Suits: a.Suits, Source: a.Source,
			HeuristicSuits: a.HeuristicSuits, Evidence: a.Evidence,
		})
	}
	return out
}

func acceptPairingsJSON(res AcceptResult) ([]AddedPairingJSON, []SkippedPairingJSON) {
	added := make([]AddedPairingJSON, 0, len(res.PairingsAdded))
	for _, a := range res.PairingsAdded {
		aj := AddedPairingJSON{ID: a.ID, SlotID: a.SlotID, Pairing: pairingJSON(a.Pairing)}
		if a.Entry != nil {
			e := planning.NewEntryResponse(res.Plan.Week, *a.Entry)
			aj.Entry = &e
		}
		if a.GroceryItem != nil {
			g := pairingGroceryItemJSON(*a.GroceryItem)
			aj.GroceryItem = &g
		}
		added = append(added, aj)
	}
	skipped := make([]SkippedPairingJSON, 0, len(res.PairingsSkipped))
	for _, s := range res.PairingsSkipped {
		skipped = append(skipped, SkippedPairingJSON(s))
	}
	return added, skipped
}

// --- handlers ---------------------------------------------------------------------

func (h *Handler) weekPairings(w http.ResponseWriter, r *http.Request) {
	m := actor(r)
	v, err := h.opts.Service.WeekPairings(r.Context(), m.HouseholdID, m.UserID, chi.URLParam(r, "week"), r.URL.Query().Get("entryId"))
	if err != nil {
		h.writeError(w, r, "get week pairings failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, newWeekPairingsResponse(v))
}

func (h *Handler) acceptPairing(w http.ResponseWriter, r *http.Request) {
	var req pairingActionRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	m := actor(r)
	res, err := h.opts.Service.AcceptPairing(r.Context(), m.HouseholdID, m.UserID, chi.URLParam(r, "week"), req.EntryID, req.Key)
	if err != nil {
		h.writeError(w, r, "accept pairing failed", err)
		return
	}
	resp := PairingAcceptResponse{Status: res.Status, Pairing: pairingJSON(res.Pairing), Plan: planning.NewPlanResponse(res.Plan)}
	if res.Entry != nil {
		e := planning.NewEntryResponse(res.Plan.Week, *res.Entry)
		resp.Entry = &e
	}
	if res.GroceryItem != nil {
		g := pairingGroceryItemJSON(*res.GroceryItem)
		resp.GroceryItem = &g
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) dismissPairing(w http.ResponseWriter, r *http.Request) {
	var req pairingActionRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	m := actor(r)
	v, err := h.opts.Service.DismissPairing(r.Context(), m.HouseholdID, m.UserID, chi.URLParam(r, "week"), req.EntryID, req.Key)
	if err != nil {
		h.writeError(w, r, "dismiss pairing failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, newWeekPairingsResponse(v))
}

func (h *Handler) makePairingRule(w http.ResponseWriter, r *http.Request) {
	var req makePairingRuleRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	in := MakePairingRuleInput{Key: req.Key}
	if req.EntryID != nil {
		in.EntryID = *req.EntryID
	}
	if req.SlotID != nil {
		in.SlotID = *req.SlotID
	}
	if req.Frequency != nil {
		in.Frequency = *req.Frequency
	}
	m := actor(r)
	res, err := h.opts.Service.MakePairingRule(r.Context(), m.HouseholdID, m.UserID, chi.URLParam(r, "week"), in)
	if err != nil {
		h.writeError(w, r, "make pairing rule failed", err)
		return
	}
	servings, err := h.opts.Service.DefaultServings(r.Context(), res.Profile)
	if err != nil {
		h.writeError(w, r, "load household servings failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, MakePairingRuleResponse{Status: res.Status, Rule: pairingRuleJSON(res.Rule), Profile: newProfileResponse(res.Profile, servings)})
}

func (h *Handler) removePairingGroceryItem(w http.ResponseWriter, r *http.Request) {
	m := actor(r)
	if err := h.opts.Service.RemovePairingGroceryItem(r.Context(), m.HouseholdID, m.UserID, chi.URLParam(r, "week"), chi.URLParam(r, "itemId")); err != nil {
		h.writeError(w, r, "remove paired grocery item failed", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) recipePairings(w http.ResponseWriter, r *http.Request) {
	res, err := h.opts.Service.RecipePairings(r.Context(), actor(r).HouseholdID, chi.URLParam(r, "recipeId"), r.URL.Query().Get("week"))
	if err != nil {
		h.writeError(w, r, "get recipe pairings failed", err)
		return
	}
	resp := RecipePairingsResponse{RecipeID: res.RecipeID, MealCategories: orEmptyStrings(res.MealCategories), Items: pairingsJSON(res.Pairings)}
	if res.Week != nil {
		week := res.Week.String()
		resp.Week = &week
	}
	if res.Entry != nil {
		id := res.Entry.ID
		resp.EntryID = &id
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}
