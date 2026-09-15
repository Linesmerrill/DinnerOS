package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// History is the owner's past-deliveries data, exported from their signed-in
// HelloFresh session. Only recipe references are needed; no account details.
type History struct {
	ExportedAt time.Time     `json:"exportedAt"`
	Source     string        `json:"source"`
	Weeks      []HistoryWeek `json:"weeks"`
}

// HistoryWeek is one delivered menu week, e.g. "2026-W30".
type HistoryWeek struct {
	Week   string        `json:"week"`
	MenuID string        `json:"menuId"`
	Meals  []HistoryMeal `json:"meals"`
}

// HistoryMeal is a recipe (or add-on) delivered in a week.
type HistoryMeal struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Headline   string `json:"headline"`
	WebsiteURL string `json:"websiteURL"`
	Image      string `json:"image"`
	IsAddon    bool   `json:"isAddon"`
}

// OrderedRecipe is a unique delivered recipe with every week it was delivered.
type OrderedRecipe struct {
	DeliveredID string
	Name        string
	URL         string
	IsAddon     bool
	Weeks       []string
}

// recipeBaseURL is where HelloFresh recipe pages live. Pages resolve by the
// trailing 24-hex recipe ID; the slug is descriptive.
const recipeBaseURL = "https://www.hellofresh.com/recipes/"

// LoadHistory reads exported order history. The path may be a JSON file, a
// TSV file, or a glob matching TSV parts (e.g. "data/history-part*.tsv").
//
// TSV columns: week, menuId, type (M = meal, A = add-on), recipeId, slug.
func LoadHistory(path string) (History, error) {
	if strings.ContainsAny(path, "*?[") || strings.HasSuffix(path, ".tsv") {
		files, err := filepath.Glob(path)
		if err != nil {
			return History{}, fmt.Errorf("order history glob: %w", err)
		}
		if len(files) == 0 {
			return History{}, fmt.Errorf("order history: no files match %s", path)
		}
		sort.Strings(files)
		return LoadHistoryTSV(files...)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return History{}, fmt.Errorf("read order history: %w", err)
	}
	var h History
	if err := json.Unmarshal(data, &h); err != nil {
		return History{}, fmt.Errorf("parse order history %s: %w", path, err)
	}
	return h, nil
}

var (
	isoWeekRe = regexp.MustCompile(`^\d{4}-W\d{2}$`)
	hexIDRe   = regexp.MustCompile(`^[a-f0-9]{24}$`)
)

// LoadHistoryTSV merges TSV history parts. Rows repeated across parts are
// de-duplicated; malformed rows are rejected with their file and line number.
func LoadHistoryTSV(files ...string) (History, error) {
	type rowKey struct{ week, kind, id string }
	seen := map[rowKey]bool{}
	weeks := map[string]*HistoryWeek{}

	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return History{}, fmt.Errorf("read order history: %w", err)
		}
		for i, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			cols := strings.Split(line, "\t")
			if len(cols) != 5 {
				return History{}, fmt.Errorf("%s:%d: want 5 tab-separated columns, got %d", file, i+1, len(cols))
			}
			week, menuID, kind, id, slug := cols[0], cols[1], cols[2], cols[3], strings.TrimSpace(cols[4])
			if !isoWeekRe.MatchString(week) || !hexIDRe.MatchString(id) || (kind != "M" && kind != "A") {
				return History{}, fmt.Errorf("%s:%d: malformed row %q", file, i+1, line)
			}
			key := rowKey{week, kind, id}
			if seen[key] {
				continue
			}
			seen[key] = true

			w, ok := weeks[week]
			if !ok {
				w = &HistoryWeek{Week: week, MenuID: menuID}
				weeks[week] = w
			}
			if slug == "" {
				slug = "recipe"
			}
			w.Meals = append(w.Meals, HistoryMeal{
				ID:         id,
				Name:       strings.ReplaceAll(slug, "-", " "),
				WebsiteURL: recipeBaseURL + slug + "-" + id,
				IsAddon:    kind == "A",
			})
		}
	}

	h := History{Source: "hellofresh-past-deliveries-tsv"}
	for _, w := range weeks {
		h.Weeks = append(h.Weeks, *w)
	}
	sort.Slice(h.Weeks, func(i, j int) bool { return h.Weeks[i].Week < h.Weeks[j].Week })
	return h, nil
}

// UniqueRecipes returns each delivered recipe once, with its delivery weeks
// merged and sorted, ordered by delivered ID for deterministic output.
func (h History) UniqueRecipes() []OrderedRecipe {
	byID := map[string]*OrderedRecipe{}
	weeks := map[string]map[string]bool{}
	for _, w := range h.Weeks {
		for _, m := range w.Meals {
			if m.ID == "" {
				continue
			}
			r, ok := byID[m.ID]
			if !ok {
				r = &OrderedRecipe{DeliveredID: m.ID, Name: m.Name, URL: m.WebsiteURL, IsAddon: m.IsAddon}
				byID[m.ID] = r
				weeks[m.ID] = map[string]bool{}
			}
			if r.URL == "" {
				r.URL = m.WebsiteURL
			}
			if w.Week != "" {
				weeks[m.ID][w.Week] = true
			}
		}
	}

	out := make([]OrderedRecipe, 0, len(byID))
	for id, r := range byID {
		for wk := range weeks[id] {
			r.Weeks = append(r.Weeks, wk)
		}
		sort.Strings(r.Weeks)
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DeliveredID < out[j].DeliveredID })
	return out
}
