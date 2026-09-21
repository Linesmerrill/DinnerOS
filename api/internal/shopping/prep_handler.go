package shopping

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/pantry"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

// PrepSessionResponse is the week's prep checklist.
type PrepSessionResponse struct {
	Week string `json:"week"`
	// State is nothing_to_prep, ready, or finished.
	State string `json:"state"`
	// Headline is ready to show, including when there is nothing to do.
	Headline string `json:"headline"`
	Pending  int    `json:"pending"`
	Done     int    `json:"done"`
	Skipped  int    `json:"skipped"`
	// UpdatedAt is when a card was last answered, or null for a session
	// nobody has touched.
	UpdatedAt *time.Time `json:"updatedAt"`
	// Cards is never null; empty means nothing needed prepping.
	Cards []PrepCardResponse `json:"cards"`
}

// PrepCardResponse is one thing to prep.
type PrepCardResponse struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	HandoffID string `json:"handoffId"`
	LineID    string `json:"lineId"`
	// Status is pending, done, or skipped.
	Status        string  `json:"status"`
	IngredientKey string  `json:"ingredientKey"`
	IngredientID  *string `json:"ingredientId"`
	Name          string  `json:"name"`
	Category      string  `json:"category"`
	ProductName   string  `json:"productName"`
	Unit          string  `json:"unit"`
	// Bought, Needed, and Surplus are exact ("27/2"); the *Value fields are
	// the same numbers for display.
	Bought         string  `json:"bought"`
	BoughtValue    float64 `json:"boughtValue"`
	Needed         string  `json:"needed"`
	NeededValue    float64 `json:"neededValue"`
	Surplus        string  `json:"surplus"`
	SurplusValue   float64 `json:"surplusValue"`
	SurplusPercent int     `json:"surplusPercent"`
	SurplusText    string  `json:"surplusText"`
	Freezable      bool    `json:"freezable"`
	Frozen         bool    `json:"frozen"`
	// Instruction is the card's one line of guidance, ready to show.
	Instruction string `json:"instruction"`
	// Reminder is none, thaw, or list: which reminder the app may promise.
	Reminder     string `json:"reminder"`
	ReminderText string `json:"reminderText"`
	// Meals are the planned meals the reserved amount is for, never null.
	Meals []PrepMealResponse `json:"meals"`
	// Portions is null for a card with nothing to freeze.
	Portions *PortionPlanResponse `json:"portions"`
	// FrozenItemID and FrozenPortions describe what finishing it recorded.
	FrozenItemID   *string    `json:"frozenItemId"`
	FrozenPortions int        `json:"frozenPortions"`
	AnsweredBy     *string    `json:"answeredBy"`
	AnsweredAt     *time.Time `json:"answeredAt"`
	// Suggestions are second meals to plan this week, best first, never null.
	Suggestions []BulkPackSuggestionResponse `json:"suggestions"`
}

// PrepMealResponse is one planned meal a card's ingredient was bought for.
type PrepMealResponse struct {
	RecipeID   string `json:"recipeId"`
	RecipeName string `json:"recipeName"`
	// Day and Date are null for a meal this week with no day yet.
	Day  *string `json:"day"`
	Date *string `json:"date"`
	// Past is true when the meal's date has already gone by.
	Past bool `json:"past"`
}

// PortionPlanResponse is the card's portioning advice.
type PortionPlanResponse struct {
	Unit             string  `json:"unit"`
	Reserved         string  `json:"reserved"`
	ReservedValue    float64 `json:"reservedValue"`
	ReservedText     string  `json:"reservedText"`
	Surplus          string  `json:"surplus"`
	SurplusValue     float64 `json:"surplusValue"`
	Meals            int     `json:"meals"`
	TypicalMeal      string  `json:"typicalMeal"`
	TypicalMealValue float64 `json:"typicalMealValue"`
	TypicalMealText  string  `json:"typicalMealText"`
	// Basis is meal or week: where one portion's size came from.
	Basis            string  `json:"basis"`
	Portions         int     `json:"portions"`
	PortionSize      string  `json:"portionSize"`
	PortionSizeValue float64 `json:"portionSizeValue"`
	PortionSizeText  string  `json:"portionSizeText"`
	// Thaw is the estimate for one portion at this count.
	Thaw pantry.ThawResponse `json:"thaw"`
	// Options are the counts the app offers, each with its own size and
	// thaw estimate. Never null.
	Options []PortionOptionResponse `json:"options"`
}

// PortionOptionResponse is one choice of portion count.
type PortionOptionResponse struct {
	Portions  int                 `json:"portions"`
	Size      string              `json:"size"`
	SizeValue float64             `json:"sizeValue"`
	SizeText  string              `json:"sizeText"`
	Thaw      pantry.ThawResponse `json:"thaw"`
}

// PrepCardResultResponse is returned by finishing or skipping a card: the
// card as it now stands, and the session around it, so the app never has to
// reload to know what is left.
type PrepCardResultResponse struct {
	Card    PrepCardResponse    `json:"card"`
	Session PrepSessionResponse `json:"session"`
}

