package households

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/auth"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

// HouseholdIDParam is the chi URL parameter every household-scoped route uses.
const HouseholdIDParam = "householdId"

// Authorizer loads a caller's membership and checks a permission. *Service
// implements it.
type Authorizer interface {
	Authorize(ctx context.Context, householdID, userID string, perm Permission) (Membership, error)
}

type membershipKey struct{}

// ContextWithMembership returns a copy of ctx carrying the caller's membership.
func ContextWithMembership(ctx context.Context, m Membership) context.Context {
	return context.WithValue(ctx, membershipKey{}, m)
}

// MembershipFromContext returns the membership stored by RequirePermission.
func MembershipFromContext(ctx context.Context) (Membership, bool) {
	m, ok := ctx.Value(membershipKey{}).(Membership)
	return m, ok
}

// RequirePermission authorizes household-scoped routes. It must run after
// auth.RequireAuth on a route with a {householdId} parameter. It loads the
// caller's membership and stores it in the request context
// (MembershipFromContext).
//
// Non-members, unknown households, and malformed IDs all get 404 not_found so
// a household's existence is never revealed. Members whose role lacks perm get
// 403 forbidden.
func RequirePermission(authz Authorizer, perm Permission, logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := auth.UserIDFromContext(r.Context())
			if !ok {
				httpx.WriteError(w, r, http.StatusUnauthorized, "unauthenticated", "a bearer access token is required")
				return
			}
			m, err := authz.Authorize(r.Context(), chi.URLParam(r, HouseholdIDParam), userID, perm)
			switch {
			case err == nil:
				next.ServeHTTP(w, r.WithContext(ContextWithMembership(r.Context(), m)))
			case errors.Is(err, ErrNotFound):
				writeHouseholdNotFound(w, r)
			case errors.Is(err, ErrForbidden):
				writeForbidden(w, r)
			default:
				logger.ErrorContext(r.Context(), "authorize household access", "error", err)
				httpx.WriteError(w, r, http.StatusInternalServerError, "internal", "internal server error")
			}
		})
	}
}

func writeHouseholdNotFound(w http.ResponseWriter, r *http.Request) {
	httpx.WriteError(w, r, http.StatusNotFound, "not_found", "household not found")
}

func writeForbidden(w http.ResponseWriter, r *http.Request) {
	httpx.WriteError(w, r, http.StatusForbidden, "forbidden", "your role in this household does not allow this action")
}
