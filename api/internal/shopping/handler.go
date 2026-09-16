package shopping

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/auth"
	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
	"github.com/Linesmerrill/DinnerOS/api/internal/pantry"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
	"github.com/Linesmerrill/DinnerOS/api/internal/providers"
)

// PantryRenderer renders pantry items for confirm responses.
// *pantry.Service implements it.
type PantryRenderer interface {
	ItemResponse(ctx context.Context, item pantry.Item) pantry.PantryItemResponse
}

// HouseholdLister lists the households a user belongs to. *households.Service
// implements it. The store catalog uses it to mark what the caller's own
// household already asked for; it is optional, and without it every
// requestedByHousehold is false.
type HouseholdLister interface {
	ListForUser(ctx context.Context, userID string) ([]households.UserHousehold, error)
}

// HandlerOptions configures the shopping HTTP handlers.
type HandlerOptions struct {
	Service    *Service
	Pantry     PantryRenderer
	Authorizer households.Authorizer
	// Households is optional; see HouseholdLister.
	Households HouseholdLister
	Tokens     auth.AccessTokenValidator
	Logger     *slog.Logger
}

// Handler serves the shopping endpoints.
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
		edit := households.RequirePermission(h.opts.Authorizer, households.PermShoppingEdit, h.logger)
		pantryEdit := households.RequirePermission(h.opts.Authorizer, households.PermPantryEdit, h.logger)
		const base = "/households/{householdId}/shopping"
		r.Get("/shopping/providers", h.listProviders)
		r.Get("/shopping/catalog", h.storeCatalog)
		r.With(view).Get(base+"/settings", h.getSettings)
		r.With(edit).Put(base+"/settings", h.putSettings)
		r.With(view).Get(base+"/handoffs", h.listHandoffs)
		r.With(view).Get(base+"/handoffs/{handoffId}", h.getHandoff)
		r.With(pantryEdit).Post(base+"/handoffs/{handoffId}/confirm", h.confirm)
		r.With(view).Get(base+"/requests", h.listStoreRequests)
		r.With(view).Post(base+"/requests", h.requestStore)
		r.With(view).Delete(base+"/requests/{requestId}", h.deleteStoreRequest)
		r.With(view).Get(base+"/weeks/{week}/order", h.getOrderReminder)
		r.With(edit).Put(base+"/weeks/{week}/order", h.setWeekOrdered)
		r.With(view).Get(base+"/{provider}/preferences", h.listPreferences)
		r.With(view).Get(base+"/{provider}/preferences/{ingredientKey}", h.getPreference)
		r.With(edit).Put(base+"/{provider}/preferences/{ingredientKey}", h.putPreference)
		r.With(edit).Delete(base+"/{provider}/preferences/{ingredientKey}", h.deletePreference)
		const plan = "/households/{householdId}/plans/{week}/shopping/{provider}"
		r.With(view).Post(plan+"/match", h.match)
		r.With(edit).Post(plan+"/handoffs", h.createHandoff)
		r.With(edit).Post(plan+"/handoffs/start-over", h.startOver)
		r.With(edit).Post(plan+"/handoffs/send-again", h.sendAgain)
	})
}

// --- Wire types ---------------------------------------------------------------

// CapabilitiesResponse says what a provider can do.
type CapabilitiesResponse struct {
	Handoff          providers.HandoffKind `json:"handoff"`
	PasteProductLink bool                  `json:"pasteProductLink"`
	StoreID          bool                  `json:"storeId"`
	ProductSearch    bool                  `json:"productSearch"`
	ProductLookup    bool                  `json:"productLookup"`
	StoreFinder      bool                  `json:"storeFinder"`
	CartWrite        bool                  `json:"cartWrite"`
	OrderImport      bool                  `json:"orderImport"`
}

// ProviderResponse is an enabled shopping provider.
type ProviderResponse struct {
	Key  providers.Key `json:"key"`
	Name string        `json:"name"`
	// AffiliateTracked is true when handoff links carry affiliate tracking,
	// which the app must disclose next to the button.
	AffiliateTracked bool                 `json:"affiliateTracked"`
	Capabilities     CapabilitiesResponse `json:"capabilities"`
}

// ProviderListResponse is returned by GET /shopping/providers.
type ProviderListResponse struct {
	Items []ProviderResponse `json:"items"`
}

// SettingsResponse is the household's shopping settings.
type SettingsResponse struct {
	Provider  *providers.Key `json:"provider"`
	StoreID   *string        `json:"storeId"`
	UpdatedBy *string        `json:"updatedBy"`
	UpdatedAt *time.Time     `json:"updatedAt"`
}

// UpdateSettingsRequest is the body of PUT .../shopping/settings.
type UpdateSettingsRequest struct {
	Provider string  `json:"provider"`
	StoreID  *string `json:"storeId"`
}

// PackageSizeRequest is a package size in a request.
type PackageSizeRequest struct {
	Quantity string `json:"quantity"`
	Unit     string `json:"unit"`
}

// PackageSizeResponse is how much one package holds.
type PackageSizeResponse struct {
	Quantity      string  `json:"quantity"`
	QuantityValue float64 `json:"quantityValue"`
	Unit          string  `json:"unit"`
	Text          string  `json:"text"`
}

