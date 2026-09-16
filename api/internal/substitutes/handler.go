package substitutes

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/auth"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/pantry"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

// PantryRenderer renders pantry items for batch responses. *pantry.Service
// implements it.
type PantryRenderer interface {
	ItemResponse(ctx context.Context, item pantry.Item) pantry.PantryItemResponse
}

// HandlerOptions configures the specialty ingredient HTTP handlers.
type HandlerOptions struct {
	Service    *Service
	Pantry     PantryRenderer
	Authorizer households.Authorizer
	Tokens     auth.AccessTokenValidator
	Logger     *slog.Logger
}

// Handler serves the specialty ingredient endpoints.
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

// Mount registers the routes on r, which is expected to be the /api/v1 router.
func (h *Handler) Mount(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireAuth(h.opts.Tokens))
		view := households.RequirePermission(h.opts.Authorizer, households.PermHouseholdView, h.logger)
		edit := households.RequirePermission(h.opts.Authorizer, households.PermPantryEdit, h.logger)
		const base = "/households/{householdId}/specialty-ingredients"
		r.With(view).Get(base, h.list)
		// A static segment wins over {specialtyId}, like choices/defaults.
		r.With(view).Get(base+"/settings", h.settings)
		r.With(edit).Put(base+"/settings", h.setSettings)
		r.With(edit).Post(base+"/choices/defaults", h.applyDefaults)
		r.With(view).Get(base+"/{specialtyId}", h.get)
		r.With(edit).Put(base+"/{specialtyId}/choice", h.setChoice)
		r.With(edit).Delete(base+"/{specialtyId}/choice", h.clearChoice)
		r.With(edit).Post(base+"/{specialtyId}/options", h.createOption)
		r.With(edit).Put(base+"/{specialtyId}/options/{optionId}", h.updateOption)
		r.With(edit).Delete(base+"/{specialtyId}/options/{optionId}", h.deleteOption)
		r.With(edit).Post(base+"/{specialtyId}/batches", h.recordBatch)
	})
}

// --- Wire types ---------------------------------------------------------------

// AmountResponse is an exact amount with a number and text for display.
type AmountResponse struct {
	Quantity      string  `json:"quantity"`
	QuantityValue float64 `json:"quantityValue"`
	Unit          string  `json:"unit"`
	Text          string  `json:"text"`
}

// UnitSizeResponse says one Per ("count", a packet) holds Quantity Unit.
type UnitSizeResponse struct {
	Per           string  `json:"per"`
	Quantity      string  `json:"quantity"`
	QuantityValue float64 `json:"quantityValue"`
	Unit          string  `json:"unit"`
	Text          string  `json:"text"`
}

// ComponentResponse is one regular ingredient of an option. Quantity,
// QuantityValue, and Unit are null for "to taste".
type ComponentResponse struct {
	Name          string   `json:"name"`
	Quantity      *string  `json:"quantity"`
	QuantityValue *float64 `json:"quantityValue"`
	Unit          *string  `json:"unit"`
	Text          string   `json:"text"`
	Category      *string  `json:"category"`
}

// OptionResponse is a curated or household option.
type OptionResponse struct {
	ID          string       `json:"id"`
	SpecialtyID string       `json:"specialtyId"`
	Source      OptionSource `json:"source"`
	Type        OptionType   `json:"type"`
	Name        string       `json:"name"`
	Notes       string       `json:"notes"`
	IsDefault   bool         `json:"isDefault"`
	// Per is set for store alternatives: Per of the specialty is Ingredients.
	Per         *AmountResponse     `json:"per"`
	Ingredients []ComponentResponse `json:"ingredients"`
	// Steps, Yield, and ShelfLifeDays describe a batch.
	Steps           []string        `json:"steps"`
	Yield           *AmountResponse `json:"yield"`
	ShelfLifeDays   *int            `json:"shelfLifeDays"`
	BasedOnOptionID *string         `json:"basedOnOptionId"`
	Summary         string          `json:"summary"`
	CreatedBy       *string         `json:"createdBy"`
	UpdatedBy       *string         `json:"updatedBy"`
	CreatedAt       *time.Time      `json:"createdAt"`
	UpdatedAt       *time.Time      `json:"updatedAt"`
}

