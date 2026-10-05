package recipes

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/Linesmerrill/DinnerOS/api/internal/grocery"
	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/httpx"
)

// LeftOutSource says what a household leaves out of one recipe when it cooks
// it. *skips.Service implements it — the same skips the week's grocery list
// holds back, so the steps and the list agree about what goes in.
type LeftOutSource interface {
	RecipeLeftOut(ctx context.Context, householdID, recipeID string) (grocery.LeftOutSet, error)
}

// SpecialtySource resolves the specialty ingredients among a set of
// ingredient lines, with the household's choices already applied.
// *substitutes.Service implements it — the same method the week's grocery
// list uses, so a recipe's instructions and the list can never disagree
// about what the household is cooking with.
type SpecialtySource interface {
	GrocerySpecialties(ctx context.Context, householdID string, lines []grocery.Line) (grocery.Specialties, error)
}

// SwapSource resolves a meal's protein choice for one of the recipe's lines
// (by ingredient key) to the protein cooked and how much of it. ok is false
// for a choice that doesn't apply. *customize.Service implements it.
type SwapSource interface {
	InstructionSwap(r Recipe, servings int, ingredientKey, choiceID string) (Swap, bool)
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
	// LeftOutApplied is false when the server could not consult what the
	// household leaves out, so nothing is marked left out.
	LeftOutApplied bool `json:"leftOutApplied"`
	// Ingredients are the recipe's ingredients in its order, as the household
	// cooks them: the key a skip is made with, whether it's left out, and
	// what a component is made of.
	Ingredients []IngredientStateResponse `json:"ingredients"`
	// Checklist is the cooking screen's ingredient checklist, all together
	// and by step, ready to show.
	Checklist ChecklistResponse `json:"checklist"`
	// SafeTemperatures are USDA safe minimum internal temperatures for the
	// meat and seafood the household cooks, shown under the steps with
	// SafeTemperaturesSource; empty when there's none.
	SafeTemperatures       []SafeTemperatureResponse `json:"safeTemperatures"`
	SafeTemperaturesSource string                    `json:"safeTemperaturesSource"`
}

// SafeTemperatureResponse is one ingredient's safe minimum internal
// temperature: "Chicken Breasts: 165°F".
type SafeTemperatureResponse struct {
	Name        string `json:"name"`
	Fahrenheit  int    `json:"fahrenheit"`
	RestMinutes int    `json:"restMinutes"`
	Text        string `json:"text"`
}

// ChecklistResponse is the cooking screen's ingredient checklist.
type ChecklistResponse struct {
	All    []CookIngredientResponse `json:"all"`
	ByStep []CookStepGroupResponse  `json:"byStep"`
}

// CookIngredientResponse is one row of the all-together checklist.
type CookIngredientResponse struct {
	ID            string             `json:"id"`
	Index         int                `json:"index"`
	Name          string             `json:"name"`
	AmountText    string             `json:"amountText,omitempty"`
	LeftOut       bool               `json:"leftOut,omitempty"`
	IngredientKey string             `json:"ingredientKey"`
	Parts         []CookPartResponse `json:"parts"`
	Components    []string           `json:"components"`
}

// CookPartResponse is one step's share of an ingredient.
type CookPartResponse struct {
	ID         string `json:"id"`
	AmountText string `json:"amountText"`
	StepIndex  int    `json:"stepIndex"`
}

// CookStepGroupResponse is what one step needs; index 0 is "Have Ready".
type CookStepGroupResponse struct {
	Index int                    `json:"index"`
	Items []CookStepItemResponse `json:"items"`
}

// CookStepItemResponse is one ingredient as a step uses it.
type CookStepItemResponse struct {
	ID              string   `json:"id"`
	IngredientIndex int      `json:"ingredientIndex"`
	Name            string   `json:"name"`
	AmountText      string   `json:"amountText,omitempty"`
	Prep            string   `json:"prep,omitempty"`
	Parts           []string `json:"parts"`
	LeftOut         bool     `json:"leftOut,omitempty"`
	IngredientKey   string   `json:"ingredientKey"`
}

// StepTimerResponse is one cooking time in a step.
type StepTimerResponse struct {
	Text         string `json:"text"`
	LowSeconds   int    `json:"lowSeconds"`
	HighSeconds  int    `json:"highSeconds"`
	StartSeconds int    `json:"startSeconds"`
	Subject      string `json:"subject,omitempty"`
}

