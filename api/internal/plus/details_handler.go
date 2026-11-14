package plus

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

// DetailsResponse is Your Plus (GET .../plus/details).
type DetailsResponse struct {
	Status StatusResponse `json:"status"`
	// Plan is the subscription's plan; nil without one.
	Plan *PlanLine `json:"plan,omitempty"`
	// State is trial, active, canceled, expired, free (months from codes
	// only), or none. StateText says it.
	State     string `json:"state"`
	StateText string `json:"stateText"`
	// DateLabel ("Renews on", "Ends on", "Ended on", "Free until") goes
	// with Date.
	DateLabel string     `json:"dateLabel,omitempty"`
	Date      *time.Time `json:"date,omitempty"`
	// Since is when the household first had Plus.
	Since *time.Time `json:"since,omitempty"`
	// SinceText is "Plus since", shown with Since.
	SinceText string `json:"sinceText"`
	// Badge is "Founding member" for households that started early.
	Badge string `json:"badge,omitempty"`
	// StartedBy is the member who started Plus, when known.
	StartedBy string `json:"startedBy,omitempty"`
	// CanManage is true for the member who started Plus (or an admin when
	// that isn't known). Everyone else gets status only: no history.
	CanManage bool `json:"canManage"`
	// ManageNote explains why someone can't manage.
	ManageNote string `json:"manageNote,omitempty"`
	// History is billing events and free months from codes, newest first.
	History     []HistoryItem `json:"history"`
	HistoryNote string        `json:"historyNote"`
	// Stats is the "What Plus did for you" strip.
	Stats      []StatItem `json:"stats"`
	StatsTitle string     `json:"statsTitle"`
	// Invites are free months earned from the household's code.
	MonthsEarned int `json:"monthsEarned"`
	MonthsCap    int `json:"monthsCap"`
}

// PlanLine is a plan's name and price.
type PlanLine struct {
	ProductID string `json:"productId"`
	Name      string `json:"name"`
	// PriceText is the API's price; the app shows the App Store's when it
	// has it.
	PriceText string `json:"priceText"`
}

// HistoryItem is one line of Your Plus history.
type HistoryItem struct {
	At    time.Time `json:"at"`
	Title string    `json:"title"`
	// Amount and Currency are set when money changed hands.
	Amount   *float64 `json:"amount,omitempty"`
	Currency string   `json:"currency,omitempty"`
	// Kind is billing or invite; invite lines link to Your Invites.
	Kind string `json:"kind"`
}

// StatItem is one number in the stats strip.
type StatItem struct {
	// Value is a count; Cents is set instead for money.
	Value *int   `json:"value,omitempty"`
	Cents *int64 `json:"cents,omitempty"`
	Label string `json:"label"`
	// Symbol is an SF Symbol name.
	Symbol string `json:"symbol"`
}

// Your Plus words.
const (
	HistoryNote   = "Apple emails your receipts."
	ManageNote    = "Only the person who started Plus can change it, in their Apple account."
	StatsTitle    = "What Plus did for you"
	SinceText     = "Plus since"
	FoundingBadge = "Founding member"
)

