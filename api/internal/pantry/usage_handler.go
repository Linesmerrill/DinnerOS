package pantry

import (
	"context"
	"math/big"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

// --- Wire types ---------------------------------------------------------------

// AmountResponse is an exact amount with a number for display.
type AmountResponse struct {
	Quantity      string  `json:"quantity"`
	QuantityValue float64 `json:"quantityValue"`
}

// UnitSizeResponse says one Per ("package") holds Quantity Unit ("8 oz").
type UnitSizeResponse struct {
	Per           string  `json:"per"`
	Quantity      string  `json:"quantity"`
	QuantityValue float64 `json:"quantityValue"`
	Unit          string  `json:"unit"`
}

// RecipeUseResponse is what cooked recipes used in the cycle.
type RecipeUseResponse struct {
	Count         int     `json:"count"`
	Quantity      string  `json:"quantity"`
	QuantityValue float64 `json:"quantityValue"`
}

// DailyRateResponse is the learned non-recipe use per day.
type DailyRateResponse struct {
	Quantity        string  `json:"quantity"`
	QuantityValue   float64 `json:"quantityValue"`
	BasedOnSegments int     `json:"basedOnSegments"`
}

// EstimateResponse is an item's usage estimate. Every amount is in Unit.
type EstimateResponse struct {
	CycleID        string      `json:"cycleId"`
	CycleSource    CycleSource `json:"cycleSource"`
	CycleStartedAt time.Time   `json:"cycleStartedAt"`
	// AdjustedAt is when a person last corrected the amount in this cycle.
	AdjustedAt       *time.Time         `json:"adjustedAt"`
	Unit             string             `json:"unit"`
	StartAmount      AmountResponse     `json:"startAmount"`
	Remaining        AmountResponse     `json:"remaining"`
	PercentRemaining int                `json:"percentRemaining"`
	PercentUsed      int                `json:"percentUsed"`
	RecipeUse        RecipeUseResponse  `json:"recipeUse"`
	OtherUse         AmountResponse     `json:"otherUse"`
	DailyRate        *DailyRateResponse `json:"dailyRate"`
	SkippedRecipes   int                `json:"skippedRecipes"`
	// LowThresholdPercent is the threshold in effect; ThresholdSource says
	// whether it's the item's override or the household's.
	LowThresholdPercent int       `json:"lowThresholdPercent"`
	ThresholdSource     string    `json:"thresholdSource"`
	BelowThreshold      bool      `json:"belowThreshold"`
	Summary             string    `json:"summary"`
	EstimatedAt         time.Time `json:"estimatedAt"`
}

// UnitSizeRequest says how much one purchased unit holds.
type UnitSizeRequest struct {
	Quantity string `json:"quantity"`
	Unit     string `json:"unit"`
}

// RecordPurchaseRequest is the body of POST .../pantry/purchases.
type RecordPurchaseRequest struct {
	ItemID           string           `json:"itemId"`
	IngredientID     string           `json:"ingredientId"`
	Name             string           `json:"name"`
	Source           PurchaseSource   `json:"source"`
	Quantity         string           `json:"quantity"`
	Unit             string           `json:"unit"`
	UnitSize         *UnitSizeRequest `json:"unitSize"`
	Week             string           `json:"week"`
	ClientPurchaseID string           `json:"clientPurchaseId"`
}

// PurchaseResponse is a recorded purchase.
type PurchaseResponse struct {
	ID               string            `json:"id"`
	HouseholdID      string            `json:"householdId"`
	ItemID           string            `json:"itemId"`
	Source           PurchaseSource    `json:"source"`
	Quantity         *string           `json:"quantity"`
	QuantityValue    *float64          `json:"quantityValue"`
	Unit             *string           `json:"unit"`
	UnitSize         *UnitSizeResponse `json:"unitSize"`
	Week             *string           `json:"week"`
	ClientPurchaseID *string           `json:"clientPurchaseId"`
	// Provider is set for source provider: the handoff line it confirms.
	Provider    *PurchaseProviderResponse `json:"provider"`
	RecordedBy  string                    `json:"recordedBy"`
	PurchasedAt time.Time                 `json:"purchasedAt"`
}

// PurchaseProviderResponse links a provider purchase to its handoff line.
type PurchaseProviderResponse struct {
	Key       string `json:"key"`
	HandoffID string `json:"handoffId"`
	LineID    string `json:"lineId"`
	ProductID string `json:"productId"`
}

// RecordPurchaseResponse is returned by POST .../pantry/purchases.
type RecordPurchaseResponse struct {
	Purchase PurchaseResponse   `json:"purchase"`
	Item     PantryItemResponse `json:"item"`
}

// PurchaseListResponse is returned by GET .../pantry/{itemId}/purchases.
type PurchaseListResponse struct {
	Items []PurchaseResponse `json:"items"`
}

// PantrySettingsResponse is the household's pantry settings.
type PantrySettingsResponse struct {
	LowThresholdPercent        int        `json:"lowThresholdPercent"`
	DefaultLowThresholdPercent int        `json:"defaultLowThresholdPercent"`
	UpdatedBy                  *string    `json:"updatedBy"`
	UpdatedAt                  *time.Time `json:"updatedAt"`
}

// UpdatePantrySettingsRequest is the body of PUT .../pantry/settings.
type UpdatePantrySettingsRequest struct {
	LowThresholdPercent *int `json:"lowThresholdPercent"`
}

func ratValue(r *big.Rat) float64 {
	f, _ := r.Float64()
	return f
}

func amountResponse(r *big.Rat) AmountResponse {
	return AmountResponse{Quantity: r.RatString(), QuantityValue: ratValue(r)}
}

func unitSizeResponse(s *UnitSize) *UnitSizeResponse {
	if s == nil {
		return nil
	}
	q := ratOf(s.Quantity)
	return &UnitSizeResponse{Per: s.Unit, Quantity: s.Quantity, QuantityValue: ratValue(q), Unit: s.SizeUnit}
}

func newEstimateResponse(e *Estimate) *EstimateResponse {
	if e == nil {
		return nil
	}
	resp := &EstimateResponse{
		CycleID: e.CycleID, CycleSource: e.CycleSource, CycleStartedAt: e.CycleStartedAt.UTC(), Unit: e.Unit,
		StartAmount: amountResponse(e.Reference), Remaining: amountResponse(e.Remaining),
		PercentRemaining: e.PercentRemaining, PercentUsed: e.PercentUsed,
		RecipeUse:      RecipeUseResponse{Count: e.RecipeUses, Quantity: e.RecipeUsed.RatString(), QuantityValue: ratValue(e.RecipeUsed)},
		OtherUse:       amountResponse(e.OtherUsed),
		SkippedRecipes: e.SkippedUses, LowThresholdPercent: e.ThresholdPct, ThresholdSource: "household",
		BelowThreshold: e.BelowThreshold, Summary: e.Summary, EstimatedAt: e.EstimatedAt.UTC(),
	}
	if e.ThresholdItem {
		resp.ThresholdSource = "item"
	}
	if !e.AdjustedAt.IsZero() {
		at := e.AdjustedAt.UTC()
		resp.AdjustedAt = &at
	}
	if e.DailyRate != nil {
		resp.DailyRate = &DailyRateResponse{Quantity: e.DailyRate.RatString(), QuantityValue: ratValue(e.DailyRate), BasedOnSegments: e.RateSegments}
	}
	return resp
}

func newPurchaseResponse(p Purchase) PurchaseResponse {
	resp := PurchaseResponse{
		ID: p.ID, HouseholdID: p.HouseholdID, ItemID: p.ItemID, Source: p.Source,
		UnitSize: unitSizeResponse(p.UnitSize), RecordedBy: p.RecordedBy, PurchasedAt: p.PurchasedAt.UTC(),
	}
	if p.Quantity != "" {
		exact, unit := p.Quantity, p.Unit
		resp.Quantity, resp.Unit = &exact, &unit
		if q, err := ingredients.ParseQuantity(exact); err == nil {
			v := q.Float64()
			resp.QuantityValue = &v
		}
	}
	if p.Week != "" {
		week := p.Week
		resp.Week = &week
	}
	if p.ClientPurchaseID != "" {
		id := p.ClientPurchaseID
		resp.ClientPurchaseID = &id
	}
	if pr := p.Provider; pr != nil {
		resp.Provider = &PurchaseProviderResponse{Key: pr.Key, HandoffID: pr.HandoffID, LineID: pr.LineID, ProductID: pr.ProductID}
	}
	return resp
}

func newSettingsResponse(s Settings) PantrySettingsResponse {
	resp := PantrySettingsResponse{LowThresholdPercent: s.LowThresholdPercent, DefaultLowThresholdPercent: DefaultLowThresholdPercent}
	if !s.UpdatedAt.IsZero() {
		by, at := s.UpdatedBy, s.UpdatedAt.UTC()
		resp.UpdatedBy, resp.UpdatedAt = &by, &at
	}
	return resp
}

// itemResponses renders items with their estimates under the household's
// settings. A settings read failure is logged and the defaults are used.
func (h *Handler) itemResponses(r *http.Request, householdID string, items []Item) []PantryItemResponse {
	settings, err := h.opts.Service.Settings(r.Context(), householdID)
	if err != nil {
		h.logger.WarnContext(r.Context(), "load pantry settings failed; using defaults", "householdId", householdID, "error", err)
		settings = Settings{HouseholdID: householdID, LowThresholdPercent: DefaultLowThresholdPercent}
	}
	out := make([]PantryItemResponse, 0, len(items))
	for _, item := range items {
		resp := newItemResponse(item)
		resp.Estimate = newEstimateResponse(h.opts.Service.Estimate(item, settings))
		out = append(out, resp)
	}
	return out
}

// ItemResponse renders item with its usage estimate under the household's
// settings, for other modules' responses (a recorded batch). A settings read
// failure is logged and the defaults are used.
func (s *Service) ItemResponse(ctx context.Context, item Item) PantryItemResponse {
	settings, err := s.Settings(ctx, item.HouseholdID)
	if err != nil {
		s.logger.WarnContext(ctx, "load pantry settings failed; using defaults", "householdId", item.HouseholdID, "error", err)
		settings = Settings{HouseholdID: item.HouseholdID, LowThresholdPercent: DefaultLowThresholdPercent}
	}
	resp := newItemResponse(item)
	resp.Estimate = newEstimateResponse(s.Estimate(item, settings))
	return resp
}

// NewPurchaseResponse renders a purchase, for other modules' responses.
func NewPurchaseResponse(p Purchase) PurchaseResponse { return newPurchaseResponse(p) }

func (h *Handler) itemResponse(r *http.Request, item Item) PantryItemResponse {
	return h.itemResponses(r, item.HouseholdID, []Item{item})[0]
}

// --- Handlers -----------------------------------------------------------------

func (h *Handler) recordPurchase(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	var req RecordPurchaseRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	in := PurchaseInput{
		ItemID: req.ItemID, IngredientID: req.IngredientID, Name: req.Name, Source: req.Source,
		Quantity: req.Quantity, Unit: req.Unit, Week: req.Week, ClientPurchaseID: req.ClientPurchaseID,
	}
	if req.UnitSize != nil {
		in.UnitSizeQuantity, in.UnitSizeUnit = req.UnitSize.Quantity, req.UnitSize.Unit
		if in.UnitSizeQuantity == "" && in.UnitSizeUnit == "" {
			h.writeError(w, r, "", invalid("unitSize needs a positive quantity and a unit"))
			return
		}
	}
	res, err := h.opts.Service.RecordPurchase(r.Context(), actor, in)
	if err != nil {
		h.writeError(w, r, "record pantry purchase failed", err)
		return
	}
	status := http.StatusOK
	if res.Created {
		status = http.StatusCreated
	}
	httpx.WriteJSON(w, status, RecordPurchaseResponse{Purchase: newPurchaseResponse(res.Purchase), Item: h.itemResponse(r, res.Item)})
}

func (h *Handler) listPurchases(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	list, err := h.opts.Service.ListPurchases(r.Context(), actor.HouseholdID, chi.URLParam(r, "itemId"))
	if err != nil {
		h.writeError(w, r, "list pantry purchases failed", err)
		return
	}
	resp := PurchaseListResponse{Items: make([]PurchaseResponse, 0, len(list))}
	for _, p := range list {
		resp.Items = append(resp.Items, newPurchaseResponse(p))
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) getSettings(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	settings, err := h.opts.Service.Settings(r.Context(), actor.HouseholdID)
	if err != nil {
		h.writeError(w, r, "get pantry settings failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, newSettingsResponse(settings))
}

func (h *Handler) putSettings(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	var req UpdatePantrySettingsRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.LowThresholdPercent == nil {
		h.writeError(w, r, "", invalid("lowThresholdPercent is required"))
		return
	}
	settings, err := h.opts.Service.UpdateSettings(r.Context(), actor, *req.LowThresholdPercent)
	if err != nil {
		h.writeError(w, r, "update pantry settings failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, newSettingsResponse(settings))
}
