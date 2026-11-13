package recommendations

import (
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

// Building a week with Autopilot is behind Plus; reading the profile isn't.
func TestAutopilotGenerateIsBehindPlus(t *testing.T) {
	env := newTestEnv(t)
	blocked := func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			httpx.WriteError(w, r, http.StatusPaymentRequired, "plus_required", "This is part of DinnerOS Plus.")
		})
	}
	r := chi.NewRouter()
	r.Route("/api/v1", NewHandler(HandlerOptions{Service: env.svc, Authorizer: testAuthorizer, Tokens: fakeTokens{}, RequirePlus: blocked}).Mount)
	wantStatus(t, do(t, r, http.MethodPost, "/weeks/2026-W38/generate", `{}`, userAda), http.StatusPaymentRequired, "plus_required")
	wantStatus(t, do(t, r, http.MethodGet, "/profile", "", userAda), http.StatusOK, "")
	// Permission comes first: a viewer is told no, not offered Plus.
	wantStatus(t, do(t, r, http.MethodPost, "/weeks/2026-W38/generate", `{}`, userView), http.StatusForbidden, "forbidden")
}
