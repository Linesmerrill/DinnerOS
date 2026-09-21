package mealkit

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/auth"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

// HandlerOptions configures the meal-kit import HTTP handlers.
type HandlerOptions struct {
	Service    *Service
	Authorizer households.Authorizer
	Tokens     auth.AccessTokenValidator
	Logger     *slog.Logger
}

// Handler serves the meal-kit import endpoints.
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
// Every route needs recipes.import, the same permission a file import needs:
// this is the same pipeline reached a different way. There is no link route,
// because there is nothing to link: an import carries the order history the
// member's own browser session just read, and the server keeps no credential.
func (h *Handler) Mount(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireAuth(h.opts.Tokens))
		imports := households.RequirePermission(h.opts.Authorizer, households.PermRecipesImport, h.logger)
		r.With(imports).Get("/households/{householdId}/meal-kit/{source}", h.status)
		r.With(imports).Post("/households/{householdId}/meal-kit/{source}/imports", h.start)
		r.With(imports).Get("/households/{householdId}/meal-kit/{source}/imports", h.listJobs)
		r.With(imports).Delete("/households/{householdId}/meal-kit/{source}/imports", h.stop)
	})
}

// --- Wire types ---------------------------------------------------------------

// OrderedRecipeBody is one recipe of the harvested order history.
//
// Every field is untrusted input: it was read from someone else's web page in
// a web view. The service puts each one through the source's own allow-list
// before anything is stored or fetched.
type OrderedRecipeBody struct {
	// SourceRecipeID is the meal kit's id for the recipe. Required.
	SourceRecipeID string `json:"sourceRecipeId"`
	// Name is a label for the app to show before the page is read.
	Name string `json:"name,omitempty"`
	// URL is the public recipe page. It is used only when it already points
	// at the service's own recipe pages; otherwise it is rebuilt from the id.
	URL string `json:"url,omitempty"`
	// Weeks are the ISO weeks ("2026-W38") it was delivered.
	Weeks []string `json:"weeks,omitempty"`
	// IsAddon marks sides and extras rather than a main meal.
	IsAddon bool `json:"isAddon,omitempty"`
}

// StartImportBody is the body of POST .../meal-kit/{source}/imports.
//
// It carries the order history and nothing else. There is deliberately no
// field for a token, a cookie, or an account: the app must not send one, and
// this server has nowhere to put one.
type StartImportBody struct {
	Recipes []OrderedRecipeBody `json:"recipes"`
}

// FailedRecipeResponse is one recipe of the order history that did not make it.
type FailedRecipeResponse struct {
	SourceRecipeID string `json:"sourceRecipeId"`
	Name           string `json:"name,omitempty"`
	Reason         string `json:"reason"`
}

// JobErrorResponse is the last thing that went wrong, ready to show.
type JobErrorResponse struct {
	Code    string    `json:"code"`
	Message string    `json:"message"`
	At      time.Time `json:"at"`
}

// JobResponse is one import run.
type JobResponse struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Status string `json:"status"`
	// Phase is recipes, import, or done.
	Phase string `json:"phase"`
	// RecipesFound is how many recipes the submitted order history carried.
	RecipesFound int `json:"recipesFound"`
	RecipesDone  int `json:"recipesDone"`
	Imported     int `json:"imported"`
	Updated      int `json:"updated"`
	Unchanged    int `json:"unchanged"`
	// ReviewItems counts things the importer could not map confidently; they
	// are read through the recipes import-reviews route.
	ReviewItems int                    `json:"reviewItems"`
	Failures    []FailedRecipeResponse `json:"failures"`
	Attempts    int                    `json:"attempts"`
	MaxAttempts int                    `json:"maxAttempts"`
	LastError   *JobErrorResponse      `json:"lastError"`
	CreatedAt   time.Time              `json:"createdAt"`
	UpdatedAt   time.Time              `json:"updatedAt"`
	FinishedAt  *time.Time             `json:"finishedAt"`
}

// StatusResponse is returned by GET .../meal-kit/{source}.
type StatusResponse struct {
	Source string `json:"source"`
	// Enabled is false when the server has meal-kit import turned off, so the
	// app offers adding recipes by hand instead.
	Enabled bool `json:"enabled"`
	// LatestJob is the newest run, or null when there has never been one.
	LatestJob *JobResponse `json:"latestJob"`
}

