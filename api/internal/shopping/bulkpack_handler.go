package shopping

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
	"github.com/Linesmerrill/DinnerOS/api/internal/providers"
)

// BulkPacksResponse is returned by GET .../handoffs/{handoffId}/bulk-packs.
type BulkPacksResponse struct {
	Week      string        `json:"week"`
	Provider  providers.Key `json:"provider"`
	HandoffID string        `json:"handoffId"`
	// Packs is never null; an empty list means the week's packages matched
	// what it needs.
	Packs []BulkPackResponse `json:"packs"`
}

// BulkPackResponse is one line the week over-bought, with what to do about it.
type BulkPackResponse struct {
	LineID        string  `json:"lineId"`
	IngredientKey string  `json:"ingredientKey"`
	IngredientID  *string `json:"ingredientId"`
	Name          string  `json:"name"`
	Category      string  `json:"category"`
	ProductID     string  `json:"productId"`
	ProductName   string  `json:"productName"`
	Packages      int     `json:"packages"`
	// Unit is the package size's unit; bought, needed, and surplus are in it.
	Unit string `json:"unit"`
	// Bought, Needed, and Surplus are exact ("27/2"); the *Value fields are
	// the same numbers for display.
	Bought         string  `json:"bought"`
	BoughtValue    float64 `json:"boughtValue"`
	Needed         string  `json:"needed"`
	NeededValue    float64 `json:"neededValue"`
	Surplus        string  `json:"surplus"`
	SurplusValue   float64 `json:"surplusValue"`
	SurplusPercent int     `json:"surplusPercent"`
	// SurplusText is ready to show: "the week uses 10 oz of 64 oz".
	SurplusText string `json:"surplusText"`
	// Freezable says whether to offer sealing it; Frozen says it already is.
	Freezable bool `json:"freezable"`
	Frozen    bool `json:"frozen"`
	// Suggestions are second meals to plan this week, best first, never null.
	Suggestions []BulkPackSuggestionResponse `json:"suggestions"`
}

// BulkPackSuggestionResponse is a recipe to plan later in the same week.
type BulkPackSuggestionResponse struct {
	RecipeID   string  `json:"recipeId"`
	RecipeName string  `json:"recipeName"`
	ImageURL   *string `json:"imageUrl"`
	// Day is the open day the pick was ranked for ("thu").
	Day         string   `json:"day"`
	Servings    int      `json:"servings"`
	CookMinutes *int     `json:"cookMinutes"`
	Reasons     []string `json:"reasons"`
}

func bulkPacksResponse(p BulkPacks) BulkPacksResponse {
	resp := BulkPacksResponse{
		Week: p.Week, Provider: p.Provider, HandoffID: p.HandoffID,
		Packs: make([]BulkPackResponse, 0, len(p.Packs)),
	}
	for _, pack := range p.Packs {
		resp.Packs = append(resp.Packs, bulkPackResponse(pack))
	}
	return resp
}

func bulkPackResponse(p BulkPack) BulkPackResponse {
	out := BulkPackResponse{
		LineID: p.LineID, IngredientKey: p.IngredientKey, IngredientID: optionalString(p.IngredientID()),
		Name: p.Name, Category: p.Category, ProductID: p.ProductID, ProductName: p.ProductName,
		Packages: p.Packages, Unit: p.Unit,
		Bought: p.Bought, Needed: p.Needed, Surplus: p.Surplus, SurplusPercent: p.SurplusPercent,
		SurplusText: surplusText(p), Freezable: p.Freezable, Frozen: p.Frozen,
		Suggestions: make([]BulkPackSuggestionResponse, 0, len(p.Suggestions)),
	}
	out.BoughtValue, out.NeededValue, out.SurplusValue = exactValue(p.Bought), exactValue(p.Needed), exactValue(p.Surplus)
	for _, pick := range p.Suggestions {
		s := BulkPackSuggestionResponse{
			RecipeID: pick.RecipeID, RecipeName: pick.RecipeName, ImageURL: optionalString(pick.ImageURL),
			Day: pick.Day, Servings: pick.Servings, Reasons: make([]string, 0, len(pick.Reasons)),
		}
		if pick.CookMinutes > 0 {
			minutes := pick.CookMinutes
			s.CookMinutes = &minutes
		}
		for _, r := range pick.Reasons {
			s.Reasons = append(s.Reasons, r.Text)
		}
		out.Suggestions = append(out.Suggestions, s)
	}
	return out
}

// surplusText renders the comparison the member actually cares about.
func surplusText(p BulkPack) string {
	return fmt.Sprintf("This week uses %s of %s", amountText(p.Needed, p.Unit), amountText(p.Bought, p.Unit))
}

// amountText renders an exact amount with its unit label.
func amountText(exact, unitCode string) string {
	q, err := ingredients.ParseQuantity(exact)
	if err != nil {
		return exact
	}
	text := q.Format()
	if u, err := ingredients.LookupUnit(unitCode); err == nil {
		if label := u.Label(q); label != "" {
			text += " " + label
		}
	}
	return text
}

func exactValue(exact string) float64 {
	q, err := ingredients.ParseQuantity(exact)
	if err != nil {
		return 0
	}
	return q.Float64()
}

func (h *Handler) bulkPacks(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	packs, err := h.opts.Service.BulkPacks(r.Context(), actor.HouseholdID, chi.URLParam(r, "handoffId"))
	if err != nil {
		h.writeError(w, r, "list bulk packs failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, bulkPacksResponse(packs))
}
