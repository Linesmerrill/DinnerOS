package liveactivity

import (
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/auth"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/mealkit"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
	"github.com/Linesmerrill/DinnerOS/api/internal/push"
)

// HandlerOptions configures the Live Activity token endpoints.
type HandlerOptions struct {
	Store      Store
	Authorizer households.Authorizer
	Tokens     auth.AccessTokenValidator
	Logger     *slog.Logger
	Now        func() time.Time
}

// Handler serves PUT and DELETE
// /households/{householdId}/meal-kit/{source}/imports/{jobId}/live-activity.
type Handler struct {
	opts HandlerOptions
}

// NewHandler returns a Handler.
func NewHandler(opts HandlerOptions) *Handler {
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Handler{opts: opts}
}

// Mount registers the routes on r, the /api/v1 router. They need
// recipes.import, like every meal-kit import route: a member who could not
// start the import has no business attaching to it. The token travels in the
// body, never the path, so it stays out of request logs.
func (h *Handler) Mount(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireAuth(h.opts.Tokens))
		imports := households.RequirePermission(h.opts.Authorizer, households.PermRecipesImport, h.opts.Logger)
		const path = "/households/{householdId}/meal-kit/{source}/imports/{jobId}/live-activity"
		r.With(imports).Put(path, h.register)
		r.With(imports).Delete(path, h.unregister)
	})
}

// RegisterRequest is the body of PUT .../live-activity.
type RegisterRequest struct {
	// Token is the activity's push token as lowercase hex.
	Token string `json:"token"`
	// Environment is the APNs gateway the build's tokens belong to.
	Environment push.Environment `json:"environment"`
}

// tokenPattern matches an ActivityKit push token as hex. Apple does not
// promise a length; this bounds it.
var tokenPattern = regexp.MustCompile(`^[0-9a-f]{16,400}$`)

// Validate checks a registration.
func (r RegisterRequest) Validate() error {
	if !tokenPattern.MatchString(r.Token) {
		return &ValidationError{Message: "token must be the activity's push token in lowercase hex"}
	}
	if !r.Environment.Valid() {
		return &ValidationError{Message: `environment must be "sandbox" or "production"`}
	}
	return nil
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	householdID, source, jobID := chi.URLParam(r, "householdId"), chi.URLParam(r, "source"), chi.URLParam(r, "jobId")
	if !mealkit.KnownSource(source) {
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", "unknown meal-kit service")
		return
	}
	var req RegisterRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if err := req.Validate(); err != nil {
		h.writeError(w, r, "register live activity failed", err)
		return
	}
	err := h.opts.Store.Register(r.Context(), householdID, source, jobID, req.Token, req.Environment, h.opts.Now().UTC())
	if err != nil {
		h.writeError(w, r, "register live activity failed", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) unregister(w http.ResponseWriter, r *http.Request) {
	householdID, source, jobID := chi.URLParam(r, "householdId"), chi.URLParam(r, "source"), chi.URLParam(r, "jobId")
	if !mealkit.KnownSource(source) {
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", "unknown meal-kit service")
		return
	}
	if err := h.opts.Store.Unregister(r.Context(), householdID, source, jobID); err != nil {
		h.writeError(w, r, "unregister live activity failed", err)
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
		// The job finished, was stopped, or is not this household's. The app
		// ends its activity locally from the status it already polls.
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", "no import in progress with that id")
	default:
		h.opts.Logger.ErrorContext(r.Context(), msg, "error", err)
		httpx.WriteError(w, r, http.StatusInternalServerError, "internal", "internal server error")
	}
}
