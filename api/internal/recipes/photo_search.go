package recipes

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/photosearch"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

// Photo search (decision 692): instead of taking a photo, a member searches
// for one by the recipe's name and picks it. The search runs here so no key
// ships in the app. A picked photo is downloaded and kept like an uploaded
// one, so it still shows offline and if the provider removes it.

// errPhotoSearchOff means no photo search provider is configured.
var errPhotoSearchOff = errors.New("recipes: photo search is off")

// WithPhotoSearch turns on photo search and returns s.
func (s *Service) WithPhotoSearch(p photosearch.Searcher) *Service {
	s.photoSearch = p
	return s
}

// SearchPhotos returns a page of photos for query.
func (s *Service) SearchPhotos(ctx context.Context, query string, page int) (photosearch.Page, error) {
	if s.photoSearch == nil {
		return photosearch.Page{}, errPhotoSearchOff
	}
	return s.photoSearch.Search(ctx, query, page)
}

// SavePhotoFromSearch downloads a search result and keeps it as the
// household's photo, returning its ID.
func (s *Service) SavePhotoFromSearch(ctx context.Context, householdID, resultID string) (string, error) {
	if s.photoSearch == nil {
		return "", errPhotoSearchOff
	}
	img, err := s.photoSearch.Fetch(ctx, resultID)
	if err != nil {
		return "", err
	}
	return s.SavePhoto(ctx, householdID, img.Data)
}

// --- HTTP -----------------------------------------------------------------------

// PhotoSearchResponse is the body of GET .../recipe-photos/search.
type PhotoSearchResponse struct {
	Photos []PhotoSearchResult `json:"photos"`
	// NextPage is the page to ask for next, or 0 when there are no more.
	NextPage int `json:"nextPage"`
}

// PhotoSearchResult is one photo the member can pick.
type PhotoSearchResult struct {
	ID           string `json:"id"`
	ThumbnailURL string `json:"thumbnailUrl"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	// Credit is the photographer's name.
	Credit string `json:"credit"`
}

// PhotoSearchStatusResponse is the body of GET .../recipe-photos/search-status:
// whether the app should offer photo search at all.
type PhotoSearchStatusResponse struct {
	Available bool `json:"available"`
}

// PhotoSearchAvailable reports whether a photo search provider is configured.
func (s *Service) PhotoSearchAvailable() bool { return s.photoSearch != nil }

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
		httpx.WriteJSON(w, http.StatusOK, PhotoSearchResponse{Photos: []PhotoSearchResult{}})
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
	out := PhotoSearchResponse{Photos: make([]PhotoSearchResult, 0, len(result.Photos)), NextPage: result.NextPage}
	for _, p := range result.Photos {
		out.Photos = append(out.Photos, PhotoSearchResult{
			ID: p.ID, ThumbnailURL: p.ThumbnailURL, Width: p.Width, Height: p.Height, Credit: p.Credit,
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
	actor, _ := households.MembershipFromContext(r.Context())
	id, err := h.opts.Service.SavePhotoFromSearch(r.Context(), actor.HouseholdID, req.ID)
	switch {
	case errors.Is(err, errPhotoSearchOff):
		photoSearchOff(w, r)
		return
	case errors.Is(err, photosearch.ErrNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", "That photo isn't there anymore. Pick another one.")
		return
	case errors.Is(err, photosearch.ErrTooBig), errors.Is(err, errNotAPhoto):
		httpx.WriteError(w, r, http.StatusBadGateway, "photo_search_failed", "That photo can't be used. Pick another one.")
		return
	case err != nil:
		h.logger.WarnContext(r.Context(), "photo from search failed", "error", err)
		httpx.WriteError(w, r, http.StatusBadGateway, "photo_search_failed", "Couldn't add that photo. Try again.")
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, PhotoUploadResponse{ImageURL: photoURL(r, id)})
}
