package main

import (
	"strings"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/catalog"
)

func TestCount(t *testing.T) {
	entries := []catalog.GroupEntry{
		{ID: "a1", Name: "Tacos", Group: "url:t", Current: true},
		{ID: "a2", Name: "Tacos", Group: "url:t"},
		{ID: "a3", Name: "Tacos", Group: "url:t"},
		{ID: "b1", Name: "Stew", Group: "typed:s", Current: true},
		{ID: "b2", Name: "Stew", Group: "typed:s", Current: true},
		{ID: "c1", Name: "Soup", Group: "src:p", Current: true},
	}
	// Three households reported different copies of the stew: no single
	// entry reaches the threshold, but the group does.
	reporters := map[string][]string{"b1": {"h1", "h2"}, "b2": {"h3"}}
	c := Count(entries, reporters)
	if c.Stored != 6 || c.Before != 6 || c.Groups != 3 || c.After != 2 || c.Stale != 2 {
		t.Errorf("counts = %+v", c)
	}
	if len(c.Largest) != 2 || c.Largest[0].Name != "Tacos" || c.Largest[0].Copies != 3 {
		t.Errorf("largest = %+v", c.Largest)
	}

	var b strings.Builder
	Print(&b, c, 1)
	got := b.String()
	for _, want := range []string{"shown before dedupe:   6", "shown after dedupe:    2", "3 copies  url    Tacos"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if strings.Contains(got, "Stew") {
		t.Errorf("printed more than the top 1:\n%s", got)
	}
	if err := run(t.Context(), []string{"-top", "-1"}, func(string) string { return "" }, &b); err == nil {
		t.Error("accepted a negative -top")
	}
}
