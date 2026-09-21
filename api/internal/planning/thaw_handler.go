package planning

import (
	"net/http"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

// ThawDueResponse is returned by GET .../thaw: what today's meals need out of
// the freezer.
type ThawDueResponse struct {
	// Date is today in the household's time zone.
	Date string `json:"date"`
	// ReminderHour is the local hour the household's thaw reminders go out.
	ReminderHour int `json:"reminderHour"`
	// Items is never null; empty means nothing frozen is needed today.
	Items []ThawItemResponse `json:"items"`
}

// ThawItemResponse is one frozen item today's meals need.
type ThawItemResponse struct {
	ItemID string `json:"itemId"`
	Name   string `json:"name"`
	// Recipes are the meals that need it, by name, never null.
	Recipes []string `json:"recipes"`
	// Hours is the fridge thaw estimate for one portion; Measured is false
	// when it is the category default rather than a weight calculation.
	Hours    int  `json:"hours"`
	Measured bool `json:"measured"`
	// MoveBy is the local time to put it in the fridge ("14:00").
	MoveBy string `json:"moveBy"`
	// Overnight is true when MoveBy has already passed today, so the honest
	// advice is "move it now".
	Overnight bool   `json:"overnight"`
	Summary   string `json:"summary"`
}

func newThawDueResponse(d ThawDue) ThawDueResponse {
	resp := ThawDueResponse{Date: d.Date, ReminderHour: d.Hour, Items: make([]ThawItemResponse, 0, len(d.Items))}
	for _, item := range d.Items {
		recipes := item.Recipes
		if recipes == nil {
			recipes = []string{}
		}
		resp.Items = append(resp.Items, ThawItemResponse{
			ItemID: item.ItemID, Name: item.Name, Recipes: recipes,
			Hours: item.Hours, Measured: item.Measured,
			MoveBy: item.MoveBy, Overnight: item.Overnight, Summary: item.Summary,
		})
	}
	return resp
}

func (h *Handler) thawDue(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	due, err := h.opts.Service.ThawDue(r.Context(), actor.HouseholdID)
	if err != nil {
		h.writeError(w, r, "list thaw reminders failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, newThawDueResponse(due))
}
