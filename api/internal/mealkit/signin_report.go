package mealkit

import (
	"log/slog"
	"net/http"
	"regexp"
	"slices"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

// This file is the sign-in report: one line in the server log for every time
// a member tries the meal-kit sign-in on their phone, however it ended. The
// sign-in and the order-history read happen on the phone, so without it a
// timeout, a crashed page, or a changed API at the service would leave no
// trace anywhere we can read (docs/meal-kit-import.md#sign-in-reports).
//
// It carries only fixed codes, counts, and versions, each checked against an
// allow-list: never page text, URLs, cookies, tokens, or anything typed.

// SignInReportBody is the body of POST .../meal-kit/{source}/sign-in-reports.
type SignInReportBody struct {
	// Outcome is how the attempt ended: harvested, up_to_date, failed,
	// no_session (the sign-in was never picked up), page_crashed,
	// page_failed (the sign-in page didn't load), or cancelled.
	Outcome string `json:"outcome"`
	// Reason is the failure code when Outcome is failed: forbidden, empty,
	// unreadable, or unavailable.
	Reason string `json:"reason,omitempty"`
	// Stage is the step of the read that failed: plans or deliveries.
	Stage string `json:"stage,omitempty"`
	// HTTPStatus is the service's status for that step, 0 when none.
	HTTPStatus int `json:"httpStatus,omitempty"`
	// Phase is where a cancelled attempt was: signing_in, reading, harvesting.
	Phase string `json:"phase,omitempty"`
	// ElapsedMs is how long the attempt took, from opening the sign-in.
	ElapsedMs int64 `json:"elapsedMs,omitempty"`
	// Attempt counts Try Again taps in this sheet, starting at 1.
	Attempt int `json:"attempt,omitempty"`
	// Retried says a read cut off by a page change was tried again.
	Retried bool `json:"retried,omitempty"`
	// PageCrashes counts crashes of the sign-in page's process.
	PageCrashes int `json:"pageCrashes,omitempty"`
	// NavigationError is the last failed navigation as "domain code", e.g.
	// "NSURLErrorDomain -1009".
	NavigationError string `json:"navigationError,omitempty"`
	// Recipes, Weeks, and Pages size a read that worked; Stopped is why it
	// stopped (cap, empty, end, caught_up).
	Recipes int    `json:"recipes,omitempty"`
	Weeks   int    `json:"weeks,omitempty"`
	Pages   int    `json:"pages,omitempty"`
	Stopped string `json:"stopped,omitempty"`
	// AppVersion is "0.1.0 (86)"; OSVersion is "26.2"; Device is the model
	// identifier, e.g. "iPhone17,1".
	AppVersion string `json:"appVersion,omitempty"`
	OSVersion  string `json:"osVersion,omitempty"`
	Device     string `json:"device,omitempty"`
}

var (
	signInOutcomes = []string{"harvested", "up_to_date", "failed", "no_session", "page_crashed", "page_failed", "cancelled"}
	signInReasons  = []string{"forbidden", "empty", "unreadable", "unavailable", "nothingNew"}
	signInStages   = []string{"plans", "deliveries", "unknown"}
	signInPhases   = []string{"signing_in", "reading", "harvesting", "failed"}
	signInStopped  = []string{"cap", "empty", "end", "caught_up"}
	// Short codes and versions only: letters, digits, dots, commas, spaces,
	// dashes, parentheses. Nothing that can carry a URL or a sentence.
	signInShortText = regexp.MustCompile(`^[A-Za-z0-9., ()_-]{1,48}$`)
)

// oneOf returns v when it is in allowed, and "" otherwise.
func oneOf(v string, allowed []string) string {
	if slices.Contains(allowed, v) {
		return v
	}
	return ""
}

// shortCode returns v when it is a short code or version, and "" otherwise.
func shortCode(v string) string {
	if signInShortText.MatchString(v) {
		return v
	}
	return ""
}

func clampInt(v, lo, hi int) int {
	return max(lo, min(v, hi))
}

func (h *Handler) signInReport(w http.ResponseWriter, r *http.Request) {
	source := chi.URLParam(r, "source")
	if !KnownSource(source) {
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", "unknown meal-kit service")
		return
	}
	var body SignInReportBody
	if !httpx.DecodeJSON(w, r, &body) {
		return
	}
	outcome := oneOf(body.Outcome, signInOutcomes)
	if outcome == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, "validation_failed", "unknown outcome")
		return
	}
	actor, _ := households.MembershipFromContext(r.Context())
	attrs := []any{
		"source", source, "householdId", actor.HouseholdID, "userId", actor.UserID, "outcome", outcome,
		"elapsedMs", max(0, min(body.ElapsedMs, 3_600_000)), "attempt", clampInt(body.Attempt, 0, 100),
	}
	add := func(key string, value any, keep bool) {
		if keep {
			attrs = append(attrs, key, value)
		}
	}
	reason, stage, phase, stopped := oneOf(body.Reason, signInReasons), oneOf(body.Stage, signInStages),
		oneOf(body.Phase, signInPhases), oneOf(body.Stopped, signInStopped)
	add("reason", reason, reason != "")
	add("stage", stage, stage != "")
	add("httpStatus", clampInt(body.HTTPStatus, 0, 999), body.HTTPStatus > 0)
	add("phase", phase, phase != "")
	add("retried", true, body.Retried)
	add("pageCrashes", clampInt(body.PageCrashes, 0, 100), body.PageCrashes > 0)
	nav := shortCode(body.NavigationError)
	add("navigationError", nav, nav != "")
	add("recipes", clampInt(body.Recipes, 0, 100_000), body.Recipes > 0)
	add("weeks", clampInt(body.Weeks, 0, 10_000), body.Weeks > 0)
	add("pages", clampInt(body.Pages, 0, 10_000), body.Pages > 0)
	add("stopped", stopped, stopped != "")
	for key, value := range map[string]string{"appVersion": body.AppVersion, "osVersion": body.OSVersion, "device": body.Device} {
		if v := shortCode(value); v != "" {
			attrs = append(attrs, key, v)
		}
	}
	level := slog.LevelInfo
	switch outcome {
	case "failed", "no_session", "page_crashed", "page_failed":
		level = slog.LevelWarn
	}
	h.logger.Log(r.Context(), level, "meal-kit sign-in report", attrs...)
	w.WriteHeader(http.StatusNoContent)
}
