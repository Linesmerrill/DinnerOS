package companions

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/auth"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

// HandlerOptions configures the companion HTTP handlers.
type HandlerOptions struct {
	Service    *Service
	Authorizer households.Authorizer
	Tokens     auth.AccessTokenValidator
	Logger     *slog.Logger
}

// Handler serves the companion endpoints.
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

// Mount registers the routes on r, the /api/v1 router. Companions sit beside
// the household's recipes, so they take the same permissions: anyone in the
// household reads them, and recipes.edit changes them.
func (h *Handler) Mount(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireAuth(h.opts.Tokens))
		view := households.RequirePermission(h.opts.Authorizer, households.PermHouseholdView, h.logger)
		edit := households.RequirePermission(h.opts.Authorizer, households.PermRecipesEdit, h.logger)
		const base = "/households/{householdId}/companions"
		r.With(view).Get(base, h.list)
		r.With(edit).Post(base, h.create)
		r.With(view).Get(base+"/{companionId}", h.get)
		r.With(edit).Put(base+"/{companionId}", h.update)
		r.With(edit).Delete(base+"/{companionId}", h.delete)
	})
}

// --- wire types -------------------------------------------------------------------

// RecipeRefJSON is a recipe a companion names.
type RecipeRefJSON struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// OptionJSON is a choice with the words the app shows for it.
type OptionJSON struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// CompanionJSON is one companion.
type CompanionJSON struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// StoreItem is null when it goes on the list under its name.
	StoreItem *string  `json:"storeItem"`
	Quantity  *float64 `json:"quantity"`
	Unit      *string  `json:"unit"`
	// ProductURL is an http or https link, or null.
	ProductURL *string `json:"productUrl"`
	// LinkedRecipe is the add-on recipe it was made from, or null.
	LinkedRecipe   *RecipeRefJSON  `json:"linkedRecipe"`
	Cuisines       []string        `json:"cuisines"`
	Recipes        []RecipeRefJSON `json:"recipes"`
	MealCategories []string        `json:"mealCategories"`
	Frequency      string          `json:"frequency"`
	Mode           string          `json:"mode"`
	// Summary is the line the list shows under the name.
	Summary        string     `json:"summary"`
	LastListedWeek *string    `json:"lastListedWeek"`
	LastListedAt   *time.Time `json:"lastListedAt"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

// ListResponse is returned by GET .../companions, with the choices the
// editor offers, in order.
type ListResponse struct {
	Items       []CompanionJSON `json:"items"`
	Frequencies []OptionJSON    `json:"frequencies"`
	Modes       []OptionJSON    `json:"modes"`
}

// Request is a companion as the app sends it (POST and PUT).
type Request struct {
	Name           string   `json:"name"`
	StoreItem      *string  `json:"storeItem"`
	Quantity       *float64 `json:"quantity"`
	Unit           *string  `json:"unit"`
	ProductURL     *string  `json:"productUrl"`
	Cuisines       []string `json:"cuisines"`
	RecipeIDs      []string `json:"recipeIds"`
	MealCategories []string `json:"mealCategories"`
	Frequency      string   `json:"frequency"`
	Mode           string   `json:"mode"`
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (req Request) input() (Input, error) {
	in := Input{
		Name: req.Name, StoreItem: str(req.StoreItem), Unit: str(req.Unit), ProductURL: str(req.ProductURL),
		Cuisines: req.Cuisines, RecipeIDs: req.RecipeIDs, MealCategories: req.MealCategories, Frequency: req.Frequency, Mode: req.Mode,
	}
	if req.Quantity != nil {
		q, err := ingredients.QuantityFromFloat(*req.Quantity)
		if err != nil {
			return Input{}, invalid("Pick an amount from 1 to %d.", MaxQuantity)
		}
		in.Quantity = q.String()
	}
	return in, nil
}

func optionsJSON(opts []Option) []OptionJSON {
	out := make([]OptionJSON, 0, len(opts))
	for _, o := range opts {
		out = append(out, OptionJSON(o))
	}
	return out
}

func refs(in []RecipeRef) []RecipeRefJSON {
	out := make([]RecipeRefJSON, 0, len(in))
	for _, r := range in {
		out = append(out, RecipeRefJSON(r))
	}
	return out
}

func orEmpty(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

func (h *Handler) companionJSON(c Companion) CompanionJSON {
	cj := CompanionJSON{
		ID: c.ID, Name: c.Name, StoreItem: optional(c.StoreItem), ProductURL: optional(c.ProductURL),
		Cuisines: orEmpty(c.Cuisines), Recipes: refs(c.Recipes), MealCategories: orEmpty(c.MealCategories),
		Frequency: c.Frequency, Mode: c.Mode, Summary: c.Summary(h.opts.Service.CuisineLabel),
		LastListedWeek: optional(c.LastListedWeek), LastListedAt: c.LastListedAt, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
	if q, err := ingredients.ParseQuantity(c.Quantity); c.Quantity != "" && err == nil {
		f, unit := q.Float64(), c.Unit
		cj.Quantity, cj.Unit = &f, &unit
	}
	if c.LinkedRecipe != nil {
		cj.LinkedRecipe = &RecipeRefJSON{ID: c.LinkedRecipe.ID, Name: c.LinkedRecipe.Name}
	}
	return cj
}

// --- handlers ---------------------------------------------------------------------

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	list, err := h.opts.Service.List(r.Context(), actor.HouseholdID)
	if err != nil {
		h.writeError(w, r, "list companions failed", err)
		return
	}
	resp := ListResponse{
		Items: make([]CompanionJSON, 0, len(list)), Frequencies: optionsJSON(FrequencyOptions), Modes: optionsJSON(ModeOptions),
	}
	for _, c := range list {
		resp.Items = append(resp.Items, h.companionJSON(c))
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	c, err := h.opts.Service.Get(r.Context(), actor.HouseholdID, chi.URLParam(r, "companionId"))
	if err != nil {
		h.writeError(w, r, "get companion failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, h.companionJSON(c))
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req Request
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	in, err := req.input()
	if err != nil {
		h.writeError(w, r, "create companion failed", err)
		return
	}
	actor, _ := households.MembershipFromContext(r.Context())
	c, err := h.opts.Service.Create(r.Context(), actor.HouseholdID, actor.UserID, in)
	if err != nil {
		h.writeError(w, r, "create companion failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, h.companionJSON(c))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	var req Request
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	in, err := req.input()
	if err != nil {
		h.writeError(w, r, "update companion failed", err)
		return
	}
	actor, _ := households.MembershipFromContext(r.Context())
	c, err := h.opts.Service.Update(r.Context(), actor.HouseholdID, chi.URLParam(r, "companionId"), in)
	if err != nil {
		h.writeError(w, r, "update companion failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, h.companionJSON(c))
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	if err := h.opts.Service.Delete(r.Context(), actor.HouseholdID, chi.URLParam(r, "companionId")); err != nil {
		h.writeError(w, r, "delete companion failed", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	var validation *ValidationError
	switch {
	case errors.As(err, &validation):
		httpx.WriteError(w, r, http.StatusBadRequest, "validation_failed", validation.Message)
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", "That companion isn’t here anymore.")
	default:
		h.logger.ErrorContext(r.Context(), msg, "error", err)
		httpx.WriteError(w, r, http.StatusInternalServerError, "internal", "Something went wrong. Try again.")
	}
}
