package plus

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/auth"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

// HandlerOptions configures the Plus routes.
type HandlerOptions struct {
	Service    *Service
	Authorizer households.Authorizer
	Tokens     auth.AccessTokenValidator
	// WebhookSecret is the Authorization header RevenueCat sends with each
	// webhook (REVENUECAT_WEBHOOK_SECRET). Empty turns the webhook off.
	WebhookSecret string
	// Operators are the user IDs who may read referral totals across
	// households (OPERATOR_USER_IDS).
	Operators []string
	// MemberName returns a member's name for "Started by"; empty if unknown.
	MemberName func(ctx context.Context, userID string) string
	// CodeRateLimit limits the code change and availability routes, keyed
	// by user (RateLimitKey); nil for none.
	CodeRateLimit func(http.Handler) http.Handler
	Logger        *slog.Logger
}

// RateLimitKey keys a rate limiter by the signed-in user.
func RateLimitKey(r *http.Request) string {
	userID, _ := auth.UserIDFromContext(r.Context())
	return "user:" + userID
}

// Handler serves the Plus routes.
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

// Mount registers the routes on the /api/v1 router.
func (h *Handler) Mount(r chi.Router) {
	r.Post("/webhooks/revenuecat", h.webhook)
	r.With(auth.RequireAuth(h.opts.Tokens)).Get("/plus/referrals/summary", h.referralSummary)
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireAuth(h.opts.Tokens))
		r.Use(households.RequirePermission(h.opts.Authorizer, households.PermHouseholdView, h.logger))
		r.Get("/households/{householdId}/plus", h.status)
		r.Get("/households/{householdId}/plus/referral", h.referral)
		r.Get("/households/{householdId}/plus/details", h.details)
	})
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireAuth(h.opts.Tokens))
		r.Use(households.RequirePermission(h.opts.Authorizer, households.PermPlanEdit, h.logger))
		r.Post("/households/{householdId}/plus/referral/redeem", h.redeem)
		r.Group(func(r chi.Router) {
			if h.opts.CodeRateLimit != nil {
				r.Use(h.opts.CodeRateLimit)
			}
			r.Get("/households/{householdId}/plus/referral/code-availability", h.codeAvailability)
			r.Post("/households/{householdId}/plus/referral/code", h.changeCode)
		})
	})
}

// StatusResponse is a household's DinnerOS Plus.
type StatusResponse struct {
	// Enabled is false while Plus is off: show no paywall, open everything.
	Enabled      bool       `json:"enabled"`
	Active       bool       `json:"active"`
	Trial        bool       `json:"trial"`
	ExpiresAt    *time.Time `json:"expiresAt,omitempty"`
	WillRenew    bool       `json:"willRenew"`
	BillingIssue bool       `json:"billingIssue"`
	// FreeUntil is when free months from referrals end.
	FreeUntil *time.Time `json:"freeUntil,omitempty"`
	// Offer is the purchase screen's words, while Plus is on.
	Offer *Offer `json:"offer,omitempty"`
	// Comp is set while DinnerOS gives the household Plus.
	Comp *CompResponse `json:"comp,omitempty"`
	// ReferralProgress is set when this household entered another's code:
	// how far it is toward the free month (decision 698).
	ReferralProgress *ProgressResponse `json:"referralProgress,omitempty"`
}

// ProgressResponse is a referred household's way to its free month.
type ProgressResponse struct {
	// State is pending, qualified, or expired.
	State   string                 `json:"state"`
	Message string                 `json:"message"`
	Steps   []ProgressStepResponse `json:"steps"`
	// ExpiresAt is when an unfinished setup stops counting.
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

// ProgressStepResponse is one step: dinner or recipes.
type ProgressStepResponse struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Done   bool   `json:"done"`
	Count  int    `json:"count"`
	Target int    `json:"target"`
}

