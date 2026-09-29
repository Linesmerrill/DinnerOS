// Package cooking keeps a household's cooking screen in step across its
// devices: what's been checked off, the step someone is on, and the timers
// that are running. A session is one dish on one day; the kitchen's timers are
// one list per day, since a timer outlives switching dishes.
//
// Changes arrive as small operations ("check these", "step 4", "this timer")
// applied atomically, so two people tapping at once never overwrite each
// other. Sessions expire a day after their last change.
package cooking

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"github.com/go-chi/chi/v5"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/Linesmerrill/DinnerOS/api/internal/auth"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
)

// Collection is the cooking sessions collection.
const Collection = "cook_sessions"

// kitchen is the session key for the day's timers.
const kitchen = "_kitchen"

// Limits.
const (
	MaxOps     = 50
	MaxIDs     = 200
	MaxIDLen   = 200
	MaxTimers  = 20
	MaxLabel   = 80
	sessionTTL = 24 * time.Hour
)

var (
	// ErrInvalid is a request that can't be applied.
	ErrInvalid = errors.New("cooking: invalid request")
	dateRe     = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

// Timer is a kitchen timer as every device shows it. A running timer has
// EndsAt; a paused one has RemainingSeconds; a finished one has Finished.
type Timer struct {
	ID               string     `bson:"id" json:"id"`
	Label            string     `bson:"label" json:"label"`
	TotalSeconds     int        `bson:"totalSeconds" json:"totalSeconds"`
	EndsAt           *time.Time `bson:"endsAt,omitempty" json:"endsAt"`
	RemainingSeconds *int       `bson:"remainingSeconds,omitempty" json:"remainingSeconds"`
	Finished         bool       `bson:"finished" json:"finished"`
}

// Session is one dish's cooking state plus the day's timers.
type Session struct {
	RecipeID    string    `json:"recipeId"`
	Date        string    `json:"date"`
	Checked     []string  `json:"checked"`
	CurrentStep *int      `json:"currentStep"`
	Timers      []Timer   `json:"timers"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// Op is one change. Kind is check, uncheck, step, timer, or removeTimer.
type Op struct {
	Kind  string   `json:"op"`
	IDs   []string `json:"ids,omitempty"`
	Step  *int     `json:"step,omitempty"`
	Timer *Timer   `json:"timer,omitempty"`
	ID    string   `json:"id,omitempty"`
}

type doc struct {
	HouseholdID bson.ObjectID `bson:"householdId"`
	Key         string        `bson:"key"`
	Date        string        `bson:"date"`
	Checked     []string      `bson:"checked,omitempty"`
	CurrentStep *int          `bson:"currentStep,omitempty"`
	Timers      []Timer       `bson:"timers,omitempty"`
	UpdatedAt   time.Time     `bson:"updatedAt"`
	ExpiresAt   time.Time     `bson:"expiresAt"`
}

// Indexes returns the indexes the store relies on: one session per dish and
// day, and expiry a day after the last change.
func Indexes() []mongodb.IndexSet {
	return []mongodb.IndexSet{{
		Collection: Collection,
		Indexes: []mongo.IndexModel{{
			Keys:    bson.D{{Key: "householdId", Value: 1}, {Key: "date", Value: 1}, {Key: "key", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("householdId_date_key_unique"),
		}, {
			Keys:    bson.D{{Key: "expiresAt", Value: 1}},
			Options: options.Index().SetExpireAfterSeconds(0).SetName("expiresAt_ttl"),
		}},
	}}
}

// Store reads and changes sessions.
type Store struct {
	sessions *mongo.Collection
	now      func() time.Time
}

// NewStore returns a store using db.
func NewStore(db *mongo.Database) *Store {
	return &Store{sessions: db.Collection(Collection), now: time.Now}
}

// PurgeHousehold deletes the household's sessions. It is idempotent.
func (s *Store) PurgeHousehold(ctx context.Context, householdID string) error {
	return mongodb.DeleteByID(ctx, "householdId", householdID, s.sessions)
}

func (s *Store) filter(hid bson.ObjectID, date, key string) bson.D {
	return bson.D{{Key: "householdId", Value: hid}, {Key: "date", Value: date}, {Key: "key", Value: key}}
}

// Get returns a dish's session with the day's timers; an empty one when
// nothing has happened yet.
func (s *Store) Get(ctx context.Context, householdID, recipeID, date string) (Session, error) {
	hid, err := mongodb.ParseID(householdID)
	if err != nil {
		return Session{}, ErrInvalid
	}
	if !dateRe.MatchString(date) || recipeID == "" || recipeID == kitchen || len(recipeID) > MaxIDLen {
		return Session{}, fmt.Errorf("%w: a recipe and a date (YYYY-MM-DD) are required", ErrInvalid)
	}
	out := Session{RecipeID: recipeID, Date: date, Checked: []string{}, Timers: []Timer{}}
	var d doc
	if err := s.sessions.FindOne(ctx, s.filter(hid, date, recipeID)).Decode(&d); err == nil {
		out.Checked = append(out.Checked, d.Checked...)
		out.CurrentStep, out.UpdatedAt = d.CurrentStep, d.UpdatedAt.UTC()
	} else if !errors.Is(err, mongo.ErrNoDocuments) {
		return Session{}, err
	}
	var k doc
	if err := s.sessions.FindOne(ctx, s.filter(hid, date, kitchen)).Decode(&k); err == nil {
		out.Timers = append(out.Timers, k.Timers...)
		if k.UpdatedAt.After(out.UpdatedAt) {
			out.UpdatedAt = k.UpdatedAt.UTC()
		}
	} else if !errors.Is(err, mongo.ErrNoDocuments) {
		return Session{}, err
	}
	return out, nil
}

// Apply applies ops in order and returns the session as it now stands.
func (s *Store) Apply(ctx context.Context, householdID, recipeID, date string, ops []Op) (Session, error) {
	if _, err := s.Get(ctx, householdID, recipeID, date); err != nil {
		return Session{}, err
	}
	if len(ops) == 0 || len(ops) > MaxOps {
		return Session{}, fmt.Errorf("%w: send 1 to %d ops", ErrInvalid, MaxOps)
	}
	hid, _ := mongodb.ParseID(householdID)
	now := s.now().UTC()
	touch := bson.E{Key: "$set", Value: bson.D{{Key: "updatedAt", Value: now}, {Key: "expiresAt", Value: now.Add(sessionTTL)}}}
	upsert := options.UpdateOne().SetUpsert(true)
	for _, op := range ops {
		if err := validate(op); err != nil {
			return Session{}, err
		}
		key, update := recipeID, bson.D{}
		switch op.Kind {
		case "check":
			update = bson.D{{Key: "$addToSet", Value: bson.D{{Key: "checked", Value: bson.D{{Key: "$each", Value: op.IDs}}}}}}
		case "uncheck":
			update = bson.D{{Key: "$pull", Value: bson.D{{Key: "checked", Value: bson.D{{Key: "$in", Value: op.IDs}}}}}}
		case "step":
			if op.Step == nil {
				update = bson.D{{Key: "$unset", Value: bson.D{{Key: "currentStep", Value: ""}}}}
			} else {
				update = bson.D{{Key: "$set", Value: bson.D{{Key: "currentStep", Value: *op.Step}}}}
			}
		case "timer", "removeTimer":
			key = kitchen
			id := op.ID
			if op.Timer != nil {
				id = op.Timer.ID
			}
			pull := bson.D{{Key: "$pull", Value: bson.D{{Key: "timers", Value: bson.D{{Key: "id", Value: id}}}}}}
			if _, err := s.sessions.UpdateOne(ctx, s.filter(hid, date, kitchen), append(pull, touch), upsert); err != nil {
				return Session{}, err
			}
			if op.Kind == "removeTimer" {
				continue
			}
			update = bson.D{{Key: "$push", Value: bson.D{{Key: "timers", Value: bson.D{
				{Key: "$each", Value: []Timer{*op.Timer}}, {Key: "$slice", Value: -MaxTimers},
			}}}}}
		}
		update = append(update, touch)
		if _, err := s.sessions.UpdateOne(ctx, s.filter(hid, date, key), update, upsert); err != nil {
			return Session{}, err
		}
	}
	return s.Get(ctx, householdID, recipeID, date)
}

func validate(op Op) error {
	switch op.Kind {
	case "check", "uncheck":
		if len(op.IDs) == 0 || len(op.IDs) > MaxIDs {
			return fmt.Errorf("%w: %s needs 1 to %d ids", ErrInvalid, op.Kind, MaxIDs)
		}
		for _, id := range op.IDs {
			if id == "" || len(id) > MaxIDLen {
				return fmt.Errorf("%w: ids must be 1 to %d characters", ErrInvalid, MaxIDLen)
			}
		}
	case "step":
		if op.Step != nil && (*op.Step < 0 || *op.Step > 100) {
			return fmt.Errorf("%w: step must be between 0 and 100", ErrInvalid)
		}
	case "timer":
		t := op.Timer
		if t == nil || t.ID == "" || len(t.ID) > MaxIDLen || len(t.Label) > MaxLabel ||
			t.TotalSeconds <= 0 || t.TotalSeconds > 12*3600 {
			return fmt.Errorf("%w: a timer needs an id, a label up to %d characters, and 1 second to 12 hours", ErrInvalid, MaxLabel)
		}
	case "removeTimer":
		if op.ID == "" || len(op.ID) > MaxIDLen {
			return fmt.Errorf("%w: removeTimer needs an id", ErrInvalid)
		}
	default:
		return fmt.Errorf("%w: unknown op %q", ErrInvalid, op.Kind)
	}
	return nil
}

// --- HTTP ---------------------------------------------------------------------

// Handler serves the cooking session endpoints.
type Handler struct {
	store      *Store
	authorizer households.Authorizer
	tokens     auth.AccessTokenValidator
	logger     *slog.Logger
}

// NewHandler returns a Handler.
func NewHandler(store *Store, authorizer households.Authorizer, tokens auth.AccessTokenValidator, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Handler{store: store, authorizer: authorizer, tokens: tokens, logger: logger}
}

// Mount registers the routes on the /api/v1 router. Any member may cook.
func (h *Handler) Mount(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireAuth(h.tokens))
		r.Use(households.RequirePermission(h.authorizer, households.PermHouseholdView, h.logger))
		r.Get("/households/{householdId}/cook-sessions/{recipeId}", h.get)
		r.Post("/households/{householdId}/cook-sessions/{recipeId}/ops", h.apply)
	})
}

// OpsRequest is the body of POST .../cook-sessions/{recipeId}/ops.
type OpsRequest struct {
	Date string `json:"date"`
	Ops  []Op   `json:"ops"`
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	s, err := h.store.Get(r.Context(), actor.HouseholdID, chi.URLParam(r, "recipeId"), r.URL.Query().Get("date"))
	if h.writeError(w, r, err) {
		return
	}
	httpx.WriteJSON(w, http.StatusOK, s)
}

func (h *Handler) apply(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	var req OpsRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	s, err := h.store.Apply(r.Context(), actor.HouseholdID, chi.URLParam(r, "recipeId"), req.Date, req.Ops)
	if h.writeError(w, r, err) {
		return
	}
	httpx.WriteJSON(w, http.StatusOK, s)
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, ErrInvalid):
		httpx.WriteError(w, r, http.StatusBadRequest, "validation_failed", err.Error())
	default:
		h.logger.ErrorContext(r.Context(), "cooking session failed", "error", err)
		httpx.WriteError(w, r, http.StatusInternalServerError, "internal", "internal server error")
	}
	return true
}
