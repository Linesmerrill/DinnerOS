package pantry

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/auth"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

// HandlerOptions configures the pantry HTTP handlers.
type HandlerOptions struct {
	Service    *Service
	Authorizer households.Authorizer
	Tokens     auth.AccessTokenValidator
	Logger     *slog.Logger
}

// Handler serves the pantry endpoints.
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
		const base = "/households/{householdId}/pantry"
		r.With(view).Get(base, h.list)
		r.With(edit).Post(base, h.add)
		r.With(edit).Post(base+"/bulk", h.bulk)
		r.With(edit).Post(base+"/staples/defaults", h.addDefaultStaples)
		r.With(edit).Post(base+"/freezer", h.freeze)
		r.With(edit).Post(base+"/purchases", h.recordPurchase)
		r.With(edit).Patch(base+"/purchases/{purchaseId}", h.setPurchasePrice)
		r.With(view).Get(base+"/settings", h.getSettings)
		r.With(edit).Put(base+"/settings", h.putSettings)
		r.With(view).Get(base+"/{itemId}/purchases", h.listPurchases)
		r.With(edit).Patch(base+"/{itemId}", h.update)
		r.With(edit).Delete(base+"/{itemId}", h.delete)
	})
}

// --- Wire types ---------------------------------------------------------------

// PantryItemResponse is a pantry item. Quantity is exact ("3/2");
// QuantityValue is the same amount as a number, for display. Quantity,
// QuantityValue, and Unit are null together when the household didn't record
// an amount. Estimate is null when the item has no usage cycle
// (docs/pantry-usage.md).
type PantryItemResponse struct {
	ID            string   `json:"id"`
	HouseholdID   string   `json:"householdId"`
	IngredientID  *string  `json:"ingredientId"`
	Key           string   `json:"key"`
	DisplayName   string   `json:"displayName"`
	Category      string   `json:"category"`
	Quantity      *string  `json:"quantity"`
	QuantityValue *float64 `json:"quantityValue"`
	Unit          *string  `json:"unit"`
	Status        Status   `json:"status"`
	IsStaple      bool     `json:"isStaple"`
	ExpiresOn     *string  `json:"expiresOn"`
	Note          string   `json:"note"`
	// Storage is pantry or freezer. Frozen is set only for a freezer item:
	// when it was sealed, into how many portions, and how long one portion
	// takes to thaw (docs/pantry-usage.md#the-freezer).
	Storage Storage         `json:"storage"`
	Frozen  *FrozenResponse `json:"frozen"`
	// StatusSource is person, or estimate when the usage estimate marked
	// the item low.
	StatusSource StatusSource `json:"statusSource"`
	// LowThresholdPercent is the item's own threshold, or null for the
	// household's.
	LowThresholdPercent *int              `json:"lowThresholdPercent"`
	UnitSize            *UnitSizeResponse `json:"unitSize"`
	Estimate            *EstimateResponse `json:"estimate"`
	UpdatedBy           string            `json:"updatedBy"`
	CreatedAt           time.Time         `json:"createdAt"`
	UpdatedAt           time.Time         `json:"updatedAt"`
}

// PantryListResponse is returned by GET .../pantry.
type PantryListResponse struct {
	Items []PantryItemResponse `json:"items"`
}

// AddPantryItemRequest is the body of POST .../pantry.
type AddPantryItemRequest struct {
	IngredientID string `json:"ingredientId"`
	Name         string `json:"name"`
	Category     string `json:"category"`
	Quantity     string `json:"quantity"`
	Unit         string `json:"unit"`
	Status       Status `json:"status"`
	IsStaple     *bool  `json:"isStaple"`
	ExpiresOn    string `json:"expiresOn"`
	Note         string `json:"note"`
}

// Nullable is a request field that distinguishes absent, null, and a value.
type Nullable[T any] struct {
	Set   bool
	Null  bool
	Value T
}

