package catalog

import (
	"slices"
	"testing"
	"time"

	"github.com/Linesmerrill/DinnerOS/api/internal/recipes"
)

// Chef’s Picks leads with cards that have a photo and a time: a blank picture
// with no time isn't a favorite to show off (build 123, Hark's review). The
// rest keep their order behind them.
func TestChefsPicksLeadsWithPhotosAndTimes(t *testing.T) {
	card := func(name, image string, prep int) Result {
		return Result{Recipe: Recipe{ID: name, Content: recipes.Recipe{Name: name, ImageURL: image, PrepMinutes: prep}}}
	}
	pool := []Result{
		card("No Photo", "", 20),
		card("Tacos", "https://example.com/tacos.jpg", 25),
		card("No Time", "https://example.com/soup.jpg", 0),
		card("Pasta", "https://example.com/pasta.jpg", 30),
	}
	got, err := byRule(t.Context(), RuleBest, pool, &signals{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if names := itemNames(got); !slices.Equal(names, []string{"Tacos", "Pasta", "No Photo", "No Time"}) {
		t.Errorf("Chef’s Picks = %v, want the cards with a photo and a time first", names)
	}
	if itemNames(pool)[0] != "No Photo" {
		t.Error("the pool was reordered")
	}
}
