package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

type userIDKey struct{}

// ContextWithUserID returns a copy of ctx carrying an authenticated user ID.
// RequireAuth sets it; tests of other modules may use it directly.
func ContextWithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDKey{}, userID)
}

// UserIDFromContext returns the authenticated user ID set by RequireAuth.
func UserIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(userIDKey{}).(string)
	return id, ok && id != ""
}

// AccessTokenValidator validates bearer tokens. *TokenService implements it.
type AccessTokenValidator interface {
	ValidateAccessToken(token string) (string, error)
}

// RequireAuth rejects requests without a valid DinnerOS access token with
// 401 unauthenticated (or token_expired) and stores the user ID in the
// request context for downstream handlers.
func RequireAuth(tokens AccessTokenValidator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := bearerToken(r)
			if !ok {
				w.Header().Set("WWW-Authenticate", `Bearer realm="dinneros"`)
				httpx.WriteError(w, r, http.StatusUnauthorized, "unauthenticated", "a bearer access token is required")
				return
			}
			userID, err := tokens.ValidateAccessToken(token)
			if errors.Is(err, ErrAccessTokenExpired) {
				w.Header().Set("WWW-Authenticate", `Bearer realm="dinneros", error="invalid_token"`)
				httpx.WriteError(w, r, http.StatusUnauthorized, "token_expired", "access token has expired")
				return
			}
			if err != nil {
				w.Header().Set("WWW-Authenticate", `Bearer realm="dinneros", error="invalid_token"`)
				httpx.WriteError(w, r, http.StatusUnauthorized, "unauthenticated", "access token is invalid")
				return
			}
			next.ServeHTTP(w, r.WithContext(ContextWithUserID(r.Context(), userID)))
		})
	}
}

func bearerToken(r *http.Request) (string, bool) {
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token = strings.TrimSpace(token)
	return token, token != ""
}