func progressResponse(p *Progress) *ProgressResponse {
	if p == nil {
		return nil
	}
	out := &ProgressResponse{State: p.State, Message: p.Message, Steps: make([]ProgressStepResponse, 0, len(p.Steps)), ExpiresAt: timePtr(p.ExpiresAt)}
	for _, st := range p.Steps {
		out.Steps = append(out.Steps, ProgressStepResponse(st))
	}
	return out
}

// CompResponse is Plus given by DinnerOS, and the thank-you the app shows
// once for it.
type CompResponse struct {
	Reason      string    `json:"reason"`
	Since       time.Time `json:"since"`
	ThanksTitle string    `json:"thanksTitle"`
	ThanksText  string    `json:"thanksText"`
	// Benefits are what Plus includes, the purchase screen's list.
	BenefitsTitle string   `json:"benefitsTitle"`
	Benefits      []string `json:"benefits"`
}

func statusResponse(st Status) StatusResponse {
	resp := StatusResponse{Enabled: st.Enabled, Active: st.Active, Trial: st.Trial, WillRenew: st.WillRenew, BillingIssue: st.BillingIssue}
	if !st.ExpiresAt.IsZero() {
		t := st.ExpiresAt
		resp.ExpiresAt = &t
	}
	if !st.FreeUntil.IsZero() {
		t := st.FreeUntil
		resp.FreeUntil = &t
	}
	if st.Enabled {
		o := DefaultOffer()
		resp.Offer = &o
	}
	if c := st.Comp; c != nil {
		resp.Comp = &CompResponse{
			Reason: c.Reason, Since: c.GrantedAt, ThanksTitle: CompThanksTitle, ThanksText: CompThanksText,
			BenefitsTitle: CompBenefitsTitle, Benefits: DefaultOffer().Benefits,
		}
	}
	return resp
}

// statusWithProgress is the status plus the household's referral progress.
func (h *Handler) statusWithProgress(ctx context.Context, householdID string, st Status) (StatusResponse, error) {
	resp := statusResponse(st)
	p, err := h.opts.Service.ReferralProgress(ctx, householdID)
	if err != nil {
		return StatusResponse{}, err
	}
	resp.ReferralProgress = progressResponse(p)
	return resp, nil
}

