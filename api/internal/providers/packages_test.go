package providers

import (
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

func amount(t *testing.T, quantity, unit string) ingredients.Amount {
	t.Helper()
	q, err := ingredients.ParseQuantity(quantity)
	if err != nil {
		t.Fatal(err)
	}
	u, err := ingredients.LookupUnit(unit)
	if err != nil {
		t.Fatal(err)
	}
	return ingredients.Amount{Quantity: q, Unit: u}
}

func TestCountPackages(t *testing.T) {
	tests := []struct {
		name     string
		needs    [][2]string
		size     *[2]string
		packages int
		reason   Reason
		needed   string // AmountText of Needed, or ""
		coverage string
	}{
		{name: "same unit rounds up", needs: [][2]string{{"20", "oz"}}, size: &[2]string{"16", "oz"}, packages: 2, needed: "20 oz", coverage: "2 × 16 oz covers 20 oz"},
		{name: "exact fit", needs: [][2]string{{"32", "oz"}}, size: &[2]string{"16", "oz"}, packages: 2, needed: "32 oz"},
		{name: "less than one", needs: [][2]string{{"1/2", "cup"}}, size: &[2]string{"32", "floz"}, packages: 1, needed: "4 fl oz", coverage: "1 × 32 fl oz covers 4 fl oz"},
		{name: "mass converts", needs: [][2]string{{"2", "lb"}}, size: &[2]string{"16", "oz"}, packages: 2, needed: "32 oz"},
		{name: "just over converts up", needs: [][2]string{{"1", "lb"}, {"1", "tbsp"}}, size: &[2]string{"454", "g"}, packages: 1, reason: ReasonUnitNotConvertible},
		{name: "several amounts combine", needs: [][2]string{{"1", "cup"}, {"3", "tbsp"}}, size: &[2]string{"8", "floz"}, packages: 2, needed: "9 ½ fl oz"},
		{name: "metric to customary", needs: [][2]string{{"500", "g"}}, size: &[2]string{"1", "lb"}, packages: 2, needed: "1.1 lb"},
		{name: "discrete same unit", needs: [][2]string{{"2", "can"}}, size: &[2]string{"1", "can"}, packages: 2, needed: "2 cans"},
		{name: "count package", needs: [][2]string{{"13", "count"}}, size: &[2]string{"12", "count"}, packages: 2, needed: "13 ct", coverage: "2 × 12 ct covers 13 ct"},
		{name: "count does not convert", needs: [][2]string{{"4", "clove"}}, size: &[2]string{"3", "count"}, packages: 1, reason: ReasonUnitNotConvertible, coverage: "1 × 3 ct"},
		{name: "count to mass does not convert", needs: [][2]string{{"2", "count"}}, size: &[2]string{"3", "lb"}, packages: 1, reason: ReasonUnitNotConvertible},
		{name: "volume to mass does not convert", needs: [][2]string{{"1", "cup"}}, size: &[2]string{"5", "lb"}, packages: 1, reason: ReasonUnitNotConvertible},
		{name: "partly convertible counts what converts", needs: [][2]string{{"1", "count"}, {"40", "oz"}}, size: &[2]string{"32", "oz"}, packages: 2, reason: ReasonUnitNotConvertible, needed: "40 oz"},
		{name: "unquantified", needs: nil, size: &[2]string{"26", "oz"}, packages: 1, coverage: "1 × 26 oz"},
		{name: "no package size", needs: [][2]string{{"20", "oz"}}, packages: 1, reason: ReasonNoPackageSize, coverage: "1 package"},
		{name: "capped", needs: [][2]string{{"10", "lb"}}, size: &[2]string{"1", "oz"}, packages: MaxPackages, reason: ReasonCapped, needed: "160 oz"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var needs []ingredients.Amount
			for _, n := range tt.needs {
				needs = append(needs, amount(t, n[0], n[1]))
			}
			var size *ingredients.Amount
			if tt.size != nil {
				s := amount(t, tt.size[0], tt.size[1])
				size = &s
			}
			got := CountPackages(needs, size)
			if got.Packages != tt.packages || got.Reason != tt.reason || got.CheckAmount() != (tt.reason != "") {
				t.Errorf("CountPackages() = %d %q, want %d %q", got.Packages, got.Reason, tt.packages, tt.reason)
			}
			if tt.needed != "" && (got.Needed == nil || AmountText(*got.Needed) != tt.needed) {
				t.Errorf("Needed = %+v, want %s", got.Needed, tt.needed)
			}
			if tt.coverage != "" {
				if c := CoverageText(got, size); c != tt.coverage {
					t.Errorf("CoverageText() = %q, want %q", c, tt.coverage)
				}
			}
			if (ReasonText(got, size) != "") != got.CheckAmount() {
				t.Errorf("ReasonText() = %q for reason %q", ReasonText(got, size), got.Reason)
			}
		})
	}
}

func TestPackageTexts(t *testing.T) {
	c := CountPackages([]ingredients.Amount{amount(t, "4", "clove")}, &ingredients.Amount{Quantity: ingredients.NewQuantity(3, 1), Unit: mustUnit(t, "count")})
	size := amount(t, "3", "count")
	if got := ReasonText(c, &size); got != "Check amount: 4 cloves doesn't convert to a 3 ct package" {
		t.Errorf("ReasonText() = %q", got)
	}
	if got := CoverageText(PackageCount{Packages: 3, Reason: ReasonNoPackageSize}, nil); got != "3 packages" {
		t.Errorf("CoverageText() = %q", got)
	}
	for in, want := range map[[2]string]string{
		{"3/2", "cup"}: "1 ½ cups", {"1", "cup"}: "1 cup", {"1/3", "lb"}: "0.33 lb", {"12", "count"}: "12 ct", {"2", "package"}: "2 packages",
	} {
		if got := AmountText(amount(t, in[0], in[1])); got != want {
			t.Errorf("AmountText(%v) = %q, want %q", in, got, want)
		}
	}
}

func mustUnit(t *testing.T, code string) ingredients.Unit {
	t.Helper()
	u, err := ingredients.LookupUnit(code)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
