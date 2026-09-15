package planning

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/auth"
	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

// HandlerOptions configures the planning HTTP handlers.
type HandlerOptions struct {
	Service    *Service
	Authorizer households.Authorizer
	Tokens     auth.AccessTokenValidator
	Logger     *slog.Logger
}

// Handler serves the week plan endpoints.
type Handler struct {
	opts   HandlerOptions
	logger *slog.Logger
}

// NewHandler returns a Handler.
func NewHandler(opts HandlerOptions) *Handler {
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Handler{opts: opts, logger: logger}
}

// Mount registers the routes on r, which is expected to be the /api/v1 router.
func (h *Handler) Mount(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireAuth(h.opts.Tokens))
		view := households.RequirePermission(h.opts.Authorizer, households.PermHouseholdView, h.logger)
		edit := households.RequirePermission(h.opts.Authorizer, households.PermPlanEdit, h.logger)
		const plans = "/households/{householdId}/plans"
		r.With(view).Get(plans, h.list)
		r.With(view).Get(plans+"/{week}", h.get)
		r.With(view).Get(plans+"/{week}/grocery", h.groceryList)
		r.With(edit).Post(plans+"/{week}/entries", h.addEntry)
		r.With(edit).Patch(plans+"/{week}/entries/{entryId}", h.updateEntry)
		r.With(edit).Delete(plans+"/{week}/entries/{entryId}", h.deleteEntry)
		r.With(edit).Put(plans+"/{week}/status", h.setStatus)
	})
}

// --- Wire types ---------------------------------------------------------------

// PlanResponse is a household's plan for one week.
type PlanResponse struct {
	HouseholdID string          `json:"householdId"`
	Week        string          `json:"week"`
	StartDate   string          `json:"startDate"`
	EndDate     string          `json:"endDate"`
	Status      Status          `json:"status"`
	Entries     []EntryResponse `json:"entries"`
	// CreatedAt and UpdatedAt are null for a week nobody has planned.
	CreatedAt *time.Time `json:"createdAt"`
	UpdatedAt *time.Time `json:"updatedAt"`
}

// EntryResponse is a planned recipe.
type EntryResponse struct {
	ID     string              `json:"id"`
	Recipe EntryRecipeResponse `json:"recipe"`
	// Day and Date are null for an unscheduled entry.
	Day      *Day      `json:"day"`
	Date     *string   `json:"date"`
	Servings int       `json:"servings"`
	Note     string    `json:"note"`
	AddedBy  string    `json:"addedBy"`
	AddedAt  time.Time `json:"addedAt"`
	// Origin is manual, or autopilot for entries added by accepting an
	// Autopilot proposal.
	Origin Origin `json:"origin"`
}

// EntryRecipeResponse is the recipe snapshot stored on an entry.
type EntryRecipeResponse struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ImageURL string `json:"imageUrl,omitempty"`
}

// AddEntryResponse is returned by POST .../plans/{week}/entries.
type AddEntryResponse struct {
	Entry EntryResponse `json:"entry"`
	Plan  PlanResponse  `json:"plan"`
}

// PlanSummaryResponse describes one week in a range.
type PlanSummaryResponse struct {
	Week       string     `json:"week"`
	StartDate  string     `json:"startDate"`
	Status     Status     `json:"status"`
	EntryCount int        `json:"entryCount"`
	UpdatedAt  *time.Time `json:"updatedAt"`
}

// PlanListResponse is returned by GET .../plans.
type PlanListResponse struct {
	Items []PlanSummaryResponse `json:"items"`
}

// GroceryListResponse is returned by GET .../plans/{week}/grocery.
type GroceryListResponse struct {
	Week   string `json:"week"`
	Status Status `json:"status"`
	// PantryApplied is true when the household pantry decided statuses.
	PantryApplied bool                      `json:"pantryApplied"`
	Categories    []GroceryCategoryResponse `json:"categories"`
	Skipped       []SkippedEntryResponse    `json:"skipped"`
}

// GroceryCategoryResponse is one aisle.
type GroceryCategoryResponse struct {
	Category string                `json:"category"`
	Items    []GroceryItemResponse `json:"items"`
}

// GroceryItemResponse is one aggregated ingredient.
type GroceryItemResponse struct {
	IngredientKey string                  `json:"ingredientKey"`
	Name          string                  `json:"name"`
	Amounts       []GroceryAmountResponse `json:"amounts"`
	// QuantityText joins the amounts for display ("1 onion + 8 oz").
	QuantityText string                  `json:"quantityText"`
	Unquantified bool                    `json:"unquantified"`
	Status       grocery.Status          `json:"status"`
	Recipes      []GroceryRecipeResponse `json:"recipes"`
}