// JobListResponse is returned by GET .../meal-kit/{source}/imports.
type JobListResponse struct {
	Items []JobResponse `json:"items"`
}

func newJobResponse(j Job) *JobResponse {
	out := &JobResponse{
		ID: j.ID, Source: j.Source, Status: string(j.Status), Phase: string(j.Checkpoint.Phase),
		RecipesFound: j.RecipesFound(), RecipesDone: j.RecipesDone(),
		Imported: j.Checkpoint.Imported, Updated: j.Checkpoint.Updated, Unchanged: j.Checkpoint.Unchanged,
		ReviewItems: j.Checkpoint.ReviewItems, Failures: []FailedRecipeResponse{},
		Attempts: j.Attempts, MaxAttempts: j.MaxAttempts,
		CreatedAt: j.CreatedAt, UpdatedAt: j.UpdatedAt,
	}
	for _, f := range j.Checkpoint.Failures {
		out.Failures = append(out.Failures, FailedRecipeResponse(f))
	}
	if j.LastError != nil {
		out.LastError = &JobErrorResponse{Code: j.LastError.Code, Message: j.LastError.Message, At: j.LastError.At}
	}
	if !j.FinishedAt.IsZero() {
		finished := j.FinishedAt
		out.FinishedAt = &finished
	}
	return out
}

// --- Handlers -----------------------------------------------------------------

func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	householdID := chi.URLParam(r, "householdId")
	source := chi.URLParam(r, "source")
	if !KnownSource(source) {
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", "unknown meal-kit service")
		return
	}
	resp := StatusResponse{Source: source, Enabled: h.opts.Service.Enabled()}
	status, err := h.opts.Service.Status(r.Context(), householdID, source)
	if err != nil {
		h.writeError(w, r, "read meal-kit status failed", err)
		return
	}
	if status.Job != nil {
		resp.LatestJob = newJobResponse(*status.Job)
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) start(w http.ResponseWriter, r *http.Request) {
	householdID := chi.URLParam(r, "householdId")
	source := chi.URLParam(r, "source")
	userID, _ := auth.UserIDFromContext(r.Context())
	var body StartImportBody
	if !httpx.DecodeJSON(w, r, &body) {
		return
	}
	orders := make([]OrderedRecipe, 0, len(body.Recipes))
	for _, in := range body.Recipes {
		orders = append(orders, OrderedRecipe(in))
	}
	job, err := h.opts.Service.StartImport(r.Context(), ImportRequest{
		HouseholdID: householdID, UserID: userID, Source: source, Orders: orders,
	})
	if err != nil {
		h.writeError(w, r, "start meal-kit import failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, newJobResponse(job))
}

func (h *Handler) stop(w http.ResponseWriter, r *http.Request) {
	householdID := chi.URLParam(r, "householdId")
	source := chi.URLParam(r, "source")
	if _, err := h.opts.Service.StopImports(r.Context(), householdID, source); err != nil {
		h.writeError(w, r, "stop meal-kit imports failed", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) listJobs(w http.ResponseWriter, r *http.Request) {
	householdID := chi.URLParam(r, "householdId")
	source := chi.URLParam(r, "source")
	if !KnownSource(source) {
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", "unknown meal-kit service")
		return
	}
	jobs, err := h.opts.Service.ListJobs(r.Context(), householdID, 20)
	if err != nil {
		h.writeError(w, r, "list meal-kit imports failed", err)
		return
	}
	resp := JobListResponse{Items: []JobResponse{}}
	for _, j := range jobs {
		if j.Source == source {
			resp.Items = append(resp.Items, *newJobResponse(j))
		}
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	var validation *ValidationError
	switch {
	case errors.As(err, &validation):
		httpx.WriteError(w, r, http.StatusBadRequest, "validation_failed", validation.Message)
	case errors.Is(err, ErrDisabled):
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "import_disabled",
			"meal-kit recipe import is not enabled on this server")
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", "resource not found")
	default:
		// The error can carry details of the request we made to the meal-kit
		// service, so it is logged and never returned.
		h.logger.ErrorContext(r.Context(), msg, "error", err)
		httpx.WriteError(w, r, http.StatusInternalServerError, "internal", "internal server error")
	}
}
