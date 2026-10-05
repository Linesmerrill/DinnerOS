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
	// What could have been better, asked after a rating under five.
	TagTooSalty        Tag = "too-salty"
	TagTooSweet        Tag = "too-sweet"
	TagTookTooLong     Tag = "took-too-long"
	TagTooDry          Tag = "too-dry"
	TagTooSoggy        Tag = "too-soggy"
	TagPortionTooSmall Tag = "portion-too-small"
	TagPortionTooBig   Tag = "portion-too-big"
)

var allTags = []Tag{
	TagMakeAgain, TagNeverAgain, TagKidFavorite, TagKidsDisliked,
	TagTooSpicy, TagTooBland, TagTooMuchWork, TagGreatLeftovers,
	TagTooSalty, TagTooSweet, TagTookTooLong, TagTooDry, TagTooSoggy, TagPortionTooSmall, TagPortionTooBig,
}

// MissReason is what was wrong with one ingredient.
type MissReason string

// Miss reasons.
const (
	// MissDidntLike: the member doesn't like the ingredient; Autopilot leans
	// away from other meals with it.
	MissDidntLike MissReason = "didnt-like"
	MissTooMuch   MissReason = "too-much"
	MissTooLittle MissReason = "too-little"
	// MissProduct: the ingredient was fine, the product bought for it wasn't
	// ("pre-cut sprouts were dry"); the saved product gets flagged to pick a
	// different one, and the recipe isn't held against it.
	MissProduct MissReason = "product"
)

var missReasons = []MissReason{MissDidntLike, MissTooMuch, MissTooLittle, MissProduct}

// MaxMisses bounds the ingredients one rating can name.
const MaxMisses = 20

// Miss is one ingredient that didn't work, optionally one part of it ("the
// onions" in the sauce).
type Miss struct {
	// IngredientKey is the recipe line's key: a catalog ID or "name:" and
	// the normalized name.
	IngredientKey string
	Name          string
	// Part is the piece of a made ingredient that was the problem, or "".
	Part   string
	Reason MissReason
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
	Tags []Tag
	// Misses are the ingredients that didn't work, from the follow-up to a
	// rating under five.
	Misses    []Miss
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
	// Misses nil keeps the saved ones (an app that doesn't ask sends none);
	// empty clears them.
	Misses *[]Miss
}

type validInput struct {
	score   int
	comment string
	tags    []Tag
	misses  *[]Miss
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
	v := validInput{score: in.Score, comment: comment, tags: tags}
	if in.Misses != nil {
		misses, err := validMisses(*in.Misses)
		if err != nil {
			return validInput{}, err
		}
		v.misses = &misses
	}
	return v, nil
}

func validMisses(in []Miss) ([]Miss, error) {
	if len(in) > MaxMisses {
		return nil, invalid(fmt.Sprintf("at most %d ingredients can be named", MaxMisses))
	}
	out := []Miss{}
	for _, m := range in {
		m.IngredientKey, m.Name, m.Part = strings.TrimSpace(m.IngredientKey), strings.TrimSpace(m.Name), strings.TrimSpace(m.Part)
		if m.Name == "" || utf8.RuneCountInString(m.Name) > 200 || utf8.RuneCountInString(m.Part) > 200 || len(m.IngredientKey) > 300 {
			return nil, invalid("each ingredient needs a name of at most 200 characters")
		}
		if !slices.Contains(missReasons, m.Reason) {
			return nil, invalid(fmt.Sprintf("reason %q is not one of didnt-like, too-much, too-little, product", m.Reason))
		}
		if !slices.ContainsFunc(out, func(o Miss) bool { return o == m }) {
			out = append(out, m)
		}
	}
	return out, nil
}

func joinTags(tags []Tag) string {
	s := make([]string, 0, len(tags))
	for _, t := range tags {
		s = append(s, string(t))
	}
	return strings.Join(s, ", ")
}
