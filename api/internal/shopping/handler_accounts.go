package shopping

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
	"github.com/Linesmerrill/DinnerOS/api/internal/providers"
)

// Routes for providers a member signs in to and searches (decision 649).

// ConnectionResponse is a household's provider account, never its tokens.
type ConnectionResponse struct {
	Provider    providers.Key `json:"provider"`
	Connected   bool          `json:"connected"`
	ConnectedBy *string       `json:"connectedBy"`
	ConnectedAt *time.Time    `json:"connectedAt"`
}

// ConnectResponse is where the app sends the member to sign in.
type ConnectResponse struct {
	AuthorizeURL string `json:"authorizeUrl"`
	// CallbackScheme is the URL scheme the sign-in ends on, for the app's
	// web authentication session.
	CallbackScheme string `json:"callbackScheme"`
}

// StoreResponse is a store from a provider's store finder.
type StoreResponse struct {
	StoreID string `json:"storeId"`
	Name    string `json:"name"`
	Address string `json:"address"`
	// Banner is the store's brand in the Kroger family ("ralphs"), or
	// absent for other providers. Saving the store sets the household's
	// banner.
	Banner string `json:"banner,omitempty"`
}

// StoreListResponse lists stores.
type StoreListResponse struct {
	Items []StoreResponse `json:"items"`
}

// SearchProductResponse is a product from a provider's search.
type SearchProductResponse struct {
	ProductID   string               `json:"productId"`
	Name        string               `json:"name"`
	Brand       *string              `json:"brand"`
	Size        *string              `json:"size"`
	PackageSize *PackageSizeResponse `json:"packageSize"`
	ImageURL    *string              `json:"imageUrl"`
	URL         string               `json:"url"`
	PriceCents  *int64               `json:"priceCents"`
	// Stock is "high", "low", "out", or null when unknown.
	Stock *string `json:"stock"`
}

// SearchProductListResponse lists products.
type SearchProductListResponse struct {
	Items []SearchProductResponse `json:"items"`
}

func connectionResponse(c Connection) ConnectionResponse {
	return ConnectionResponse{Provider: c.Provider, Connected: c.Connected, ConnectedBy: optionalString(c.ConnectedBy), ConnectedAt: optionalTime(c.ConnectedAt)}
}

