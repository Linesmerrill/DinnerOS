package shopping

import (
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/providers"
)

func TestSettingsStoreAndTripProvider(t *testing.T) {
	one := Settings{Provider: providers.KeyWalmart, StoreID: "1234"}
	if c, ok := one.Store(providers.KeyWalmart); !ok || c.StoreID != "1234" {
		t.Errorf("one store = %+v, %v: the default is the only store", c, ok)
	}
	if _, ok := one.Store(providers.KeyKroger); ok {
		t.Error("a store not set up was found")
	}

	two := Settings{
		Provider: providers.KeyWalmart,
		Stores:   []StoreChoice{{Provider: providers.KeyWalmart}, {Provider: providers.KeyKroger, StoreID: "66000123", Banner: "frys"}},
		Trips:    []TripStore{{Week: "2026-W45", Provider: providers.KeyKroger}, {Week: "2026-W46", Provider: "target"}},
	}
	for week, want := range map[string]providers.Key{"2026-W45": providers.KeyKroger, "2026-W46": providers.KeyWalmart, "2026-W47": providers.KeyWalmart} {
		if got := two.TripProvider(week); got != want {
			t.Errorf("%s shops at %q, want %q", week, got, want)
		}
	}
	if d := two.withDefault(providers.KeyKroger); d.Provider != providers.KeyKroger || d.StoreID != "66000123" || d.Banner != "frys" || len(d.Stores) != 2 {
		t.Errorf("withDefault = %+v", d)
	}
	if none := two.withDefault(""); none.Provider != "" || none.StoreID != "" {
		t.Errorf("no default = %+v", none)
	}
	if got := (Settings{}).TripProvider("2026-W45"); got != "" {
		t.Errorf("no stores = %q", got)
	}
}