// PreferenceRequest is the body of PUT .../preferences/{ingredientKey}.
type PreferenceRequest struct {
	ProductURL string `json:"productUrl"`
	ProductID  string `json:"productId"`
	// DisplayName is optional when productUrl's slug names the product.
	DisplayName    string              `json:"displayName"`
	PackageSize    *PackageSizeRequest `json:"packageSize"`
	IngredientName string              `json:"ingredientName"`
	// Coverage overrides how a package covers a week's need; omitted or ""
	// follows the ingredient's grocery category.
	Coverage providers.Coverage `json:"coverage"`
}

// PreferenceResponse is a saved product for an ingredient.
type PreferenceResponse struct {
	ID             string               `json:"id"`
	Provider       providers.Key        `json:"provider"`
	IngredientKey  string               `json:"ingredientKey"`
	IngredientID   *string              `json:"ingredientId"`
	IngredientName string               `json:"ingredientName"`
	ProductID      string               `json:"productId"`
	ProductURL     string               `json:"productUrl"`
	DisplayName    string               `json:"displayName"`
	PackageSize    *PackageSizeResponse `json:"packageSize"`
	// Coverage is the household's override, or "" to follow the category.
	Coverage  providers.Coverage `json:"coverage"`
	CreatedBy string             `json:"createdBy"`
	CreatedAt time.Time          `json:"createdAt"`
	UpdatedBy string             `json:"updatedBy"`
	UpdatedAt time.Time          `json:"updatedAt"`
}

// PreferenceListResponse is returned by GET .../preferences.
type PreferenceListResponse struct {
	Items []PreferenceResponse `json:"items"`
}

// LineSelectionRequest selects a grocery line.
type LineSelectionRequest struct {
	IngredientKey string `json:"ingredientKey"`
	Packages      int    `json:"packages"`
}

// MatchRequest is the body of POST .../match and .../handoffs.
type MatchRequest struct {
	Lines          []LineSelectionRequest `json:"lines"`
	CheckedOffKeys []string               `json:"checkedOffKeys"`
	ExcludeKeys    []string               `json:"excludeKeys"`
}

// AmountResponse is a grocery amount.
type AmountResponse struct {
	Quantity      string  `json:"quantity"`
	QuantityValue float64 `json:"quantityValue"`
	Unit          string  `json:"unit"`
	Text          string  `json:"text"`
}

// ProductResponse is the product a line hands off.
type ProductResponse struct {
	ProductID   string               `json:"productId"`
	DisplayName string               `json:"displayName"`
	ProductURL  string               `json:"productUrl"`
	PackageSize *PackageSizeResponse `json:"packageSize"`
}

// ConfirmationResponse is a handoff line's confirmation state.
type ConfirmationResponse struct {
	Status      LineStatus `json:"status"`
	Packages    *int       `json:"packages"`
	PurchaseID  *string    `json:"purchaseId"`
	ConfirmedBy *string    `json:"confirmedBy"`
	ConfirmedAt *time.Time `json:"confirmedAt"`
	SkippedBy   *string    `json:"skippedBy"`
	SkippedAt   *time.Time `json:"skippedAt"`
}

// HandoffLineResponse is a grocery line matched to a product.
type HandoffLineResponse struct {
	ID            string           `json:"id"`
	IngredientKey string           `json:"ingredientKey"`
	IngredientID  *string          `json:"ingredientId"`
	Name          string           `json:"name"`
	Category      string           `json:"category"`
	Amounts       []AmountResponse `json:"amounts"`
	QuantityText  string           `json:"quantityText"`
	Unquantified  bool             `json:"unquantified"`
	GroceryStatus grocery.Status   `json:"groceryStatus"`
	Product       ProductResponse  `json:"product"`
	// ComputedPackages is the package math; Packages is what the link asks
	// for.
	ComputedPackages   int               `json:"computedPackages"`
	Packages           int               `json:"packages"`
	PackagesOverridden bool              `json:"packagesOverridden"`
	CheckAmount        bool              `json:"checkAmount"`
	Reason             *providers.Reason `json:"reason"`
	ReasonText         *string           `json:"reasonText"`
	CoverageText       string            `json:"coverageText"`
	// Coverage is the rule the count was computed under.
	Coverage providers.Coverage `json:"coverage"`
	// CoversWeek is true when the count assumes one package covers the
	// week, because the need couldn't be measured against the package.
	CoversWeek bool `json:"coversWeek"`
	// SearchTerms is what to search for to find this product again.
	SearchTerms SearchTermsResponse `json:"searchTerms"`
	// Confirmation is null on a match, which isn't stored.
	Confirmation *ConfirmationResponse `json:"confirmation"`
	// Cart is set on a match when the week has a current handoff, and null
	// otherwise (and on stored handoffs, whose packages are what was sent).
	Cart *LineCartResponse `json:"cart"`
}

// LineCartResponse is what a match line already has in the cart.
type LineCartResponse struct {
	SentPackages   int `json:"sentPackages"`
	AddPackages    int `json:"addPackages"`
	RemovePackages int `json:"removePackages"`
}

