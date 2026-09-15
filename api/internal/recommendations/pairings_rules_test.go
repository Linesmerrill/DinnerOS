package recommendations

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/events"
)

func isInvalid(err error) bool { return errors.Is(err, ErrInvalid) }

func pastaBreadRule() PairingRule {
	return PairingRule{When: PairingWhen{MealCategories: []string{"pasta"}}, Add: PairingTarget{RecipeID: rBread}, Frequency: PairingAlways}
}

func crackersRule() PairingRule {
	return PairingRule{
		When: PairingWhen{MealCategories: []string{"soup"}},
		Add:  PairingTarget{GroceryItem: &GroceryItem{Name: "Club crackers", Quantity: "1", Unit: "package"}},
	}
}

func TestNormalizePairingRules(t *testing.T) {
	for _, tt := range []struct {
		name  string
		rules []PairingRule
		want  string // "" means valid
	}{
		{name: "a recipe rule", rules: []PairingRule{pastaBreadRule()}},
		{name: "a grocery rule", rules: []PairingRule{crackersRule()}},
		{
			name:  "no condition",
			rules: []PairingRule{{Add: PairingTarget{RecipeID: rBread}}},
			want:  "needs at least one meal category",
		},
		{
			name:  "both targets",
			rules: []PairingRule{{When: PairingWhen{MealCategories: []string{"pasta"}}, Add: PairingTarget{RecipeID: rBread, GroceryItem: &GroceryItem{Name: "Club crackers"}}}},
			want:  "exactly one of recipeId or groceryItem",
		},
		{
			name:  "no target",
			rules: []PairingRule{{When: PairingWhen{MealCategories: []string{"pasta"}}}},
			want:  "exactly one of recipeId or groceryItem",
		},
		{
			name:  "unknown meal category",
			rules: []PairingRule{{When: PairingWhen{MealCategories: []string{"casserole"}}, Add: PairingTarget{RecipeID: rBread}}},
			want:  "must be one of",
		},
		{
			name:  "recipe id that isn't an id",
			rules: []PairingRule{{When: PairingWhen{MealCategories: []string{"pasta"}}, Add: PairingTarget{RecipeID: "garlic-bread"}}},
			want:  "must be a recipe id",
		},
		{
			name:  "unknown frequency",
			rules: []PairingRule{{When: PairingWhen{MealCategories: []string{"pasta"}}, Add: PairingTarget{RecipeID: rBread}, Frequency: "sometimes"}},
			want:  "always or suggest",
		},
		{
			name:  "a unit without a quantity",
			rules: []PairingRule{{When: PairingWhen{MealCategories: []string{"soup"}}, Add: PairingTarget{GroceryItem: &GroceryItem{Name: "Club crackers", Unit: "package"}}}},
			want:  "needs a quantity",
		},
		{
			name:  "an unknown unit",
			rules: []PairingRule{{When: PairingWhen{MealCategories: []string{"soup"}}, Add: PairingTarget{GroceryItem: &GroceryItem{Name: "Club crackers", Quantity: "1", Unit: "sleeve"}}}},
			want:  "unit must be one of",
		},
		{
			name:  "quantity out of range",
			rules: []PairingRule{{When: PairingWhen{MealCategories: []string{"soup"}}, Add: PairingTarget{GroceryItem: &GroceryItem{Name: "Club crackers", Quantity: "200"}}}},
			want:  "at most 99",
		},
		{
			name:  "an empty grocery name",
			rules: []PairingRule{{When: PairingWhen{MealCategories: []string{"soup"}}, Add: PairingTarget{GroceryItem: &GroceryItem{Name: "  "}}}},
			want:  "must be 1 to 60 characters",
		},
		{
			name:  "the same rule twice",
			rules: []PairingRule{pastaBreadRule(), pastaBreadRule()},
			want:  "repeats another rule",
		},
		{
			name:  "an id that isn't a rule id",
			rules: []PairingRule{{ID: "rule-1", When: PairingWhen{MealCategories: []string{"pasta"}}, Add: PairingTarget{RecipeID: rBread}}},
			want:  "must be the id of a saved rule",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizePairingRules(tt.rules)
			switch {
			case tt.want == "" && err != nil:
				t.Fatalf("normalizePairingRules() error = %v", err)
			case tt.want == "":
				for _, r := range got {
					if !hexID.MatchString(r.ID) || r.Frequency == "" {
						t.Errorf("normalized rule = %+v", r)
					}
				}
			case err == nil:
				t.Fatalf("normalizePairingRules() = %+v, want an error about %q", got, tt.want)
			case !isInvalid(err) || !strings.Contains(err.Error(), tt.want):
				t.Errorf("error = %v, want one about %q", err, tt.want)
			}
		})
	}
}

