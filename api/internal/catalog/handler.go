package catalog

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/auth"
	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// HandlerOptions configures the catalog HTTP handlers.
type HandlerOptions struct {
	Service    *Service
	Authorizer households.Authorizer
	Tokens     auth.AccessTokenValidator
	Logger     *slog.Logger
}

// Handler serves the discovery and catalog endpoints.
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

// Mount registers the routes on r, the /api/v1 router.
//
// Every route is household-scoped even though the catalog is global: reading
// it is only useful marked against a household's library, and scoping the
// route means one authorization rule for the whole module instead of two.
func (h *Handler) Mount(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireAuth(h.opts.Tokens))
		view := households.RequirePermission(h.opts.Authorizer, households.PermHouseholdView, h.logger)
		edit := households.RequirePermission(h.opts.Authorizer, households.PermRecipesEdit, h.logger)
		const base = "/households/{householdId}"
		r.With(view).Get(base+"/discover", h.discover)
		r.With(view).Get(base+"/catalog/recipes", h.search)
		r.With(view).Get(base+"/catalog/recipes/{catalogRecipeId}", h.get)
		r.With(edit).Post(base+"/catalog/recipes/{catalogRecipeId}/add", h.add)
	})
}

// --- Wire types ---------------------------------------------------------------

// CatalogSummaryResponse is a catalog recipe in a list. It deliberately
// carries no rating, order history, or note: those belong to a household, and
// this document belongs to no household.
type CatalogSummaryResponse struct {
	ID         string `json:"id"`
	CatalogKey string `json:"catalogKey"`
	Source     string `json:"source"`
	Name       string `json:"name"`
	Headline   string `json:"headline,omitempty"`
	ImageURL   string `json:"imageUrl,omitempty"`
	IsAddon    bool   `json:"isAddon"`
	// CookMinutes is max(prepMinutes, totalMinutes), omitted when unknown.
	TotalMinutes int                 `json:"totalMinutes,omitempty"`
	CookMinutes  int                 `json:"cookMinutes,omitempty"`
	TimeBand     *autopilot.TimeBand `json:"timeBand"`
	Calories     *int                `json:"calories"`
	ProteinGrams *int                `json:"proteinGrams"`
	Cuisines     []string            `json:"cuisines"`
	Tags         []string            `json:"tags"`
	// InLibrary is true when the household already has this recipe;
	// LibraryRecipeID is its own recipe's ID.
	InLibrary       bool   `json:"inLibrary"`
	LibraryRecipeID string `json:"libraryRecipeId,omitempty"`
	// Reasons explain the discovery ranking ("You like Thai"). Empty for
	// search and for a household with no taste profile.
	Reasons []string `json:"reasons"`
}

// CatalogListResponse is returned by GET .../discover and .../catalog/recipes.
type CatalogListResponse struct {
	Items      []CatalogSummaryResponse `json:"items"`
	NextCursor string                   `json:"nextCursor,omitempty"`
	// Total is how many entries matched, capped at the paging limit.
	Total int `json:"total"`
}

// CatalogRecipeResponse is returned by GET .../catalog/recipes/{id}.
type CatalogRecipeResponse struct {
	CatalogSummaryResponse
	Description string                      `json:"description,omitempty"`
	SourceURL   string                      `json:"sourceUrl,omitempty"`
	Servings    []int                       `json:"servings"`
	PrepMinutes int                         `json:"prepMinutes,omitempty"`
	Difficulty  int                         `json:"difficulty,omitempty"`
	Utensils    []string                    `json:"utensils"`
	Allergens   []string                    `json:"allergens"`
	Nutrition   []recipes.NutrientResponse  `json:"nutritionPerServing"`
	Ingredients []CatalogIngredientResponse `json:"ingredients"`
	Steps       []recipes.StepResponse      `json:"steps"`
}

// CatalogIngredientResponse is an ingredient line on a catalog recipe. Unlike
// a household recipe's line it carries no category or image: those are read
// from the global ingredient catalog when the recipe is in a library.
type CatalogIngredientResponse struct {
	IngredientID string                   `json:"ingredientId"`
	Name         string                   `json:"name"`
	PantryStaple bool                     `json:"pantryStaple"`
	Amounts      []recipes.AmountResponse `json:"amounts"`
}

// AddToLibraryResponse is returned by POST .../catalog/recipes/{id}/add.
type AddToLibraryResponse struct {
	// RecipeID is the household's own copy.
	RecipeID string `json:"recipeId"`
	// Created is false when the household already had this recipe, which is
	// not an error: the request's goal is met either way.
	Created bool `json:"created"`
}

// --- Handlers -----------------------------------------------------------------

