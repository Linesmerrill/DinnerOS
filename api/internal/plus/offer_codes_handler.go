package plus

import (
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/auth"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

// offerImportMaxBytes caps one CSV import: far more than Apple's largest
// batch of one-time-use codes.
const offerImportMaxBytes = 8 << 20

// RevealResponse is a household's own free month code.
type RevealResponse struct {
	Code string `json:"code"`
	// RedeemURL opens Apple's redeem page with the code filled in, when the
	// API knows the app's Apple ID.
	RedeemURL string `json:"redeemUrl,omitempty"`
	// CopiedText is what the app says once it copies the code.
	CopiedText string `json:"copiedText"`
}

func (h *Handler) revealFreeMonth(w http.ResponseWriter, r *http.Request) {
	householdID := chi.URLParam(r, households.HouseholdIDParam)
	got, err := h.opts.Service.RevealFreeMonth(r.Context(), householdID, chi.URLParam(r, "monthId"))
	switch {
	case err == nil:
		// The response holds a code: never cache it.
		w.Header().Set("Cache-Control", "no-store")
		httpx.WriteJSON(w, http.StatusOK, RevealResponse{Code: got.Code, RedeemURL: got.RedeemURL, CopiedText: FreeMonthCopiedText})
	case errors.Is(err, ErrFreeMonthNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, "free_month_not_found", FreeMonthNotFoundText)
	case errors.Is(err, ErrFreeMonthOwed):
		httpx.WriteError(w, r, http.StatusConflict, "free_month_on_the_way", FreeMonthOwedError)
	case errors.Is(err, ErrFreeMonthRedeemed):
		httpx.WriteError(w, r, http.StatusConflict, "free_month_redeemed", FreeMonthRedeemedErr)
	default:
		h.logger.ErrorContext(r.Context(), "reveal free month failed", "error", err)
		httpx.WriteError(w, r, http.StatusInternalServerError, "internal", "Something went wrong. Try again.")
	}
}

func (h *Handler) operator(w http.ResponseWriter, r *http.Request) bool {
	userID, _ := auth.UserIDFromContext(r.Context())
	if userID == "" || !slices.Contains(h.opts.Operators, userID) {
		httpx.WriteError(w, r, http.StatusForbidden, "forbidden", "Only the people who run DinnerOS can do this.")
		return false
	}
	return true
}

// ImportResponse is what an import did, and the pool after it.
type ImportResponse struct {
	ImportResult
	Stock OfferStock `json:"stock"`
}

// importOfferCodes serves POST /admin/plus/offer-codes?offer=&batch=&expiresAt=
// with Apple's CSV download as the body. The codes are never logged or
// returned.
func (h *Handler) importOfferCodes(w http.ResponseWriter, r *http.Request) {
	if !h.operator(w, r) {
		return
	}
	q := r.URL.Query()
	b := OfferBatch{Offer: strings.TrimSpace(q.Get("offer")), Batch: strings.TrimSpace(q.Get("batch"))}
	if b.Offer == "" || b.Batch == "" || len(b.Offer) > 100 || len(b.Batch) > 100 {
		httpx.WriteError(w, r, http.StatusBadRequest, "validation_failed", "Give the offer's reference name and a batch name.")
		return
	}
	expires, err := parseExpiry(q.Get("expiresAt"))
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "validation_failed", "Give the date the codes expire, like 2027-02-15.")
		return
	}
	if !expires.After(time.Now()) {
		httpx.WriteError(w, r, http.StatusBadRequest, "validation_failed", "Those codes have already expired.")
		return
	}
	b.ExpiresAt = expires
	body, err := io.ReadAll(r.Body)
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		httpx.WriteError(w, r, http.StatusRequestEntityTooLarge, "payload_too_large", "That file is too big. Split it and try again.")
		return
	}
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "validation_failed", "We couldn't read that file.")
		return
	}
	codes, skipped := ParseOfferCodes(string(body))
	if len(codes) == 0 {
		httpx.WriteError(w, r, http.StatusBadRequest, "validation_failed", "That file has no codes in it.")
		return
	}
	res, err := h.opts.Service.ImportOfferCodes(r.Context(), codes, b)
	res.Skipped = skipped
	if err != nil {
		if errors.Is(err, ErrReferralsOff) {
			httpx.WriteError(w, r, http.StatusNotFound, "not_found", "resource not found")
			return
		}
		h.logger.ErrorContext(r.Context(), "import offer codes failed", "error", err)
		httpx.WriteError(w, r, http.StatusInternalServerError, "internal", "Something went wrong. Try again.")
		return
	}
	stock, err := h.opts.Service.OfferStock(r.Context())
	if err != nil {
		h.logger.ErrorContext(r.Context(), "offer code stock failed", "error", err)
	}
	// Counts only: the batch name and how many, never a code.
	h.logger.InfoContext(r.Context(), "offer codes imported", "offer", b.Offer, "batch", b.Batch,
		"added", res.Added, "duplicates", res.Duplicates, "skipped", res.Skipped, "filled", res.Filled)
	httpx.WriteJSON(w, http.StatusOK, ImportResponse{ImportResult: res, Stock: stock})
}

// parseExpiry takes a date (the codes work through that day, UTC) or an
// RFC 3339 time.
func parseExpiry(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return time.Time{}, err
	}
	return d.Add(24*time.Hour - time.Second).UTC(), nil
}

func (h *Handler) offerStock(w http.ResponseWriter, r *http.Request) {
	if !h.operator(w, r) {
		return
	}
	st, err := h.opts.Service.OfferStock(r.Context())
	if errors.Is(err, ErrReferralsOff) {
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", "resource not found")
		return
	}
	if err != nil {
		h.logger.ErrorContext(r.Context(), "offer code stock failed", "error", err)
		httpx.WriteError(w, r, http.StatusInternalServerError, "internal", "Something went wrong. Try again.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, st)
}