// SentLineResponse is a product in the cart that isn't one of the match's
// lines.
type SentLineResponse struct {
	IngredientKey  string          `json:"ingredientKey"`
	IngredientID   *string         `json:"ingredientId"`
	Name           string          `json:"name"`
	Category       string          `json:"category"`
	Product        ProductResponse `json:"product"`
	SentPackages   int             `json:"sentPackages"`
	RemovePackages int             `json:"removePackages"`
	Reason         SentReason      `json:"reason"`
	Text           string          `json:"text"`
}

// CartStateResponse is what the week's current handoff put in the cart.
type CartStateResponse struct {
	HandoffID string             `json:"handoffId"`
	SentAt    time.Time          `json:"sentAt"`
	Other     []SentLineResponse `json:"other"`
}

// SendAgainRequest is the body of POST .../handoffs/send-again.
type SendAgainRequest struct {
	IngredientKey string `json:"ingredientKey"`
}

// SearchTermsResponse is the search DinnerOS suggests for a grocery line.
// DinnerOS has no product search API (docs/shopping-providers.md), so these
// are words for the member to search with, not results.
type SearchTermsResponse struct {
	// Query is what to search for.
	Query string `json:"query"`
	// Qualifiers are the words added ahead of the ingredient name.
	Qualifiers []string `json:"qualifiers"`
	// Avoid are wrong-form products to skip. DinnerOS cannot filter them
	// out itself without a search API, so the app shows them as a hint.
	Avoid []string `json:"avoid"`
	// Why explains the bias in one sentence, or "" when none was applied.
	Why string `json:"why"`
}

// ExcludedResponse is a grocery line left out of the cart links.
type ExcludedResponse struct {
	IngredientKey string           `json:"ingredientKey"`
	IngredientID  *string          `json:"ingredientId"`
	Name          string           `json:"name"`
	Category      string           `json:"category"`
	Amounts       []AmountResponse `json:"amounts"`
	QuantityText  string           `json:"quantityText"`
	Unquantified  bool             `json:"unquantified"`
	GroceryStatus *grocery.Status  `json:"groceryStatus"`
	Reason        ExclusionReason  `json:"reason"`
	Text          string           `json:"text"`
	// SearchTerms is what to search for when choosing a product for this
	// line, which is what the "no_product" rows need.
	SearchTerms SearchTermsResponse `json:"searchTerms"`
}

// CartLinkResponse is one handoff URL.
type CartLinkResponse struct {
	URL       string   `json:"url"`
	LineIDs   []string `json:"lineIds"`
	ItemCount int      `json:"itemCount"`
}

// ProposalResponse is returned by POST .../match.
type ProposalResponse struct {
	Provider         providers.Key         `json:"provider"`
	Week             string                `json:"week"`
	StoreID          *string               `json:"storeId"`
	Lines            []HandoffLineResponse `json:"lines"`
	Excluded         []ExcludedResponse    `json:"excluded"`
	CartLinks        []CartLinkResponse    `json:"cartLinks"`
	AffiliateTracked bool                  `json:"affiliateTracked"`
	// Cart is set on a match when the week has a current handoff; null
	// otherwise, and always on a stored handoff.
	Cart *CartStateResponse `json:"cart"`
}

// HandoffResponse is a stored handoff.
type HandoffResponse struct {
	ID string `json:"id"`
	ProposalResponse
	Status HandoffStatus `json:"status"`
	// Active is true while it is the week's current handoff.
	Active       bool         `json:"active"`
	ClosedAt     *time.Time   `json:"closedAt"`
	ClosedReason *CloseReason `json:"closedReason"`
	CreatedBy    string       `json:"createdBy"`
	CreatedAt    time.Time    `json:"createdAt"`
	UpdatedAt    time.Time    `json:"updatedAt"`
}

// HandoffListResponse is returned by GET .../shopping/handoffs.
type HandoffListResponse struct {
	Items []HandoffResponse `json:"items"`
}

// ConfirmLineRequest is one ordered line.
type ConfirmLineRequest struct {
	LineID   string `json:"lineId"`
	Packages int    `json:"packages"`
}

// ConfirmRequest is the body of POST .../handoffs/{handoffId}/confirm.
type ConfirmRequest struct {
	All      bool                 `json:"all"`
	Lines    []ConfirmLineRequest `json:"lines"`
	SkipRest bool                 `json:"skipRest"`
}

// ConfirmedPurchaseResponse is the pantry purchase for one line.
type ConfirmedPurchaseResponse struct {
	LineID        string                    `json:"lineId"`
	IngredientKey string                    `json:"ingredientKey"`
	Created       bool                      `json:"created"`
	Purchase      pantry.PurchaseResponse   `json:"purchase"`
	Item          pantry.PantryItemResponse `json:"item"`
}

// ConfirmResponse is returned by POST .../confirm.
type ConfirmResponse struct {
	Handoff   HandoffResponse             `json:"handoff"`
	Purchases []ConfirmedPurchaseResponse `json:"purchases"`
}