func (h *Handler) discover(w http.ResponseWriter, r *http.Request) {
	q, ok := h.query(w, r, false)
	if !ok {
		return
	}
	page, err := h.opts.Service.Discover(r.Context(), householdID(r), q)
	if err != nil {
		h.fail(w, r, "discover failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, listResponse(page))
}

func (h *Handler) search(w http.ResponseWriter, r *http.Request) {
	q, ok := h.query(w, r, true)
	if !ok {
		return
	}
	page, err := h.opts.Service.Search(r.Context(), householdID(r), q)
	if err != nil {
		h.fail(w, r, "catalog search failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, listResponse(page))
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	found, err := h.opts.Service.Get(r.Context(), householdID(r), chi.URLParam(r, "catalogRecipeId"))
	if err != nil {
		h.fail(w, r, "catalog get failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, recipeResponse(found))
}

func (h *Handler) add(w http.ResponseWriter, r *http.Request) {
	stored, created, err := h.opts.Service.Add(r.Context(), householdID(r), chi.URLParam(r, "catalogRecipeId"))
	if err != nil {
		h.fail(w, r, "add catalog recipe failed", err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	httpx.WriteJSON(w, status, AddToLibraryResponse{RecipeID: stored.ID, Created: created})
}

// --- Helpers ------------------------------------------------------------------

func householdID(r *http.Request) string { return chi.URLParam(r, households.HouseholdIDParam) }

// query reads the shared list parameters. withText decides whether q is read;
// discovery has no free-text term of its own.
func (h *Handler) query(w http.ResponseWriter, r *http.Request, withText bool) (Query, bool) {
	v := r.URL.Query()
	q := Query{
		Cuisine:    v.Get("cuisine"),
		Tag:        v.Get("tag"),
		Ingredient: v.Get("ingredient"),
		Cursor:     v.Get("cursor"),
	}
	if withText {
		q.Text = v.Get("q")
	}
	if s := v.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 {
			httpx.WriteError(w, r, http.StatusBadRequest, "validation_failed", "limit must be a positive integer")
			return Query{}, false
		}
		q.Limit = n
	}
	return q, true
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, msg string, err error) {
	switch {
	case errors.Is(err, ErrInvalidQuery):
		httpx.WriteError(w, r, http.StatusBadRequest, "validation_failed", publicMessage(err))
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", "recipe not found")
	default:
		h.logger.ErrorContext(r.Context(), msg, "error", err)
		httpx.WriteError(w, r, http.StatusInternalServerError, "internal", "something went wrong")
	}
}

// publicMessage strips the package prefix an ErrInvalidQuery carries, leaving
// the part written for the caller.
func publicMessage(err error) string {
	msg := err.Error()
	if _, rest, ok := strings.Cut(msg, "invalid query: "); ok {
		return rest
	}
	return "request is invalid"
}

func listResponse(p Page) CatalogListResponse {
	out := CatalogListResponse{Items: make([]CatalogSummaryResponse, 0, len(p.Items)), NextCursor: p.NextCursor, Total: p.Total}
	for _, it := range p.Items {
		out.Items = append(out.Items, summaryResponse(it))
	}
	return out
}

func summaryResponse(it Result) CatalogSummaryResponse {
	c := it.Content
	cook := recipes.CookMinutes(c.PrepMinutes, c.TotalMinutes)
	facts := recipes.FactsOf(c.Nutrition)
	return CatalogSummaryResponse{
		ID: it.ID, CatalogKey: it.CatalogKey, Source: c.Source, Name: c.Name, Headline: c.Headline,
		ImageURL: c.ImageURL, IsAddon: c.IsAddon, TotalMinutes: c.TotalMinutes, CookMinutes: cook,
		TimeBand: recipes.TimeBandOf(cook, autopilot.TimeBands{}), Calories: facts.Calories, ProteinGrams: facts.ProteinGrams,
		Cuisines: orEmpty(c.Cuisines), Tags: orEmpty(c.Tags),
		InLibrary: it.LibraryRecipeID != "", LibraryRecipeID: it.LibraryRecipeID, Reasons: orEmpty(it.Reasons),
	}
}

func recipeResponse(it Result) CatalogRecipeResponse {
	c := it.Content
	out := CatalogRecipeResponse{
		CatalogSummaryResponse: summaryResponse(it),
		Description:            c.Description, SourceURL: c.SourceURL, Servings: orEmpty(c.Servings),
		PrepMinutes: c.PrepMinutes, Difficulty: c.Difficulty,
		Utensils: orEmpty(c.Utensils), Allergens: orEmpty(c.Allergens),
		Nutrition:   make([]recipes.NutrientResponse, 0, len(c.Nutrition)),
		Ingredients: make([]CatalogIngredientResponse, 0, len(c.Ingredients)),
		Steps:       make([]recipes.StepResponse, 0, len(c.Steps)),
	}
	for _, n := range c.Nutrition {
		out.Nutrition = append(out.Nutrition, recipes.NutrientResponse(n))
	}
	for _, line := range c.Ingredients {
		l := CatalogIngredientResponse{
			IngredientID: line.IngredientID, Name: line.Name, PantryStaple: line.PantryStaple,
			Amounts: make([]recipes.AmountResponse, 0, len(line.Amounts)),
		}
		for _, a := range line.Amounts {
			l.Amounts = append(l.Amounts, recipes.NewAmountResponse(a))
		}
		out.Ingredients = append(out.Ingredients, l)
	}
	for _, s := range c.Steps {
		out.Steps = append(out.Steps, recipes.StepResponse(s))
	}
	return out
}

func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
