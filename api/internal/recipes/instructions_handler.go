package recipes

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

// SpecialtySource resolves the specialty ingredients among a set of
// ingredient lines, with the household's choices already applied.
// *substitutes.Service implements it — the same method the week's grocery
// list uses, so a recipe's instructions and the list can never disagree
// about what the household is cooking with.
type SpecialtySource interface {
	GrocerySpecialties(ctx context.Context, householdID string, lines []grocery.Line) (grocery.Specialties, error)
}

// --- Wire types ---------------------------------------------------------------

// InstructionsResponse is returned by
// GET /households/{householdId}/recipes/{recipeId}/instructions.
type InstructionsResponse struct {
	RecipeID   string `json:"recipeId"`
	RecipeName string `json:"recipeName"`
	// Servings is the serving size every amount below is for.
	Servings       int   `json:"servings"`
	ServingOptions []int `json:"servingOptions"`
	// SpecialtiesApplied is false when the server could not consult the
	// household's specialty ingredient choices, so the steps read as the
	// recipe was written.
	SpecialtiesApplied bool                      `json:"specialtiesApplied"`
	Steps              []InstructionStepResponse `json:"steps"`
	// Substitutions are the specialty ingredients that read differently
	// because of the household's choices.
	Substitutions []SubstitutionResponse `json:"substitutions"`
	// UnchosenSpecialties are the specialty ingredients this recipe uses that
	// the household hasn't decided about. They read as the card wrote them.
	UnchosenSpecialties []SpecialtyRefResponse `json:"unchosenSpecialties"`
}

// InstructionStepResponse is one step ready to read.
type InstructionStepResponse struct {
	Index int `json:"index"`
	// Text is the whole step, and equals every segment's text joined in order.
	Text string `json:"text"`
	// OriginalText is the recipe's own wording, present only when a
	// substitution changed it.
	OriginalText string                    `json:"originalText,omitempty"`
	ImageURL     string                    `json:"imageUrl,omitempty"`
	Segments     []StepSegmentResponse     `json:"segments"`
	Notes        []InstructionNoteResponse `json:"notes"`
}

// StepSegmentResponse is one run of a step: plain text, or an ingredient the
// recipe lists. Clients render `ingredient` segments however they like —
// DinnerOS for iOS shows them bold, and spicy ones in red with a flame.
type StepSegmentResponse struct {
	// Kind is "text" or "ingredient".
	Kind string `json:"kind"`
	Text string `json:"text"`
	// IngredientID is the catalog ingredient, empty when the catalog doesn't
	// know it or on a text segment.
	IngredientID string `json:"ingredientId,omitempty"`
	// Name is what the segment stands for, after any substitution.
	Name string `json:"name,omitempty"`
	// Amount is the amount for `servings`, null when the recipe gave none,
	// when an earlier mention in the same step carried it, or when a
	// substitution's amount doesn't convert exactly.
	Amount *InstructionAmountResponse `json:"amount,omitempty"`
	// Spicy marks an ingredient that brings heat.
	Spicy bool `json:"spicy,omitempty"`
	// Substituted is true when the household's choice changed this mention.
	Substituted bool `json:"substituted,omitempty"`
	// SpecialtyID and SpecialtyName are set when the mention is a specialty
	// ingredient, chosen for or not.
	SpecialtyID   string `json:"specialtyId,omitempty"`
	SpecialtyName string `json:"specialtyName,omitempty"`
}

// InstructionAmountResponse is an amount. Quantity is exact ("1/2"); use
// QuantityValue only for arithmetic and Text for display.
type InstructionAmountResponse struct {
	Quantity      string  `json:"quantity"`
	QuantityValue float64 `json:"quantityValue"`
	Unit          string  `json:"unit"`
	Text          string  `json:"text"`
}

// InstructionNoteResponse is a sentence shown under a step when a substitution
// changes the amount or the method.
type InstructionNoteResponse struct {
	// Kind is "substitution".
	Kind        string `json:"kind"`
	SpecialtyID string `json:"specialtyId,omitempty"`
	Text        string `json:"text"`
}

// SubstitutionResponse is one specialty ingredient the household replaced.
type SubstitutionResponse struct {
	SpecialtyID   string `json:"specialtyId"`
	SpecialtyKey  string `json:"specialtyKey"`
	SpecialtyName string `json:"specialtyName"`
	OptionID      string `json:"optionId"`
	OptionName    string `json:"optionName"`
	// Type is store_alternative or house_made_batch.
	Type string `json:"type"`
	// Source is household when a member chose it, strategy when the
	// household's standing strategy did.
	Source string `json:"source"`
	Text   string `json:"text"`
}