// IngredientStateResponse is one recipe ingredient as the household cooks it.
type IngredientStateResponse struct {
	Index         int    `json:"index"`
	IngredientKey string `json:"ingredientKey"`
	Name          string `json:"name"`
	// SwapName is the protein cooked instead of this one, when the meal swaps
	// it; omitted otherwise.
	SwapName string `json:"swapName,omitempty"`
	// AmountText is the amount to show for the servings, a packet read as a
	// kitchen measure ("2 Tbsp"); omitted when there is none.
	AmountText string `json:"amountText,omitempty"`
	// LeftOut is set when the household leaves it out of this recipe: by a
	// skip for this recipe (scope recipe) or for every recipe (always).
	LeftOut *LeftOutResponse `json:"leftOut"`
	// Component is set when it is a specialty ingredient the household makes
	// from store ingredients.
	Component *ComponentResponse `json:"component"`
}

// LeftOutResponse is the skip that leaves an ingredient out.
type LeftOutResponse struct {
	SkipID string            `json:"skipId"`
	Scope  grocery.SkipScope `json:"scope"`
}

// ComponentResponse is a specialty ingredient as the household makes it.
type ComponentResponse struct {
	SpecialtyID   string             `json:"specialtyId"`
	SpecialtyName string             `json:"specialtyName"`
	OptionName    string             `json:"optionName"`
	Type          grocery.ChoiceType `json:"type"`
	Parts         []string           `json:"parts"`
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
	// Timers are the step's cooking times in text order, each named for what
	// it's cooking and with the time to start at.
	Timers []StepTimerResponse `json:"timers"`
	// LeftOut is true when every ingredient the step names is left out.
	LeftOut bool `json:"leftOut,omitempty"`
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
	// IngredientIndex is the recipe ingredient's position, on an ingredient
	// segment.
	IngredientIndex *int `json:"ingredientIndex,omitempty"`
	// Name is what the segment stands for, after any substitution.
	Name string `json:"name,omitempty"`
	// Amount is the amount for `servings`, null when the recipe gave none,
	// when an earlier mention in the same step carried it, or when a
	// substitution's amount doesn't convert exactly.
	Amount *InstructionAmountResponse `json:"amount,omitempty"`
	// Part is true when amount is this step's share of the ingredient rather
	// than the recipe's whole amount.
	Part bool `json:"part,omitempty"`
	// Spicy marks an ingredient that brings heat.
	Spicy bool `json:"spicy,omitempty"`
	// Substituted is true when the household's choice changed this mention.
	Substituted bool `json:"substituted,omitempty"`
	// SpecialtyID and SpecialtyName are set when the mention is a specialty
	// ingredient, chosen for or not.
	SpecialtyID   string `json:"specialtyId,omitempty"`
	SpecialtyName string `json:"specialtyName,omitempty"`
	// LeftOut is true when the household leaves this ingredient out of the
	// recipe. The text is the step's own words, without an amount.
	LeftOut bool `json:"leftOut,omitempty"`
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
	// Kind is "substitution" or "left_out".
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
		LeftOutApplied:      in.LeftOutApplied,
		Ingredients:         make([]IngredientStateResponse, 0, len(in.Ingredients)),
	}
	for _, st := range in.Ingredients {
		sr := IngredientStateResponse{Index: st.Index, IngredientKey: st.IngredientKey, Name: st.Name, SwapName: st.SwapName}
		if st.Amount != nil {
			sr.AmountText = st.Amount.Text()
		}
		if lo := st.LeftOut; lo != nil {
			sr.LeftOut = &LeftOutResponse{SkipID: lo.SkipID, Scope: lo.Scope}
		}
		if c := st.Component; c != nil {
			sr.Component = &ComponentResponse{
				SpecialtyID: c.SpecialtyID, SpecialtyName: c.SpecialtyName, OptionName: c.OptionName, Type: c.Type,
				Parts: orEmpty(c.Parts),
			}
		}
		resp.Ingredients = append(resp.Ingredients, sr)
	}
	for _, step := range in.Steps {
		sr := InstructionStepResponse{
			Index: step.Index, Text: step.Text, OriginalText: step.Original, ImageURL: step.ImageURL, LeftOut: step.LeftOut,
			Segments: make([]StepSegmentResponse, 0, len(step.Segments)),
			Notes:    make([]InstructionNoteResponse, 0, len(step.Notes)),
			Timers:   make([]StepTimerResponse, 0, len(step.Timers)),
		}
		for _, t := range step.Timers {
			sr.Timers = append(sr.Timers, StepTimerResponse(t))
		}
		if sr.Text == "" {
			sr.Text = step.Original
		}
		for _, seg := range step.Segments {
			var index *int
			if seg.Kind == SegmentIngredient && seg.Ingredient != NoIngredient {
				index = &seg.Ingredient
			}
			sr.Segments = append(sr.Segments, StepSegmentResponse{
				Kind: string(seg.Kind), Text: seg.Text, IngredientID: seg.IngredientID, IngredientIndex: index, Name: seg.Name,
				Amount: instructionAmount(seg.Amount), Part: seg.Part, Spicy: seg.Spicy, Substituted: seg.Substituted,
				SpecialtyID: seg.SpecialtyID, SpecialtyName: seg.SpecialtyName, LeftOut: seg.LeftOut,
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
	resp.Checklist = checklistResponse(in.Checklist)
	resp.SafeTemperatures = make([]SafeTemperatureResponse, 0, len(in.SafeTemps))
	for _, st := range in.SafeTemps {
		resp.SafeTemperatures = append(resp.SafeTemperatures, SafeTemperatureResponse(st))
	}
	if len(in.SafeTemps) > 0 {
		resp.SafeTemperaturesSource = SafeTempsSource
	}
	return resp
}

func checklistResponse(c Checklist) ChecklistResponse {
	out := ChecklistResponse{All: make([]CookIngredientResponse, 0, len(c.All)), ByStep: make([]CookStepGroupResponse, 0, len(c.ByStep))}
	for _, ci := range c.All {
		r := CookIngredientResponse{
			ID: ci.ID, Index: ci.Index, Name: ci.Name, AmountText: ci.AmountText, LeftOut: ci.LeftOut,
			IngredientKey: ci.IngredientKey, Parts: make([]CookPartResponse, 0, len(ci.Parts)), Components: orEmpty(ci.Components),
		}
		for _, p := range ci.Parts {
			r.Parts = append(r.Parts, CookPartResponse(p))
		}
		out.All = append(out.All, r)
	}
	for _, g := range c.ByStep {
		gr := CookStepGroupResponse{Index: g.Index, Items: make([]CookStepItemResponse, 0, len(g.Items))}
		for _, it := range g.Items {
			gr.Items = append(gr.Items, CookStepItemResponse{
				ID: it.ID, IngredientIndex: it.IngredientIndex, Name: it.Name, AmountText: it.AmountText, Prep: it.Prep,
				Parts: orEmpty(it.Parts), LeftOut: it.LeftOut, IngredientKey: it.IngredientKey,
			})
		}
		out.ByStep = append(out.ByStep, gr)
	}
	return out
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
	leftOut, leftOutApplied, err := h.recipeLeftOut(r.Context(), actor.HouseholdID, recipe.ID)
	if err != nil {
		// Steps that tell a member to add what they asked to leave out are
		// wrong in the same way, so this fails the request too.
		h.internalError(w, r, "load left-out ingredients failed", err)
		return
	}
	swaps := h.mealSwaps(recipe, servings, r.URL.Query()["swap"])
	in := AnnotateMeal(recipe, servings, specs, applied, leftOut, leftOutApplied, swaps)
	h.findings.check(r.Context(), actor.HouseholdID, recipe, in)
	httpx.WriteJSON(w, http.StatusOK, newInstructionsResponse(in))
}

// mealSwaps reads the swap parameters, each "<ingredientKey>=<choiceId>" as
// the meal's customizations list them. One that doesn't apply is ignored: the
// step then reads as the card wrote it.
func (h *Handler) mealSwaps(recipe Recipe, servings int, params []string) Swaps {
	if h.opts.Swaps == nil || len(params) == 0 {
		return nil
	}
	out := Swaps{}
	for _, p := range params {
		key, choice, ok := strings.Cut(p, "=")
		if !ok || key == "" || choice == "" {
			continue
		}
		if sw, ok := h.opts.Swaps.InstructionSwap(recipe, servings, key, choice); ok {
			out[key] = sw
		}
	}
	return out
}

// recipeLeftOut loads what the household leaves out of the recipe. applied is
// false when no source is configured.
func (h *Handler) recipeLeftOut(ctx context.Context, householdID, recipeID string) (grocery.LeftOutSet, bool, error) {
	if h.opts.LeftOut == nil {
		return nil, false, nil
	}
	set, err := h.opts.LeftOut.RecipeLeftOut(ctx, householdID, recipeID)
	if err != nil {
		return nil, false, err
	}
	return set, true, nil
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
