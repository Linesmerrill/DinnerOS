package customize

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/auth"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

// HandlerOptions configures the customization HTTP handlers.
type HandlerOptions struct {
	Service    *Service
	Authorizer households.Authorizer
	Tokens     auth.AccessTokenValidator
	Logger     *slog.Logger
}

// Handler serves the meal customization endpoints.
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
		r.With(view).Get("/households/{householdId}/recipes/{recipeId}/customizations", h.options)
		r.With(edit).Put("/households/{householdId}/plans/{week}/entries/{entryId}/customization", h.set)
	})
}

// --- Wire types ---------------------------------------------------------------

// CustomizationsResponse is returned by GET .../recipes/{recipeId}/customizations.
type CustomizationsResponse struct {
	RecipeID string          `json:"recipeId"`
	Servings int             `json:"servings"`
	Groups   []GroupResponse `json:"groups"`
}

// GroupResponse is one customizable protein line.
type GroupResponse struct {
	IngredientKey  string `json:"ingredientKey"`
	IngredientName string `json:"ingredientName"`
	// AmountText is "10 ounce"; Quantity and Unit are the exact amount.
	AmountText string  `json:"amountText"`
	Quantity   string  `json:"quantity"`
	Unit       string  `json:"unit"`
	ImageURL   *string `json:"imageUrl"`
	// SelectedChoiceID is the entry's choice when entryId was given.
	SelectedChoiceID *string          `json:"selectedChoiceId"`
	Choices          []ChoiceResponse `json:"choices"`
}

// ChoiceResponse is one choice for a line.
type ChoiceResponse struct {
	ID             string     `json:"id"`
	Label          string     `json:"label"`
	IngredientName string     `json:"ingredientName"`
	AmountText     string     `json:"amountText"`
	Quantity       string     `json:"quantity"`
	Unit           string     `json:"unit"`
	ImageURL       *string    `json:"imageUrl"`
	Kind           ChoiceKind `json:"kind"`
	Badge          *string    `json:"badge"`
}

type setRequest struct {
	Selections *[]selectionRequest `json:"selections"`
}

type selectionRequest struct {
	IngredientKey string `json:"ingredientKey"`
	ChoiceID      string `json:"choiceId"`
}

// massUnitNames spell mass units the way meal-kit recipes do ("10 ounce").
var massUnitNames = map[string]string{"oz": "ounce", "lb": "pound", "g": "gram", "kg": "kilogram"}

// AmountText renders an amount as "10 ounce" or "1 ½ pound".
func AmountText(q ingredients.Quantity, unit string) string {
	name, ok := massUnitNames[unit]
	if !ok {
		u, _ := ingredients.LookupUnit(unit)
		name = u.Label(q)
	}
	if name == "" {
		return q.Format()
	}
	return q.Format() + " " + name
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func newCustomizationsResponse(o Options) CustomizationsResponse {
	resp := CustomizationsResponse{RecipeID: o.RecipeID, Servings: o.Servings, Groups: make([]GroupResponse, 0, len(o.Groups))}
	for _, g := range o.Groups {
		gr := GroupResponse{
			IngredientKey: g.Line.Key, IngredientName: g.Line.Name, AmountText: AmountText(g.Line.Quantity, g.Line.Unit),
			Quantity: g.Line.Quantity.String(), Unit: g.Line.Unit, ImageURL: optional(g.ImageURL),
			SelectedChoiceID: optional(g.Selected), Choices: make([]ChoiceResponse, 0, len(g.Choices)),
		}
		for _, c := range g.Choices {
			gr.Choices = append(gr.Choices, ChoiceResponse{
				ID: c.ID, Label: c.Label, IngredientName: c.IngredientName, AmountText: AmountText(c.Quantity, c.Unit),
				Quantity: c.Quantity.String(), Unit: c.Unit, ImageURL: optional(c.ImageURL), Kind: c.Kind, Badge: optional(c.Badge),
			})
		}
		resp.Groups = append(resp.Groups, gr)
	}
	return resp
}

// --- Handlers -----------------------------------------------------------------

func (h *Handler) options(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	v := r.URL.Query()
	q := Query{EntryID: v.Get("entryId"), Week: v.Get("week")}
	if raw := v.Get("servings"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			validationFailed(w, r, "servings must be a positive integer")
			return
		}
		q.Servings = n
	}
	o, err := h.opts.Service.Options(r.Context(), actor.HouseholdID, chi.URLParam(r, "recipeId"), q)
	if err != nil {
		h.writeError(w, r, "list meal customizations failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, newCustomizationsResponse(o))
}

func (h *Handler) set(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	var req setRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Selections == nil {
		validationFailed(w, r, "selections is required; send [] to reset the meal")
		return
	}
	selections := make([]Selection, 0, len(*req.Selections))
	for _, s := range *req.Selections {
		selections = append(selections, Selection(s))
	}
	p, err := h.opts.Service.SetCustomizations(r.Context(), actor.HouseholdID, actor.UserID, chi.URLParam(r, "week"), chi.URLParam(r, "entryId"), selections)
	if err != nil {
		h.writeError(w, r, "customize meal failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, planning.NewPlanResponse(p))
}

// writeError maps service errors to responses, like the planning handlers.
func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	switch {
	case errors.Is(err, ErrInvalid), errors.Is(err, planning.ErrInvalidWeek):
		validationFailed(w, r, strings.NewReplacer("customize: ", "", "planning: ", "").Replace(err.Error()))
	case errors.Is(err, ErrRecipeNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", "recipe not found")
	case errors.Is(err, planning.ErrNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", "plan entry not found")
	case errors.Is(err, planning.ErrFinalized):
		httpx.WriteError(w, r, http.StatusConflict, "plan_finalized", "the plan is finalized; set its status to draft to change entries")
	default:
		h.logger.ErrorContext(r.Context(), msg, "error", err)
		httpx.WriteError(w, r, http.StatusInternalServerError, "internal", "internal server error")
	}
}

func validationFailed(w http.ResponseWriter, r *http.Request, message string) {
	httpx.WriteError(w, r, http.StatusBadRequest, "validation_failed", message)
}