// StoreCatalogItemResponse is one store in the catalog, with the demand
// behind it.
type StoreCatalogItemResponse struct {
	Key  string    `json:"key"`
	Name string    `json:"name"`
	Kind StoreKind `json:"kind"`
	// Status is what docs/shopping-providers.md concluded, not live data.
	Status StoreStatus `json:"status"`
	// Aliases are the other spellings the search matches.
	Aliases []string `json:"aliases"`
	// Note summarizes the research, or is null when there is none.
	Note *string `json:"note"`
	// RequestedByHousehold is whether the caller's household already asked.
	RequestedByHousehold bool `json:"requestedByHousehold"`
	// Requests counts the households that asked, across all of DinnerOS.
	Requests int `json:"requests"`
}

// StoreCatalogResponse is returned by GET /shopping/catalog.
type StoreCatalogResponse struct {
	Items []StoreCatalogItemResponse `json:"items"`
}

// StoreRequestRequest is the body of POST .../shopping/requests. Send key for
// a catalog store, or name for one the catalog doesn't list.
type StoreRequestRequest struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	Note string `json:"note"`
}

// StoreRequestResponse is one household's request for a store.
type StoreRequestResponse struct {
	ID     string      `json:"id"`
	Key    string      `json:"key"`
	Name   string      `json:"name"`
	Status StoreStatus `json:"status"`
	Note   *string     `json:"note"`
	// RequestedBy and RequestedAt are the first request for this store.
	RequestedBy string    `json:"requestedBy"`
	RequestedAt time.Time `json:"requestedAt"`
}

// StoreRequestEnvelope is returned by POST .../shopping/requests.
type StoreRequestEnvelope struct {
	Request StoreRequestResponse `json:"request"`
}

// StoreRequestListResponse is returned by GET .../shopping/requests.
type StoreRequestListResponse struct {
	Items []StoreRequestResponse `json:"items"`
}

func storeCatalogItemResponse(i CatalogItem) StoreCatalogItemResponse {
	aliases := i.Aliases
	if aliases == nil {
		aliases = []string{}
	}
	return StoreCatalogItemResponse{
		Key: i.Key, Name: i.Name, Kind: i.Kind, Status: i.Status, Aliases: aliases, Note: optionalString(i.Note),
		RequestedByHousehold: i.RequestedByHousehold, Requests: i.Requests,
	}
}

func storeRequestResponse(r StoreRequest) StoreRequestResponse {
	return StoreRequestResponse{
		ID: r.ID, Key: r.Key, Name: r.Name, Status: r.Status(), Note: optionalString(r.Note),
		RequestedBy: r.RequestedBy, RequestedAt: r.RequestedAt,
	}
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
	return &t
}

func packageSizeResponse(s *PackageSize) *PackageSizeResponse {
	a := amountFromSize(s)
	if a == nil {
		return nil
	}
	return &PackageSizeResponse{Quantity: s.Quantity, QuantityValue: a.Quantity.Float64(), Unit: s.Unit, Text: providers.AmountText(*a)}
}

func (h *Handler) providerURL(key providers.Key, productID string) string {
	if p, ok := h.opts.Service.providers.Get(key); ok {
		return p.ProductURL(productID)
	}
	return ""
}

func (h *Handler) providerName(key providers.Key) string {
	if p, ok := h.opts.Service.providers.Get(key); ok {
		return p.Name()
	}
	return string(key)
}

func (h *Handler) preferenceResponse(p Preference) PreferenceResponse {
	return PreferenceResponse{
		ID: p.ID, Provider: p.Provider, IngredientKey: p.IngredientKey, IngredientID: optionalString(catalogID(p.IngredientKey)),
		IngredientName: p.IngredientName, ProductID: p.ProductID, ProductURL: h.providerURL(p.Provider, p.ProductID),
		DisplayName: p.DisplayName, PackageSize: packageSizeResponse(p.PackageSize), Coverage: p.Coverage,
		CreatedBy: p.CreatedBy, CreatedAt: p.CreatedAt, UpdatedBy: p.UpdatedBy, UpdatedAt: p.UpdatedAt,
	}
}

// amounts renders amounts as the grocery list does: "1 ½ cups", "2 cloves",
// "3" for counts.
func amounts(list []Amount) ([]AmountResponse, string) {
	out := make([]AmountResponse, 0, len(list))
	texts := make([]string, 0, len(list))
	for _, a := range list {
		q, err1 := ingredients.ParseQuantity(a.Quantity)
		u, err2 := ingredients.LookupUnit(a.Unit)
		if err1 != nil || err2 != nil {
			continue
		}
		text := q.Format()
		if label := u.Label(q); label != "" {
			text += " " + label
		}
		texts = append(texts, text)
		out = append(out, AmountResponse{Quantity: a.Quantity, QuantityValue: q.Float64(), Unit: a.Unit, Text: text})
	}
	return out, strings.Join(texts, " + ")
}

// searchTermsResponse renders the search suggested for a line. The slices
// are never null, so clients can render them without a nil check.
func searchTermsResponse(name, category string) SearchTermsResponse {
	t := SearchTermsFor(name, category)
	return SearchTermsResponse{
		Query:      t.Query,
		Qualifiers: append([]string{}, t.Qualifiers...),
		Avoid:      append([]string{}, t.Avoid...),
		Why:        t.Why,
	}
}