// ChoiceResponse is what the household currently does about a specialty
// ingredient: the option a member chose, or the one its strategy picked.
type ChoiceResponse struct {
	// Source is household (a member chose it, including as_is) or strategy
	// (the household's default picked it; a member can still override it).
	Source   ChoiceSource `json:"source"`
	OptionID string       `json:"optionId"`
	// Type is as_is or the option's type.
	Type       string  `json:"type"`
	OptionName *string `json:"optionName"`
	// Strategy is the strategy that picked the option, null when a member
	// chose it.
	Strategy *Strategy `json:"strategy"`
	// ChosenBy and ChosenAt are null for a strategy, which nobody chose.
	ChosenBy *string    `json:"chosenBy"`
	ChosenAt *time.Time `json:"chosenAt"`
}

// StrategyOptionResponse explains one strategy in the words to show.
type StrategyOptionResponse struct {
	Value       Strategy `json:"value"`
	Label       string   `json:"label"`
	Description string   `json:"description"`
}

// SpecialtySettingsResponse is returned by GET and PUT
// .../specialty-ingredients/settings. UpdatedBy and UpdatedAt are null for a
// household that never set one.
type SpecialtySettingsResponse struct {
	Strategy  Strategy   `json:"strategy"`
	UpdatedBy *string    `json:"updatedBy"`
	UpdatedAt *time.Time `json:"updatedAt"`
	// Options explain every strategy, in the order to offer them.
	Options []StrategyOptionResponse `json:"options"`
}

// SetSpecialtySettingsRequest is the body of PUT .../settings.
type SetSpecialtySettingsRequest struct {
	Strategy string `json:"strategy"`
}

// BatchStockResponse is the specialty's batch item in the pantry.
type BatchStockResponse struct {
	PantryItemID     string          `json:"pantryItemId"`
	Status           pantry.Status   `json:"status"`
	Remaining        *AmountResponse `json:"remaining"`
	PercentRemaining *int            `json:"percentRemaining"`
	ExpiresOn        *string         `json:"expiresOn"`
}

// SpecialtyResponse is a specialty ingredient for a household.
type SpecialtyResponse struct {
	ID       string   `json:"id"`
	Key      string   `json:"key"`
	Name     string   `json:"name"`
	Aliases  []string `json:"aliases"`
	Category string   `json:"category"`
	// Note is a short shopping note, empty for most.
	Note            string             `json:"note"`
	IngredientIDs   []string           `json:"ingredientIds"`
	RecipeCount     int                `json:"recipeCount"`
	UnitSizes       []UnitSizeResponse `json:"unitSizes"`
	DefaultOptionID string             `json:"defaultOptionId"`
	Retired         bool               `json:"retired"`
	// ChoiceSource is household, strategy, or none, and says whether Choice
	// is a member's decision or the household's default.
	ChoiceSource ChoiceSource        `json:"choiceSource"`
	Choice       *ChoiceResponse     `json:"choice"`
	Options      []OptionResponse    `json:"options"`
	Batch        *BatchStockResponse `json:"batch"`
}

// SpecialtyListResponse is returned by GET .../specialty-ingredients.
type SpecialtyListResponse struct {
	Items []SpecialtyResponse `json:"items"`
}

// DefaultsResponse is returned by POST .../specialty-ingredients/choices/defaults.
type DefaultsResponse struct {
	Items   []SpecialtyResponse `json:"items"`
	Skipped int                 `json:"skipped"`
}

// SetChoiceRequest is the body of PUT .../{specialtyId}/choice.
type SetChoiceRequest struct {
	OptionID string `json:"optionId"`
}

// MeasureRequest is an amount in a request.
type MeasureRequest struct {
	Quantity string `json:"quantity"`
	Unit     string `json:"unit"`
}

// ComponentRequest is an option ingredient in a request.
type ComponentRequest struct {
	Name     string `json:"name"`
	Quantity string `json:"quantity"`
	Unit     string `json:"unit"`
	Category string `json:"category"`
}

// OptionRequest is the body of POST .../options and PUT .../options/{optionId}.
type OptionRequest struct {
	Type            OptionType         `json:"type"`
	Name            string             `json:"name"`
	Notes           string             `json:"notes"`
	Per             *MeasureRequest    `json:"per"`
	Ingredients     []ComponentRequest `json:"ingredients"`
	Steps           []string           `json:"steps"`
	Yield           *MeasureRequest    `json:"yield"`
	ShelfLifeDays   int                `json:"shelfLifeDays"`
	BasedOnOptionID string             `json:"basedOnOptionId"`
}

