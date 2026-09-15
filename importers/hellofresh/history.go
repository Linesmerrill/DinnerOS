package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
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

// LoadHistory reads an exported order history file.
func LoadHistory(path string) (History, error) {
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