func TestNormalizePairingRulesKeepsIDsAndCanonicalizes(t *testing.T) {
	rules, err := normalizePairingRules([]PairingRule{{
		Label: "  Pasta   night ", When: PairingWhen{Cuisines: []string{"North America"}, Tags: []string{"Comfort Food"}, Proteins: []string{"chicken"}},
		Add: PairingTarget{RecipeID: rBread}, Frequency: "suggest",
	}})
	if err != nil {
		t.Fatal(err)
	}
	r := rules[0]
	if r.Label != "Pasta night" || !slices.Equal(r.When.Cuisines, []string{"north american"}) || !slices.Equal(r.When.Tags, []string{"comfort food"}) {
		t.Fatalf("normalized = %+v", r)
	}
	again, err := normalizePairingRules(rules)
	if err != nil || again[0].ID != r.ID {
		t.Errorf("normalizing again changed the id: %+v, %v", again, err)
	}
	if got := len(func() []PairingRule {
		rules, _ := normalizePairingRules(make([]PairingRule, MaxPairingRules+1))
		return rules
	}()); got != 0 {
		t.Errorf("more than %d rules should be rejected", MaxPairingRules)
	}
}

func TestPairingWhenMatches(t *testing.T) {
	facts := mealFacts{categories: []string{"pasta"}, cuisines: []string{"italian", "southern european"}, tags: []string{"comfort food"}, proteins: []string{"pork"}}
	for _, tt := range []struct {
		name         string
		when         PairingWhen
		want         bool
		wantCategory string
	}{
		{"a category", PairingWhen{MealCategories: []string{"pasta"}}, true, "pasta"},
		{"another category", PairingWhen{MealCategories: []string{"soup"}}, false, ""},
		{"one of two categories", PairingWhen{MealCategories: []string{"soup", "pasta"}}, true, "pasta"},
		{"a cuisine region", PairingWhen{Cuisines: []string{"southern european"}}, true, ""},
		{"every group must match", PairingWhen{MealCategories: []string{"pasta"}, Proteins: []string{"beef"}}, false, ""},
		{"both groups match", PairingWhen{MealCategories: []string{"pasta"}, Proteins: []string{"pork"}}, true, "pasta"},
		{"a tag", PairingWhen{Tags: []string{"comfort food"}}, true, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, category := tt.when.matches(facts)
			if got != tt.want || category != tt.wantCategory {
				t.Errorf("matches() = %v, %q, want %v, %q", got, category, tt.want, tt.wantCategory)
			}
		})
	}
}

func TestProfilePairingsSection(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	rules := []PairingRule{pastaBreadRule(), crackersRule()}

	p, err := env.svc.UpdateProfile(ctx, hhA, userAda, ProfileUpdate{Pairings: &rules}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Pairings) != 2 || p.Pairings[0].Add.RecipeName != "Garlic Bread" {
		t.Fatalf("saved rules = %+v", p.Pairings)
	}
	if _, ok := p.Sections[SectionPairings]; !ok {
		t.Errorf("the pairings section should record who changed it: %+v", p.Sections)
	}
	recorded := env.events.ofType(events.TypeAutopilotPreferencesUpdated)
	change := recorded[len(recorded)-1].Payload.(events.AutopilotPreferencesUpdated)
	if !slices.Contains(change.Sections, string(SectionPairings)) || len(change.Changes) == 0 || len(change.Changes[0].Added) != 2 {
		t.Errorf("change = %+v", change)
	}

	// Replacing the profile (onboarding) keeps the rules it doesn't send.
	replaced, err := env.svc.UpdateProfile(ctx, hhA, userAda, smokerProfile(), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(replaced.Pairings) != 2 {
		t.Errorf("replacing the profile dropped the pairing rules: %+v", replaced.Pairings)
	}

	// A target must be an add-on recipe in the household that can be planned.
	for _, tt := range []struct {
		name     string
		recipeID string
		want     string
	}{
		{"a main meal", rRigatoni, "must be an add-on recipe"},
		{"another household's recipe", rBobs, "recipe not found"},
		{"a missing recipe", "ffffffffffffffffffffffff", "recipe not found"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			bad := []PairingRule{{When: PairingWhen{MealCategories: []string{"pasta"}}, Add: PairingTarget{RecipeID: tt.recipeID}}}
			_, err := env.svc.UpdateProfile(ctx, hhA, userAda, ProfileUpdate{Pairings: &bad}, false)
			if !isInvalid(err) || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want one about %q", err, tt.want)
			}
		})
	}

	// Saving the same rules again changes nothing.
	kept := slices.Clone(replaced.Pairings)
	same, err := env.svc.UpdateProfile(ctx, hhA, userAda, ProfileUpdate{Pairings: &kept}, false)
	if err != nil || same.Version != replaced.Version {
		t.Errorf("saving the same rules = version %d, %v; want %d", same.Version, err, replaced.Version)
	}
}
