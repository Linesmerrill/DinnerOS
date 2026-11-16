package recipes

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/photosearch"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

// Photo search (decisions 692, 705): instead of taking a photo, a member
// searches Unsplash by the recipe's name and picks one. The search runs here
// so no key ships in the app. A picked photo is hotlinked from Unsplash, as
// its API guidelines ask, and its credit is kept by the photo's address so
// every recipe that shows it can say who took it.

// errPhotoSearchOff means no photo search provider is configured.
var errPhotoSearchOff = errors.New("recipes: photo search is off")

// PhotoCreditsCollection holds who took each picked Unsplash photo, by its address.
const PhotoCreditsCollection = "recipe_photo_credits"

// PhotoCreditStore keeps the credit for a picked photo, by the photo's address.
type PhotoCreditStore interface {
	SavePhotoCredit(ctx context.Context, imageURL string, c photosearch.Credit) error
	// PhotoCredit answers ok=false when no credit is kept for imageURL.
	PhotoCredit(ctx context.Context, imageURL string) (photosearch.Credit, bool, error)
}

// WithPhotoSearch turns on photo search and returns s.
func (s *Service) WithPhotoSearch(p photosearch.Searcher) *Service {
	s.photoSearch = p
	return s
}

// WithPhotoCredits keeps and reads photo credits. Wired whether or not search
// is on, so photos picked earlier keep their credit.
func (s *Service) WithPhotoCredits(c PhotoCreditStore) *Service {
	s.photoCredits = c
	return s
}

// PhotoSearchAvailable reports whether a photo search provider is configured.
func (s *Service) PhotoSearchAvailable() bool { return s.photoSearch != nil }

// SearchPhotos returns a page of photos for query.
func (s *Service) SearchPhotos(ctx context.Context, query string, page int) (photosearch.Page, error) {
	if s.photoSearch == nil {
		return photosearch.Page{}, errPhotoSearchOff
	}
	return s.photoSearch.Search(ctx, query, page)
}

// PickPhoto records that a member chose a search result and returns the
// address to show it from and its credit, which is kept for later.
func (s *Service) PickPhoto(ctx context.Context, resultID string) (photosearch.Picked, error) {
	if s.photoSearch == nil {
		return photosearch.Picked{}, errPhotoSearchOff
	}
	picked, err := s.photoSearch.Pick(ctx, resultID)
	if err != nil {
		return photosearch.Picked{}, err
	}
	if s.photoCredits == nil {
		return photosearch.Picked{}, errors.New("recipes: photo credits are not wired")
	}
	if err := s.photoCredits.SavePhotoCredit(ctx, picked.ImageURL, picked.Credit); err != nil {
		return photosearch.Picked{}, err
	}
	return picked, nil
}

// PhotoCredit is who to credit for a recipe's photo, or nil when it is the
// household's own or has no kept credit.
func (s *Service) PhotoCredit(ctx context.Context, imageURL string) *photosearch.Credit {
	if s.photoCredits == nil || !photosearch.IsUnsplashImageURL(imageURL) {
		return nil
	}
	c, ok, err := s.photoCredits.PhotoCredit(ctx, imageURL)
	if err != nil || !ok {
		return nil
	}
	return &c
}

// IsPickablePhotoURL reports whether a recipe may keep raw as its photo: the
// household's own upload, or a photo hotlinked from Unsplash.
func IsPickablePhotoURL(raw string) bool {
	return IsOwnPhotoURL(raw) || photosearch.IsUnsplashImageURL(raw)
}

// --- HTTP -----------------------------------------------------------------------

// PhotoSearchResponse is the body of GET .../recipe-photos/search.
type PhotoSearchResponse struct {
	Photos []PhotoSearchResult `json:"photos"`
	// NextPage is the page to ask for next, or 0 when there are no more.
	NextPage int `json:"nextPage"`
	// SourceName and SourceURL name where the photos come from, for "Photos from Unsplash".
	SourceName string `json:"sourceName"`
	SourceURL  string `json:"sourceUrl"`
}