// RecordBatchRequest is the body of POST .../{specialtyId}/batches.
type RecordBatchRequest struct {
	OptionID         string `json:"optionId"`
	Batches          *int   `json:"batches"`
	ClientPurchaseID string `json:"clientPurchaseId"`
}

// RecordBatchResponse is returned by POST .../{specialtyId}/batches.
type RecordBatchResponse struct {
	Purchase pantry.PurchaseResponse   `json:"purchase"`
	Item     pantry.PantryItemResponse `json:"item"`
	Option   OptionResponse            `json:"option"`
}

func amountText(q ingredients.Quantity, unit string) string {
	text := q.Format()
	if u, err := ingredients.LookupUnit(unit); err == nil {
		if label := u.Label(q); label != "" {
			text += " " + label
		}
	}
	return text
}

func amountResponse(quantity, unit string) *AmountResponse {
	q, err := ingredients.ParseQuantity(quantity)
	if err != nil {
		return nil
	}
	return &AmountResponse{Quantity: q.String(), QuantityValue: q.Float64(), Unit: unit, Text: amountText(q, unit)}
}

func measureResponse(m *Measure) *AmountResponse {
	if m == nil {
		return nil
	}
	return amountResponse(m.Quantity, m.Unit)
}

func optionSummary(o Option) string {
	switch o.Type {
	case TypeStoreAlternative:
		parts := make([]string, 0, len(o.Ingredients))
		for _, c := range o.Ingredients {
			parts = append(parts, componentText(c)+" "+c.Name)
		}
		per := ""
		if a := measureResponse(o.Per); a != nil {
			per = a.Text + " = "
		}
		return per + strings.Join(parts, " + ")
	case TypeHouseMadeBatch:
		text := "Makes about "
		if a := measureResponse(o.Yield); a != nil {
			text += a.Text
		}
		return text + " and keeps " + strconv.Itoa(o.ShelfLifeDays) + " days."
	}
	return ""
}

func componentText(c Component) string {
	if c.Quantity == "" {
		return "to taste:"
	}
	if a := amountResponse(c.Quantity, c.Unit); a != nil {
		return a.Text
	}
	return c.Quantity
}

func optionalString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func optionalTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	u := t.UTC()
	return &u
}

func newOptionResponse(o Option, defaultID string) OptionResponse {
	resp := OptionResponse{
		ID: o.ID, SpecialtyID: o.SpecialtyID, Source: o.Source, Type: o.Type, Name: o.Name, Notes: o.Notes,
		IsDefault: o.ID == defaultID && o.Source == SourceCurated, Per: measureResponse(o.Per), Yield: measureResponse(o.Yield),
		Ingredients: make([]ComponentResponse, 0, len(o.Ingredients)), Steps: append([]string{}, o.Steps...),
		BasedOnOptionID: optionalString(o.BasedOnOptionID), Summary: optionSummary(o),
		CreatedBy: optionalString(o.CreatedBy), UpdatedBy: optionalString(o.UpdatedBy),
		CreatedAt: optionalTime(o.CreatedAt), UpdatedAt: optionalTime(o.UpdatedAt),
	}
	if o.Type == TypeHouseMadeBatch {
		days := o.ShelfLifeDays
		resp.ShelfLifeDays = &days
	}
	for _, c := range o.Ingredients {
		cr := ComponentResponse{Name: c.Name, Category: optionalString(c.Category), Text: c.Name}
		if a := amountResponse(c.Quantity, c.Unit); c.Quantity != "" && a != nil {
			quantity, unit, v := a.Quantity, a.Unit, a.QuantityValue
			cr.Quantity, cr.Unit, cr.QuantityValue = &quantity, &unit, &v
			cr.Text = a.Text + " " + c.Name
		}
		resp.Ingredients = append(resp.Ingredients, cr)
	}
	return resp
}

