package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadHistoryTSVMergesPartsAndDedupes(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "history-part0.tsv",
		"2026-W01\tmenu1\tM\taaaaaaaaaaaaaaaaaaaaaaaa\tsynthetic-tacos\n"+
			"2026-W01\tmenu1\tA\tbbbbbbbbbbbbbbbbbbbbbbbb\tgarlic-bread\n")
	writeFile(t, dir, "history-part1.tsv",
		"2026-W01\tmenu1\tA\tbbbbbbbbbbbbbbbbbbbbbbbb\tgarlic-bread\n"+ // duplicate across parts
			"\n"+
			"2026-W02\tmenu2\tM\taaaaaaaaaaaaaaaaaaaaaaaa\tsynthetic-tacos\r\n")

	h, err := LoadHistory(filepath.Join(dir, "history-part*.tsv"))
	if err != nil {
		t.Fatalf("LoadHistory() error = %v", err)
	}
	if len(h.Weeks) != 2 || h.Weeks[0].Week != "2026-W01" || len(h.Weeks[0].Meals) != 2 {
		t.Fatalf("weeks = %+v", h.Weeks)
	}
	meal := h.Weeks[0].Meals[0]
	if meal.WebsiteURL != recipeBaseURL+"synthetic-tacos-aaaaaaaaaaaaaaaaaaaaaaaa" || meal.IsAddon {
		t.Errorf("meal = %+v", meal)
	}
	if !h.Weeks[0].Meals[1].IsAddon {
		t.Error("type A should be an add-on")
	}

	recipes := h.UniqueRecipes()
	if len(recipes) != 2 || strings.Join(recipes[0].Weeks, ",") != "2026-W01,2026-W02" {
		t.Errorf("unique recipes = %+v", recipes)
	}
}

func TestLoadHistoryTSVRejectsMalformedRows(t *testing.T) {
	tests := map[string]string{
		"columns": "2026-W01\tmenu\tM\taaaaaaaaaaaaaaaaaaaaaaaa\n",
		"week":    "2026-1\tmenu\tM\taaaaaaaaaaaaaaaaaaaaaaaa\tslug\n",
		"type":    "2026-W01\tmenu\tX\taaaaaaaaaaaaaaaaaaaaaaaa\tslug\n",
		"id":      "2026-W01\tmenu\tM\tnot-an-id\tslug\n",
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			path := writeFile(t, t.TempDir(), "h.tsv", content)
			if _, err := LoadHistory(path); err == nil || !strings.Contains(err.Error(), "h.tsv:1") {
				t.Errorf("error = %v, want file:line context", err)
			}
		})
	}
}

func TestLoadHistoryGlobWithoutMatches(t *testing.T) {
	if _, err := LoadHistory(filepath.Join(t.TempDir(), "none-*.tsv")); err == nil {
		t.Error("expected error for glob without matches")
	}
}