func (h *Handler) lineResponse(provider providers.Key, l HandoffLine, stored bool) HandoffLineResponse {
	resp := HandoffLineResponse{
		ID: l.ID, IngredientKey: l.IngredientKey, IngredientID: optionalString(l.IngredientID()), Name: l.Name, Category: l.Category,
		Unquantified: l.Unquantified, GroceryStatus: l.GroceryStatus,
		Product: ProductResponse{
			ProductID: l.ProductID, DisplayName: l.ProductName, ProductURL: h.providerURL(provider, l.ProductID), PackageSize: packageSizeResponse(l.PackageSize),
		},
		ComputedPackages: l.ComputedPackages, Packages: l.Packages, PackagesOverridden: l.Packages != l.ComputedPackages,
	}
	resp.Amounts, resp.QuantityText = amounts(l.Amounts)
	count, size := l.PackageCount(), amountFromSize(l.PackageSize)
	count.Packages = l.Packages
	resp.CoverageText = providers.CoverageText(count, size)
	resp.Coverage, resp.CoversWeek = l.Coverage, count.CoversWeek
	resp.SearchTerms = searchTermsResponse(l.Name, l.Category)
	if l.Reason != "" {
		reason := l.Reason
		count.Reason = reason
		resp.CheckAmount, resp.Reason, resp.ReasonText = true, &reason, optionalString(providers.ReasonText(count, size))
	}
	if stored {
		c := &ConfirmationResponse{Status: l.Status, PurchaseID: optionalString(l.PurchaseID), ConfirmedBy: optionalString(l.ConfirmedBy),
			ConfirmedAt: optionalTime(l.ConfirmedAt), SkippedBy: optionalString(l.SkippedBy), SkippedAt: optionalTime(l.SkippedAt)}
		if l.Status == LineConfirmed {
			packages := l.ConfirmedPackages
			c.Packages = &packages
		}
		resp.Confirmation = c
	}
	if l.Cart != nil {
		resp.Cart = &LineCartResponse{SentPackages: l.Cart.SentPackages, AddPackages: l.Cart.AddPackages, RemovePackages: l.Cart.RemovePackages}
	}
	return resp
}

func (h *Handler) sentText(provider providers.Key, l SentLine) string {
	name := h.providerName(provider)
	switch {
	case l.RemovePackages > 0 && l.Reason == SentProductChanged:
		return fmt.Sprintf("Replaced by another product; remove %d in the %s app", l.RemovePackages, name)
	case l.RemovePackages > 0:
		return fmt.Sprintf("No longer on the list; remove %d in the %s app", l.RemovePackages, name)
	}
	return fmt.Sprintf("In %s cart · %d", name, l.SentPackages)
}

func (h *Handler) exclusionText(provider providers.Key, reason ExclusionReason) string {
	switch reason {
	case ExcludedInPantry:
		return "In pantry"
	case ExcludedPantryHint:
		return "Probably have it"
	case ExcludedHouseMade:
		return "House-made batch in pantry"
	case ExcludedCheckedOff:
		return "Already checked off"
	case ExcludedByMember:
		return "Left out"
	case ExcludedNotSelected:
		return "Not selected"
	case ExcludedNoProduct:
		return "Choose a " + h.providerName(provider) + " product"
	case ExcludedNotOnList:
		return "No longer on this week's list"
	}
	return ""
}

func (h *Handler) proposalResponse(p Proposal, stored bool) ProposalResponse {
	resp := ProposalResponse{
		Provider: p.Provider, Week: p.Week, StoreID: optionalString(p.StoreID), AffiliateTracked: p.AffiliateTracked,
		Lines:     make([]HandoffLineResponse, 0, len(p.Lines)),
		Excluded:  make([]ExcludedResponse, 0, len(p.Excluded)),
		CartLinks: make([]CartLinkResponse, 0, len(p.Links)),
	}
	for _, l := range p.Lines {
		resp.Lines = append(resp.Lines, h.lineResponse(p.Provider, l, stored))
	}
	for _, e := range p.Excluded {
		er := ExcludedResponse{
			IngredientKey: e.IngredientKey, IngredientID: optionalString(e.IngredientID()), Name: e.Name, Category: e.Category,
			Unquantified: e.Unquantified, Reason: e.Reason, Text: h.exclusionText(p.Provider, e.Reason),
			SearchTerms: searchTermsResponse(e.Name, e.Category),
		}
		er.Amounts, er.QuantityText = amounts(e.Amounts)
		if e.GroceryStatus != "" {
			status := e.GroceryStatus
			er.GroceryStatus = &status
		}
		resp.Excluded = append(resp.Excluded, er)
	}
	for _, l := range p.Links {
		resp.CartLinks = append(resp.CartLinks, CartLinkResponse{URL: l.URL, LineIDs: append([]string{}, l.LineIDs...), ItemCount: l.ItemCount})
	}
	if p.Cart != nil {
		cart := &CartStateResponse{HandoffID: p.Cart.HandoffID, SentAt: p.Cart.SentAt, Other: make([]SentLineResponse, 0, len(p.Cart.Other))}
		for _, l := range p.Cart.Other {
			cart.Other = append(cart.Other, SentLineResponse{
				IngredientKey: l.IngredientKey, IngredientID: optionalString(l.IngredientID()), Name: l.Name, Category: l.Category,
				Product: ProductResponse{
					ProductID: l.ProductID, DisplayName: l.ProductName, ProductURL: h.providerURL(p.Provider, l.ProductID),
					PackageSize: packageSizeResponse(l.PackageSize),
				},
				SentPackages: l.SentPackages, RemovePackages: l.RemovePackages, Reason: l.Reason, Text: h.sentText(p.Provider, l),
			})
		}
		resp.Cart = cart
	}
	return resp
}

