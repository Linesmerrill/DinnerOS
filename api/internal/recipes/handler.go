package recipes

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/auth"
	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
	"github.com/Linesmerrill/DinnerOS/api/internal/ratings"
)

// DefaultImportMaxBytes is the body limit for POST .../recipes/import when
// HandlerOptions.ImportMaxBytes is unset. A full HelloFresh order history
// (about 1000 recipes) is 10–15 MB.
const DefaultImportMaxBytes = 32 << 20

// HandlerOptions configures the recipe HTTP handlers.
type HandlerOptions struct {
	Service    *Service
	Authorizer households.Authorizer
	Tokens     auth.AccessTokenValidator
	Logger     *slog.Logger
	// ImportMaxBytes replaces the global body limit on the import route.
	// Default DefaultImportMaxBytes.
	ImportMaxBytes int64
	// ImportTimeout, when set, extends the server's read and write deadlines
	// for one import request, so a large upload is not cut off by the
	// server-wide ReadTimeout and WriteTimeout.
	ImportTimeout time.Duration
	// Ratings, when set, fills householdRating and myRating in list and
	// detail responses. Without it every recipe appears unrated.
	Ratings RatingReader
	// Events, when set, records import.completed after each successful import.
	Events events.Recorder
}

// RatingReader loads rating aggregates for recipes. *ratings.Service
// implements it.
type RatingReader interface {
	Summaries(ctx context.Context, householdID, userID string, recipeIDs []string) (map[string]ratings.Summary, error)
}

// Handler serves the recipe endpoints.
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
	if opts.ImportMaxBytes <= 0 {
		opts.ImportMaxBytes = DefaultImportMaxBytes
	}
	return &Handler{opts: opts, logger: logger}
}

// Mount registers the routes on r, which is expected to be the /api/v1 router.
func (h *Handler) Mount(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireAuth(h.opts.Tokens))
		// The ingredient catalog is global: signed in is enough.
		r.Get("/ingredients", h.searchIngredients)
		view := households.RequirePermission(h.opts.Authorizer, households.PermHouseholdView, h.logger)
		r.With(view).Get("/households/{householdId}/recipes", h.list)
		r.With(view).Get("/households/{householdId}/recipes/{recipeId}", h.get)
		// The larger import body limit applies only after the caller is
		// authenticated and authorized to import.
		r.With(
			households.RequirePermission(h.opts.Authorizer, households.PermRecipesImport, h.logger),
			httpx.OverrideBodyLimit(h.opts.ImportMaxBytes),
		).Post("/households/{householdId}/recipes/import", h.importRecipes)
	})
}

// --- Wire types ---------------------------------------------------------------

// RecipeSummaryResponse is a recipe in a list.
type RecipeSummaryResponse struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Headline        string   `json:"headline,omitempty"`
	ImageURL        string   `json:"imageUrl,omitempty"`
	TotalMinutes    int      `json:"totalMinutes,omitempty"`
	TimesOrdered    int      `json:"timesOrdered"`
	LastOrderedWeek string   `json:"lastOrderedWeek,omitempty"`
	IsAddon         bool     `json:"isAddon"`
	Tags            []string `json:"tags"`
	// HouseholdRating aggregates every member's rating; MyRating is the
	// caller's own, or null.
	HouseholdRating ratings.HouseholdRatingResponse `json:"householdRating"`
	MyRating        *ratings.RatingResponse         `json:"myRating"`
}

// RecipeListResponse is returned by GET /households/{householdId}/recipes.
type RecipeListResponse struct {
	Items      []RecipeSummaryResponse `json:"items"`
	NextCursor string                  `json:"nextCursor,omitempty"`
}

