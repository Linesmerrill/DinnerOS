package recipes

import (
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

// After a rating under five the app asks what could have been better: a few
// kinds of problem (taste, texture, time, portion), each with its choices, and
// the recipe's ingredients as the household cooked them, each with what was
// wrong and, for something made from parts, which part. The answers are saved
// on the rating (ratings.Miss) and teach Autopilot. The questions come from
// here so their wording can change without an app update.

// RatingQuestionsResponse is GET .../recipes/{recipeId}/rating-questions.
type RatingQuestionsResponse struct {
	Title string `json:"title"`
	// Categories are kinds of problem; picking one shows its options. The
	// "ingredients" category has no options: it shows Ingredients.
	Categories []RatingCategoryResponse `json:"categories"`
	// Ingredients are the recipe's main ingredients as cooked: protein swaps
	// read as the swap, left-out ones and seasonings aren't listed.
	Ingredients []RatingIngredientResponse `json:"ingredients"`
	// Reasons are what can be wrong with one ingredient.
	Reasons []RatingReasonResponse `json:"reasons"`
}

// RatingCategoryResponse is one kind of problem.
type RatingCategoryResponse struct {
	Code    string                 `json:"code"`
	Title   string                 `json:"title"`
	Symbol  string                 `json:"symbol"`
	Options []RatingOptionResponse `json:"options"`
}

// RatingOptionResponse is one choice: the rating tag it saves.
type RatingOptionResponse struct {
	Tag   string `json:"tag"`
	Title string `json:"title"`
}

// RatingIngredientResponse is one ingredient to ask about.
type RatingIngredientResponse struct {
	IngredientKey string `json:"ingredientKey"`
	Name          string `json:"name"`
	// Parts are what a made ingredient is made of ("Onions"), to narrow it.
	Parts []string `json:"parts"`
}

// RatingReasonResponse is one thing that can be wrong with an ingredient.
type RatingReasonResponse struct {
	Reason string `json:"reason"`
	Title  string `json:"title"`
	// Detail says what saving it does, or "".
	Detail string `json:"detail"`
}

var ratingCategories = []RatingCategoryResponse{
	{Code: "taste", Title: "Taste", Symbol: "fork.knife", Options: []RatingOptionResponse{
		{Tag: "too-salty", Title: "Too salty"}, {Tag: "too-sweet", Title: "Too sweet"},
		{Tag: "too-spicy", Title: "Too spicy"}, {Tag: "too-bland", Title: "Too bland"},
	}},
	{Code: "ingredients", Title: "An Ingredient", Symbol: "carrot", Options: []RatingOptionResponse{}},
	{Code: "texture", Title: "Texture", Symbol: "hand.raised", Options: []RatingOptionResponse{
		{Tag: "too-dry", Title: "Too dry"}, {Tag: "too-soggy", Title: "Too soggy"},
	}},
	{Code: "time", Title: "Time", Symbol: "clock", Options: []RatingOptionResponse{
		{Tag: "took-too-long", Title: "Took longer than it said"}, {Tag: "too-much-work", Title: "Too much work"},
	}},
	{Code: "portion", Title: "Portion", Symbol: "chart.pie", Options: []RatingOptionResponse{
		{Tag: "portion-too-small", Title: "Not enough food"}, {Tag: "portion-too-big", Title: "Too much food"},
	}},
}

var ratingReasons = []RatingReasonResponse{
	{Reason: "didnt-like", Title: "Didn't like it", Detail: "Autopilot will suggest fewer meals with it."},
	{Reason: "too-much", Title: "Too much", Detail: ""},
	{Reason: "too-little", Title: "Not enough", Detail: ""},
	{Reason: "product", Title: "The product I bought", Detail: "We'll ask you to pick a different product next time."},
}

// notAskedAbout are pantry basics nobody rates a meal on.
var notAskedAbout = regexp.MustCompile(`(?i)^(kosher\s+|sea\s+)?(salt|pepper|black pepper|water|oil|olive oil|cooking oil|vegetable oil|sugar|butter)$`)

// leadingAmountRe is a part's amount ("2 tsp ") before its name.
var leadingAmountRe = regexp.MustCompile(`^[\d½¼¾⅓⅔⅛⅜⅝⅞ ./-]+\s*(?:tsp|tbsp|cups?|oz|lbs?|g|ml|cloves?|pinch(?:es)?)?\.?\s+`)

// ratingQuestions builds the questions for a rendered recipe.
func ratingQuestions(in Instructions) RatingQuestionsResponse {
	resp := RatingQuestionsResponse{
		Title: "What could have been better?", Categories: ratingCategories,
		Ingredients: []RatingIngredientResponse{}, Reasons: ratingReasons,
	}
	seen := map[string]bool{}
	for _, ing := range in.Ingredients {
		name := strings.TrimSpace(ing.Name)
		if ing.SwapName != "" {
			name = ing.SwapName
		}
		if ing.LeftOut != nil || name == "" || notAskedAbout.MatchString(name) || seen[strings.ToLower(name)] {
			continue
		}
		seen[strings.ToLower(name)] = true
		item := RatingIngredientResponse{IngredientKey: ing.IngredientKey, Name: titleWords(name), Parts: []string{}}
		if ing.Component != nil {
			for _, part := range ing.Component.Parts {
				if p := strings.TrimSpace(leadingAmountRe.ReplaceAllString(part, "")); p != "" {
					item.Parts = append(item.Parts, titleWords(p))
				}
			}
		}
		resp.Ingredients = append(resp.Ingredients, item)
	}
	return resp
}

func (h *Handler) ratingQuestions(w http.ResponseWriter, r *http.Request) {
	actor, _ := households.MembershipFromContext(r.Context())
	recipe, err := h.opts.Service.Get(r.Context(), actor.HouseholdID, chi.URLParam(r, "recipeId"))
	switch {
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, "not_found", "recipe not found")
		return
	case err != nil:
		h.internalError(w, r, "get recipe failed", err)
		return
	}
	servings, ok := instructionServings(recipe, r.URL.Query().Get("servings"))
	if !ok {
		validationFailed(w, r, "servings must be one of the recipe's serving sizes")
		return
	}
	specs, applied, err := h.recipeSpecialties(r.Context(), actor.HouseholdID, recipe, servings)
	if err != nil {
		h.internalError(w, r, "load specialty choices failed", err)
		return
	}
	leftOut, leftOutApplied, err := h.recipeLeftOut(r.Context(), actor.HouseholdID, recipe.ID)
	if err != nil {
		h.internalError(w, r, "load left-out ingredients failed", err)
		return
	}
	in := AnnotateMeal(recipe, servings, specs, applied, leftOut, leftOutApplied, h.mealSwaps(recipe, servings, r.URL.Query()["swap"]))
	httpx.WriteJSON(w, http.StatusOK, ratingQuestions(in))
}