func (h *Handler) getConnection(w http.ResponseWriter, r *http.Request) {
	c, err := h.opts.Service.Connection(r.Context(), chi.URLParam(r, "householdId"), chi.URLParam(r, "provider"))
	if err != nil {
		h.writeError(w, r, "get provider connection failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, connectionResponse(c))
}

func (h *Handler) connect(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	u, err := h.opts.Service.ConnectURL(r.Context(), actor, chi.URLParam(r, "provider"))
	if err != nil {
		h.writeError(w, r, "start provider sign-in failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ConnectResponse{AuthorizeURL: u, CallbackScheme: h.opts.AppURLScheme})
}

func (h *Handler) disconnect(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	if err := h.opts.Service.Disconnect(r.Context(), actor, chi.URLParam(r, "provider")); err != nil {
		h.writeError(w, r, "disconnect provider failed", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// connectCallback is where the provider's sign-in page sends the member back.
// It's public: the signed state says whose household it is. It always ends
// by sending the browser to the app, with the outcome.
func (h *Handler) connectCallback(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")
	q := r.URL.Query()
	result := "connected"
	switch {
	case q.Get("error") != "":
		result = "cancelled"
	default:
		if _, err := h.opts.Service.CompleteConnection(r.Context(), provider, q.Get("state"), q.Get("code")); err != nil {
			result = "failed"
			if !errors.Is(err, ErrBadConnectState) && !errors.Is(err, ErrForbidden) {
				h.logger.ErrorContext(r.Context(), "finish provider sign-in failed", "provider", provider, "error", err)
			}
		}
	}
	back := url.URL{Scheme: h.opts.AppURLScheme, Host: "shopping", Path: "/connected", RawQuery: url.Values{"provider": {provider}, "result": {result}}.Encode()}
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, back.String(), http.StatusFound)
}

func (h *Handler) findStores(w http.ResponseWriter, r *http.Request) {
	stores, err := h.opts.Service.FindStores(r.Context(), chi.URLParam(r, "householdId"), chi.URLParam(r, "provider"), r.URL.Query().Get("zip"))
	if err != nil {
		h.writeError(w, r, "find stores failed", err)
		return
	}
	resp := StoreListResponse{Items: make([]StoreResponse, 0, len(stores))}
	for _, st := range stores {
		resp.Items = append(resp.Items, StoreResponse(st))
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) searchProducts(w http.ResponseWriter, r *http.Request) {
	results, err := h.opts.Service.SearchProducts(r.Context(), chi.URLParam(r, "householdId"), chi.URLParam(r, "provider"), r.URL.Query().Get("q"))
	if err != nil {
		h.writeError(w, r, "search products failed", err)
		return
	}
	resp := SearchProductListResponse{Items: make([]SearchProductResponse, 0, len(results))}
	for _, p := range results {
		resp.Items = append(resp.Items, searchProductResponse(p))
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func searchProductResponse(p SearchResult) SearchProductResponse {
	item := SearchProductResponse{
		ProductID: p.ProductID, Name: p.Name, Brand: optionalString(p.Brand), Size: optionalString(p.Size),
		ImageURL: optionalString(p.ImageURL), URL: p.URL, PriceCents: p.PriceCents, Stock: optionalString(string(p.Stock)),
	}
	item.PackageSize = packageSizeResponse(p.PackageSize)
	return item
}

// MatchSuggestionResponse is one pre-picked product for an ingredient saved
// at another store.
type MatchSuggestionResponse struct {
	IngredientKey  string `json:"ingredientKey"`
	IngredientName string `json:"ingredientName"`
	// From is the product saved at the other store.
	From SuggestionSourceResponse `json:"from"`
	// Product is the pick, or null when the store found nothing usable.
	Product      *SearchProductResponse  `json:"product"`
	Alternatives []SearchProductResponse `json:"alternatives"`
}

// SuggestionSourceResponse is the saved product a suggestion starts from.
type SuggestionSourceResponse struct {
	Provider    providers.Key        `json:"provider"`
	ProductID   string               `json:"productId"`
	DisplayName string               `json:"displayName"`
	PackageSize *PackageSizeResponse `json:"packageSize"`
}

// MatchSuggestionPageResponse is returned by GET .../{provider}/suggestions.
type MatchSuggestionPageResponse struct {
	Items []MatchSuggestionResponse `json:"items"`
	// Next is the ingredient key to send as after, or null on the last page.
	Next *string `json:"next"`
	// Total counts every ingredient still to match.
	Total int `json:"total"`
}

func (h *Handler) matchSuggestions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	from := q.Get("from")
	if from == "" {
		from = string(providers.KeyWalmart)
	}
	limit := 0
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			httpx.WriteError(w, r, http.StatusBadRequest, "validation_failed", "limit must be a positive number")
			return
		}
		limit = n
	}
	page, err := h.opts.Service.MatchSuggestions(r.Context(), chi.URLParam(r, "householdId"), chi.URLParam(r, "provider"), from, q.Get("after"), limit)
	if err != nil {
		h.writeError(w, r, "match suggestions failed", err)
		return
	}
	resp := MatchSuggestionPageResponse{Items: make([]MatchSuggestionResponse, 0, len(page.Items)), Next: optionalString(page.Next), Total: page.Total}
	for _, m := range page.Items {
		item := MatchSuggestionResponse{
			IngredientKey: m.IngredientKey, IngredientName: m.IngredientName,
			From: SuggestionSourceResponse{
				Provider: m.From.Provider, ProductID: m.From.ProductID, DisplayName: m.From.DisplayName,
				PackageSize: packageSizeResponse(m.From.PackageSize),
			},
			Alternatives: make([]SearchProductResponse, 0, len(m.Alternatives)),
		}
		if m.Product != nil {
			p := searchProductResponse(*m.Product)
			item.Product = &p
		}
		for _, a := range m.Alternatives {
			item.Alternatives = append(item.Alternatives, searchProductResponse(a))
		}
		resp.Items = append(resp.Items, item)
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}