func newSpecialtyResponse(v View) SpecialtyResponse {
	sp := v.Specialty
	resp := SpecialtyResponse{
		ID: sp.ID, Key: sp.Key, Name: sp.Name, Aliases: append([]string{}, sp.Aliases...), Category: sp.Category,
		Note: sp.Note, IngredientIDs: append([]string{}, v.IngredientIDs...), RecipeCount: v.RecipeCount,
		UnitSizes: make([]UnitSizeResponse, 0, len(sp.UnitSizes)), DefaultOptionID: sp.DefaultOptionID, Retired: sp.Retired,
		ChoiceSource: ChoiceSourceNone, Options: make([]OptionResponse, 0, len(v.Options)),
	}
	for _, u := range sp.UnitSizes {
		if a := amountResponse(u.Quantity, u.Unit); a != nil {
			resp.UnitSizes = append(resp.UnitSizes, UnitSizeResponse{Per: u.Per, Quantity: a.Quantity, QuantityValue: a.QuantityValue, Unit: u.Unit, Text: a.Text})
		}
	}
	for _, o := range v.Options {
		resp.Options = append(resp.Options, newOptionResponse(o, sp.DefaultOptionID))
	}
	// The plan is the explicit choice when there is one, otherwise whatever the
	// household's strategy picks; choiceSource says which.
	if r := v.Resolution; r.Source != "" && r.Source != ChoiceSourceNone {
		resp.ChoiceSource = r.Source
		cr := &ChoiceResponse{Source: r.Source, OptionID: OptionAsIs, Type: OptionAsIs}
		if o := r.Option; o != nil {
			name := o.Name
			cr.OptionID, cr.Type, cr.OptionName = o.ID, string(o.Type), &name
		}
		switch r.Source {
		case ChoiceSourceStrategy:
			strategy := r.Strategy
			cr.Strategy = &strategy
		case ChoiceSourceHousehold:
			if c := v.Choice; c != nil {
				by, at := c.ChosenBy, c.ChosenAt.UTC()
				cr.ChosenBy, cr.ChosenAt = &by, &at
			}
		}
		resp.Choice = cr
	}
	if b := v.Batch; b != nil {
		br := &BatchStockResponse{PantryItemID: b.ItemID, Status: b.Status, ExpiresOn: optionalString(b.ExpiresOn)}
		if b.Remaining != nil {
			br.Remaining = amountResponse(b.Remaining.RatString(), b.Unit)
			pct := b.PercentRemaining
			br.PercentRemaining = &pct
		}
		resp.Batch = br
	}
	return resp
}

func (req OptionRequest) input() OptionInput {
	in := OptionInput{
		Type: req.Type, Name: req.Name, Notes: req.Notes, Steps: req.Steps, ShelfLifeDays: req.ShelfLifeDays,
		BasedOnOptionID: req.BasedOnOptionID,
	}
	if req.Per != nil {
		in.Per = &Measure{Quantity: req.Per.Quantity, Unit: req.Per.Unit}
	}
	if req.Yield != nil {
		in.Yield = &Measure{Quantity: req.Yield.Quantity, Unit: req.Yield.Unit}
	}
	for _, c := range req.Ingredients {
		in.Ingredients = append(in.Ingredients, Component(c))
	}
	return in
}