// CompletePrepCardRequest is the body of finishing a card.
type CompletePrepCardRequest struct {
	// Portions overrides the suggested count; omit it to take the
	// suggestion.
	Portions int `json:"portions"`
}

func (r CompletePrepCardRequest) input() PrepInput { return PrepInput(r) }

func prepSessionResponse(s PrepSession) PrepSessionResponse {
	resp := PrepSessionResponse{
		Week: s.Week, State: string(s.State), Headline: s.Headline,
		Pending: s.Pending, Done: s.Done, Skipped: s.Skipped,
		Cards: make([]PrepCardResponse, 0, len(s.Cards)),
	}
	if !s.UpdatedAt.IsZero() {
		at := s.UpdatedAt
		resp.UpdatedAt = &at
	}
	for _, c := range s.Cards {
		resp.Cards = append(resp.Cards, prepCardResponse(c))
	}
	return resp
}

func prepCardResponse(c PrepCard) PrepCardResponse {
	p := c.Pack
	out := PrepCardResponse{
		ID: c.ID, Kind: string(c.Kind), HandoffID: c.HandoffID, LineID: c.LineID, Status: string(c.Status),
		IngredientKey: p.IngredientKey, IngredientID: optionalString(p.IngredientID()),
		Name: p.Name, Category: p.Category, ProductName: p.ProductName, Unit: p.Unit,
		Bought: p.Bought, Needed: p.Needed, Surplus: p.Surplus, SurplusPercent: p.SurplusPercent,
		SurplusText: surplusText(p), Freezable: p.Freezable, Frozen: p.Frozen,
		Instruction: c.Instruction, Reminder: string(c.Reminder), ReminderText: c.ReminderText,
		Meals: make([]PrepMealResponse, 0, len(c.Meals)), FrozenPortions: c.FrozenPortions,
		Suggestions: bulkPackResponse(p).Suggestions,
	}
	out.BoughtValue, out.NeededValue, out.SurplusValue = exactValue(p.Bought), exactValue(p.Needed), exactValue(p.Surplus)
	for _, m := range c.Meals {
		meal := PrepMealResponse{RecipeID: m.RecipeID, RecipeName: m.RecipeName, Past: m.Past}
		if m.Day != "" {
			day := m.Day
			meal.Day = &day
		}
		if m.Date != "" {
			date := m.Date
			meal.Date = &date
		}
		out.Meals = append(out.Meals, meal)
	}
	if p.Freezable {
		out.Portions = portionPlanResponse(c.Portions)
	}
	out.FrozenItemID = optionalString(c.FrozenItemID)
	out.AnsweredBy = optionalString(c.AnsweredBy)
	if !c.AnsweredAt.IsZero() {
		at := c.AnsweredAt
		out.AnsweredAt = &at
	}
	return out
}

func portionPlanResponse(p PortionPlan) *PortionPlanResponse {
	out := &PortionPlanResponse{
		Unit: p.Unit, Reserved: p.Reserved, ReservedValue: exactValue(p.Reserved),
		ReservedText: amountText(p.Reserved, p.Unit),
		Surplus:      p.Surplus, SurplusValue: exactValue(p.Surplus), Meals: p.Meals,
		TypicalMeal: p.TypicalMeal, TypicalMealValue: exactValue(p.TypicalMeal),
		TypicalMealText: amountText(p.TypicalMeal, p.Unit), Basis: string(p.Basis),
		Portions: p.Portions, PortionSize: p.PortionSize, PortionSizeValue: exactValue(p.PortionSize),
		PortionSizeText: amountText(p.PortionSize, p.Unit), Thaw: pantry.NewThawResponse(p.Thaw),
		Options: make([]PortionOptionResponse, 0, len(p.Options)),
	}
	for _, o := range p.Options {
		out.Options = append(out.Options, PortionOptionResponse{
			Portions: o.Portions, Size: o.Size, SizeValue: exactValue(o.Size),
			SizeText: amountText(o.Size, p.Unit), Thaw: pantry.NewThawResponse(o.Thaw),
		})
	}
	return out
}

func (h *Handler) prepSession(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	session, err := h.opts.Service.PrepSession(r.Context(), actor.HouseholdID, chi.URLParam(r, "week"))
	if err != nil {
		h.writeError(w, r, "load prep session failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, prepSessionResponse(session))
}

func (h *Handler) completePrepCard(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	var req CompletePrepCardRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpx.WriteError(w, r, http.StatusBadRequest, "validation_failed", "invalid JSON body")
			return
		}
	}
	session, card, err := h.opts.Service.CompletePrepCard(
		r.Context(), actor, chi.URLParam(r, "week"), chi.URLParam(r, "cardId"), req.input())
	if err != nil {
		h.writeError(w, r, "complete prep card failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, PrepCardResultResponse{
		Card: prepCardResponse(card), Session: prepSessionResponse(session),
	})
}

func (h *Handler) skipPrepCard(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	session, card, err := h.opts.Service.SkipPrepCard(
		r.Context(), actor, chi.URLParam(r, "week"), chi.URLParam(r, "cardId"))
	if err != nil {
		h.writeError(w, r, "skip prep card failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, PrepCardResultResponse{
		Card: prepCardResponse(card), Session: prepSessionResponse(session),
	})
}
