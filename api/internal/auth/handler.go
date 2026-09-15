package auth

import (
	"errors"
	"log/slog"
	"net/http"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
	"github.com/Linesmerrill/DinnerOS/api/internal/users"
)

// Request field limits. Identity tokens are a few KB; the body limit
// (HTTP_MAX_BODY_BYTES) bounds them further.
const (
	maxTokenLength       = 16 << 10
	maxNonceLength       = 512
	maxDisplayNameLength = 100
	maxSubjectLength     = 256
	maxEmailLength       = 254
)

// HandlerOptions configures the auth HTTP handlers.
type HandlerOptions struct {
	Service *Service
	Tokens  AccessTokenValidator
	Logger  *slog.Logger
	// RateLimit, when set, wraps every /auth/* route.
	RateLimit func(http.Handler) http.Handler
	// DevLoginEnabled mounts POST /auth/dev. Only set it in development.
	DevLoginEnabled bool
}

// Handler serves the authentication endpoints.
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
	r.Route("/auth", func(r chi.Router) {
		if h.opts.RateLimit != nil {
			r.Use(h.opts.RateLimit)
		}
		r.Post("/apple", h.signInWithApple)
		r.Post("/google", h.signInWithGoogle)
		r.Post("/refresh", h.refresh)
		r.Post("/logout", h.logout)
		if h.opts.DevLoginEnabled {
			r.Post("/dev", h.signInDev)
		}
	})
	r.With(RequireAuth(h.opts.Tokens)).Get("/me", h.me)
}

// --- Wire types ---------------------------------------------------------------