// --- Handlers -----------------------------------------------------------------

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	all := false
	if v := r.URL.Query().Get("all"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			h.writeError(w, r, "", invalid("all must be true or false"))
			return
		}
		all = b
	}
	views, err := h.opts.Service.List(r.Context(), actor.HouseholdID, all)
	if err != nil {
		h.writeError(w, r, "list specialty ingredients failed", err)
		return
	}
	resp := SpecialtyListResponse{Items: make([]SpecialtyResponse, 0, len(views))}
	for _, v := range views {
		resp.Items = append(resp.Items, newSpecialtyResponse(v))
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func newSettingsResponse(set Settings) SpecialtySettingsResponse {
	opts := StrategyOptions()
	resp := SpecialtySettingsResponse{Strategy: set.Strategy, Options: make([]StrategyOptionResponse, 0, len(opts))}
	if set.UpdatedBy != "" {
		by := set.UpdatedBy
		resp.UpdatedBy = &by
	}
	if !set.UpdatedAt.IsZero() {
		at := set.UpdatedAt.UTC()
		resp.UpdatedAt = &at
	}
	for _, o := range opts {
		resp.Options = append(resp.Options, StrategyOptionResponse(o))
	}
	return resp
}

func (h *Handler) settings(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	set, err := h.opts.Service.Settings(r.Context(), actor.HouseholdID)
	if err != nil {
		h.writeError(w, r, "get specialty ingredient settings failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, newSettingsResponse(set))
}

func (h *Handler) setSettings(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	var req SetSpecialtySettingsRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	set, err := h.opts.Service.SetStrategy(r.Context(), actor, req.Strategy)
	if err != nil {
		h.writeError(w, r, "set specialty ingredient strategy failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, newSettingsResponse(set))
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	v, err := h.opts.Service.Get(r.Context(), actor.HouseholdID, chi.URLParam(r, "specialtyId"))
	if err != nil {
		h.writeError(w, r, "get specialty ingredient failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, newSpecialtyResponse(v))
}

func (h *Handler) setChoice(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	var req SetChoiceRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	v, err := h.opts.Service.SetChoice(r.Context(), actor, chi.URLParam(r, "specialtyId"), req.OptionID)
	if err != nil {
		h.writeError(w, r, "set specialty ingredient choice failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, newSpecialtyResponse(v))
}

func (h *Handler) clearChoice(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	if err := h.opts.Service.ClearChoice(r.Context(), actor, chi.URLParam(r, "specialtyId")); err != nil {
		h.writeError(w, r, "clear specialty ingredient choice failed", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) applyDefaults(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	res, err := h.opts.Service.ApplyDefaults(r.Context(), actor)
	if err != nil {
		h.writeError(w, r, "apply specialty ingredient defaults failed", err)
		return
	}
	resp := DefaultsResponse{Items: make([]SpecialtyResponse, 0, len(res.Chosen)), Skipped: res.Skipped}
	for _, v := range res.Chosen {
		resp.Items = append(resp.Items, newSpecialtyResponse(v))
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) createOption(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	var req OptionRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	o, err := h.opts.Service.CreateOption(r.Context(), actor, chi.URLParam(r, "specialtyId"), req.input())
	if err != nil {
		h.writeError(w, r, "create specialty ingredient option failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, newOptionResponse(o, ""))
}

func (h *Handler) updateOption(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	var req OptionRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	o, err := h.opts.Service.UpdateOption(r.Context(), actor, chi.URLParam(r, "specialtyId"), chi.URLParam(r, "optionId"), req.input())
	if err != nil {
		h.writeError(w, r, "update specialty ingredient option failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, newOptionResponse(o, ""))
}

func (h *Handler) deleteOption(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	if err := h.opts.Service.DeleteOption(r.Context(), actor, chi.URLParam(r, "specialtyId"), chi.URLParam(r, "optionId")); err != nil {
		h.writeError(w, r, "delete specialty ingredient option failed", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) recordBatch(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	var req RecordBatchRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	in := BatchInput{OptionID: req.OptionID, ClientPurchaseID: req.ClientPurchaseID}
	if req.Batches != nil {
		if *req.Batches == 0 {
			h.writeError(w, r, "", invalid("batches must be between 1 and %d", MaxBatches))
			return
		}
		in.Batches = *req.Batches
	}
	res, err := h.opts.Service.RecordBatch(r.Context(), actor, chi.URLParam(r, "specialtyId"), in)
	if err != nil {
		h.writeError(w, r, "record specialty ingredient batch failed", err)
		return
	}
	resp := RecordBatchResponse{Purchase: pantry.NewPurchaseResponse(res.Purchase), Option: newOptionResponse(res.Option, "")}
	if h.opts.Pantry != nil {
		resp.Item = h.opts.Pantry.ItemResponse(r.Context(), res.Item)
	}
	status := http.StatusOK
	if res.Created {
		status = http.StatusCreated
	}
	httpx.WriteJSON(w, status, resp)
}

// writeError maps service errors to responses. msg is logged for unexpected
// errors only.
func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	var validation *ValidationError
	var pantryValidation *pantry.ValidationError
	switch {
	case errors.As(err, &validation):
		httpx.WriteError(w, r, http.StatusBadRequest, "validation_failed", validation.Message)
	case errors.As(err, &pantryValidation):
		httpx.WriteError(w, r, http.StatusBadRequest, "validation_failed", pantryValidation.Message)
	case errors.Is(err, ErrNotFound), errors.Is(err, pantry.ErrNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", "specialty ingredient or option not found")
	case errors.Is(err, ErrForbidden), errors.Is(err, pantry.ErrForbidden):
		httpx.WriteError(w, r, http.StatusForbidden, "forbidden", "your role in this household does not allow this action")
	case errors.Is(err, pantry.ErrConflict):
		httpx.WriteError(w, r, http.StatusConflict, "conflict", "the pantry item changed at the same time; retry")
	default:
		h.logger.ErrorContext(r.Context(), msg, "error", err)
		httpx.WriteError(w, r, http.StatusInternalServerError, "internal", "internal server error")
	}
}