func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	householdID := chi.URLParam(r, households.HouseholdIDParam)
	st, err := h.opts.Service.Status(r.Context(), householdID)
	var resp StatusResponse
	if err == nil {
		resp, err = h.statusWithProgress(r.Context(), householdID, st)
	}
	if err != nil {
		h.logger.ErrorContext(r.Context(), "plus status failed", "error", err)
		httpx.WriteError(w, r, http.StatusInternalServerError, "internal", "Something went wrong. Try again.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

// ReferralResponse is a household's own invite code.
type ReferralResponse struct {
	Code string `json:"code"`
	// ShareText is the whole message, the invite link last so Messages shows
	// its preview card.
	ShareText string `json:"shareText"`
	// ShareMessage and ShareURL are the same message split, for a share sheet
	// that shows the link's preview itself.
	ShareMessage string `json:"shareMessage"`
	ShareURL     string `json:"shareUrl"`
	// Explainer sits above the share button.
	Explainer  string `json:"explainer"`
	Rewarded   int    `json:"rewarded"`
	MonthsLeft int    `json:"monthsLeft"`
	// HowItWorks sits above the code, in the words of the rules in force.
	HowItWorks HowItWorksResponse `json:"howItWorks"`
	// Invites is who used the code and what it earned.
	Invites InvitesResponse `json:"invites"`
	// CodeChangesLeft is how many more times the code can change in the
	// next 30 days (decision 699); CodeChangesNext, set at 0, is when it can
	// change again. CodeChangesText says so; CodeHelp is the rule for a
	// picked code.
	CodeChangesLeft  int        `json:"codeChangesLeft"`
	CodeChangesLimit int        `json:"codeChangesLimit"`
	CodeChangesNext  *time.Time `json:"codeChangesNext,omitempty"`
	CodeChangesText  string     `json:"codeChangesText"`
	CodeHelp         string     `json:"codeHelp"`
	// ReferralProgress is set when this household entered another's code.
	ReferralProgress *ProgressResponse `json:"referralProgress,omitempty"`
}

// HowItWorksResponse explains referrals.
type HowItWorksResponse struct {
	Title string   `json:"title"`
	Steps []string `json:"steps"`
}

// InvitesResponse is the invite screen's Your Invites section.
type InvitesResponse struct {
	// Joined counts households that used the code, in any state.
	Joined int `json:"joined"`
	// Pending, Qualified, and Expired split Joined.
	Pending   int `json:"pending"`
	Qualified int `json:"qualified"`
	Expired   int `json:"expired"`
	// MonthsEarned of MonthsCap; CapText says so, like "3 of 12".
	MonthsEarned int    `json:"monthsEarned"`
	MonthsCap    int    `json:"monthsCap"`
	CapText      string `json:"capText"`
	// FreeUntil is when free months end, while they last.
	FreeUntil *time.Time `json:"freeUntil,omitempty"`
	// EmptyText shows when no one has used the code.
	EmptyText string `json:"emptyText"`
	// Items are the most recent invites, newest first, by household name.
	Items []InviteItemResponse `json:"items"`
	// Joins are the same invites without names, for app builds before
	// Items.
	Joins []InviteJoin `json:"joins"`
	// UsedCode is set once this household's own code use paid its month.
	UsedCode *UsedCode `json:"usedCode,omitempty"`
}

// InviteItemResponse is one household that used the code.
type InviteItemResponse struct {
	ID string `json:"id"`
	// HouseholdName is the name the household chose.
	HouseholdName string `json:"householdName"`
	// State is pending, qualified, or expired.
	State string `json:"state"`
	// Detail is a short line: what's left, or what it earned. Empty for a
	// plain "Pending".
	Detail string    `json:"detail"`
	At     time.Time `json:"at"`
}

// InviteJoin is one household that used the code.
type InviteJoin struct {
	At time.Time `json:"at"`
	// Counted is true once it earned the sharer a month.
	Counted bool `json:"counted"`
	// Status is "Counted", "Limit reached", "Pending", or "Expired".
	Status string `json:"status"`
}

// UsedCode is this household's own use of another's code.
type UsedCode struct {
	At time.Time `json:"at"`
	// FreeUntil is when the free month from it ends.
	FreeUntil time.Time `json:"freeUntil"`
}

// InvitesEmptyText shows when no one has used a household's code.
const InvitesEmptyText = "No one has used your code yet. Share it, and once they set up DinnerOS you both get a free month of Plus."

func invitesResponse(info ReferralInfo, rules ReferralRules) InvitesResponse {
	resp := InvitesResponse{
		Joined: info.Counts.Total(), Pending: info.Counts.Pending, Qualified: info.Counts.Qualified, Expired: info.Counts.Expired,
		MonthsEarned: info.Rewarded, MonthsCap: MaxReferralMonths,
		CapText:   fmt.Sprintf("%d of %d", info.Rewarded, MaxReferralMonths),
		EmptyText: InvitesEmptyText, Joins: make([]InviteJoin, 0, len(info.Recent)),
		Items: make([]InviteItemResponse, 0, len(info.Recent)),
	}
	if !info.FreeUntil.IsZero() {
		t := info.FreeUntil
		resp.FreeUntil = &t
	}
	for _, r := range info.Recent {
		state, detail := rules.InviteDetail(r)
		resp.Items = append(resp.Items, InviteItemResponse{
			ID: r.ID, HouseholdName: nameOr(r.HouseholdName, "A household"), State: state, Detail: detail, At: r.At,
		})
		join := InviteJoin{At: r.At, Status: InviteStatusPending}
		switch r.State {
		case StateQualified:
			join.Counted, join.Status = r.ReferrerRewarded, "Counted"
			if !r.ReferrerRewarded {
				join.Status = "Limit reached"
			}
		case StateExpired:
			join.Status = InviteExpired
		}
		resp.Joins = append(resp.Joins, join)
	}
	if u := info.UsedCode; u != nil && u.State == StateQualified {
		resp.UsedCode = &UsedCode{At: u.EarnedAt(), FreeUntil: u.EarnedAt().Add(FreeMonthDays * 24 * time.Hour)}
	}
	return resp
}

// ReferralSummaryResponse is referrals across every household, for the
// people who run DinnerOS.
type ReferralSummaryResponse struct {
	// CodesUsed counts households whose code was used at least once.
	CodesUsed int `json:"codesUsed"`
	// JoinedThisWeek counts households that used a code in the last 7 days.
	JoinedThisWeek int `json:"joinedThisWeek"`
	JoinedAllTime  int `json:"joinedAllTime"`
	// Pending, Qualified, and Expired split JoinedAllTime.
	Pending   int `json:"pending"`
	Qualified int `json:"qualified"`
	Expired   int `json:"expired"`
	// FooterText explains qualified and pending, under the totals.
	FooterText string `json:"footerText"`
	// TopReferrers are the 5 households whose codes were used most.
	TopReferrers []TopReferrer `json:"topReferrers"`
}

// TopReferrer is one household and how many used its code.
type TopReferrer struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func (h *Handler) referralSummary(w http.ResponseWriter, r *http.Request) {
	userID, _ := auth.UserIDFromContext(r.Context())
	if !slices.Contains(h.opts.Operators, userID) {
		httpx.WriteError(w, r, http.StatusForbidden, "forbidden", "Only the people who run DinnerOS can see this.")
		return
	}
	rep, err := h.opts.Service.ReferralReport(r.Context())
	if errors.Is(err, ErrReferralsOff) {
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", "resource not found")
		return
	}
	if err != nil {
		h.logger.ErrorContext(r.Context(), "plus referral summary failed", "error", err)
		httpx.WriteError(w, r, http.StatusInternalServerError, "internal", "Something went wrong. Try again.")
		return
	}
	resp := ReferralSummaryResponse{
		CodesUsed: rep.Codes, JoinedThisWeek: rep.JoinedSince, JoinedAllTime: rep.Joined,
		Pending: rep.Counts.Pending, Qualified: rep.Counts.Qualified, Expired: rep.Counts.Expired,
		FooterText:   h.opts.Service.Rules().SummaryFooter(),
		TopReferrers: make([]TopReferrer, 0, len(rep.Top)),
	}
	for _, t := range rep.Top {
		name := t.Name
		if name == "" {
			name = "A deleted household"
		}
		resp.TopReferrers = append(resp.TopReferrers, TopReferrer{Name: name, Count: t.Count})
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

// InviteURLBase is the website page a shared invite code links to.
const InviteURLBase = "https://dinneros.app/invite/"

// InviteURL is the link to code's invite page.
func InviteURL(code string) string { return InviteURLBase + code }

// ShareMessage is the invite message without its link.
func ShareMessage(code string) string {
	return "I plan our dinners with DinnerOS. Use my code " + code + " and we both get a free month of DinnerOS Plus."
}

// ReferralText returns the invite screen's words for code.
func ReferralText(code string, rewarded, monthsLeft int) (share, explainer string) {
	share = ShareMessage(code) + " " + InviteURL(code)
	switch {
	case monthsLeft == 0:
		explainer = "Thanks for sharing DinnerOS. You've earned every free month this code can give."
	case rewarded == 1:
		explainer = "Invite another household. When they set up DinnerOS with your code, you both get a free month of Plus. You've earned 1 month so far."
	case rewarded > 1:
		explainer = fmt.Sprintf("Invite another household. When they set up DinnerOS with your code, you both get a free month of Plus. You've earned %d months so far.", rewarded)
	default:
		explainer = "Invite another household. When they set up DinnerOS with your code, you both get a free month of Plus."
	}
	return share, explainer
}

func (h *Handler) referral(w http.ResponseWriter, r *http.Request) {
	info, err := h.opts.Service.Referral(r.Context(), chi.URLParam(r, households.HouseholdIDParam))
	h.writeReferral(w, r, info, err)
}

// writeReferral writes info as the referral response, or err.
func (h *Handler) writeReferral(w http.ResponseWriter, r *http.Request, info ReferralInfo, err error) {
	if errors.Is(err, ErrReferralsOff) {
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", "resource not found")
		return
	}
	if err != nil {
		h.logger.ErrorContext(r.Context(), "plus referral failed", "error", err)
		httpx.WriteError(w, r, http.StatusInternalServerError, "internal", "Something went wrong. Try again.")
		return
	}
	share, explainer := ReferralText(info.Code, info.Rewarded, info.MonthsLeft)
	rules := h.opts.Service.Rules()
	how := rules.HowItWorks()
	httpx.WriteJSON(w, http.StatusOK, ReferralResponse{
		Code: info.Code, ShareText: share, ShareMessage: ShareMessage(info.Code), ShareURL: InviteURL(info.Code),
		Explainer: explainer, Rewarded: info.Rewarded, MonthsLeft: info.MonthsLeft,
		HowItWorks: HowItWorksResponse(how),
		Invites:    invitesResponse(info, rules), ReferralProgress: progressResponse(info.Progress),
		CodeChangesLeft: info.CodeChanges.Left, CodeChangesLimit: CodeChangeLimit, CodeChangesNext: timePtr(info.CodeChanges.Next),
		CodeChangesText: CodeChangesText(info.CodeChanges, info.Location), CodeHelp: CodeHelp,
	})
}

// CodeAvailabilityResponse says whether a code can be picked, for a live
// line under the field while typing.
type CodeAvailabilityResponse struct {
	Available bool   `json:"available"`
	Message   string `json:"message"`
}

func (h *Handler) codeAvailability(w http.ResponseWriter, r *http.Request) {
	ok, msg, err := h.opts.Service.CodeAvailability(r.Context(), chi.URLParam(r, households.HouseholdIDParam), r.URL.Query().Get("code"))
	if errors.Is(err, ErrReferralsOff) {
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", "resource not found")
		return
	}
	if err != nil {
		h.logger.ErrorContext(r.Context(), "plus code availability failed", "error", err)
		httpx.WriteError(w, r, http.StatusInternalServerError, "internal", "Something went wrong. Try again.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, CodeAvailabilityResponse{Available: ok, Message: msg})
}

type changeCodeRequest struct {
	// Custom is the code the household picked; Regenerate asks for a new
	// random one instead.
	Custom     string `json:"custom"`
	Regenerate bool   `json:"regenerate"`
}

func (h *Handler) changeCode(w http.ResponseWriter, r *http.Request) {
	var req changeCodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "validation_failed", CodeMessage(ErrCodeNoChoice))
		return
	}
	info, err := h.opts.Service.ChangeCode(r.Context(), chi.URLParam(r, households.HouseholdIDParam), req.Custom, req.Regenerate)
	switch {
	case err == nil, errors.Is(err, ErrReferralsOff):
		h.writeReferral(w, r, info, err)
	case errors.Is(err, ErrCodeNoChoice), errors.Is(err, ErrCodeInvalid), errors.Is(err, ErrCodeHyphenEnds):
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "code_invalid", CodeMessage(err))
	case errors.Is(err, ErrCodeNotAllowed):
		httpx.WriteError(w, r, http.StatusUnprocessableEntity, "code_not_allowed", CodeMessage(err))
	case errors.Is(err, ErrCodeTaken), errors.Is(err, ErrCodeSame):
		httpx.WriteError(w, r, http.StatusConflict, "code_taken", CodeMessage(err))
	case errors.Is(err, ErrCodeChangedNow):
		httpx.WriteError(w, r, http.StatusConflict, "code_changed", CodeMessage(err))
	case errors.Is(err, ErrCodeLimit):
		httpx.WriteError(w, r, http.StatusTooManyRequests, "code_change_limit", CodeMessage(err))
	default:
		h.logger.ErrorContext(r.Context(), "plus code change failed", "error", err)
		httpx.WriteError(w, r, http.StatusInternalServerError, "internal", CodeMessage(err))
	}
}

type redeemRequest struct {
	Code string `json:"code"`
	// DeviceCheck is the app's DeviceCheck token (decision 698).
	DeviceCheck *households.DeviceCheckRequest `json:"deviceCheck"`
}

func (h *Handler) redeem(w http.ResponseWriter, r *http.Request) {
	var req redeemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "validation_failed", "Enter a code.")
		return
	}
	ctx := r.Context()
	if p, ok := req.DeviceCheck.Proof(); ok {
		ctx = households.WithDeviceProof(ctx, p)
	}
	householdID := chi.URLParam(r, households.HouseholdIDParam)
	st, err := h.opts.Service.Redeem(ctx, householdID, req.Code)
	var resp StatusResponse
	if err == nil {
		resp, err = h.statusWithProgress(ctx, householdID, st)
	}
	switch {
	case err == nil:
		httpx.WriteJSON(w, http.StatusOK, resp)
	case errors.Is(err, ErrReferralsOff):
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", "resource not found")
	case errors.Is(err, ErrCodeNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, "code_not_found", RedeemMessage(err))
	case errors.Is(err, ErrCodeRetired):
		httpx.WriteError(w, r, http.StatusNotFound, "code_replaced", RedeemMessage(err))
	case errors.Is(err, ErrOwnCode), errors.Is(err, ErrAlreadyRedeemed), errors.Is(err, ErrNotNewToPlus):
		httpx.WriteError(w, r, http.StatusConflict, "code_not_allowed", RedeemMessage(err))
	default:
		h.logger.ErrorContext(r.Context(), "plus redeem failed", "error", err)
		httpx.WriteError(w, r, http.StatusInternalServerError, "internal", RedeemMessage(err))
	}
}

// Require returns middleware that lets a request through only for a
// household with Plus, or while Plus is off. It reads the {householdId} URL
// parameter, so it belongs after the route's permission check. A household
// without Plus gets 402 plus_required, which the app answers with the
// purchase screen.
func Require(s *Service, logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			householdID := chi.URLParam(r, households.HouseholdIDParam)
			if s == nil || !s.EnabledFor(householdID) {
				next.ServeHTTP(w, r)
				return
			}
			ok, err := s.HasPlus(r.Context(), householdID)
			if err != nil {
				logger.ErrorContext(r.Context(), "plus check failed", "error", err)
				httpx.WriteError(w, r, http.StatusInternalServerError, "internal", "Something went wrong. Try again.")
				return
			}
			if !ok {
				httpx.WriteError(w, r, http.StatusPaymentRequired, "plus_required", RequiredMessage)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

type webhookBody struct {
	Event *Event `json:"event"`
}

// webhook serves POST /webhooks/revenuecat. A wrong or missing secret is
// 401; an event this package doesn't use is 200, so RevenueCat stops
// retrying it.
func (h *Handler) webhook(w http.ResponseWriter, r *http.Request) {
	secret := h.opts.WebhookSecret
	if secret == "" {
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", "resource not found")
		return
	}
	got := r.Header.Get("Authorization")
	if subtle.ConstantTimeCompare([]byte(got), []byte(secret)) != 1 &&
		subtle.ConstantTimeCompare([]byte(got), []byte("Bearer "+secret)) != 1 {
		httpx.WriteError(w, r, http.StatusUnauthorized, "unauthenticated", "unauthenticated")
		return
	}
	var body webhookBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Event == nil {
		httpx.WriteError(w, r, http.StatusBadRequest, "validation_failed", "not a RevenueCat event")
		return
	}
	outcome, err := h.opts.Service.Apply(r.Context(), *body.Event)
	if err != nil {
		h.logger.ErrorContext(r.Context(), "plus webhook failed", "eventType", body.Event.Type, "error", err)
		httpx.WriteError(w, r, http.StatusInternalServerError, "internal", "try again")
		return
	}
	// The app user ID is a household ID; it's safe to log, the event body isn't kept.
	h.logger.InfoContext(r.Context(), "plus webhook", "eventType", body.Event.Type, "outcome", outcome,
		"environment", body.Event.Environment)
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"outcome": string(outcome)})
}
