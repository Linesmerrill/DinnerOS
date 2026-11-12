package recipes

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

// EditRecipeRequest is the body of PUT
// /households/{householdId}/recipes/{recipeId}/edits: the recipe's
// ingredients and steps as the member left them.
type EditRecipeRequest struct {
	Ingredients []EditIngredientRequest `json:"ingredients"`
	Steps       []string                `json:"steps"`
}

// EditIngredientRequest is one ingredient line. ingredientId is the line's
// own, from the recipe, for a line the member didn't rename; omit it for a
// new line.
type EditIngredientRequest struct {
	IngredientID string              `json:"ingredientId"`
	Name         string              `json:"name"`
	PantryStaple bool                `json:"pantryStaple"`
	Amounts      []EditAmountRequest `json:"amounts"`
}

// EditAmountRequest is the amount for one serving size.
type EditAmountRequest struct {
	Servings int    `json:"servings"`
	Quantity string `json:"quantity"`
	Unit     string `json:"unit"`
}

// SwapPreviewRequest is the body of POST
// /households/{householdId}/recipes/swap-preview.
type SwapPreviewRequest struct {
	OldName string   `json:"oldName"`
	NewName string   `json:"newName"`
	Steps   []string `json:"steps"`
	// Minutes, when set, writes that cook time into the steps that cook the
	// new ingredient.
	Minutes int `json:"minutes"`
}

// SwapPreviewResponse is the steps with the new name, how long the new
// ingredient usually cooks, and the times the steps give it now.
type SwapPreviewResponse struct {
	Steps    []SwapStepResponse     `json:"steps"`
	CookTime *CookTimeResponse      `json:"cookTime"`
	Times    []SwapStepTimeResponse `json:"times"`
}

// SwapStepResponse is one step after the swap.
type SwapStepResponse struct {
	Text    string `json:"text"`
	Changed bool   `json:"changed"`
}

// CookTimeResponse is how long an ingredient usually cooks: "Lo mein noodles
// usually cook 4 to 6 minutes."
type CookTimeResponse struct {
	LowMinutes       int    `json:"lowMinutes"`
	HighMinutes      int    `json:"highMinutes"`
	SuggestedMinutes int    `json:"suggestedMinutes"`
	Text             string `json:"text"`
}

// SwapStepTimeResponse is a cook time a step gives the new ingredient.
type SwapStepTimeResponse struct {
	// Step is the step's position, from 0.
	Step    int    `json:"step"`
	Text    string `json:"text"`
	Minutes int    `json:"minutes"`
}

func (h *Handler) editRecipe(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	var req EditRecipeRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	in := EditInput{Steps: req.Steps}
	for _, ing := range req.Ingredients {
		e := EditIngredient{IngredientID: ing.IngredientID, Name: ing.Name, PantryStaple: ing.PantryStaple}
		for _, a := range ing.Amounts {
			e.Amounts = append(e.Amounts, EditAmount(a))
		}
		in.Ingredients = append(in.Ingredients, e)
	}
	recipe, err := h.opts.Service.Edit(r.Context(), actor.HouseholdID, chi.URLParam(r, "recipeId"), in)
	switch {
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", "recipe not found")
		return
	case errors.Is(err, ErrInvalidEdit):
		validationFailed(w, r, editMessage(err))
		return
	case err != nil:
		h.internalError(w, r, "edit recipe failed", err)
		return
	}
	summaries, err := h.ratingSummaries(r.Context(), actor, []string{recipe.ID})
	if err != nil {
		h.internalError(w, r, "load recipe rating failed", err)
		return
	}
	resp := newRecipeResponse(recipe, h.timeBands(r.Context(), actor.HouseholdID))
	resp.InCatalog = h.opts.Service.InCatalog(r.Context(), recipe)
	resp.HouseholdRating, resp.MyRating = ratingFields(summaries[recipe.ID])
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) swapPreview(w http.ResponseWriter, r *http.Request) {
	var req SwapPreviewRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	res, err := SwapSteps(SwapInput(req))
	if err != nil {
		validationFailed(w, r, editMessage(err))
		return
	}
	resp := SwapPreviewResponse{
		Steps: make([]SwapStepResponse, 0, len(res.Steps)), Times: make([]SwapStepTimeResponse, 0, len(res.Times)),
	}
	for i, text := range res.Steps {
		resp.Steps = append(resp.Steps, SwapStepResponse{Text: text, Changed: res.Changed[i]})
	}
	for _, t := range res.Times {
		resp.Times = append(resp.Times, SwapStepTimeResponse(t))
	}
	if ct := res.CookTime; ct != nil {
		resp.CookTime = &CookTimeResponse{
			LowMinutes: ct.Low, HighMinutes: ct.High, SuggestedMinutes: ct.Suggested(),
			Text: cookTimeSentence(req.NewName, ct.Text()),
		}
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

// editMessage is an edit error's sentence alone, for the member.
func editMessage(err error) string {
	var e editError
	if errors.As(err, &e) {
		return string(e)
	}
	return publicMessage(err)
}
