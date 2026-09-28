package ratings

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
)

// This file holds a member's private notes on a recipe: what they changed
// while cooking, what to try next time. Unlike a rating's comment, a note is
// never shown to anyone else in the household, and nothing learns from it.

// NotesCollection is the recipe notes collection.
const NotesCollection = "recipe_notes"

// MaxNoteLength bounds a note, in characters.
const MaxNoteLength = 5000

// Note is one member's private note on one recipe.
type Note struct {
	HouseholdID string
	RecipeID    string
	UserID      string
	Text        string
	UpdatedAt   time.Time
}

// NoteStore persists notes. A note is keyed by household, recipe, and user.
type NoteStore interface {
	// GetNote returns the user's note, or ErrNotFound when there is none.
	GetNote(ctx context.Context, householdID, recipeID, userID string) (Note, error)
	// PutNote saves the note, replacing any earlier one; an empty Text
	// deletes it.
	PutNote(ctx context.Context, n Note) error
}

// Note returns the actor's own note on a recipe; an empty Note when there is
// none.
func (s *Service) Note(ctx context.Context, actor households.Membership, recipeID string) (Note, error) {
	if err := authorize(actor); err != nil {
		return Note{}, err
	}
	if err := s.requireRecipe(ctx, actor.HouseholdID, recipeID); err != nil {
		return Note{}, err
	}
	empty := Note{HouseholdID: actor.HouseholdID, RecipeID: recipeID, UserID: actor.UserID}
	if s.notes == nil {
		return empty, nil
	}
	n, err := s.notes.GetNote(ctx, actor.HouseholdID, recipeID, actor.UserID)
	if errors.Is(err, ErrNotFound) {
		return empty, nil
	}
	if err != nil {
		return Note{}, fmt.Errorf("get note: %w", err)
	}
	return n, nil
}

// SaveNote replaces the actor's note on a recipe. Blank text deletes it.
func (s *Service) SaveNote(ctx context.Context, actor households.Membership, recipeID, text string) (Note, error) {
	if err := authorize(actor); err != nil {
		return Note{}, err
	}
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) > MaxNoteLength {
		return Note{}, invalid(fmt.Sprintf("text must be at most %d characters", MaxNoteLength))
	}
	if err := s.requireRecipe(ctx, actor.HouseholdID, recipeID); err != nil {
		return Note{}, err
	}
	n := Note{HouseholdID: actor.HouseholdID, RecipeID: recipeID, UserID: actor.UserID, Text: text, UpdatedAt: s.now().UTC()}
	if s.notes == nil {
		return Note{}, errors.New("ratings: notes are not configured")
	}
	if err := s.notes.PutNote(ctx, n); err != nil {
		return Note{}, fmt.Errorf("save note: %w", err)
	}
	return n, nil
}

// --- HTTP ---------------------------------------------------------------------

// NoteRequest is the body of PUT .../recipes/{recipeId}/note.
type NoteRequest struct {
	Text string `json:"text"`
}

// NoteResponse is the member's own note. updatedAt is null when there is none.
type NoteResponse struct {
	RecipeID  string     `json:"recipeId"`
	Text      string     `json:"text"`
	UpdatedAt *time.Time `json:"updatedAt"`
}

func newNoteResponse(n Note) NoteResponse {
	resp := NoteResponse{RecipeID: n.RecipeID, Text: n.Text}
	if n.Text != "" && !n.UpdatedAt.IsZero() {
		at := n.UpdatedAt
		resp.UpdatedAt = &at
	}
	return resp
}

func (h *Handler) getNote(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	n, err := h.opts.Service.Note(r.Context(), actor, chi.URLParam(r, "recipeId"))
	if h.writeServiceError(w, r, "get recipe note failed", err) {
		return
	}
	httpx.WriteJSON(w, http.StatusOK, newNoteResponse(n))
}

func (h *Handler) putNote(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	var req NoteRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	n, err := h.opts.Service.SaveNote(r.Context(), actor, chi.URLParam(r, "recipeId"), req.Text)
	if h.writeServiceError(w, r, "save recipe note failed", err) {
		return
	}
	httpx.WriteJSON(w, http.StatusOK, newNoteResponse(n))
}

// --- MongoDB ------------------------------------------------------------------

func noteIndexes() mongodb.IndexSet {
	return mongodb.IndexSet{
		Collection: NotesCollection,
		Indexes: []mongo.IndexModel{{
			Keys:    bson.D{{Key: "householdId", Value: 1}, {Key: "recipeId", Value: 1}, {Key: "userId", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("householdId_recipeId_userId_unique"),
		}, {
			// Account deletion removes a user's notes in every household.
			Keys:    bson.D{{Key: "userId", Value: 1}},
			Options: options.Index().SetName("userId"),
		}},
	}
}

type noteDoc struct {
	HouseholdID bson.ObjectID `bson:"householdId"`
	RecipeID    bson.ObjectID `bson:"recipeId"`
	UserID      bson.ObjectID `bson:"userId"`
	Text        string        `bson:"text"`
	UpdatedAt   time.Time     `bson:"updatedAt"`
}

var _ NoteStore = (*MongoStore)(nil)

// GetNote implements NoteStore.
func (s *MongoStore) GetNote(ctx context.Context, householdID, recipeID, userID string) (Note, error) {
	k, err := parseKey(householdID, recipeID, userID)
	if err != nil {
		return Note{}, ErrNotFound
	}
	var d noteDoc
	err = s.notes.FindOne(ctx, bson.D{{Key: "householdId", Value: k.household}, {Key: "recipeId", Value: k.recipe}, {Key: "userId", Value: k.user}}).Decode(&d)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return Note{}, ErrNotFound
	}
	if err != nil {
		return Note{}, err
	}
	return Note{HouseholdID: householdID, RecipeID: recipeID, UserID: userID, Text: d.Text, UpdatedAt: d.UpdatedAt.UTC()}, nil
}

// PutNote implements NoteStore.
func (s *MongoStore) PutNote(ctx context.Context, n Note) error {
	k, err := parseKey(n.HouseholdID, n.RecipeID, n.UserID)
	if err != nil {
		return err
	}
	filter := bson.D{{Key: "householdId", Value: k.household}, {Key: "recipeId", Value: k.recipe}, {Key: "userId", Value: k.user}}
	if n.Text == "" {
		_, err := s.notes.DeleteOne(ctx, filter)
		return err
	}
	_, err = s.notes.UpdateOne(ctx, filter, bson.D{{Key: "$set", Value: bson.D{
		{Key: "text", Value: n.Text}, {Key: "updatedAt", Value: n.UpdatedAt},
	}}}, options.UpdateOne().SetUpsert(true))
	return err
}
