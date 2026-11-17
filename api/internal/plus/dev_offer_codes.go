package plus

import (
	"crypto/rand"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/auth"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

// DevOfferCodesHandler serves POST /plus/dev/offer-codes, which puts made-up
// codes in the free month pool so UI tests and the QA matrix can earn a
// month on a local API (decision 755). They are not Apple's codes and
// redeem nowhere. Mount it only where development sign-in is on: it is
// never part of a deployed API.
type DevOfferCodesHandler struct {
	service *Service
	tokens  auth.AccessTokenValidator
	logger  *slog.Logger
}

// DevOfferBatch is the offer and batch made-up codes are filed under.
const (
	DevOffer      = "dev-free-month"
	DevBatchLabel = "dev"
	devMaxCodes   = 200
)

// NewDevOfferCodesHandler returns a DevOfferCodesHandler.
func NewDevOfferCodesHandler(s *Service, tokens auth.AccessTokenValidator, logger *slog.Logger) *DevOfferCodesHandler {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &DevOfferCodesHandler{service: s, tokens: tokens, logger: logger}
}

// Mount registers the route on the /api/v1 router. Any signed-in person may
// call it: it only exists on a local API.
func (h *DevOfferCodesHandler) Mount(r chi.Router) {
	r.With(auth.RequireAuth(h.tokens)).Post("/plus/dev/offer-codes", h.seed)
}

func (h *DevOfferCodesHandler) seed(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Count int `json:"count"`
	}
	if r.ContentLength != 0 {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	n := min(max(req.Count, 1), devMaxCodes)
	codes := make([]string, 0, n)
	for range n {
		codes = append(codes, devCode())
	}
	res, err := h.service.ImportOfferCodes(r.Context(), codes, OfferBatch{
		Offer: DevOffer, Batch: DevBatchLabel, ExpiresAt: time.Now().Add(90 * 24 * time.Hour),
	})
	if err != nil {
		h.logger.ErrorContext(r.Context(), "dev offer codes failed", "error", err)
		httpx.WriteError(w, r, http.StatusInternalServerError, "internal", "Something went wrong. Try again.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]int{"added": res.Added, "filled": res.Filled})
}

// devCode is "DEV" and 9 random letters and digits.
func devCode() string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, 9)
	_, _ = rand.Read(b)
	out := []byte("DEV")
	for _, c := range b {
		out = append(out, alphabet[int(c)%len(alphabet)])
	}
	return string(out)
}
