package recipes

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"github.com/go-chi/chi/v5"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
)

// Recipe photos (decision 687): a member adds their own photo to a recipe
// the household added (typed, pasted, or read from a page). The phone
// uploads the image, gets back its address, and saves that as the recipe's
// imageUrl. Photos live in MongoDB and are served by the API under an
// unguessable ID, so image views need no sign-in; they go with the household.

// PhotosCollection holds uploaded recipe photos.
const PhotosCollection = "recipe_photos"

// MaxPhotoBytes is the largest photo accepted. The app shrinks a photo to
// well under this before uploading.
const MaxPhotoBytes = 4 << 20

// PhotoPathPrefix is where photos are served, under the API root.
const PhotoPathPrefix = "/api/v1/recipe-photos/"

// ErrPhotoNotFound means no photo has that ID.
var ErrPhotoNotFound = errors.New("recipes: photo not found")

// Photo is one uploaded image.
type Photo struct {
	ID          string
	HouseholdID string
	ContentType string
	Data        []byte
	CreatedAt   time.Time
}

// PhotoStore keeps photos.
type PhotoStore interface {
	SavePhoto(ctx context.Context, p Photo) error
	GetPhoto(ctx context.Context, id string) (Photo, error)
}

// WithPhotos turns on photo uploads and returns s.
func (s *Service) WithPhotos(p PhotoStore) *Service {
	s.photos = p
	return s
}

// SavePhoto checks and stores an uploaded image and returns its ID.
func (s *Service) SavePhoto(ctx context.Context, householdID string, data []byte) (string, error) {
	if s.photos == nil {
		return "", errors.New("recipes: photos are not wired")
	}
	contentType, ok := SniffPhoto(data)
	if !ok {
		return "", errNotAPhoto
	}
	id, err := newPhotoID()
	if err != nil {
		return "", err
	}
	if err := s.photos.SavePhoto(ctx, Photo{
		ID: id, HouseholdID: householdID, ContentType: contentType, Data: data, CreatedAt: s.now().UTC(),
	}); err != nil {
		return "", err
	}
	return id, nil
}

// Photo returns a stored photo.
func (s *Service) Photo(ctx context.Context, id string) (Photo, error) {
	if s.photos == nil || !photoIDRe.MatchString(id) {
		return Photo{}, ErrPhotoNotFound
	}
	return s.photos.GetPhoto(ctx, id)
}

var errNotAPhoto = errors.New("recipes: not a photo")

var (
	photoIDRe   = regexp.MustCompile(`^[0-9a-f]{32}$`)
	photoPathRe = regexp.MustCompile(`^` + regexp.QuoteMeta(PhotoPathPrefix) + `[0-9a-f]{32}$`)
)

func newPhotoID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("photo id: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// SniffPhoto returns the image type of data, from its first bytes: JPEG,
// PNG, or HEIC. Anything else is not a photo.
func SniffPhoto(data []byte) (string, bool) {
	switch {
	case len(data) >= 3 && bytes.Equal(data[:3], []byte{0xFF, 0xD8, 0xFF}):
		return "image/jpeg", true
	case len(data) >= 8 && bytes.Equal(data[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1A, '\n'}):
		return "image/png", true
	case len(data) >= 12 && string(data[4:8]) == "ftyp":
		switch string(data[8:12]) {
		case "heic", "heix", "hevc", "hevx", "heim", "heis", "mif1", "msf1":
			return "image/heic", true
		}
	}
	return "", false
}

// IsOwnPhotoURL reports whether raw is the address of a photo this API
// serves. Its host is whatever the phone called, so it isn't checked.
func IsOwnPhotoURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Host == "" {
		return false
	}
	return u.RawQuery == "" && u.Fragment == "" && photoPathRe.MatchString(u.Path)
}

// --- HTTP -----------------------------------------------------------------------

// PhotoUploadResponse is the body of a 201 from POST .../recipe-photos.
type PhotoUploadResponse struct {
	ImageURL string `json:"imageUrl"`
}

var photoContentTypes = map[string]bool{"image/jpeg": true, "image/png": true, "image/heic": true, "image/heif": true}