// RecipeResponse is returned by GET /households/{householdId}/recipes/{recipeId}.
type RecipeResponse struct {
	ID              string                     `json:"id"`
	HouseholdID     string                     `json:"householdId"`
	Source          string                     `json:"source"`
	SourceRecipeID  string                     `json:"sourceRecipeId"`
	SourceAliases   []string                   `json:"sourceAliases"`
	SourceURL       string                     `json:"sourceUrl,omitempty"`
	Name            string                     `json:"name"`
	Headline        string                     `json:"headline,omitempty"`
	Description     string                     `json:"description,omitempty"`
	ImageURL        string                     `json:"imageUrl,omitempty"`
	IsAddon         bool                       `json:"isAddon"`
	Servings        []int                      `json:"servings"`
	PrepMinutes     int                        `json:"prepMinutes,omitempty"`
	TotalMinutes    int                        `json:"totalMinutes,omitempty"`
	Difficulty      int                        `json:"difficulty,omitempty"`
	Cuisines        []string                   `json:"cuisines"`
	Tags            []string                   `json:"tags"`
	Utensils        []string                   `json:"utensils"`
	Allergens       []string                   `json:"allergens"`
	Nutrition       []NutrientResponse         `json:"nutritionPerServing"`
	Ingredients     []RecipeIngredientResponse `json:"ingredients"`
	Steps           []StepResponse             `json:"steps"`
	OrderWeeks      []string                   `json:"orderWeeks"`
	TimesOrdered    int                        `json:"timesOrdered"`
	LastOrderedWeek string                     `json:"lastOrderedWeek,omitempty"`
	CreatedAt       time.Time                  `json:"createdAt"`
	UpdatedAt       time.Time                  `json:"updatedAt"`
	// HouseholdRating aggregates every member's rating; MyRating is the
	// caller's own, or null.
	HouseholdRating ratings.HouseholdRatingResponse `json:"householdRating"`
	MyRating        *ratings.RatingResponse         `json:"myRating"`
}

// NutrientResponse is a per-serving nutrition value.
type NutrientResponse struct {
	Name   string  `json:"name"`
	Amount float64 `json:"amount"`
	Unit   string  `json:"unit"`
}

// StepResponse is one instruction.
type StepResponse struct {
	Index    int    `json:"index"`
	Text     string `json:"text"`
	ImageURL string `json:"imageUrl,omitempty"`
}

// RecipeIngredientResponse is an ingredient line with its catalog category.
type RecipeIngredientResponse struct {
	IngredientID string           `json:"ingredientId"`
	Name         string           `json:"name"`
	Category     string           `json:"category"`
	PantryStaple bool             `json:"pantryStaple"`
	Amounts      []AmountResponse `json:"amounts"`
}

// AmountResponse is an ingredient amount for one serving size. Quantity is
// exact ("3/4"); QuantityValue is the same amount as a number, for display.
// Both are null when the source gave no amount.
type AmountResponse struct {
	Servings      int      `json:"servings"`
	Quantity      *string  `json:"quantity"`
	QuantityValue *float64 `json:"quantityValue"`
	Unit          string   `json:"unit"`
	SourceUnit    string   `json:"sourceUnit"`
	RawText       string   `json:"rawText"`
}

// ImportResultResponse is returned by POST /households/{householdId}/recipes/import.
type ImportResultResponse struct {
	Created            int                   `json:"created"`
	Updated            int                   `json:"updated"`
	Unchanged          int                   `json:"unchanged"`
	IngredientsCreated int                   `json:"ingredientsCreated"`
	ReviewItems        int                   `json:"reviewItems"`
	Errors             []RecipeErrorResponse `json:"errors"`
}

// RecipeErrorResponse explains why one recipe in the file was not imported.
type RecipeErrorResponse struct {
	Index          int      `json:"index"`
	SourceRecipeID string   `json:"sourceRecipeId,omitempty"`
	Name           string   `json:"name,omitempty"`
	Problems       []string `json:"problems"`
}

