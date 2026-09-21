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
// this is the same pipeline reached a different way. The credential travels
// in a body, never a path or a query, so it cannot reach a request log.
func (h *Handler) Mount(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireAuth(h.opts.Tokens))
		imports := households.RequirePermission(h.opts.Authorizer, households.PermRecipesImport, h.logger)
		r.With(imports).Get("/households/{householdId}/meal-kit/{source}", h.status)
		r.With(imports).Put("/households/{householdId}/meal-kit/{source}/link", h.link)
		r.With(imports).Delete("/households/{householdId}/meal-kit/{source}/link", h.unlink)
		r.With(imports).Post("/households/{householdId}/meal-kit/{source}/imports", h.start)
		r.With(imports).Get("/households/{householdId}/meal-kit/{source}/imports", h.listJobs)
	})
}

// --- Wire types ---------------------------------------------------------------

// LinkRequestBody is the body of PUT .../meal-kit/{source}/link.
//
// It carries the session the member's own sign-in on the meal kit's website
// produced — never a password: they never type one into this app. Nothing here
// is logged, and nothing is echoed back.
type LinkRequestBody struct {
	// AccessToken is the meal-kit session token. Required.
	AccessToken string `json:"accessToken"`
	// RefreshToken is optional: a session without one simply expires sooner
	// and the member links again.
	RefreshToken string `json:"refreshToken,omitempty"`
	// ExpiresAt is when the session stops working, when the client could work
	// it out. Omitted means "we do not know", and the worker finds out.
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	// StartImport defaults to true: linking an account is how onboarding
	// starts an import.
	StartImport *bool `json:"startImport,omitempty"`
}

// LinkResponse describes a stored link. It deliberately carries no email
// address and nothing derived from the tokens.
type LinkResponse struct {
	Source string `json:"source"`
	Status string `json:"status"`
	// AccountLabel is a display string such as "HelloFresh account".
	AccountLabel string     `json:"accountLabel"`
	LinkedAt     time.Time  `json:"linkedAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
	LastUsedAt   *time.Time `json:"lastUsedAt"`
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
	// Phase is orders, recipes, import, or done.
	Phase string `json:"phase"`
	// RecipesFound is 0 until the order history has been read.
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
	// Enabled is false when the server has no import encryption key, so the
	// app offers adding recipes by hand instead.
	Enabled bool          `json:"enabled"`
	Link    *LinkResponse `json:"link"`
	// LatestJob is the newest run, or null when there has never been one.
	LatestJob *JobResponse `json:"latestJob"`
}

// JobListResponse is returned by GET .../meal-kit/{source}/imports.
type JobListResponse struct {
	Items []JobResponse `json:"items"`
}

func newLinkResponse(l Link) *LinkResponse {
	out := &LinkResponse{
		Source: l.Source, Status: string(l.Status), AccountLabel: l.AccountLabel,
		LinkedAt: l.CreatedAt, UpdatedAt: l.UpdatedAt,
	}
	if !l.LastUsedAt.IsZero() {
		used := l.LastUsedAt
		out.LastUsedAt = &used
	}
	return out
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
	if status.Link != nil {
		resp.Link = newLinkResponse(*status.Link)
	}
	if status.Job != nil {
		resp.LatestJob = newJobResponse(*status.Job)
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) link(w http.ResponseWriter, r *http.Request) {
	householdID := chi.URLParam(r, "householdId")
	source := chi.URLParam(r, "source")
	userID, _ := auth.UserIDFromContext(r.Context())
	var body LinkRequestBody
	if !httpx.DecodeJSON(w, r, &body) {
		return
	}
	start := true
	if body.StartImport != nil {
		start = *body.StartImport
	}
	tokens := Tokens{AccessToken: body.AccessToken, RefreshToken: body.RefreshToken}
	if body.ExpiresAt != nil {
		tokens.ExpiresAt = body.ExpiresAt.UTC()
	}
	status, err := h.opts.Service.Link(r.Context(), LinkRequest{
		HouseholdID: householdID, UserID: userID, Source: source,
		Tokens: tokens, StartImport: start,
	})
	// body goes out of scope here; the tokens are only ever written sealed.
	if err != nil {
		h.writeError(w, r, "link meal-kit account failed", err)
		return
	}
	resp := StatusResponse{Source: source, Enabled: true}
	if status.Link != nil {
		resp.Link = newLinkResponse(*status.Link)
	}
	if status.Job != nil {
		resp.LatestJob = newJobResponse(*status.Job)
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) unlink(w http.ResponseWriter, r *http.Request) {
	householdID := chi.URLParam(r, "householdId")
	source := chi.URLParam(r, "source")
	if _, err := h.opts.Service.Unlink(r.Context(), householdID, source); err != nil {
		h.writeError(w, r, "unlink meal-kit account failed", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) start(w http.ResponseWriter, r *http.Request) {
	householdID := chi.URLParam(r, "householdId")
	source := chi.URLParam(r, "source")
	userID, _ := auth.UserIDFromContext(r.Context())
	job, err := h.opts.Service.StartImport(r.Context(), householdID, userID, source)
	if err != nil {
		h.writeError(w, r, "start meal-kit import failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, newJobResponse(job))
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
	case errors.Is(err, ErrNoLink):
		httpx.WriteError(w, r, http.StatusConflict, "not_linked",
			"link a meal-kit account before starting an import")
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", "resource not found")
	default:
		// The error can carry details of the request we made to the meal-kit
		// service, so it is logged and never returned.
		h.logger.ErrorContext(r.Context(), msg, "error", err)
		httpx.WriteError(w, r, http.StatusInternalServerError, "internal", "internal server error")
	}
}