// UnmarshalJSON implements json.Unmarshaler. It is only called when the field
// is present, including when it is null.
func (n *Nullable[T]) UnmarshalJSON(b []byte) error {
	n.Set = true
	if string(b) == "null" {
		n.Null = true
		return nil
	}
	return json.Unmarshal(b, &n.Value)
}

// UpdatePantryItemRequest is the body of PATCH .../pantry/{itemId}. Absent
// fields are unchanged. quantity, expiresOn, and note may be null (or "") to
// clear them; clearing quantity also clears unit. lowThresholdPercent null
// returns the item to the household threshold.
type UpdatePantryItemRequest struct {
	DisplayName         Nullable[string] `json:"displayName"`
	Category            Nullable[string] `json:"category"`
	Quantity            Nullable[string] `json:"quantity"`
	Unit                Nullable[string] `json:"unit"`
	Status              Nullable[Status] `json:"status"`
	IsStaple            Nullable[bool]   `json:"isStaple"`
	ExpiresOn           Nullable[string] `json:"expiresOn"`
	Note                Nullable[string] `json:"note"`
	LowThresholdPercent Nullable[int]    `json:"lowThresholdPercent"`
}

// BulkStatusRequest is the body of POST .../pantry/bulk.
type BulkStatusRequest struct {
	Items []BulkStatusItem `json:"items"`
}

// BulkStatusItem sets one item's status.
type BulkStatusItem struct {
	ID     string `json:"id"`
	Status Status `json:"status"`
}

// BulkStatusResponse is returned by POST .../pantry/bulk. Missing lists
// requested IDs that aren't in the pantry.
type BulkStatusResponse struct {
	Items   []PantryItemResponse `json:"items"`
	Missing []string             `json:"missing"`
}

// DefaultStaplesResponse is returned by POST .../pantry/staples/defaults.
// Items are the staples added; Skipped counts defaults the pantry already had.
type DefaultStaplesResponse struct {
	Items   []PantryItemResponse `json:"items"`
	Skipped int                  `json:"skipped"`
}

func newItemResponse(item Item) PantryItemResponse {
	resp := PantryItemResponse{
		ID: item.ID, HouseholdID: item.HouseholdID, Key: item.Key, DisplayName: item.DisplayName, Category: item.Category,
		Status: item.Status, IsStaple: item.IsStaple, Note: item.Note, UpdatedBy: item.UpdatedBy,
		Storage:      item.Storage.Or(),
		StatusSource: item.StatusSource, UnitSize: unitSizeResponse(item.UnitSize),
		CreatedAt: item.CreatedAt.UTC(), UpdatedAt: item.UpdatedAt.UTC(),
	}
	if resp.StatusSource == "" {
		resp.StatusSource = StatusSourcePerson
	}
	if item.LowThresholdPercent != 0 {
		pct := item.LowThresholdPercent
		resp.LowThresholdPercent = &pct
	}
	if item.IngredientID != "" {
		id := item.IngredientID
		resp.IngredientID = &id
	}
	if item.Quantity != "" {
		exact, unit := item.Quantity, item.Unit
		resp.Quantity, resp.Unit = &exact, &unit
		if q, err := ingredients.ParseQuantity(exact); err == nil {
			v := q.Float64()
			resp.QuantityValue = &v
		}
	}
	if item.ExpiresOn != "" {
		date := item.ExpiresOn
		resp.ExpiresOn = &date
	}
	if item.Storage == StorageFreezer {
		resp.Frozen = newFrozenResponse(item)
	}
	return resp
}

// FrozenResponse describes a freezer item: when it went in, how it was
// portioned, and the thaw estimate for one portion.
type FrozenResponse struct {
	// FrozenOn is a calendar date, or null for an item frozen before the
	// date was recorded.
	FrozenOn *string `json:"frozenOn"`
	// Portions is at least 1.
	Portions int          `json:"portions"`
	Thaw     ThawResponse `json:"thaw"`
}

// ThawResponse is how long one portion takes to thaw in the fridge.
// Measured is false when the amount isn't a weight and Hours is the default.
type ThawResponse struct {
	Hours         int      `json:"hours"`
	Measured      bool     `json:"measured"`
	PortionOunces *float64 `json:"portionOunces"`
	Summary       string   `json:"summary"`
}