// UserResponse is the public representation of a user.
type UserResponse struct {
	ID           string    `json:"id"`
	DisplayName  string    `json:"displayName"`
	PrimaryEmail string    `json:"primaryEmail,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
}

func newUserResponse(u users.User) UserResponse {
	return UserResponse{ID: u.ID, DisplayName: u.DisplayName, PrimaryEmail: u.PrimaryEmail, CreatedAt: u.CreatedAt.UTC()}
}

// TokenPairResponse is returned by POST /auth/refresh.
type TokenPairResponse struct {
	AccessToken           string    `json:"accessToken"`
	AccessTokenExpiresAt  time.Time `json:"accessTokenExpiresAt"`
	RefreshToken          string    `json:"refreshToken"`
	RefreshTokenExpiresAt time.Time `json:"refreshTokenExpiresAt"`
}

func newTokenPairResponse(p TokenPair) TokenPairResponse {
	return TokenPairResponse{
		AccessToken:           p.AccessToken,
		AccessTokenExpiresAt:  p.AccessTokenExpiresAt.UTC(),
		RefreshToken:          p.RefreshToken,
		RefreshTokenExpiresAt: p.RefreshTokenExpiresAt.UTC(),
	}
}

// SessionResponse is returned by every sign-in endpoint.
type SessionResponse struct {
	TokenPairResponse
	User      UserResponse `json:"user"`
	IsNewUser bool         `json:"isNewUser"`
}

// IdentityResponse is a login method attached to the current user.
type IdentityResponse struct {
	Provider string `json:"provider"`
	Email    string `json:"email,omitempty"`
}

// MeResponse is returned by GET /me.
type MeResponse struct {
	User       UserResponse       `json:"user"`
	Identities []IdentityResponse `json:"identities"`
}

type appleSignInRequest struct {
	IdentityToken string `json:"identityToken"`
	Nonce         string `json:"nonce"`
	FullName      string `json:"fullName"`
}

type googleSignInRequest struct {
	IDToken string `json:"idToken"`
	Nonce   string `json:"nonce"`
}

type refreshTokenRequest struct {
	RefreshToken string `json:"refreshToken"`
}

type devSignInRequest struct {
	Subject     string `json:"subject"`
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
}

// --- Handlers -----------------------------------------------------------------

func (h *Handler) signInWithApple(w http.ResponseWriter, r *http.Request) {
	var req appleSignInRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	fullName := strings.TrimSpace(req.FullName)
	switch {
	case req.IdentityToken == "":
		validationFailed(w, r, "identityToken is required")
		return
	case len(req.IdentityToken) > maxTokenLength:
		validationFailed(w, r, "identityToken is too long")
		return
	case req.Nonce == "":
		validationFailed(w, r, "nonce is required")
		return
	case len(req.Nonce) > maxNonceLength:
		validationFailed(w, r, "nonce is too long")
		return
	case utf8.RuneCountInString(fullName) > maxDisplayNameLength:
		validationFailed(w, r, "fullName is too long")
		return
	}
	res, err := h.opts.Service.SignInWithProvider(r.Context(), users.ProviderApple, req.IdentityToken, req.Nonce, fullName)
	h.writeSignIn(w, r, users.ProviderApple, res, err)
}

func (h *Handler) signInWithGoogle(w http.ResponseWriter, r *http.Request) {
	var req googleSignInRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	switch {
	case req.IDToken == "":
		validationFailed(w, r, "idToken is required")
		return
	case len(req.IDToken) > maxTokenLength:
		validationFailed(w, r, "idToken is too long")
		return
	case len(req.Nonce) > maxNonceLength:
		validationFailed(w, r, "nonce is too long")
		return
	}
	res, err := h.opts.Service.SignInWithProvider(r.Context(), users.ProviderGoogle, req.IDToken, req.Nonce, "")
	h.writeSignIn(w, r, users.ProviderGoogle, res, err)
}

func (h *Handler) signInDev(w http.ResponseWriter, r *http.Request) {
	var req devSignInRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	subject := strings.TrimSpace(req.Subject)
	email := strings.ToLower(strings.TrimSpace(req.Email))
	displayName := strings.TrimSpace(req.DisplayName)
	switch {
	case subject == "":
		validationFailed(w, r, "subject is required")
		return
	case len(subject) > maxSubjectLength:
		validationFailed(w, r, "subject is too long")
		return
	case email != "" && (len(email) > maxEmailLength || !validEmail(email)):
		validationFailed(w, r, "email is invalid")
		return
	case utf8.RuneCountInString(displayName) > maxDisplayNameLength:
		validationFailed(w, r, "displayName is too long")
		return
	}
	res, err := h.opts.Service.SignInDev(r.Context(), subject, email, displayName)
	h.writeSignIn(w, r, users.ProviderDev, res, err)
}

func (h *Handler) writeSignIn(w http.ResponseWriter, r *http.Request, provider users.Provider, res SignInResult, err error) {
	switch {
	case err == nil:
		httpx.WriteJSON(w, http.StatusOK, SessionResponse{
			TokenPairResponse: newTokenPairResponse(res.Tokens),
			User:              newUserResponse(res.User),
			IsNewUser:         res.IsNewUser,
		})
	case errors.Is(err, ErrInvalidIdentityToken):
		// The wrapped reason (bad audience, expired, ...) never contains the token.
		h.logger.InfoContext(r.Context(), "identity token rejected", "provider", string(provider), "reason", err.Error())
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthenticated", "identity token is invalid")
	case errors.Is(err, ErrProviderNotConfigured):
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "provider_unavailable", "this sign-in method is not available")
	case errors.Is(err, ErrKeysUnavailable):
		h.logger.ErrorContext(r.Context(), "provider keys unavailable", "provider", string(provider), "error", err)
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "provider_unavailable", "sign-in is temporarily unavailable")
	default:
		h.internalError(w, r, "sign-in failed", err)
	}
}

func (h *Handler) refresh(w http.ResponseWriter, r *http.Request) {
	var req refreshTokenRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.RefreshToken == "" {
		validationFailed(w, r, "refreshToken is required")
		return
	}
	pair, err := h.opts.Service.Refresh(r.Context(), req.RefreshToken)
	switch {
	case err == nil:
		httpx.WriteJSON(w, http.StatusOK, newTokenPairResponse(pair))
	case errors.Is(err, ErrInvalidRefreshToken), errors.Is(err, ErrRefreshTokenReused):
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthenticated", "refresh token is invalid")
	default:
		h.internalError(w, r, "refresh failed", err)
	}
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	var req refreshTokenRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.RefreshToken == "" {
		validationFailed(w, r, "refreshToken is required")
		return
	}
	if err := h.opts.Service.Logout(r.Context(), req.RefreshToken); err != nil {
		h.internalError(w, r, "logout failed", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	userID, _ := UserIDFromContext(r.Context())
	user, identities, err := h.opts.Service.Me(r.Context(), userID)
	if errors.Is(err, users.ErrNotFound) {
		// A valid token for a user that no longer exists.
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthenticated", "user no longer exists")
		return
	}
	if err != nil {
		h.internalError(w, r, "load current user failed", err)
		return
	}
	resp := MeResponse{User: newUserResponse(user), Identities: make([]IdentityResponse, 0, len(identities))}
	for _, identity := range identities {
		resp.Identities = append(resp.Identities, IdentityResponse{Provider: string(identity.Provider), Email: identity.Email})
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) internalError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	h.logger.ErrorContext(r.Context(), msg, "error", err)
	httpx.WriteError(w, r, http.StatusInternalServerError, "internal", "internal server error")
}

func validationFailed(w http.ResponseWriter, r *http.Request, message string) {
	httpx.WriteError(w, r, http.StatusBadRequest, "validation_failed", message)
}

func validEmail(s string) bool {
	addr, err := mail.ParseAddress(s)
	return err == nil && addr.Address == s
}