func (h *Handler) mountPhotos(r chi.Router, edit func(http.Handler) http.Handler) {
	r.With(edit, httpx.OverrideBodyLimit(MaxPhotoBytes)).Post("/households/{householdId}/recipe-photos", h.uploadPhoto)
}

func (h *Handler) uploadPhoto(w http.ResponseWriter, r *http.Request) {
	ct := r.Header.Get("Content-Type")
	if i := bytes.IndexByte([]byte(ct), ';'); i >= 0 {
		ct = ct[:i]
	}
	if !photoContentTypes[ct] {
		validationFailed(w, r, "That file isn't a photo.")
		return
	}
	data, err := io.ReadAll(r.Body)
	var tooBig *http.MaxBytesError
	switch {
	case errors.As(err, &tooBig):
		httpx.WriteError(w, r, http.StatusRequestEntityTooLarge, "payload_too_large", "That photo is too big.")
		return
	case err != nil:
		validationFailed(w, r, "That photo didn't upload. Try again.")
		return
	}
	actor, _ := households.MembershipFromContext(r.Context())
	id, err := h.opts.Service.SavePhoto(r.Context(), actor.HouseholdID, data)
	switch {
	case errors.Is(err, errNotAPhoto):
		validationFailed(w, r, "That file isn't a photo.")
		return
	case err != nil:
		h.internalError(w, r, "save recipe photo failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, PhotoUploadResponse{ImageURL: photoURL(r, id)})
}

// photoURL is the photo's address on the host the phone called.
func photoURL(r *http.Request, id string) string {
	scheme := r.Header.Get("X-Forwarded-Proto")
	if scheme != "http" && scheme != "https" {
		scheme = "http"
		if r.TLS != nil {
			scheme = "https"
		}
	}
	return scheme + "://" + r.Host + PhotoPathPrefix + id
}

// servePhoto serves GET /recipe-photos/{photoId}. It needs no sign-in: the
// ID is 128 random bits, and image views can't send a token.
func (h *Handler) servePhoto(w http.ResponseWriter, r *http.Request) {
	p, err := h.opts.Service.Photo(r.Context(), chi.URLParam(r, "photoId"))
	switch {
	case errors.Is(err, ErrPhotoNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", "photo not found")
		return
	case err != nil:
		h.internalError(w, r, "load recipe photo failed", err)
		return
	}
	w.Header().Set("Content-Type", p.ContentType)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(p.Data)
}

// --- MongoDB --------------------------------------------------------------------

type photoDoc struct {
	ID          string        `bson:"_id"`
	HouseholdID bson.ObjectID `bson:"householdId"`
	ContentType string        `bson:"contentType"`
	Data        []byte        `bson:"data"`
	Size        int           `bson:"size"`
	CreatedAt   time.Time     `bson:"createdAt"`
}

// SavePhoto implements PhotoStore.
func (s *MongoStore) SavePhoto(ctx context.Context, p Photo) error {
	hid, err := mongodb.ParseID(p.HouseholdID)
	if err != nil {
		return fmt.Errorf("save photo: %w", err)
	}
	_, err = s.photos.InsertOne(ctx, photoDoc{
		ID: p.ID, HouseholdID: hid, ContentType: p.ContentType, Data: p.Data, Size: len(p.Data), CreatedAt: p.CreatedAt,
	})
	if err != nil {
		return fmt.Errorf("save photo: %w", err)
	}
	return nil
}

// GetPhoto implements PhotoStore.
func (s *MongoStore) GetPhoto(ctx context.Context, id string) (Photo, error) {
	var d photoDoc
	err := s.photos.FindOne(ctx, bson.D{{Key: "_id", Value: id}}).Decode(&d)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return Photo{}, ErrPhotoNotFound
	}
	if err != nil {
		return Photo{}, fmt.Errorf("get photo: %w", err)
	}
	return Photo{ID: d.ID, HouseholdID: d.HouseholdID.Hex(), ContentType: d.ContentType, Data: d.Data, CreatedAt: d.CreatedAt.UTC()}, nil
}