func newFrozenResponse(item Item) *FrozenResponse {
	e := ThawFor(item)
	resp := &FrozenResponse{Portions: e.Portions, Thaw: NewThawResponse(e)}
	if item.FrozenOn != "" {
		on := item.FrozenOn
		resp.FrozenOn = &on
	}
	return resp
}

// NewThawResponse renders a thaw estimate, for other modules' responses.
func NewThawResponse(e ThawEstimate) ThawResponse {
	resp := ThawResponse{Hours: e.Hours, Measured: e.Measured, Summary: e.Summary}
	if e.PortionOunces != nil {
		v, _ := e.PortionOunces.Float64()
		resp.PortionOunces = &v
	}
	return resp
}

// FreezeRequest is the body of POST .../pantry/freezer: what was sealed.
type FreezeRequest struct {
	IngredientID string               `json:"ingredientId"`
	Name         string               `json:"name"`
	Quantity     string               `json:"quantity"`
	Unit         string               `json:"unit"`
	Portions     int                  `json:"portions"`
	Note         string               `json:"note"`
	Source       *FreezeSourceRequest `json:"source"`
}

// FreezeSourceRequest names the handoff line a remainder came from, which
// makes freezing it idempotent.
type FreezeSourceRequest struct {
	Provider  string `json:"provider"`
	HandoffID string `json:"handoffId"`
	LineID    string `json:"lineId"`
}

// FreezeResponse is returned by POST .../pantry/freezer.
type FreezeResponse struct {
	Item PantryItemResponse `json:"item"`
	// AlreadyFrozen is true when this handoff line had been sealed before
	// and nothing changed.
	AlreadyFrozen bool         `json:"alreadyFrozen"`
	Thaw          ThawResponse `json:"thaw"`
}

func (req UpdatePantryItemRequest) input() (UpdateInput, error) {
	var in UpdateInput
	var err error
	if in.DisplayName, err = nonNull(req.DisplayName, "displayName"); err != nil {
		return UpdateInput{}, err
	}
	if in.Category, err = nonNull(req.Category, "category"); err != nil {
		return UpdateInput{}, err
	}
	if in.Status, err = nonNull(req.Status, "status"); err != nil {
		return UpdateInput{}, err
	}
	if in.IsStaple, err = nonNull(req.IsStaple, "isStaple"); err != nil {
		return UpdateInput{}, err
	}
	in.Quantity, in.Unit = clearable(req.Quantity), clearable(req.Unit)
	in.ExpiresOn, in.Note = clearable(req.ExpiresOn), clearable(req.Note)
	if req.LowThresholdPercent.Set {
		pct := req.LowThresholdPercent.Value // null is 0, which clears the override
		if !req.LowThresholdPercent.Null && pct == 0 {
			return UpdateInput{}, invalid("lowThresholdPercent must be between 1 and 100, or null")
		}
		in.LowThresholdPercent = &pct
	}
	if in == (UpdateInput{}) {
		return UpdateInput{}, invalid("the request must change at least one field")
	}
	return in, nil
}

func nonNull[T any](n Nullable[T], field string) (*T, error) {
	switch {
	case !n.Set:
		return nil, nil
	case n.Null:
		return nil, invalid("%s can't be null", field)
	}
	v := n.Value
	return &v, nil
}

// clearable maps null to "", which clears the field.
func clearable(n Nullable[string]) *string {
	if !n.Set {
		return nil
	}
	v := n.Value
	return &v
}

