package recipes

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

// DeleteRecipeResponse is the body of a 200 from DELETE
// /households/{householdId}/recipes/{recipeId} (decision 694).
type DeleteRecipeResponse struct {
	// RemovedFromWeeks are the ISO weeks ("2026-W47") the recipe was taken
	// off, in order; empty when it wasn't planned.
	RemovedFromWeeks []string `json:"removedFromWeeks"`
	// UndoUntil is the last moment POST .../restore brings it back.
	UndoUntil time.Time `json:"undoUntil"`
}

func (h *Handler) mountDelete(r chi.Router, edit func(http.Handler) http.Handler) {
	r.With(edit).Delete("/households/{householdId}/recipes/{recipeId}", h.deleteRecipe)
	r.With(edit).Post("/households/{householdId}/recipes/{recipeId}/restore", h.restoreRecipe)
}

func (h *Handler) deleteRecipe(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	result, err := h.opts.Service.Delete(r.Context(), actor.HouseholdID, actor.UserID, chi.URLParam(r, "recipeId"))
	switch {
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", "That recipe isn't in your recipes.")
		return
	case err != nil:
		h.internalError(w, r, "delete recipe failed", err)
		return
	}
	weeks := result.RemovedFromWeeks
	if weeks == nil {
		weeks = []string{}
	}
	httpx.WriteJSON(w, http.StatusOK, DeleteRecipeResponse{RemovedFromWeeks: weeks, UndoUntil: result.UndoUntil.UTC()})
}

func (h *Handler) restoreRecipe(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	recipe, err := h.opts.Service.Restore(r.Context(), actor.HouseholdID, actor.UserID, chi.URLParam(r, "recipeId"))
	switch {
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", "That recipe isn't in your recipes.")
		return
	case errors.Is(err, ErrRestoreExpired):
		httpx.WriteError(w, r, http.StatusGone, "restore_expired", "It's too late to bring that recipe back.")
		return
	case err != nil:
		h.internalError(w, r, "restore recipe failed", err)
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
