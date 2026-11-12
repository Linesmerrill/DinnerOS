package ingredients

import (
	"fmt"
	"strings"
)

// When a member swaps the ramen for lo mein, the step that boils the noodles
// still says ramen's 3 minutes. CookTimeFor is how long the new one takes, so
// the edit can say so instead of sending them to look it up. Times are boiling
// (or simmering, for grains) times for dried pasta and noodles as packages
// commonly give them; a member can change any of them.

// CookTime is how long an ingredient cooks, in minutes.
type CookTime struct {
	// Name is what the table matched ("lo mein noodles").
	Name string
	Low  int
	High int
}

// Text is "6 minutes" or "4 to 6 minutes".
func (c CookTime) Text() string {
	if c.Low == c.High {
		return fmt.Sprintf("%d minutes", c.Low)
	}
	return fmt.Sprintf("%d to %d minutes", c.Low, c.High)
}

// Suggested is the minute count to start the wheel on: the top of the range,
// since undercooked pasta is the failure a cook notices.
func (c CookTime) Suggested() int { return c.High }

// cookTimes are matched against the normalized name: the longest entry the
// name ends with, or equals, wins ("fresh lo mein noodles" before "lo mein
// noodles", "brown rice" before "rice").
var cookTimes = []struct {
	names     []string
	low, high int
}{
	// Asian noodles.
	{[]string{"lo mein noodles", "lo mein noodle", "lo mein", "chow mein noodles"}, 4, 6},
	{[]string{"fresh lo mein noodles", "fresh egg noodles", "fresh ramen noodles"}, 2, 3},
	{[]string{"ramen noodles", "ramen noodle", "ramen", "instant noodles"}, 3, 3},
	{[]string{"udon noodles", "udon"}, 8, 10},
	{[]string{"fresh udon noodles", "frozen udon noodles"}, 2, 3},
	{[]string{"soba noodles", "soba"}, 4, 5},
	{[]string{"rice noodles", "pad thai noodles", "rice stick noodles"}, 6, 8},
	{[]string{"rice vermicelli", "vermicelli rice noodles", "vermicelli noodles"}, 3, 5},
	{[]string{"egg noodles", "wide egg noodles"}, 6, 8},
	{[]string{"glass noodles", "cellophane noodles", "bean thread noodles"}, 3, 5},
	{[]string{"somen noodles", "somen"}, 2, 3},
	// Pasta.
	{[]string{"spaghetti", "spaghetti pasta"}, 9, 11},
	{[]string{"thin spaghetti", "spaghettini"}, 6, 8},
	{[]string{"angel hair", "angel hair pasta", "capellini"}, 4, 5},
	{[]string{"linguine", "linguini"}, 9, 11},
	{[]string{"fettuccine", "fettuccini"}, 10, 12},
	{[]string{"bucatini"}, 9, 11},
	{[]string{"penne", "penne pasta", "penne rigate"}, 10, 12},
	{[]string{"rigatoni"}, 11, 13},
	{[]string{"fusilli", "rotini"}, 10, 12},
	{[]string{"farfalle", "bow tie pasta", "bowtie pasta"}, 10, 12},
	{[]string{"orecchiette"}, 10, 12},
	{[]string{"cavatappi"}, 9, 11},
	{[]string{"campanelle"}, 9, 11},
	{[]string{"gemelli"}, 10, 12},
	{[]string{"radiatori"}, 9, 11},
	{[]string{"elbow macaroni", "macaroni", "elbow pasta"}, 7, 8},
	{[]string{"ditalini"}, 8, 10},
	{[]string{"orzo"}, 8, 10},
	{[]string{"pappardelle"}, 7, 9},
	{[]string{"tagliatelle"}, 8, 10},
	{[]string{"lasagna noodles", "lasagne noodles"}, 9, 11},
	{[]string{"tortellini", "cheese tortellini"}, 3, 5},
	{[]string{"ravioli", "cheese ravioli"}, 4, 6},
	{[]string{"gnocchi", "potato gnocchi"}, 2, 3},
	{[]string{"fresh pasta", "fresh fettuccine", "fresh linguine"}, 2, 3},
	// Grains, simmered covered.
	{[]string{"rice", "white rice", "long grain rice", "long grain white rice", "jasmine rice", "basmati rice"}, 15, 18},
	{[]string{"brown rice"}, 40, 45},
	{[]string{"arborio rice"}, 18, 20},
	{[]string{"quinoa"}, 15, 15},
	{[]string{"couscous"}, 5, 5},
	{[]string{"pearl couscous", "israeli couscous"}, 8, 10},
	{[]string{"farro"}, 25, 30},
	{[]string{"bulgur", "bulgur wheat"}, 12, 12},
	{[]string{"barley", "pearl barley"}, 25, 30},
}

// CookTimeFor returns how long the named ingredient cooks, and false when the
// table doesn't know it. Nothing is guessed.
func CookTimeFor(name string) (CookTime, bool) {
	n := NormalizeName(name)
	if n == "" {
		return CookTime{}, false
	}
	var best CookTime
	bestLen := 0
	for _, entry := range cookTimes {
		for _, w := range entry.names {
			if (n == w || strings.HasSuffix(n, " "+w)) && len(w) > bestLen {
				best, bestLen = CookTime{Name: w, Low: entry.low, High: entry.high}, len(w)
			}
		}
	}
	return best, bestLen > 0
}