// --- Handlers -----------------------------------------------------------------

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	q, err := parseListQuery(r.URL.Query())
	if err != nil {
		h.writeError(w, r, "", err)
		return
	}
	items, err := h.opts.Service.List(r.Context(), actor.HouseholdID, q)
	if err != nil {
		h.writeError(w, r, "list pantry items failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, PantryListResponse{Items: h.itemResponses(r, actor.HouseholdID, items)})
}

func parseListQuery(v url.Values) (ListQuery, error) {
	q := ListQuery{
		Status: v.Get("status"), Category: v.Get("category"), Search: v.Get("q"), Storage: v.Get("storage"),
	}
	if s := v.Get("staple"); s != "" {
		b, err := strconv.ParseBool(s)
		if err != nil {
			return ListQuery{}, invalid("staple must be true or false")
		}
		q.Staple = &b
	}
	return q, nil
}

func (h *Handler) add(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	var req AddPantryItemRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	item, created, err := h.opts.Service.Add(r.Context(), actor, AddInput(req))
	if err != nil {
		h.writeError(w, r, "add pantry item failed", err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	httpx.WriteJSON(w, status, h.itemResponse(r, item))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	var req UpdatePantryItemRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	in, err := req.input()
	if err != nil {
		h.writeError(w, r, "", err)
		return
	}
	item, err := h.opts.Service.Update(r.Context(), actor, chi.URLParam(r, "itemId"), in)
	if err != nil {
		h.writeError(w, r, "update pantry item failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, h.itemResponse(r, item))
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	if err := h.opts.Service.Delete(r.Context(), actor, chi.URLParam(r, "itemId")); err != nil {
		h.writeError(w, r, "delete pantry item failed", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) bulk(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	var req BulkStatusRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	updates := make([]StatusUpdate, 0, len(req.Items))
	for _, it := range req.Items {
		updates = append(updates, StatusUpdate{ItemID: it.ID, Status: it.Status})
	}
	res, err := h.opts.Service.SetStatuses(r.Context(), actor, updates)
	if err != nil {
		h.writeError(w, r, "set pantry statuses failed", err)
		return
	}
	missing := res.Missing
	if missing == nil {
		missing = []string{}
	}
	httpx.WriteJSON(w, http.StatusOK, BulkStatusResponse{Items: h.itemResponses(r, actor.HouseholdID, res.Items), Missing: missing})
}

func (h *Handler) addDefaultStaples(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	res, err := h.opts.Service.AddDefaultStaples(r.Context(), actor)
	if err != nil {
		h.writeError(w, r, "add default staples failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, DefaultStaplesResponse{Items: h.itemResponses(r, actor.HouseholdID, res.Added), Skipped: res.Skipped})
}

// writeError maps service errors to responses. msg is logged for unexpected
// errors only.
func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	var validation *ValidationError
	switch {
	case errors.As(err, &validation):
		httpx.WriteError(w, r, http.StatusBadRequest, "validation_failed", validation.Message)
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", "pantry item not found")
	case errors.Is(err, ErrForbidden):
		httpx.WriteError(w, r, http.StatusForbidden, "forbidden", "your role in this household does not allow this action")
	case errors.Is(err, ErrConflict):
		httpx.WriteError(w, r, http.StatusConflict, "conflict", "the pantry item changed at the same time; retry")
	default:
		h.logger.ErrorContext(r.Context(), msg, "error", err)
		httpx.WriteError(w, r, http.StatusInternalServerError, "internal", "internal server error")
	}
}

func (h *Handler) freeze(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	var req FreezeRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	in := FreezeInput{
		IngredientID: req.IngredientID, Name: req.Name, Quantity: req.Quantity, Unit: req.Unit,
		Portions: req.Portions, Note: req.Note,
	}
	if src := req.Source; src != nil {
		in.Source = &FreezeSource{Provider: src.Provider, HandoffID: src.HandoffID, LineID: src.LineID}
	}
	res, err := h.opts.Service.Freeze(r.Context(), actor, in)
	if err != nil {
		h.writeError(w, r, "freeze pantry item failed", err)
		return
	}
	status := http.StatusOK
	if res.Created {
		status = http.StatusCreated
	}
	httpx.WriteJSON(w, status, FreezeResponse{
		Item:          h.itemResponse(r, res.Item),
		AlreadyFrozen: res.AlreadyFrozen,
		Thaw:          NewThawResponse(res.Thaw),
	})
}
