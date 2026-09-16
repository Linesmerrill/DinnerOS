package recommendations

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/autopilot"
	"github.com/Linesmerrill/DinnerOS/api/internal/events"
	"github.com/Linesmerrill/DinnerOS/api/internal/planning"
)

// What Autopilot learned (docs/autopilot.md#learning-from-feedback).
//
// Learned adjustments aren't stored: the provider recomputes them from events
// on every request, which costs one indexed events read. The only state is
// the reset, kept as an autopilot.learning_reset event.

// MaxLearnedAdjustments bounds the learned adjustments listed.
const MaxLearnedAdjustments = 20

// ErrLearningUnsupported means the provider doesn't report what it learned.
var ErrLearningUnsupported = errors.New("recommendations: the recommendation provider doesn't report learning")

// LearnedAdjustment is one thing Autopilot learned, labeled for display.
type LearnedAdjustment struct {
	// Kind is item, busySkips, cuisine, protein, mealCategory, or timeBand.
	Kind string
	Key  string
	// Label names the recipe or attribute ("Beef Tacos", "Mexican").
	Label string
	// RecipeID is set for the item and busySkips kinds.
	RecipeID string
	Value    float64
	Text     string
	Evidence int
}

// Learning is what Autopilot learned from a household's feedback.
type Learning struct {
	ModelVersion string
	// Interactions is how many feedback interactions were considered.
	Interactions int
	Adjustments  []LearnedAdjustment
	// ResetAt and ResetBy are the last reset within the learning window.
	ResetAt time.Time
	ResetBy string
}

// Learning returns the strongest adjustments Autopilot learned from the
// household's feedback, as of the current week.
func (s *Service) Learning(ctx context.Context, householdID string) (Learning, error) {
	reporter, ok := s.provider.(autopilot.LearningReporter)
	if !ok {
		return Learning{}, ErrLearningUnsupported
	}
	household, err := s.households.GetHousehold(ctx, householdID)
	if err != nil {
		return Learning{}, fmt.Errorf("load household: %w", err)
	}
	loc, err := time.LoadLocation(household.TimeZone)
	if err != nil {
		loc = time.UTC
	}
	w := planning.WeekOf(s.now().In(loc))
	profile, err := s.Profile(ctx, householdID)
	if err != nil {
		return Learning{}, err
	}
	in, data, err := s.buildInput(ctx, householdID, w, profile, WeekContext{}, planning.Plan{}, DeviceSignals{})
	if err != nil {
		return Learning{}, err
	}
	res, err := reporter.Learning(ctx, in)
	if err != nil {
		return Learning{}, fmt.Errorf("learning: %w", err)
	}
	out := Learning{ModelVersion: res.ModelVersion, Interactions: res.Interactions, ResetAt: data.resetAt, ResetBy: data.resetBy}
	for _, a := range res.Adjustments {
		if len(out.Adjustments) == MaxLearnedAdjustments {
			break
		}
		la := LearnedAdjustment{Kind: a.Kind, Key: a.Key, Value: a.Value, Text: a.Text, Evidence: a.Evidence}
		switch a.Kind {
		case autopilot.LearnedItem, autopilot.LearnedBusySkips:
			r, ok := data.byID[a.Key]
			if !ok {
				continue
			}
			la.RecipeID, la.Label = r.ID, r.Name
		case autopilot.LearnedCuisine:
			la.Label = cuisineLabel(a.Key)
		case autopilot.LearnedProtein:
			la.Label = optionLabel(ProteinOptions, a.Key)
		case autopilot.LearnedMealCategory:
			la.Label = optionLabel(MealCategoryOptions, a.Key)
		case autopilot.LearnedTimeBand:
			la.Label = map[string]string{"quick": "Quick meals", "medium": "Medium-length cooks", "long": "Long cooks"}[a.Key]
		default:
			la.Label = a.Key
		}
		out.Adjustments = append(out.Adjustments, la)
	}
	return out, nil
}

// ResetLearning clears what Autopilot learned: feedback before now no longer
// adjusts suggestions, while ratings, history, and preferences keep working
// as before. The reset is recorded as an autopilot.learning_reset event; a
// failure to record it fails the reset.
func (s *Service) ResetLearning(ctx context.Context, householdID, userID string) (Learning, error) {
	if err := required(householdID, userID); err != nil {
		return Learning{}, err
	}
	current, err := s.Learning(ctx, householdID)
	if err != nil {
		return Learning{}, err
	}
	now := s.now().UTC()
	if err := s.events.Record(ctx, events.Event{
		HouseholdID: householdID, UserID: userID, Type: events.TypeAutopilotLearningReset, OccurredAt: now,
		Payload: events.AutopilotLearningReset{Adjustments: len(current.Adjustments)},
	}); err != nil {
		return Learning{}, fmt.Errorf("record learning reset: %w", err)
	}
	return s.Learning(ctx, householdID)
}