// GroceryAmountResponse is a combined amount in one unit.
type GroceryAmountResponse struct {
	Quantity      string  `json:"quantity"`
	QuantityValue float64 `json:"quantityValue"`
	Unit          string  `json:"unit"`
	Text          string  `json:"text"`
}

// GroceryRecipeResponse is a recipe that needs the item.
type GroceryRecipeResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// SkippedEntryResponse is an entry left out of the grocery list.
type SkippedEntryResponse struct {
	EntryID    string     `json:"entryId"`
	RecipeID   string     `json:"recipeId"`
	RecipeName string     `json:"recipeName"`
	Reason     SkipReason `json:"reason"`
}

type addEntryRequest struct {
	RecipeID string  `json:"recipeId"`
	Day      *string `json:"day"`
	Servings int     `json:"servings"`
	Note     string  `json:"note"`
}

type updateEntryRequest struct {
	// Day is raw so an explicit null (unschedule) differs from an absent field.
	Day      json.RawMessage `json:"day"`
	Servings *int            `json:"servings"`
	Note     *string         `json:"note"`
}

type setStatusRequest struct {
	Status string `json:"status"`
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// NewPlanResponse returns the wire form of a plan, for other modules that
// return plans (accepting an Autopilot proposal).
func NewPlanResponse(p Plan) PlanResponse { return newPlanResponse(p) }

// NewEntryResponse returns the wire form of an entry in week w.
func NewEntryResponse(w Week, e Entry) EntryResponse { return newEntryResponse(w, e) }

func newPlanResponse(p Plan) PlanResponse {
	resp := PlanResponse{
		HouseholdID: p.HouseholdID, Week: p.Week.String(),
		StartDate: p.Week.Date(Monday), EndDate: p.Week.Date(Sunday),
		Status: p.Status, Entries: make([]EntryResponse, 0, len(p.Entries)),
		CreatedAt: timePtr(p.CreatedAt), UpdatedAt: timePtr(p.UpdatedAt),
	}
	for _, e := range p.Entries {
		resp.Entries = append(resp.Entries, newEntryResponse(p.Week, e))
	}
	return resp
}

func newEntryResponse(w Week, e Entry) EntryResponse {
	resp := EntryResponse{
		ID:       e.ID,
		Recipe:   EntryRecipeResponse{ID: e.RecipeID, Name: e.RecipeName, ImageURL: e.RecipeImageURL},
		Servings: e.Servings, Note: e.Note, AddedBy: e.AddedBy, AddedAt: e.AddedAt.UTC(), Origin: e.Origin,
	}
	if resp.Origin == "" {
		resp.Origin = OriginManual
	}
	if e.Day != "" {
		day, date := e.Day, w.Date(e.Day)
		resp.Day, resp.Date = &day, &date
	}
	return resp
}

func newGroceryListResponse(g GroceryList) GroceryListResponse {
	resp := GroceryListResponse{
		Week: g.Week.String(), Status: g.Status, PantryApplied: g.PantryApplied,
		Categories: make([]GroceryCategoryResponse, 0, len(g.Categories)),
		Skipped:    make([]SkippedEntryResponse, 0, len(g.Skipped)),
	}
	for _, c := range g.Categories {
		cr := GroceryCategoryResponse{Category: c.Category, Items: make([]GroceryItemResponse, 0, len(c.Items))}
		for _, item := range c.Items {
			ir := GroceryItemResponse{
				IngredientKey: item.IngredientKey, Name: item.Name, Unquantified: item.Unquantified, Status: item.Status,
				Amounts: make([]GroceryAmountResponse, 0, len(item.Amounts)),
				Recipes: make([]GroceryRecipeResponse, 0, len(item.Sources)),
			}
			texts := make([]string, 0, len(item.Amounts))
			for _, a := range item.Amounts {
				text := amountText(a)
				texts = append(texts, text)
				ir.Amounts = append(ir.Amounts, GroceryAmountResponse{
					Quantity: a.Quantity.String(), QuantityValue: a.Quantity.Float64(), Unit: a.Unit.Code, Text: text,
				})
			}
			ir.QuantityText = strings.Join(texts, " + ")
			for _, src := range item.Sources {
				ir.Recipes = append(ir.Recipes, GroceryRecipeResponse{ID: src.RecipeID, Name: src.RecipeName})
			}
			cr.Items = append(cr.Items, ir)
		}
		resp.Categories = append(resp.Categories, cr)
	}
	for _, s := range g.Skipped {
		resp.Skipped = append(resp.Skipped, SkippedEntryResponse(s))
	}
	return resp
}

// amountText renders "1 ½ cups", "2 cloves", or "3" for counts.
func amountText(a grocery.Amount) string {
	text := a.Quantity.Format()
	if label := a.Unit.Label(a.Quantity); label != "" {
		text += " " + label
	}
	return text
}

// --- Handlers -----------------------------------------------------------------

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	q := r.URL.Query()
	items, err := h.opts.Service.List(r.Context(), actor.HouseholdID, q.Get("from"), q.Get("to"))
	if err != nil {
		h.writeError(w, r, "list plans failed", err)
		return
	}
	resp := PlanListResponse{Items: make([]PlanSummaryResponse, 0, len(items))}
	for _, s := range items {
		resp.Items = append(resp.Items, PlanSummaryResponse{
			Week: s.Week.String(), StartDate: s.Week.Date(Monday), Status: s.Status,
			EntryCount: s.EntryCount, UpdatedAt: timePtr(s.UpdatedAt),
		})
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	p, err := h.opts.Service.Get(r.Context(), actor.HouseholdID, chi.URLParam(r, "week"))
	if err != nil {
		h.writeError(w, r, "get plan failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, newPlanResponse(p))
}

func (h *Handler) groceryList(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	g, err := h.opts.Service.GroceryList(r.Context(), actor.HouseholdID, chi.URLParam(r, "week"))
	if err != nil {
		h.writeError(w, r, "build grocery list failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, newGroceryListResponse(g))
}

func (h *Handler) addEntry(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	var req addEntryRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	in := NewEntry{RecipeID: req.RecipeID, Servings: req.Servings, Note: req.Note}
	if req.Day != nil {
		if *req.Day == "" {
			validationFailed(w, r, "day must be one of mon, tue, wed, thu, fri, sat, sun, or null")
			return
		}
		in.Day = *req.Day
	}
	p, e, err := h.opts.Service.AddEntry(r.Context(), actor.HouseholdID, actor.UserID, chi.URLParam(r, "week"), in)
	if err != nil {
		h.writeError(w, r, "add plan entry failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, AddEntryResponse{Entry: newEntryResponse(p.Week, e), Plan: newPlanResponse(p)})
}

func (h *Handler) updateEntry(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	var req updateEntryRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	changes := EntryChanges{Servings: req.Servings, Note: req.Note}
	if len(req.Day) > 0 {
		var day *string
		if err := json.Unmarshal(req.Day, &day); err != nil {
			httpx.WriteError(w, r, http.StatusBadRequest, "invalid_request", `field "day" has the wrong type`)
			return
		}
		d := Day("")
		if day != nil {
			if *day == "" {
				validationFailed(w, r, "day must be one of mon, tue, wed, thu, fri, sat, sun, or null")
				return
			}
			d = Day(*day)
		}
		changes.Day = &d
	}
	p, err := h.opts.Service.UpdateEntry(r.Context(), actor.HouseholdID, chi.URLParam(r, "week"), chi.URLParam(r, "entryId"), changes)
	if err != nil {
		h.writeError(w, r, "update plan entry failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, newPlanResponse(p))
}

func (h *Handler) deleteEntry(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	if _, err := h.opts.Service.DeleteEntry(r.Context(), actor.HouseholdID, actor.UserID, chi.URLParam(r, "week"), chi.URLParam(r, "entryId")); err != nil {
		h.writeError(w, r, "delete plan entry failed", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) setStatus(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	var req setStatusRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	p, err := h.opts.Service.SetStatus(r.Context(), actor.HouseholdID, chi.URLParam(r, "week"), req.Status)
	if err != nil {
		h.writeError(w, r, "set plan status failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, newPlanResponse(p))
}

// writeError maps service errors to responses.
func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	switch {
	case errors.Is(err, ErrInvalidWeek), errors.Is(err, ErrInvalidRange), errors.Is(err, ErrInvalidEntry),
		errors.Is(err, ErrInvalidStatus), errors.Is(err, ErrRecipeNotFound):
		validationFailed(w, r, publicMessage(err))
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", "plan entry not found")
	case errors.Is(err, ErrFinalized):
		httpx.WriteError(w, r, http.StatusConflict, "plan_finalized", "the plan is finalized; set its status to draft to change entries")
	case errors.Is(err, ErrPlanFull):
		httpx.WriteError(w, r, http.StatusConflict, "plan_full", fmt.Sprintf("a week holds at most %d entries", MaxEntriesPerWeek))
	default:
		h.logger.ErrorContext(r.Context(), msg, "error", err)
		httpx.WriteError(w, r, http.StatusInternalServerError, "internal", "internal server error")
	}
}

func validationFailed(w http.ResponseWriter, r *http.Request, message string) {
	httpx.WriteError(w, r, http.StatusBadRequest, "validation_failed", message)
}

// publicMessage strips package prefixes from sentinel-wrapped errors whose
// details are safe to show clients.
func publicMessage(err error) string {
	return strings.ReplaceAll(err.Error(), "planning: ", "")
}