// PhotoSearchResult is one photo the member can pick.
type PhotoSearchResult struct {
	ID           string `json:"id"`
	ThumbnailURL string `json:"thumbnailUrl"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	// Credit is the photographer's name; CreditURL is their Unsplash page.
	Credit    string `json:"credit"`
	CreditURL string `json:"creditUrl"`
}

// ImageCreditResponse says who took a recipe's photo: "Photo by <name> on
// <sourceName>", each linked.
type ImageCreditResponse struct {
	Name       string `json:"name"`
	ProfileURL string `json:"profileUrl"`
	SourceName string `json:"sourceName"`
	SourceURL  string `json:"sourceUrl"`
}

// NewImageCreditResponse is the wire form of c, or nil.
func NewImageCreditResponse(c *photosearch.Credit) *ImageCreditResponse {
	if c == nil {
		return nil
	}
	return &ImageCreditResponse{
		Name: c.Name, ProfileURL: c.ProfileURL, SourceName: photosearch.SourceName, SourceURL: photosearch.SourceURL(),
	}
}

// PhotoPickResponse is the body of a 201 from POST .../from-search.
type PhotoPickResponse struct {
	ImageURL string               `json:"imageUrl"`
	Credit   *ImageCreditResponse `json:"credit"`
}

// PhotoSearchStatusResponse is the body of GET .../recipe-photos/search-status:
// whether the app should offer photo search at all.
type PhotoSearchStatusResponse struct {
	Available bool `json:"available"`
}

// PhotoFromSearchRequest is the body of POST .../recipe-photos/from-search.
type PhotoFromSearchRequest struct {
	ID string `json:"id"`
}

func (h *Handler) mountPhotoSearch(r chi.Router, edit func(http.Handler) http.Handler) {
	r.With(edit).Get("/households/{householdId}/recipe-photos/search", h.searchPhotos)
	r.With(edit).Get("/households/{householdId}/recipe-photos/search-status", h.photoSearchStatus)
	r.With(edit).Post("/households/{householdId}/recipe-photos/from-search", h.photoFromSearch)
}

func photoSearchOff(w http.ResponseWriter, r *http.Request) {
	httpx.WriteError(w, r, http.StatusServiceUnavailable, "photo_search_off", "Photo search isn't available right now.")
}

func (h *Handler) photoSearchStatus(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, PhotoSearchStatusResponse{Available: h.opts.Service.PhotoSearchAvailable()})
}

func (h *Handler) searchPhotos(w http.ResponseWriter, r *http.Request) {
	query := photosearch.CleanQuery(r.URL.Query().Get("q"))
	if query == "" {
		validationFailed(w, r, "Type what to search for.")
		return
	}
	page := 1
	if raw := r.URL.Query().Get("page"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			validationFailed(w, r, "That page doesn't exist.")
			return
		}
		page = n
	}
	if page > photosearch.MaxPage {
		httpx.WriteJSON(w, http.StatusOK, PhotoSearchResponse{
			Photos: []PhotoSearchResult{}, SourceName: photosearch.SourceName, SourceURL: photosearch.SourceURL(),
		})
		return
	}
	result, err := h.opts.Service.SearchPhotos(r.Context(), query, page)
	switch {
	case errors.Is(err, errPhotoSearchOff):
		photoSearchOff(w, r)
		return
	case err != nil:
		h.logger.WarnContext(r.Context(), "photo search failed", "error", err)
		httpx.WriteError(w, r, http.StatusBadGateway, "photo_search_failed", "Couldn't search for photos. Try again.")
		return
	}
	out := PhotoSearchResponse{
		Photos: make([]PhotoSearchResult, 0, len(result.Photos)), NextPage: result.NextPage,
		SourceName: photosearch.SourceName, SourceURL: photosearch.SourceURL(),
	}
	for _, p := range result.Photos {
		out.Photos = append(out.Photos, PhotoSearchResult{
			ID: p.ID, ThumbnailURL: p.ThumbnailURL, Width: p.Width, Height: p.Height,
			Credit: p.Credit.Name, CreditURL: p.Credit.ProfileURL,
		})
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) photoFromSearch(w http.ResponseWriter, r *http.Request) {
	var req PhotoFromSearchRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.ID == "" {
		validationFailed(w, r, "Pick a photo first.")
		return
	}
	picked, err := h.opts.Service.PickPhoto(r.Context(), req.ID)
	switch {
	case errors.Is(err, errPhotoSearchOff):
		photoSearchOff(w, r)
		return
	case errors.Is(err, photosearch.ErrNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", "That photo isn't there anymore. Pick another one.")
		return
	case err != nil:
		h.logger.WarnContext(r.Context(), "photo pick failed", "error", err)
		httpx.WriteError(w, r, http.StatusBadGateway, "photo_search_failed", "Couldn't add that photo. Try again.")
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, PhotoPickResponse{ImageURL: picked.ImageURL, Credit: NewImageCreditResponse(&picked.Credit)})
}

// creditPhoto fills in who took the recipe's photo when it came from photo search.
func (h *Handler) creditPhoto(ctx context.Context, resp *RecipeResponse) {
	resp.ImageCredit = NewImageCreditResponse(h.opts.Service.PhotoCredit(ctx, resp.ImageURL))
}