func (h *Handler) handoffResponse(ho Handoff) HandoffResponse {
	resp := HandoffResponse{
		ID: ho.ID, ProposalResponse: h.proposalResponse(ho.Proposal, true), Status: ho.Status(),
		Active: ho.Active, ClosedAt: optionalTime(ho.ClosedAt),
		CreatedBy: ho.CreatedBy, CreatedAt: ho.CreatedAt, UpdatedAt: ho.UpdatedAt,
	}
	if ho.ClosedReason != "" {
		reason := ho.ClosedReason
		resp.ClosedReason = &reason
	}
	return resp
}

func (req MatchRequest) input() MatchInput {
	in := MatchInput{CheckedOffKeys: req.CheckedOffKeys, ExcludeKeys: req.ExcludeKeys}
	if req.Lines != nil {
		in.Lines = make([]LineSelection, 0, len(req.Lines))
		for _, l := range req.Lines {
			in.Lines = append(in.Lines, LineSelection(l))
		}
	}
	return in
}

// --- Handlers -----------------------------------------------------------------

func (h *Handler) listProviders(w http.ResponseWriter, _ *http.Request) {
	resp := ProviderListResponse{Items: []ProviderResponse{}}
	for _, p := range h.opts.Service.Providers() {
		c := providers.CapabilitiesOf(p)
		resp.Items = append(resp.Items, ProviderResponse{
			Key: p.Key(), Name: p.Name(), AffiliateTracked: p.AffiliateTracked(),
			Capabilities: CapabilitiesResponse(c),
		})
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func settingsResponse(s Settings) SettingsResponse {
	resp := SettingsResponse{StoreID: optionalString(s.StoreID), UpdatedBy: optionalString(s.UpdatedBy), UpdatedAt: optionalTime(s.UpdatedAt)}
	if s.Provider != "" {
		p := s.Provider
		resp.Provider = &p
	}
	return resp
}

func (h *Handler) getSettings(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	s, err := h.opts.Service.Settings(r.Context(), actor.HouseholdID)
	if err != nil {
		h.writeError(w, r, "get shopping settings failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, settingsResponse(s))
}

func (h *Handler) putSettings(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	var req UpdateSettingsRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	storeID := ""
	if req.StoreID != nil {
		if strings.TrimSpace(*req.StoreID) == "" {
			h.writeError(w, r, "", invalid("storeId must be a store number, or null for none"))
			return
		}
		storeID = *req.StoreID
	}
	s, err := h.opts.Service.UpdateSettings(r.Context(), actor, req.Provider, storeID)
	if err != nil {
		h.writeError(w, r, "update shopping settings failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, settingsResponse(s))
}

func (h *Handler) listPreferences(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	prefs, err := h.opts.Service.ListPreferences(r.Context(), actor.HouseholdID, chi.URLParam(r, "provider"))
	if err != nil {
		h.writeError(w, r, "list saved products failed", err)
		return
	}
	resp := PreferenceListResponse{Items: make([]PreferenceResponse, 0, len(prefs))}
	for _, p := range prefs {
		resp.Items = append(resp.Items, h.preferenceResponse(p))
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

// ingredientKeyParam reads the {ingredientKey} path segment, which clients
// percent-encode ("name:red%20onion").
func ingredientKeyParam(r *http.Request) string {
	raw := chi.URLParam(r, "ingredientKey")
	if key, err := url.PathUnescape(raw); err == nil {
		return key
	}
	return raw
}

func (h *Handler) getPreference(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	p, err := h.opts.Service.GetPreference(r.Context(), actor.HouseholdID, chi.URLParam(r, "provider"), ingredientKeyParam(r))
	if err != nil {
		h.writeError(w, r, "get saved product failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, h.preferenceResponse(p))
}

func (h *Handler) putPreference(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	var req PreferenceRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	in := PreferenceInput{
		ProductURL: req.ProductURL, ProductID: req.ProductID, DisplayName: req.DisplayName,
		IngredientName: req.IngredientName, Coverage: req.Coverage,
	}
	if req.PackageSize != nil {
		in.PackageSize = &PackageSize{Quantity: req.PackageSize.Quantity, Unit: req.PackageSize.Unit}
	}
	p, created, err := h.opts.Service.PutPreference(r.Context(), actor, chi.URLParam(r, "provider"), ingredientKeyParam(r), in)
	if err != nil {
		h.writeError(w, r, "save product failed", err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	httpx.WriteJSON(w, status, h.preferenceResponse(p))
}

func (h *Handler) deletePreference(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	if err := h.opts.Service.DeletePreference(r.Context(), actor, chi.URLParam(r, "provider"), ingredientKeyParam(r)); err != nil {
		h.writeError(w, r, "delete saved product failed", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) match(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	var req MatchRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	p, err := h.opts.Service.Match(r.Context(), actor.HouseholdID, chi.URLParam(r, "week"), chi.URLParam(r, "provider"), req.input())
	if err != nil {
		h.writeError(w, r, "match grocery list failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, h.proposalResponse(p, false))
}

func (h *Handler) createHandoff(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	var req MatchRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	ho, created, err := h.opts.Service.CreateHandoff(r.Context(), actor, chi.URLParam(r, "week"), chi.URLParam(r, "provider"), req.input())
	if err != nil {
		h.writeError(w, r, "create shopping handoff failed", err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	httpx.WriteJSON(w, status, h.handoffResponse(ho))
}

func (h *Handler) startOver(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	if err := h.opts.Service.StartOver(r.Context(), actor, chi.URLParam(r, "week"), chi.URLParam(r, "provider")); err != nil {
		h.writeError(w, r, "start shopping handoff over failed", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) sendAgain(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	var req SendAgainRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if err := h.opts.Service.SendAgain(r.Context(), actor, chi.URLParam(r, "week"), chi.URLParam(r, "provider"), req.IngredientKey); err != nil {
		h.writeError(w, r, "send shopping line again failed", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) listHandoffs(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	q := r.URL.Query()
	f := HandoffFilter{Week: q.Get("week"), Status: HandoffStatus(q.Get("status"))}
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			h.writeError(w, r, "", invalid("limit must be between 1 and %d", MaxHandoffList))
			return
		}
		f.Limit = n
	}
	list, err := h.opts.Service.ListHandoffs(r.Context(), actor.HouseholdID, f)
	if err != nil {
		h.writeError(w, r, "list shopping handoffs failed", err)
		return
	}
	resp := HandoffListResponse{Items: make([]HandoffResponse, 0, len(list))}
	for _, ho := range list {
		resp.Items = append(resp.Items, h.handoffResponse(ho))
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) getHandoff(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	ho, err := h.opts.Service.GetHandoff(r.Context(), actor.HouseholdID, chi.URLParam(r, "handoffId"))
	if err != nil {
		h.writeError(w, r, "get shopping handoff failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, h.handoffResponse(ho))
}

func (h *Handler) confirm(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	var req ConfirmRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	in := ConfirmInput{All: req.All, SkipRest: req.SkipRest}
	for _, l := range req.Lines {
		in.Lines = append(in.Lines, ConfirmLine(l))
	}
	res, err := h.opts.Service.Confirm(r.Context(), actor, chi.URLParam(r, "handoffId"), in)
	if err != nil {
		h.writeError(w, r, "confirm shopping order failed", err)
		return
	}
	resp := ConfirmResponse{Handoff: h.handoffResponse(res.Handoff), Purchases: make([]ConfirmedPurchaseResponse, 0, len(res.Purchases))}
	for _, p := range res.Purchases {
		pr := ConfirmedPurchaseResponse{LineID: p.LineID, Created: p.Created, Purchase: pantry.NewPurchaseResponse(p.Purchase)}
		if l, ok := res.Handoff.Line(p.LineID); ok {
			pr.IngredientKey = l.IngredientKey
		}
		if h.opts.Pantry != nil {
			pr.Item = h.opts.Pantry.ItemResponse(r.Context(), p.Item)
		}
		resp.Purchases = append(resp.Purchases, pr)
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

// callerHousehold returns the household whose requests the catalog marks: the
// signed-in user's first household (oldest membership). The catalog is not
// household-scoped, so this is a best effort — "" when the user belongs to no
// household, no lister is configured, or the lookup fails.
func (h *Handler) callerHousehold(r *http.Request) string {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok || h.opts.Households == nil {
		return ""
	}
	list, err := h.opts.Households.ListForUser(r.Context(), userID)
	if err != nil {
		h.logger.WarnContext(r.Context(), "list households for the store catalog failed", "error", err)
		return ""
	}
	if len(list) == 0 {
		return ""
	}
	return list[0].Membership.HouseholdID
}

func (h *Handler) storeCatalog(w http.ResponseWriter, r *http.Request) {
	items, err := h.opts.Service.StoreCatalog(r.Context(), h.callerHousehold(r), r.URL.Query().Get("q"))
	if err != nil {
		h.writeError(w, r, "list store catalog failed", err)
		return
	}
	resp := StoreCatalogResponse{Items: make([]StoreCatalogItemResponse, 0, len(items))}
	for _, i := range items {
		resp.Items = append(resp.Items, storeCatalogItemResponse(i))
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) requestStore(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	var req StoreRequestRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	saved, created, err := h.opts.Service.RequestStore(r.Context(), actor, StoreRequestInput(req))
	if err != nil {
		h.writeError(w, r, "request store failed", err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	httpx.WriteJSON(w, status, StoreRequestEnvelope{Request: storeRequestResponse(saved)})
}

func (h *Handler) listStoreRequests(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	list, err := h.opts.Service.ListStoreRequests(r.Context(), actor.HouseholdID)
	if err != nil {
		h.writeError(w, r, "list store requests failed", err)
		return
	}
	resp := StoreRequestListResponse{Items: make([]StoreRequestResponse, 0, len(list))}
	for _, req := range list {
		resp.Items = append(resp.Items, storeRequestResponse(req))
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) deleteStoreRequest(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	if err := h.opts.Service.DeleteStoreRequest(r.Context(), actor, chi.URLParam(r, "requestId")); err != nil {
		h.writeError(w, r, "withdraw store request failed", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeError maps service errors to responses. msg is logged for unexpected
// errors only.
// OrderReminderResponse is a week's order state. Every field is derived on
// read except `ordered`, which a member sets.
type OrderReminderResponse struct {
	Week string `json:"week"`
	// OrderDay is null when the household hasn't picked one, which turns the
	// reminder off.
	OrderDay *string `json:"orderDay"`
	DueOn    *string `json:"dueOn"`
	Due      bool    `json:"due"`
	// Remind is what an app shows a reminder for: due and not yet ordered.
	Remind    bool       `json:"remind"`
	Ordered   bool       `json:"ordered"`
	OrderedBy *string    `json:"orderedBy"`
	OrderedAt *time.Time `json:"orderedAt"`
}

// SetWeekOrderedRequest is the body of PUT .../weeks/{week}/order.
type SetWeekOrderedRequest struct {
	// Ordered false takes the mark back, which brings the reminder back.
	Ordered bool `json:"ordered"`
}

// orderTextOrNil renders an empty string as JSON null.
func orderTextOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func newOrderReminderResponse(r OrderReminder) OrderReminderResponse {
	resp := OrderReminderResponse{
		Week: r.Week, OrderDay: orderTextOrNil(r.OrderDay), DueOn: orderTextOrNil(r.DueOn),
		Due: r.Due, Remind: r.Remind, Ordered: r.Ordered, OrderedBy: orderTextOrNil(r.OrderedBy),
	}
	if !r.OrderedAt.IsZero() {
		at := r.OrderedAt.UTC()
		resp.OrderedAt = &at
	}
	return resp
}

func (h *Handler) getOrderReminder(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	reminder, err := h.opts.Service.OrderReminder(r.Context(), actor.HouseholdID, chi.URLParam(r, "week"))
	if err != nil {
		h.writeError(w, r, "get order reminder failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, newOrderReminderResponse(reminder))
}

func (h *Handler) setWeekOrdered(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	var req SetWeekOrderedRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	reminder, err := h.opts.Service.SetWeekOrdered(r.Context(), actor, chi.URLParam(r, "week"), req.Ordered)
	if err != nil {
		h.writeError(w, r, "set week ordered failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, newOrderReminderResponse(reminder))
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	var validation *ValidationError
	var providerValidation *providers.ValidationError
	var pantryValidation *pantry.ValidationError
	switch {
	case errors.As(err, &validation):
		httpx.WriteError(w, r, http.StatusBadRequest, "validation_failed", validation.Message)
	case errors.As(err, &providerValidation):
		httpx.WriteError(w, r, http.StatusBadRequest, "validation_failed", providerValidation.Message)
	case errors.As(err, &pantryValidation):
		httpx.WriteError(w, r, http.StatusBadRequest, "validation_failed", pantryValidation.Message)
	case errors.Is(err, planning.ErrInvalidWeek):
		httpx.WriteError(w, r, http.StatusBadRequest, "validation_failed", strings.ReplaceAll(err.Error(), "planning: ", ""))
	case errors.Is(err, ErrUnknownProvider):
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", "unknown shopping provider")
	case errors.Is(err, ErrProviderUnavailable):
		httpx.WriteError(w, r, http.StatusServiceUnavailable, "provider_unavailable", "this shopping provider isn't available yet")
	case errors.Is(err, ErrNotFound), errors.Is(err, pantry.ErrNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", "saved product or handoff not found")
	case errors.Is(err, ErrForbidden), errors.Is(err, pantry.ErrForbidden):
		httpx.WriteError(w, r, http.StatusForbidden, "forbidden", "your role in this household does not allow this action")
	case errors.Is(err, ErrTooManyRequests):
		httpx.WriteError(w, r, http.StatusConflict, "conflict",
			fmt.Sprintf("a household can ask for at most %d stores; withdraw one first", MaxStoreRequestsPerHousehold))
	case errors.Is(err, ErrConflict):
		httpx.WriteError(w, r, http.StatusConflict, "conflict", "another request changed this handoff at the same time; retry")
	case errors.Is(err, pantry.ErrConflict):
		httpx.WriteError(w, r, http.StatusConflict, "conflict", "a pantry item changed at the same time; retry")
	default:
		h.logger.ErrorContext(r.Context(), msg, "error", err)
		httpx.WriteError(w, r, http.StatusInternalServerError, "internal", "internal server error")
	}
}
