package main

import (
	"strings"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/metrics"
)

func TestPrint(t *testing.T) {
	var b strings.Builder
	Print(&b, []metrics.Week{{Week: "2026-W46", ActiveHouseholds: 3, CartsSent: 2, Stores: map[string]metrics.Store{
		"walmart":   {Carts: 1, PricedCarts: 1, ValueCents: 8450},
		"instacart": {Carts: 1},
	}}})
	got := b.String()
	for _, want := range []string{"2026-W46", "instacart 1, no prices", "walmart 1, $84.50 (1)"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if err := run(t.Context(), []string{"-weeks", "0"}, func(string) string { return "" }, &b); err == nil {
		t.Error("accepted zero weeks")
	}
}

func TestPrintPlus(t *testing.T) {
	var b strings.Builder
	PrintPlus(&b, []metrics.Week{{Week: "2026-W46"}, {Week: "2026-W47"}},
		metrics.Plus{Paying: 2, Trial: 1, FreeMonths: 3, Referrals: map[string]int{"2026-W47": 4}})
	want := "\nPlus now: 2 paying, 1 in trial, 3 on free months\nReferral codes used: 2026-W46 0, 2026-W47 4\n"
	if b.String() != want {
		t.Errorf("got %q, want %q", b.String(), want)
	}
}
