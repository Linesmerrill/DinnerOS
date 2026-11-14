package providers

import (
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

func icAmount(t *testing.T, q, unit string) ingredients.Amount {
	t.Helper()
	qty, err := ingredients.ParseQuantity(q)
	if err != nil {
		t.Fatal(err)
	}
	u, err := ingredients.LookupUnit(unit)
	if err != nil {
		t.Fatal(err)
	}
	return ingredients.Amount{Quantity: qty, Unit: u}
}

func TestInstacartMeasurements(t *testing.T) {
	cases := []struct {
		q, unit string
		want    ListMeasurement
	}{
		{"2", "count", ListMeasurement{2, "each"}},
		{"1/2", "cup", ListMeasurement{0.5, "cup"}},
		{"2", "tbsp", ListMeasurement{2, "tablespoon"}},
		{"1", "tsp", ListMeasurement{1, "teaspoon"}},
		{"20", "oz", ListMeasurement{20, "ounce"}},
		{"1", "lb", ListMeasurement{1, "pound"}},
		{"500", "g", ListMeasurement{500, "gram"}},
		{"1", "kg", ListMeasurement{1, "kilogram"}},
		{"250", "ml", ListMeasurement{250, "milliliter"}},
		{"1", "l", ListMeasurement{1, "liter"}},
		{"2", "can", ListMeasurement{2, "can"}},
		{"1", "package", ListMeasurement{1, "package"}},
		{"2", "bag", ListMeasurement{2, "package"}},
		{"1", "bunch", ListMeasurement{1, "bunch"}},
		{"4", "clove", ListMeasurement{1, "each"}},
		{"12", "clove", ListMeasurement{2, "each"}},
		{"12", "floz", ListMeasurement{1.5, "cup"}},
		{"6", "wedge", ListMeasurement{2, "each"}},
		{"3", "slice", ListMeasurement{1, "package"}},
		{"1", "pinch", ListMeasurement{1, "each"}},
		{"1", "thumb", ListMeasurement{1, "each"}},
		{"1/3", "cup", ListMeasurement{0.33, "cup"}},
	}
	for _, c := range cases {
		got := InstacartMeasurements([]ingredients.Amount{icAmount(t, c.q, c.unit)})
		if len(got) != 1 || got[0] != c.want {
			t.Errorf("%s %s = %+v, want %+v", c.q, c.unit, got, c.want)
		}
	}
	if got := InstacartMeasurements(nil); len(got) != 0 {
		t.Errorf("no amounts = %+v", got)
	}
}

func TestInstacartProvider(t *testing.T) {
	i := NewInstacart(false)
	if i.Name() != "Instacart" || i.AffiliateTracked() || i.ProductURL("1") != "" {
		t.Error("identity")
	}
	if _, err := i.ParseProduct("https://www.walmart.com/ip/1/123456789"); err == nil {
		t.Error("parsed a product")
	}
	if _, err := i.NormalizeStoreID("123"); err == nil {
		t.Error("took a store ID")
	}
	if c := CapabilitiesOf(i); c.Handoff != HandoffListPage || c.PasteProductLink || c.StoreID {
		t.Errorf("capabilities = %+v", c)
	}
	links, err := i.BuildCartLinks([]CartItem{{Quantity: 1, LineIDs: []string{"l1"}}, {Quantity: 1, LineIDs: []string{"l2"}}}, StoreRef{})
	if err != nil || len(links) != 1 || links[0].URL != "" || len(links[0].Items) != 2 {
		t.Fatalf("links = %+v, %v", links, err)
	}
	if _, err := i.BuildCartLinks([]CartItem{{Quantity: 0}}, StoreRef{}); err == nil {
		t.Error("took zero packages")
	}
	if _, err := i.BuildCartLinks([]CartItem{{Quantity: 1}}, StoreRef{StoreID: "1"}); err == nil {
		t.Error("took a store")
	}
	if links, _ := i.BuildCartLinks(nil, StoreRef{}); links != nil {
		t.Error("links for nothing")
	}
	if InstacartItemName("  Yellow   Onion ") != "Yellow Onion" {
		t.Error("name")
	}
}
