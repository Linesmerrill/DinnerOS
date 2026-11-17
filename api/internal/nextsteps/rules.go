package nextsteps

import (
	"context"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
	"github.com/Linesmerrill/DinnerOS/api/internal/plus"
	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
	"github.com/Linesmerrill/DinnerOS/api/internal/recommendations"
)

// Row keys: the stable names of the tabs and rows a path badges. The app maps
// each to one element; a new rule needs a new key only for a row the app
// hasn't badged before.
const (
	RowHousehold            = "household"
	RowPlanning             = "planning"
	RowAutopilotPreferences = "autopilotPreferences"
	RowImportReview         = "importReview"
	RowGiveFreeMonth        = "giveFreeMonth"
)

// Step IDs, one per rule. The app clears a step's dots by ID the moment the
// member finishes it there.
const (
	StepAutopilotSetup = "autopilotSetup"
	StepImportReview   = "importReview"
	StepFreeMonth      = "freeMonthSteps"
)

// Sources is what the rules read. Each is a service the server already has.
type Sources struct {
	Plus      PlusSource
	Autopilot AutopilotSource
	Imports   ImportReviewSource
}

// PlusSource is the household's Plus and the free month it is working on.
type PlusSource interface {
	// HasPlus is true when Plus features are open to the household (always,
	// while Plus is switched off).
	HasPlus(ctx context.Context, householdID string) (bool, error)
	// ReferralProgress is nil unless the household entered another's code.
	ReferralProgress(ctx context.Context, householdID string) (*plus.Progress, error)
}

// AutopilotSource is the household's Autopilot profile.
type AutopilotSource interface {
	Profile(ctx context.Context, householdID string) (recommendations.Profile, error)
}

// ImportReviewSource lists the household's import review items.
type ImportReviewSource interface {
	ImportReviews(ctx context.Context, householdID, status string, limit int) ([]recipes.ReviewRecord, error)
}

// Registry is every rule, in the order the app lists them. Adding a rule is
// one entry here: what makes it pending, the rows it badges from the tab down,
// who can act on it, and (in its comment) what clears it.
func Registry(s Sources) []Rule {
	var rules []Rule
	if s.Plus != nil && s.Autopilot != nil {
		// Autopilot is open to the household but has never been set up.
		// Clears when a planner finishes setup or saves any preference (the
		// profile then has a version, which is what "configured" means).
		rules = append(rules, Rule{
			ID:         StepAutopilotSetup,
			Path:       []string{RowHousehold, RowPlanning, RowAutopilotPreferences},
			Permission: households.PermPlanEdit,
			Pending: func(ctx context.Context, householdID string) (bool, error) {
				ok, err := s.Plus.HasPlus(ctx, householdID)
				if err != nil || !ok {
					return false, err
				}
				p, err := s.Autopilot.Profile(ctx, householdID)
				return !p.Configured(), err
			},
		})
	}
	if s.Imports != nil {
		// Import Review has items waiting. Clears when the last one is
		// resolved.
		rules = append(rules, Rule{
			ID:         StepImportReview,
			Path:       []string{RowHousehold, RowPlanning, RowImportReview},
			Permission: households.PermRecipesImport,
			Pending: func(ctx context.Context, householdID string) (bool, error) {
				items, err := s.Imports.ImportReviews(ctx, householdID, recipes.ReviewStatusOpen, 1)
				return len(items) > 0, err
			},
		})
	}
	if s.Plus != nil {
		// The household entered another's code and its free month still has
		// steps left. Clears when the last step is done (the month is then
		// on, or waiting on checks the household can't speed up) or the
		// invite expires.
		rules = append(rules, Rule{
			ID:         StepFreeMonth,
			Path:       []string{RowHousehold, RowGiveFreeMonth},
			Permission: households.PermPlanEdit,
			Pending: func(ctx context.Context, householdID string) (bool, error) {
				p, err := s.Plus.ReferralProgress(ctx, householdID)
				if err != nil || p == nil || p.State != string(plus.StatePending) {
					return false, err
				}
				for _, step := range p.Steps {
					if !step.Done {
						return true, nil
					}
				}
				return false, nil
			},
		})
	}
	return rules
}