// SpecialtyRefResponse names a specialty ingredient.
type SpecialtyRefResponse struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Name string `json:"name"`
}

func newInstructionsResponse(in Instructions) InstructionsResponse {
	resp := InstructionsResponse{
		RecipeID: in.RecipeID, RecipeName: in.RecipeName, Servings: in.Servings,
		ServingOptions: orEmpty(in.ServingOptions), SpecialtiesApplied: in.SpecialtiesApplied,
		Steps:               make([]InstructionStepResponse, 0, len(in.Steps)),
		Substitutions:       make([]SubstitutionResponse, 0, len(in.Substitutions)),
		UnchosenSpecialties: make([]SpecialtyRefResponse, 0, len(in.Unchosen)),
	}
	for _, step := range in.Steps {
		sr := InstructionStepResponse{
			Index: step.Index, Text: step.Text, OriginalText: step.Original, ImageURL: step.ImageURL,
			Segments: make([]StepSegmentResponse, 0, len(step.Segments)),
			Notes:    make([]InstructionNoteResponse, 0, len(step.Notes)),
		}
		if sr.Text == "" {
			sr.Text = step.Original
		}
		for _, seg := range step.Segments {
			sr.Segments = append(sr.Segments, StepSegmentResponse{
				Kind: string(seg.Kind), Text: seg.Text, IngredientID: seg.IngredientID, Name: seg.Name,
				Amount: instructionAmount(seg.Amount), Spicy: seg.Spicy, Substituted: seg.Substituted,
				SpecialtyID: seg.SpecialtyID, SpecialtyName: seg.SpecialtyName,
			})
		}
		for _, n := range step.Notes {
			sr.Notes = append(sr.Notes, InstructionNoteResponse{Kind: string(n.Kind), SpecialtyID: n.SpecialtyID, Text: n.Text})
		}
		resp.Steps = append(resp.Steps, sr)
	}
	for _, s := range in.Substitutions {
		resp.Substitutions = append(resp.Substitutions, SubstitutionResponse{
			SpecialtyID: s.ID, SpecialtyKey: s.Key, SpecialtyName: s.Name,
			OptionID: s.OptionID, OptionName: s.OptionName, Type: string(s.Type), Source: s.Source, Text: s.Text,
		})
	}
	for _, s := range in.Unchosen {
		resp.UnchosenSpecialties = append(resp.UnchosenSpecialties, SpecialtyRefResponse(s))
	}
	return resp
}

func instructionAmount(m *Measure) *InstructionAmountResponse {
	if m == nil {
		return nil
	}
	return &InstructionAmountResponse{
		Quantity: m.Quantity.String(), QuantityValue: m.Quantity.Float64(), Unit: m.Unit, Text: m.Text(),
	}
}

// --- Handler ------------------------------------------------------------------

// instructions renders a recipe's steps for one serving size, with the
// household's specialty ingredient choices applied.
func (h *Handler) instructions(w http.ResponseWriter, r *http.Request) {
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
		// Reading a step that names the wrong ingredient is worse than an
		// error, so a failed lookup fails the request (decision 58).
		h.internalError(w, r, "load specialty choices failed", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, newInstructionsResponse(Annotate(recipe, servings, specs, applied)))
}

// instructionServings picks the serving size to render. Amounts are never
// scaled from another size, so only a size the recipe was authored at is
// accepted; without the parameter, the smallest one is used.
func instructionServings(r Recipe, param string) (int, bool) {
	if param == "" {
		if len(r.Servings) == 0 {
			return 0, true
		}
		return slices.Min(r.Servings), true
	}
	n, err := strconv.Atoi(param)
	if err != nil || n < 1 {
		return 0, false
	}
	if len(r.Servings) > 0 && !slices.Contains(r.Servings, n) {
		return 0, false
	}
	return n, true
}

// recipeSpecialties resolves the recipe's specialty ingredients with the
// household's choices. applied is false when no source is configured.
func (h *Handler) recipeSpecialties(ctx context.Context, householdID string, recipe Recipe, servings int) (grocery.Specialties, bool, error) {
	if h.opts.Specialties == nil {
		return nil, false, nil
	}
	specs, err := h.opts.Specialties.GrocerySpecialties(ctx, householdID, GroceryLines(recipe, servings))
	if err != nil {
		return nil, false, err
	}
	return specs, true, nil
}
