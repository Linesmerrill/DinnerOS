package menu

import (
	"context"
	"fmt"
	"slices"

	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
	"github.com/Linesmerrill/DinnerOS/api/internal/recommendations"
)

// FilterOption is a chip with how many main meals it matches.
type FilterOption struct {
	Value string
	Label string
	Count int
}

// SortOption is a sort chip.
type SortOption struct {
	Value Sort
	Label string
}

// Filters are the All Meals chips.
type Filters struct {
	Proteins   []FilterOption
	Cuisines   []FilterOption
	Tags       []FilterOption
	MaxMinutes []int
	Sorts      []SortOption
}

// MaxMinutesOptions are the cook-time chips.
var MaxMinutesOptions = []int{15, 20, 30, 45}

// SortOptions are the sort chips in display order.
var SortOptions = []SortOption{
	{Value: SortRecommended, Label: "Recommended"},
	{Value: SortPopular, Label: "Most Ordered"},
	{Value: SortRecent, Label: "Recently Ordered"},
	{Value: SortQuick, Label: "Quickest"},
	{Value: SortName, Label: "A–Z"},
}

// Filters returns chip options with counts over the household's main meals.
// Values and counts come from the Autopilot vocabulary, so a chip's count is
// how many main meals the matching list filter returns.
func (s *Service) Filters(ctx context.Context, householdID string) (Filters, error) {
	catalog, err := s.opts.Recipes.MenuCatalog(ctx, householdID)
	if err != nil {
		return Filters{}, fmt.Errorf("load catalog: %w", err)
	}
	return filtersOf(catalog), nil
}

func filtersOf(catalog []recipes.Recipe) Filters {
	v := recommendations.CatalogVocabulary(catalog)
	f := Filters{
		Proteins: counted(v.Proteins), Cuisines: counted(v.Cuisines), Tags: counted(v.Tags),
		MaxMinutes: slices.Clone(MaxMinutesOptions), Sorts: slices.Clone(SortOptions),
	}
	// Cuisines and tags arrive most used first; proteins in a fixed order.
	slices.SortStableFunc(f.Proteins, func(a, b FilterOption) int { return higher(a.Count, b.Count) })
	return f
}

// counted keeps options the catalog uses.
func counted(options []recommendations.Option) []FilterOption {
	out := []FilterOption{}
	for _, o := range options {
		if o.RecipeCount > 0 {
			out = append(out, FilterOption{Value: o.Value, Label: o.Label, Count: o.RecipeCount})
		}
	}
	return out
}
