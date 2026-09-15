package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// capturedRecipe is the subset of an account capture checked before saving.
type capturedRecipe struct {
	Name        string            `json:"name"`
	WebsiteURL  string            `json:"websiteUrl"`
	Ingredients []json.RawMessage `json:"ingredients"`
}

// SaveAccountCaptures writes recipes captured from the owner's signed-in past
// deliveries view, keyed by delivered ID, to dir/delivered/<id>.json. It rejects
// the whole batch if any entry is malformed, and returns the number saved.
func SaveAccountCaptures(dir string, captures map[string]json.RawMessage, now time.Time) (int, error) {
	ids := make([]string, 0, len(captures))
	for id, recipe := range captures {
		if !recipeIDRe.MatchString(id) {
			return 0, fmt.Errorf("capture %q: delivered ID must be 24 hex characters", id)
		}
		var c capturedRecipe
		if err := json.Unmarshal(recipe, &c); err != nil {
			return 0, fmt.Errorf("capture %s: %w", id, err)
		}
		if c.Name == "" || len(c.Ingredients) == 0 {
			return 0, fmt.Errorf("capture %s: recipe has no name or ingredients", id)
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)

	outDir := filepath.Join(dir, "delivered")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return 0, err
	}
	for _, id := range ids {
		var c capturedRecipe
		_ = json.Unmarshal(captures[id], &c)
		raw := RawRecipe{
			DeliveredID: id,
			Origin:      OriginAccount,
			FinalURL:    c.WebsiteURL,
			FetchedAt:   now.UTC(),
			Recipe:      captures[id],
		}
		if err := writeJSONAtomic(filepath.Join(outDir, id+".json"), raw); err != nil {
			return 0, err
		}
	}
	return len(ids), nil
}
