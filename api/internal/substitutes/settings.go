package substitutes

import (
	"slices"
	"strings"
	"time"
)

// This file holds the household-level strategy for specialty ingredients: one
// standing answer ("buy something similar" or "make it as close as possible")
// instead of a decision per ingredient. It is resolved at read time, so
// changing the setting changes future grocery lists and nothing is written as
// a side effect of building one. docs/specialty-ingredients.md explains it.

// Strategy is a household's standing answer for the specialty ingredients
// nobody has chosen an option for.
type Strategy string

// Strategies.
const (
	// StrategySimilar prefers a curated store alternative, falling back to a
	// house-made batch when the specialty ingredient has no store option.
	StrategySimilar Strategy = "similar"
	// StrategyClosest prefers a curated house-made batch, falling back to a
	// store alternative when there is no batch.
	StrategyClosest Strategy = "closest"
	// StrategyAsk applies nothing: a specialty ingredient stays on the list by
	// its own name until a member chooses.
	StrategyAsk Strategy = "ask"
)

// DefaultStrategy applies to a household that never set one.
const DefaultStrategy = StrategySimilar

// Valid reports whether s is a known strategy.
func (s Strategy) Valid() bool {
	return s == StrategySimilar || s == StrategyClosest || s == StrategyAsk
}

// normalizeStrategy validates a strategy from a request.
func normalizeStrategy(v string) (Strategy, error) {
	s := Strategy(strings.ToLower(strings.TrimSpace(v)))
	if !s.Valid() {
		return "", invalid("strategy must be similar, closest, or ask")
	}
	return s, nil
}

// Settings is a household's specialty ingredient settings. UpdatedBy and
// UpdatedAt are zero for a household that never set one.
type Settings struct {
	HouseholdID string
	Strategy    Strategy
	UpdatedBy   string
	UpdatedAt   time.Time
}

// DefaultSettings is what a household gets before it sets anything.
func DefaultSettings(householdID string) Settings {
	return Settings{HouseholdID: householdID, Strategy: DefaultStrategy}
}

// StrategyOption explains one strategy in the words the app shows.
type StrategyOption struct {
	Value       Strategy
	Label       string
	Description string
}

// StrategyOptions describes every strategy, in the order to offer them.
func StrategyOptions() []StrategyOption {
	return []StrategyOption{
		{Value: StrategySimilar, Label: "Something similar",
			Description: "Buy something close from the store — quicker, tastes a little different"},
		{Value: StrategyClosest, Label: "As close as possible",
			Description: "Make a jar you reuse across several meals — more work, closest to the original"},
		{Value: StrategyAsk, Label: "Ask me each time",
			Description: "Leave each specialty ingredient on the list until someone picks an option"},
	}
}

// ChoiceSource says where a specialty ingredient's current plan came from.
type ChoiceSource string

// Choice sources.
const (
	// ChoiceSourceHousehold: a member chose this option (or as_is).
	ChoiceSourceHousehold ChoiceSource = "household"
	// ChoiceSourceStrategy: the household's strategy picked it, and a member
	// can still override it.
	ChoiceSourceStrategy ChoiceSource = "strategy"
	// ChoiceSourceNone: nothing applies, so the ingredient stays on the list.
	ChoiceSourceNone ChoiceSource = "none"
)

// Resolution is what a household currently does about one specialty
// ingredient: an explicit choice, the strategy's pick, or nothing.
type Resolution struct {
	Source ChoiceSource
	// Strategy produced Option; empty unless Source is ChoiceSourceStrategy.
	Strategy Strategy
	// Option is the option to apply, or nil for as_is and for no plan.
	Option *Option
	// AsIs is true when the household chose to keep the ingredient by name.
	AsIs bool
}

// curatedOptionOfType returns the curated option of kind a strategy should
// use: the specialty's default when that is of the right kind, else the first
// one in curated order. Only curated options are considered — a household that
// wrote its own option chooses it explicitly.
func curatedOptionOfType(sp Specialty, kind OptionType) *Option {
	if o, ok := sp.option(sp.DefaultOptionID); ok && o.Type == kind {
		return &o
	}
	for i := range sp.Options {
		if sp.Options[i].Type == kind {
			o := sp.Options[i]
			return &o
		}
	}
	return nil
}

// strategyOption is the option strategy implies for sp, or nil when the
// strategy applies nothing (ask) or sp has no curated option at all.
func strategyOption(sp Specialty, strategy Strategy) *Option {
	var preferred, fallback OptionType
	switch strategy {
	case StrategySimilar:
		preferred, fallback = TypeStoreAlternative, TypeHouseMadeBatch
	case StrategyClosest:
		preferred, fallback = TypeHouseMadeBatch, TypeStoreAlternative
	default:
		return nil
	}
	if o := curatedOptionOfType(sp, preferred); o != nil {
		return o
	}
	return curatedOptionOfType(sp, fallback)
}

// resolve returns what to do about sp. An explicit choice always wins,
// including as_is; the strategy applies only where there is none. A choice of
// an option that no longer exists is treated as no choice, so the strategy
// takes over rather than the line silently staying.
func resolve(sp Specialty, choice *Choice, householdOptions []Option, strategy Strategy) Resolution {
	if choice != nil {
		if choice.OptionID == OptionAsIs {
			return Resolution{Source: ChoiceSourceHousehold, AsIs: true}
		}
		if o, ok := sp.option(choice.OptionID); ok {
			return Resolution{Source: ChoiceSourceHousehold, Option: &o}
		}
		if i := slices.IndexFunc(householdOptions, func(h Option) bool {
			return h.ID == choice.OptionID && h.SpecialtyID == sp.ID
		}); i >= 0 {
			o := householdOptions[i]
			return Resolution{Source: ChoiceSourceHousehold, Option: &o}
		}
	}
	if o := strategyOption(sp, strategy); o != nil {
		return Resolution{Source: ChoiceSourceStrategy, Strategy: strategy, Option: o}
	}
	return Resolution{Source: ChoiceSourceNone}
}
