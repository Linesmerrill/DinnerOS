package shopping

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

// Routes for Add Item and "Your usuals" (usuals.go), mounted by Handler.Mount.
func (h *Handler) mountUsuals(r chi.Router, view, planEdit func(http.Handler) http.Handler) {
	const base = "/households/{householdId}/shopping"
	r.With(planEdit).Post(base+"/weeks/{week}/items", h.addItems)
	r.With(planEdit).Delete(base+"/weeks/{week}/items/{itemId}", h.removeAddedItem)
	// By ?ingredientKey=, for the Shop tab, whose lines carry keys.
	r.With(planEdit).Delete(base+"/weeks/{week}/items", h.removeAddedItemByKey)
	r.With(view).Get(base+"/weeks/{week}/usuals", h.getUsuals)
	r.With(planEdit).Post(base+"/weeks/{week}/usuals/dismiss", h.dismissUsuals)
	r.With(planEdit).Put(base+"/usuals", h.setUsual)
}

// AddItemRequest is one item in AddItemsRequest.
type AddItemRequest struct {
	Name string `json:"name"`
	// Count defaults to 1.
	Count int `json:"count"`
}

// AddItemsRequest is the body of POST .../shopping/weeks/{week}/items.
type AddItemsRequest struct {
	Items []AddItemRequest `json:"items"`
	// FromUsuals is true for "Add your usuals", which also answers the
	// week's prompt.
	FromUsuals bool `json:"fromUsuals"`
}

// AddedItemResponse is an item a member added to a week's list.
type AddedItemResponse struct {
	ID            string    `json:"id"`
	Week          string    `json:"week"`
	IngredientKey string    `json:"ingredientKey"`
	Name          string    `json:"name"`
	Count         int       `json:"count"`
	FromUsuals    bool      `json:"fromUsuals"`
	AddedBy       string    `json:"addedBy"`
	AddedAt       time.Time `json:"addedAt"`
}

// AddedItemsResponse lists added items.
type AddedItemsResponse struct {
	Items []AddedItemResponse `json:"items"`
}

// UsualResponse is one of the household's usuals.
type UsualResponse struct {
	IngredientKey string `json:"ingredientKey"`
	Name          string `json:"name"`
	Count         int    `json:"count"`
	Pinned        bool   `json:"pinned"`
	Weeks         int    `json:"weeks"`
	OnList        bool   `json:"onList"`
}

// UsualPrefResponse is a pin or a "not a usual" mark.
type UsualPrefResponse struct {
	IngredientKey string `json:"ingredientKey"`
	Name          string `json:"name"`
	Count         int    `json:"count"`
	Pinned        bool   `json:"pinned"`
	NotUsual      bool   `json:"notUsual"`
}

// UsualsResponse is GET .../shopping/weeks/{week}/usuals.
type UsualsResponse struct {
	Week  string          `json:"week"`
	Items []UsualResponse `json:"items"`
	// Prompt is true when the week should ask "Add your usuals?".
	Prompt bool `json:"prompt"`
	// ShoppingWeeks is how many recent shopping weeks were counted;
	// Window and MinWeeks are the rule ("3 of your last 6").
	ShoppingWeeks int                 `json:"shoppingWeeks"`
	Window        int                 `json:"window"`
	MinWeeks      int                 `json:"minWeeks"`
	NotUsual      []UsualPrefResponse `json:"notUsual"`
}

// SetUsualRequest is the body of PUT .../shopping/usuals. With neither
// pinned nor notUsual the item is counted like any other again.
type SetUsualRequest struct {
	IngredientKey string `json:"ingredientKey"`
	Name          string `json:"name"`
	Count         int    `json:"count"`
	Pinned        bool   `json:"pinned"`
	NotUsual      bool   `json:"notUsual"`
}

// UsualPrefEnvelope wraps a saved pin or mark.
type UsualPrefEnvelope struct {
	Usual UsualPrefResponse `json:"usual"`
}

func addedItemResponse(a AddedItem) AddedItemResponse {
	return AddedItemResponse{
		ID: a.ID, Week: a.Week, IngredientKey: a.IngredientKey, Name: a.Name, Count: a.Count,
		FromUsuals: a.FromUsuals, AddedBy: a.AddedBy, AddedAt: a.AddedAt,
	}
}

func usualPrefResponse(p UsualPref) UsualPrefResponse {
	return UsualPrefResponse{IngredientKey: p.IngredientKey, Name: p.Name, Count: p.Count, Pinned: p.Pinned, NotUsual: p.NotUsual}
}

func (h *Handler) addItems(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	var req AddItemsRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	in := make([]AddItemInput, 0, len(req.Items))
	for _, it := range req.Items {
		in = append(in, AddItemInput(it))
	}
	added, err := h.opts.Service.AddItems(r.Context(), actor, chi.URLParam(r, "week"), in, req.FromUsuals)
	if err != nil {
		h.writeError(w, r, "add items failed", err)
		return
	}
	resp := AddedItemsResponse{Items: make([]AddedItemResponse, 0, len(added))}
	for _, a := range added {
		resp.Items = append(resp.Items, addedItemResponse(a))
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) removeAddedItem(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	if err := h.opts.Service.RemoveAddedItem(r.Context(), actor, chi.URLParam(r, "itemId")); err != nil {
		h.writeError(w, r, "remove added item failed", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) removeAddedItemByKey(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	err := h.opts.Service.RemoveAddedItemByKey(r.Context(), actor, chi.URLParam(r, "week"), r.URL.Query().Get("ingredientKey"))
	if err != nil {
		h.writeError(w, r, "remove added item failed", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) getUsuals(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	view, err := h.opts.Service.Usuals(r.Context(), actor.HouseholdID, chi.URLParam(r, "week"))
	if err != nil {
		h.writeError(w, r, "load usuals failed", err)
		return
	}
	resp := UsualsResponse{
		Week: view.Week, Items: make([]UsualResponse, 0, len(view.Items)), Prompt: view.Prompt,
		ShoppingWeeks: view.ShoppingWeeks, Window: UsualWindow, MinWeeks: UsualMinWeeks,
		NotUsual: make([]UsualPrefResponse, 0, len(view.NotUsual)),
	}
	for _, u := range view.Items {
		resp.Items = append(resp.Items, UsualResponse(u))
	}
	for _, p := range view.NotUsual {
		resp.NotUsual = append(resp.NotUsual, usualPrefResponse(p))
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) dismissUsuals(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	if err := h.opts.Service.DismissUsuals(r.Context(), actor, chi.URLParam(r, "week")); err != nil {
		h.writeError(w, r, "dismiss usuals failed", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) setUsual(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	var req SetUsualRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	saved, err := h.opts.Service.SetUsual(r.Context(), actor, UsualInput(req))
	if err != nil {
		h.writeError(w, r, "set usual failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, UsualPrefEnvelope{Usual: usualPrefResponse(saved)})
}
