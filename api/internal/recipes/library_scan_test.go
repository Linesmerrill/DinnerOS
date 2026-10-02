package recipes

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb"
)

// TestLibraryScan renders every recipe in a local copy of a library at every
// size, runs CheckSteps, and writes the findings (and every rendered step and
// checklist row, to SCAN_LIB.texts) for review. Run it before and after a
// change to the step reader and diff the .texts files: a test pins the
// phrasings someone wrote down, and this shows everything else that moved.
//
//	SCAN_LIB=/tmp/after.txt SCAN_LIB_DB=scan_lib SCAN_LIB_HOUSEHOLD=<id> \
//	  go test ./internal/recipes -run TestLibraryScan
//
// The copy is local and never committed.
func TestLibraryScan(t *testing.T) {
	if os.Getenv("SCAN_LIB") == "" || os.Getenv("SCAN_LIB_HOUSEHOLD") == "" {
		t.Skip("set SCAN_LIB, SCAN_LIB_DB, and SCAN_LIB_HOUSEHOLD to scan a local library copy")
	}
	ctx := context.Background()
	c, err := mongodb.Connect(ctx, mongodb.Config{URI: cmpOr(os.Getenv("SCAN_LIB_URI"), "mongodb://localhost:27017"), Database: cmpOr(os.Getenv("SCAN_LIB_DB"), "scan_lib")})
	if err != nil {
		t.Fatal(err)
	}
	store := NewMongoStore(c.Database())
	counts := map[string]int{}
	texts, _ := os.Create(os.Getenv("SCAN_LIB") + ".texts")
	defer texts.Close()
	examples := map[string][]string{}
	// SCAN_LIB.steps.json is every recipe's steps at its smallest size, as the
	// app receives them, for the app's own scan (StepBlocksLibraryScan).
	type scannedRecipe struct {
		Name  string                    `json:"name"`
		Steps []InstructionStepResponse `json:"steps"`
	}
	var scanned []scannedRecipe
	after := ""
	for {
		page, err := store.ListRecipesAfter(ctx, os.Getenv("SCAN_LIB_HOUSEHOLD"), after, 100)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		for _, r := range page {
			after = r.ID
			if len(r.Servings) > 0 {
				scanned = append(scanned, scannedRecipe{Name: r.Name, Steps: newInstructionsResponse(Annotate(r, smallest(r.Servings), nil, true)).Steps})
			}
			for _, n := range r.Servings {
				in := Annotate(r, n, nil, true)
				for _, st := range in.Steps {
					fmt.Fprintf(texts, "TEXT\t%s\t%d\t%d\t%s\n", r.Name, n, st.Index, st.Text)
				}
				for _, g := range in.Checklist.ByStep {
					for _, it := range g.Items {
						fmt.Fprintf(texts, "LIST\t%s\t%d\t%d\t%s\t%s\t%s\n", r.Name, n, g.Index, it.AmountText, it.Name, it.Prep)
					}
				}
				for _, f := range CheckSteps(in) {
					counts[f.Code]++
					if len(examples[f.Code]) < 400 {
						examples[f.Code] = append(examples[f.Code], fmt.Sprintf("%s [%d] s%d: %s", r.Name, n, f.Step, f.Detail))
					}
				}
			}
		}
	}
	if data, err := json.Marshal(scanned); err == nil {
		_ = os.WriteFile(os.Getenv("SCAN_LIB")+".steps.json", data, 0o600)
	}
	var codes []string
	for k := range counts {
		codes = append(codes, k)
	}
	sort.Strings(codes)
	out, _ := os.Create(os.Getenv("SCAN_LIB"))
	defer out.Close()
	for _, k := range codes {
		fmt.Fprintf(out, "## %s %d\n", k, counts[k])
		for _, e := range examples[k] {
			fmt.Fprintln(out, e)
		}
	}
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