func orEmpty[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func newRecipeResponse(r Recipe) RecipeResponse {
	resp := RecipeResponse{
		ID: r.ID, HouseholdID: r.HouseholdID, Source: r.Source, SourceRecipeID: r.SourceRecipeID,
		SourceAliases: orEmpty(r.SourceAliases), SourceURL: r.SourceURL,
		Name: r.Name, Headline: r.Headline, Description: r.Description, ImageURL: r.ImageURL, IsAddon: r.IsAddon,
		Servings: orEmpty(r.Servings), PrepMinutes: r.PrepMinutes, TotalMinutes: r.TotalMinutes, Difficulty: r.Difficulty,
		Cuisines: orEmpty(r.Cuisines), Tags: orEmpty(r.Tags), Utensils: orEmpty(r.Utensils), Allergens: orEmpty(r.Allergens),
		Nutrition:   make([]NutrientResponse, 0, len(r.Nutrition)),
		Ingredients: make([]RecipeIngredientResponse, 0, len(r.Ingredients)),
		Steps:       make([]StepResponse, 0, len(r.Steps)),
		OrderWeeks:  orEmpty(r.OrderWeeks), TimesOrdered: r.TimesOrdered, LastOrderedWeek: r.LastOrderedWeek,
		CreatedAt: r.CreatedAt.UTC(), UpdatedAt: r.UpdatedAt.UTC(),
	}
	for _, n := range r.Nutrition {
		resp.Nutrition = append(resp.Nutrition, NutrientResponse(n))
	}
	for _, s := range r.Steps {
		resp.Steps = append(resp.Steps, StepResponse(s))
	}
	for _, line := range r.Ingredients {
		lr := RecipeIngredientResponse{
			IngredientID: line.IngredientID, Name: line.Name, Category: line.Category, PantryStaple: line.PantryStaple,
			Amounts: make([]AmountResponse, 0, len(line.Amounts)),
		}
		for _, a := range line.Amounts {
			ar := AmountResponse{Servings: a.Servings, Unit: a.Unit, SourceUnit: a.SourceUnit, RawText: a.RawText}
			if q, ok := a.ExactQuantity(); ok {
				exact, value := a.Quantity, q.Float64()
				ar.Quantity, ar.QuantityValue = &exact, &value
			}
			lr.Amounts = append(lr.Amounts, ar)
		}
		resp.Ingredients = append(resp.Ingredients, lr)
	}
	return resp
}

func newImportResultResponse(res ImportResult) ImportResultResponse {
	resp := ImportResultResponse{
		Created: res.Created, Updated: res.Updated, Unchanged: res.Unchanged,
		IngredientsCreated: res.IngredientsCreated, ReviewItems: res.ReviewItems,
		Errors: make([]RecipeErrorResponse, 0, len(res.Errors)),
	}
	for _, e := range res.Errors {
		resp.Errors = append(resp.Errors, RecipeErrorResponse(e))
	}
	return resp
}

// --- Handlers -----------------------------------------------------------------

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	q, err := parseListQuery(r.URL.Query())
	if err != nil {
		validationFailed(w, r, err.Error())
		return
	}
	page, err := h.opts.Service.List(r.Context(), actor.HouseholdID, q)
	switch {
	case errors.Is(err, ErrInvalidQuery):
		validationFailed(w, r, publicMessage(err))
		return
	case err != nil:
		h.internalError(w, r, "list recipes failed", err)
		return
	}
	ids := make([]string, 0, len(page.Items))
	for _, s := range page.Items {
		ids = append(ids, s.ID)
	}
	summaries, err := h.ratingSummaries(r.Context(), actor, ids)
	if err != nil {
		h.internalError(w, r, "load recipe ratings failed", err)
		return
	}
	resp := RecipeListResponse{Items: make([]RecipeSummaryResponse, 0, len(page.Items)), NextCursor: page.NextCursor}
	for _, s := range page.Items {
		item := RecipeSummaryResponse{
			ID: s.ID, Name: s.Name, Headline: s.Headline, ImageURL: s.ImageURL, TotalMinutes: s.TotalMinutes,
			TimesOrdered: s.TimesOrdered, LastOrderedWeek: s.LastOrderedWeek, IsAddon: s.IsAddon, Tags: orEmpty(s.Tags),
		}
		item.HouseholdRating, item.MyRating = ratingFields(summaries[s.ID])
		resp.Items = append(resp.Items, item)
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func parseListQuery(v url.Values) (ListQuery, error) {
	q := ListQuery{
		Search:  v.Get("q"),
		Tag:     v.Get("tag"),
		Cuisine: v.Get("cuisine"),
		Sort:    Sort(v.Get("sort")),
		Cursor:  v.Get("cursor"),
	}
	if s := v.Get("addons"); s != "" {
		b, err := strconv.ParseBool(s)
		if err != nil {
			return ListQuery{}, errors.New("addons must be true or false")
		}
		q.Addons = &b
	}
	if s := v.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 {
			return ListQuery{}, errors.New("limit must be a positive integer")
		}
		q.Limit = n
	}
	return q, nil
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	recipe, err := h.opts.Service.Get(r.Context(), actor.HouseholdID, chi.URLParam(r, "recipeId"))
	switch {
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", "recipe not found")
		return
	case err != nil:
		h.internalError(w, r, "get recipe failed", err)
		return
	}
	summaries, err := h.ratingSummaries(r.Context(), actor, []string{recipe.ID})
	if err != nil {
		h.internalError(w, r, "load recipe rating failed", err)
		return
	}
	resp := newRecipeResponse(recipe)
	resp.HouseholdRating, resp.MyRating = ratingFields(summaries[recipe.ID])
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) importRecipes(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	if h.opts.ImportTimeout > 0 {
		extendDeadlines(w, h.opts.ImportTimeout)
	}
	var file ImportFile
	if !httpx.DecodeJSON(w, r, &file) {
		return
	}
	res, err := h.opts.Service.Import(r.Context(), actor.HouseholdID, file)
	switch {
	case errors.Is(err, ErrInvalidImport):
		validationFailed(w, r, publicMessage(err))
		return
	case errors.Is(err, ErrDuplicate):
		httpx.WriteError(w, r, http.StatusConflict, "conflict", "another import changed these recipes at the same time; retry")
		return
	case err != nil:
		h.internalError(w, r, "import recipes failed", err)
		return
	}
	h.logger.InfoContext(r.Context(), "recipes imported",
		"householdId", actor.HouseholdID, "userId", actor.UserID,
		"created", res.Created, "updated", res.Updated, "unchanged", res.Unchanged,
		"ingredientsCreated", res.IngredientsCreated, "reviewItems", res.ReviewItems, "rejected", len(res.Errors))
	// Best effort: the import already succeeded, so a failed event is only logged.
	events.RecordOrLog(r.Context(), h.opts.Events, h.logger, events.Event{
		HouseholdID: actor.HouseholdID, UserID: actor.UserID, Type: events.TypeImportCompleted,
		Payload: events.ImportCompleted{Source: file.Source, Created: res.Created, Updated: res.Updated, Unchanged: res.Unchanged, Rejected: len(res.Errors)},
	})
	httpx.WriteJSON(w, http.StatusOK, newImportResultResponse(res))
}

// ratingSummaries loads rating aggregates, and the actor's own ratings, for
// recipes the actor is authorized to view. Without a RatingReader, recipes
// are unrated.
func (h *Handler) ratingSummaries(ctx context.Context, actor households.Membership, recipeIDs []string) (map[string]ratings.Summary, error) {
	if h.opts.Ratings == nil || len(recipeIDs) == 0 {
		return nil, nil
	}
	return h.opts.Ratings.Summaries(ctx, actor.HouseholdID, actor.UserID, recipeIDs)
}

func ratingFields(s ratings.Summary) (ratings.HouseholdRatingResponse, *ratings.RatingResponse) {
	var mine *ratings.RatingResponse
	if s.Mine != nil {
		r := ratings.NewRatingResponse(*s.Mine)
		mine = &r
	}
	return ratings.NewHouseholdRatingResponse(s), mine
}

// extendDeadlines lets one request outlive the server-wide ReadTimeout and
// WriteTimeout. It must run before the body is read. Response writers that
// can't set deadlines (such as test recorders) are left alone.
func extendDeadlines(w http.ResponseWriter, d time.Duration) {
	rc := http.NewResponseController(w)
	deadline := time.Now().Add(d)
	_ = rc.SetReadDeadline(deadline)
	_ = rc.SetWriteDeadline(deadline)
}

func (h *Handler) internalError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	h.logger.ErrorContext(r.Context(), msg, "error", err)
	httpx.WriteError(w, r, http.StatusInternalServerError, "internal", "internal server error")
}

func validationFailed(w http.ResponseWriter, r *http.Request, message string) {
	httpx.WriteError(w, r, http.StatusBadRequest, "validation_failed", message)
}

// publicMessage strips the package prefix from a sentinel-wrapped error whose
// details are safe to show clients.
func publicMessage(err error) string {
	return strings.TrimPrefix(err.Error(), "recipes: ")
}
