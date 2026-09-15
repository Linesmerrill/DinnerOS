package ingredients

import (
	"bufio"
	"os"
	"sort"
	"strings"
	"testing"
)

// TestCategorizeCoverageReport prints which ingredient names are not
// categorized. It only runs when CATEGORIZE_NAMES_FILE points at a
// newline-separated list of names (for example, extracted from an importer's
// local data), so it never depends on personal data in CI.
func TestCategorizeCoverageReport(t *testing.T) {
	path := os.Getenv("CATEGORIZE_NAMES_FILE")
	if path == "" {
		t.Skip("CATEGORIZE_NAMES_FILE not set")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	counts := map[string]int{}
	var uncategorized []string
	total := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		name := strings.TrimSpace(scanner.Text())
		if name == "" {
			continue
		}
		total++
		category, confident := Categorize(name)
		counts[category]++
		if !confident {
			uncategorized = append(uncategorized, name)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}

	sort.Strings(uncategorized)
	t.Logf("names=%d categorized=%d uncategorized=%d", total, total-len(uncategorized), len(uncategorized))
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		t.Logf("  %-13s %d", k, counts[k])
	}
	for _, n := range uncategorized {
		t.Logf("  UNCATEGORIZED: %s", n)
	}
}
