package ratings

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// ErrNotFound means the recipe is not one of the household's recipes.
var ErrNotFound = errors.New("ratings: not found")

// ValidationError describes invalid input. Its message is safe to return to
// API clients.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

func invalid(msg string) error { return &ValidationError{Message: msg} }

// Rating limits.
const (
	MinScore         = 1
	MaxScore         = 5
	MaxCommentLength = 500
)

// Tag is a structured reaction to a recipe. Only the Tags allowlist is
// accepted, so the recommender can rely on a closed vocabulary; anything else
// belongs in the free-text comment.
type Tag string

// Allowed tags.
const (
	TagMakeAgain      Tag = "make-again"
	TagNeverAgain     Tag = "never-again"
	TagKidFavorite    Tag = "kid-favorite"
	TagKidsDisliked   Tag = "kids-disliked"
	TagTooSpicy       Tag = "too-spicy"
	TagTooBland       Tag = "too-bland"
	TagTooMuchWork    Tag = "too-much-work"
	TagGreatLeftovers Tag = "great-leftovers"
)

var allTags = []Tag{
	TagMakeAgain, TagNeverAgain, TagKidFavorite, TagKidsDisliked,
	TagTooSpicy, TagTooBland, TagTooMuchWork, TagGreatLeftovers,
}

// Tags returns the allowed tags in their canonical order.
func Tags() []Tag { return slices.Clone(allTags) }

// Rating is one member's rating of one recipe in a household.
type Rating struct {
	ID          string
	HouseholdID string
	RecipeID    string
	UserID      string
	Score       int
	Comment     string
	// Tags are distinct and in canonical (Tags) order.
	Tags      []Tag
	CreatedAt time.Time
	UpdatedAt time.Time
}

// MemberRating is a rating with the rater's display name.
type MemberRating struct {
	Rating
	// DisplayName may be empty when the user has none.
	DisplayName string
}

// Summary aggregates a recipe's ratings in a household.
type Summary struct {
	Count int
	Sum   int
	// Mine is the requesting user's own rating, if any.
	Mine *Rating
}

// Average returns the mean score rounded to two decimals. ok is false when
// there are no ratings.
func (s Summary) Average() (avg float64, ok bool) {
	if s.Count == 0 {
		return 0, false
	}
	return math.Round(float64(s.Sum)/float64(s.Count)*100) / 100, true
}

// RateInput is a member's rating as submitted.
type RateInput struct {
	Score   int
	Comment string
	Tags    []string
}

type validInput struct {
	score   int
	comment string
	tags    []Tag
}

// validate trims the comment and returns tags distinct and in canonical order.
func (in RateInput) validate() (validInput, error) {
	if in.Score < MinScore || in.Score > MaxScore {
		return validInput{}, invalid(fmt.Sprintf("score must be between %d and %d", MinScore, MaxScore))
	}
	comment := strings.TrimSpace(in.Comment)
	if !utf8.ValidString(comment) || utf8.RuneCountInString(comment) > MaxCommentLength {
		return validInput{}, invalid(fmt.Sprintf("comment must be at most %d characters", MaxCommentLength))
	}
	var tags []Tag
	for _, raw := range in.Tags {
		tag := Tag(strings.TrimSpace(raw))
		if !slices.Contains(allTags, tag) {
			return validInput{}, invalid(fmt.Sprintf("tag %q is not allowed; tags must be from: %s", raw, joinTags(allTags)))
		}
		if !slices.Contains(tags, tag) {
			tags = append(tags, tag)
		}
	}
	if slices.Contains(tags, TagMakeAgain) && slices.Contains(tags, TagNeverAgain) {
		return validInput{}, invalid("tags make-again and never-again can't be used together")
	}
	slices.SortFunc(tags, func(a, b Tag) int { return slices.Index(allTags, a) - slices.Index(allTags, b) })
	return validInput{score: in.Score, comment: comment, tags: tags}, nil
}

func joinTags(tags []Tag) string {
	s := make([]string, 0, len(tags))
	for _, t := range tags {
		s = append(s, string(t))
	}
	return strings.Join(s, ", ")
}
