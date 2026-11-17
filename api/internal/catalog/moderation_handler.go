package catalog

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/auth"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

// HideCookResponse confirms POST .../catalog/recipes/{id}/hide-cook.
type HideCookResponse struct {
	Hidden  bool   `json:"hidden"`
	Message string `json:"message"`
}

// What the app shows after hiding a cook, or a recipe with no cook.
const (
	HideCookThanks   = "Done. You won't see recipes from this cook again."
	HideRecipeThanks = "Done. You won't see this recipe again."
)

func (h *Handler) hideCook(w http.ResponseWriter, r *http.Request) {
	userID, _ := auth.UserIDFromContext(r.Context())
	e, err := h.opts.Service.Block(r.Context(), householdID(r), userID, chi.URLParam(r, "catalogRecipeId"))
	if err != nil {
		h.fail(w, r, "hide cook failed", err)
		return
	}
	msg := HideRecipeThanks
	if HidesCook(e) {
		msg = HideCookThanks
	}
	httpx.WriteJSON(w, http.StatusOK, HideCookResponse{Hidden: true, Message: msg})
}

// ReportListResponse is GET /admin/catalog/reports.
type ReportListResponse struct {
	Recipes []ReportedRecipeResponse `json:"recipes"`
}

// ReportedRecipeResponse is one reported recipe for an operator.
type ReportedRecipeResponse struct {
	CatalogRecipeID   string               `json:"catalogRecipeId"`
	Name              string               `json:"name"`
	Households        int                  `json:"households"`
	Decision          string               `json:"decision,omitempty"`
	HiddenForEveryone bool                 `json:"hiddenForEveryone"`
	Reports           []ReportItemResponse `json:"reports"`
}

// ReportItemResponse is one report. It names no household or member.
type ReportItemResponse struct {
	Reason string    `json:"reason"`
	Note   string    `json:"note,omitempty"`
	At     time.Time `json:"at"`
}

// ReviewRequest is the body of POST /admin/catalog/reports/{id}/review.
type ReviewRequest struct {
	// Decision is "keep" or "remove".
	Decision string `json:"decision"`
}

func (h *Handler) listReports(w http.ResponseWriter, r *http.Request) {
	if !h.operator(w, r) {
		return
	}
	list, err := h.opts.Service.Reported(r.Context())
	if err != nil {
		h.fail(w, r, "list reports failed", err)
		return
	}
	out := ReportListResponse{Recipes: make([]ReportedRecipeResponse, 0, len(list))}
	for _, it := range list {
		item := ReportedRecipeResponse{
			CatalogRecipeID: it.CatalogID, Name: it.Name, Households: it.Households, Decision: it.Decision,
			HiddenForEveryone: it.HiddenForEveryone, Reports: make([]ReportItemResponse, 0, len(it.Reports)),
		}
		for _, rep := range it.Reports {
			item.Reports = append(item.Reports, ReportItemResponse{Reason: rep.Reason, Note: rep.Note, At: rep.At})
		}
		out.Recipes = append(out.Recipes, item)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) reviewReport(w http.ResponseWriter, r *http.Request) {
	if !h.operator(w, r) {
		return
	}
	var req ReviewRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	userID, _ := auth.UserIDFromContext(r.Context())
	id := chi.URLParam(r, "catalogRecipeId")
	err := h.opts.Service.Review(r.Context(), Review{CatalogID: id, Decision: req.Decision, UserID: userID})
	if errors.Is(err, ErrInvalidReview) {
		httpx.WriteError(w, r, http.StatusBadRequest, "validation_failed", "Pick keep or remove.")
		return
	}
	if err != nil {
		h.fail(w, r, "review report failed", err)
		return
	}
	h.logger.InfoContext(r.Context(), "shared recipe reviewed", "catalogRecipeId", id, "decision", req.Decision)
	w.WriteHeader(http.StatusNoContent)
}

// alertTimeout bounds one report email.
const alertTimeout = 15 * time.Second

// alert emails the operators about a new report without holding up the
// member's request. A failed email is logged; the report is already stored.
func (h *Handler) alert(ctx context.Context, id, reason, note string) {
	if h.opts.ReportAlerts == nil || h.opts.ReportAlertTo == "" {
		return
	}
	name, households := h.opts.Service.reportSummary(ctx, id)
	text := ReportAlertText(id, name, reason, note, households)
	ctx = context.WithoutCancel(ctx)
	go func() {
		ctx, cancel := context.WithTimeout(ctx, alertTimeout)
		defer cancel()
		if err := h.opts.ReportAlerts.SendPlain(ctx, h.opts.ReportAlertTo, ReportAlertSubject, text); err != nil {
			h.logger.ErrorContext(ctx, "report alert email failed", "catalogRecipeId", id, "error", err)
		}
	}()
}

// ReportAlertSubject is the subject of a report email.
const ReportAlertSubject = "A shared recipe was reported"

// ReportAlertText is the body of a report email. It names no household or
// member.
func ReportAlertText(id, name, reason, note string, households int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Someone reported a recipe in Shared Recipes.\n\n")
	fmt.Fprintf(&b, "Recipe: %s (%s)\nReason: %s\n", name, id, strings.ToLower(strings.TrimSpace(reason)))
	if note = strings.TrimSpace(note); note != "" {
		fmt.Fprintf(&b, "Note: %s\n", note)
	}
	fmt.Fprintf(&b, "Households that reported it: %d. At %d it is hidden for everyone until you review it.\n\n", households, PullAfter)
	fmt.Fprintf(&b, "To review it: GET /api/v1/admin/catalog/reports, then\n")
	fmt.Fprintf(&b, "POST /api/v1/admin/catalog/reports/%s/review with {\"decision\":\"keep\"} or {\"decision\":\"remove\"}.\n", id)
	return b.String()
}