func planLine(productID string) *PlanLine {
	if productID == "" {
		return nil
	}
	for _, p := range DefaultOffer().Plans {
		if p.ProductID == productID {
			return &PlanLine{ProductID: p.ProductID, Name: p.Name, PriceText: p.PriceText}
		}
	}
	name := "Plus"
	if strings.HasSuffix(productID, ".monthly") {
		name = "Monthly"
	} else if strings.HasSuffix(productID, ".yearly") {
		name = "Yearly"
	}
	return &PlanLine{ProductID: productID, Name: name}
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// DetailsFor builds the response for d at now.
func DetailsFor(d Details, startedBy string, now time.Time) DetailsResponse {
	resp := DetailsResponse{
		Status: statusResponse(d.Status), State: "none", StateText: "No Plus yet",
		Since: timePtr(d.Since), SinceText: SinceText, StartedBy: startedBy, CanManage: d.CanManage,
		History: []HistoryItem{}, HistoryNote: HistoryNote, Stats: []StatItem{}, StatsTitle: StatsTitle,
		MonthsCap: MaxReferralMonths,
	}
	if !d.CanManage {
		resp.ManageNote = ManageNote
	}
	if !d.Since.IsZero() && d.Since.Before(FoundingMemberBefore) {
		resp.Badge = FoundingBadge
	}
	e := d.Entitlement
	switch {
	case d.Status.Comp != nil:
		// Given by DinnerOS: nothing to renew or pay. A store subscription
		// on top still shows its plan, so it can be canceled.
		resp.State, resp.StateText = "comp", CompStateText
		if e != nil && e.Active(now) {
			resp.Plan = planLine(e.ProductID)
		}
	case e != nil && e.Active(now):
		resp.Plan = planLine(e.ProductID)
		switch {
		case e.Trial:
			resp.State, resp.StateText = "trial", "Free trial"
		case e.WillRenew:
			resp.State, resp.StateText = "active", "Active"
		default:
			resp.State, resp.StateText = "canceled", "Canceled"
		}
		resp.DateLabel = "Ends on"
		if e.WillRenew {
			resp.DateLabel = "Renews on"
		}
		resp.Date = timePtr(e.ExpiresAt)
	case !d.Status.FreeUntil.IsZero() && d.Status.FreeUntil.After(now):
		resp.State, resp.StateText = "free", "Free months from invites"
		resp.DateLabel, resp.Date = "Free until", timePtr(d.Status.FreeUntil)
	case e != nil:
		resp.Plan = planLine(e.ProductID)
		resp.State, resp.StateText = "expired", "Expired"
		resp.DateLabel, resp.Date = "Ended on", timePtr(e.ExpiresAt)
	}

	for _, b := range d.Billing {
		item := HistoryItem{At: b.At, Title: b.Title(), Kind: "billing"}
		if b.Amount > 0 {
			a := b.Amount
			item.Amount, item.Currency = &a, b.Currency
		}
		if item.Title != "" {
			resp.History = append(resp.History, item)
		}
	}
	if u := d.UsedCode; u != nil {
		resp.History = append(resp.History, HistoryItem{At: u.At, Title: "Free month from a code you used", Kind: "invite"})
	}
	for _, j := range d.InviteJoins {
		if j.ReferrerRewarded {
			resp.MonthsEarned++
			resp.History = append(resp.History, HistoryItem{At: j.At, Title: "Free month from a household you invited", Kind: "invite"})
		}
	}
	sortHistory(resp.History)

	if st := d.Stats; st != nil {
		// A zero says nothing yet; leave it out.
		count := func(n int, label, symbol string) {
			if n <= 0 {
				return
			}
			v := n
			resp.Stats = append(resp.Stats, StatItem{Value: &v, Label: label, Symbol: symbol})
		}
		count(st.WeeksPlanned, plural(st.WeeksPlanned, "week planned by Autopilot", "weeks planned by Autopilot"), "sparkles")
		count(st.MealsPlanned, plural(st.MealsPlanned, "meal planned", "meals planned"), "fork.knife")
		if st.SavedCents != nil && *st.SavedCents > 0 {
			c := *st.SavedCents
			resp.Stats = append(resp.Stats, StatItem{Cents: &c, Label: "saved vs meal kits", Symbol: "dollarsign.circle"})
		}
		count(st.CartsSent, plural(st.CartsSent, "cart sent to a store", "carts sent to a store"), "cart")
	}
	return resp
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func sortHistory(h []HistoryItem) {
	for i := 1; i < len(h); i++ {
		for j := i; j > 0 && h[j].At.After(h[j-1].At); j-- {
			h[j], h[j-1] = h[j-1], h[j]
		}
	}
}

func (h *Handler) details(w http.ResponseWriter, r *http.Request) {
	m, ok := households.MembershipFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusForbidden, "forbidden", "forbidden")
		return
	}
	d, err := h.opts.Service.Details(r.Context(), chi.URLParam(r, households.HouseholdIDParam), m)
	if errors.Is(err, ErrReferralsOff) {
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", "resource not found")
		return
	}
	if err != nil {
		h.logger.ErrorContext(r.Context(), "plus details failed", "error", err)
		httpx.WriteError(w, r, http.StatusInternalServerError, "internal", "Something went wrong. Try again.")
		return
	}
	startedBy := ""
	if d.Entitlement != nil && d.Entitlement.StartedBy != "" && h.opts.MemberName != nil {
		startedBy = h.opts.MemberName(r.Context(), d.Entitlement.StartedBy)
	}
	httpx.WriteJSON(w, http.StatusOK, DetailsFor(d, startedBy, h.opts.Service.now()))
}
