package providers

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/Linesmerrill/DinnerOS/api/internal/ingredients"
)

// Reason says why a package count needs a person to check it.
type Reason string

// Reasons a package count is flagged "check amount".
const (
	// ReasonNoPackageSize: the saved product has no package size, so the
	// count is 1.
	ReasonNoPackageSize Reason = "no_package_size"
	// ReasonUnitNotConvertible: some or all of the amount is in a unit that
	// doesn't convert to the package size (4 cloves vs a 3 count package).
	// The count covers only what converts, and is at least 1.
	ReasonUnitNotConvertible Reason = "unit_not_convertible"
	// ReasonCapped: the amount needs more than MaxPackages packages.
	ReasonCapped Reason = "package_count_capped"
)

// PackageCount is how many packages of a product cover a grocery line.
type PackageCount struct {
	Packages int
	// Reason is set when the count needs checking ("check amount").
	Reason Reason
	// Needed is the part of the line's amount that converts, in the package
	// size's unit; nil when nothing converts or there's no size.
	Needed *ingredients.Amount
	// Unconverted are the amounts that don't convert to the package size.
	Unconverted []ingredients.Amount
	// CoversWeek is true when the count rests on the weekly coverage rule
	// rather than on measured package math: the need couldn't be measured
	// against the package, and one package is assumed to cover the week.
	CoversWeek bool
}

// CheckAmount reports whether the count is flagged.
func (c PackageCount) CheckAmount() bool { return c.Reason != "" }

// CountPackages computes the packages of size that cover needs, exactly:
//
//   - No package size (nil or zero): 1, flagged no_package_size.
//   - No amounts (an unquantified line such as "salt to taste"): 1.
//   - Amounts that convert to the size's unit (same unit, or the same
//     measurable kind) are converted and summed; the count is that total ÷
//     size, rounded up. Discrete units convert only to themselves: 2 cans
//     and a 1 can package is 2.
//   - Amounts that don't convert are left out and flag the count
//     unit_not_convertible; with nothing that converts the count is 1.
//   - More than MaxPackages is MaxPackages, flagged package_count_capped.
func CountPackages(needs []ingredients.Amount, size *ingredients.Amount) PackageCount {
	return countPackagesExact(needs, size)
}

// Coverage says how one bought package maps to a week's need.
type Coverage string

// Coverage rules.
const (
	// CoverageAuto leaves the rule to the caller's default, which shopping
	// takes from the grocery category.
	CoverageAuto Coverage = ""
	// CoveragePerAmount is exact package math: the count comes from the
	// amounts alone. It is the rule for anything measurable.
	CoveragePerAmount Coverage = "per_amount"
	// CoveragePerWeek assumes one package covers the week when the need
	// can't be measured against the package: a recipe asking for 1 clove
	// and a bulb sold as 1 ct buys one bulb, not one bulb per clove. The
	// week's list is rebuilt each week, so the following week buys another.
	CoveragePerWeek Coverage = "per_week"
)

// CountPackagesFor is CountPackages with a coverage rule applied.
//
// CoveragePerWeek changes exactly one case: a need in a unit that doesn't
// convert to the package at all (4 cloves against a 1 ct bulb). Exact math
// is otherwise untouched, so 2 ¼ lb of beef against 16 oz packages is still
// 3 packages under either rule — a week's need that *can* be measured is
// never collapsed to one package.
func CountPackagesFor(needs []ingredients.Amount, size *ingredients.Amount, coverage Coverage) PackageCount {
	out := countPackagesExact(needs, size)
	if coverage != CoveragePerWeek {
		return out
	}
	// Only when nothing converted: a partly converted line keeps its exact
	// count and its flag, because the measured part is real.
	if out.Reason == ReasonUnitNotConvertible && out.Needed == nil {
		out.Packages, out.Reason, out.CoversWeek = 1, "", true
	}
	return out
}

func countPackagesExact(needs []ingredients.Amount, size *ingredients.Amount) PackageCount {
	if size == nil || size.Quantity.IsZero() {
		return PackageCount{Packages: 1, Reason: ReasonNoPackageSize}
	}
	if len(needs) == 0 {
		return PackageCount{Packages: 1}
	}
	var out PackageCount
	total := ingredients.NewQuantity(0, 1)
	converted := false
	for _, n := range needs {
		q, err := ingredients.Convert(n.Quantity, n.Unit, size.Unit)
		if err != nil {
			out.Unconverted = append(out.Unconverted, n)
			continue
		}
		total, converted = total.Add(q), true
	}
	if !converted {
		out.Packages, out.Reason = 1, ReasonUnitNotConvertible
		return out
	}
	out.Needed = &ingredients.Amount{Quantity: total, Unit: size.Unit}
	ratio := new(big.Rat).Quo(total.Rat(), size.Quantity.Rat())
	count, rem := new(big.Int).QuoRem(ratio.Num(), ratio.Denom(), new(big.Int))
	if rem.Sign() != 0 {
		count.Add(count, big.NewInt(1))
	}
	switch {
	case count.Cmp(big.NewInt(MaxPackages)) > 0:
		out.Packages, out.Reason = MaxPackages, ReasonCapped
	case count.Sign() == 0:
		out.Packages = 1
	default:
		out.Packages = int(count.Int64())
	}
	if len(out.Unconverted) > 0 && out.Reason == "" {
		out.Reason = ReasonUnitNotConvertible
	}
	return out
}

// AmountText renders an amount for people: kitchen fractions when exact
// ("1 ½ cups"), otherwise at most two decimals ("17.64 oz"). A count reads
// "12 ct".
func AmountText(a ingredients.Amount) string {
	q := a.Quantity
	text := q.Format()
	eighths := new(big.Rat).Mul(q.Rat(), big.NewRat(8, 1))
	if !eighths.IsInt() {
		text = strconv.FormatFloat(q.Float64(), 'f', 2, 64)
		text = strings.TrimRight(strings.TrimRight(text, "0"), ".")
	}
	label := a.Unit.Label(q)
	if a.Unit.Code == "count" {
		label = "ct"
	}
	if label != "" {
		text += " " + label
	}
	return text
}

// CoverageText explains a count for size: "2 × 16 oz covers 20 oz",
// "1 × 16 oz", or "1 package" without a size.
func CoverageText(c PackageCount, size *ingredients.Amount) string {
	if size == nil || size.Quantity.IsZero() {
		if c.Packages == 1 {
			return "1 package"
		}
		return fmt.Sprintf("%d packages", c.Packages)
	}
	text := fmt.Sprintf("%d × %s", c.Packages, AmountText(*size))
	switch {
	case c.Needed != nil:
		text += " covers " + AmountText(*c.Needed)
	case c.CoversWeek:
		// Say what the assumption is, and what it is covering.
		text += " covers this week"
		if parts := amountList(c.Unconverted); parts != "" {
			text += " (" + parts + ")"
		}
	}
	return text
}

// amountList renders amounts as "4 cloves + 1 bunch", or "" for none.
func amountList(list []ingredients.Amount) string {
	parts := make([]string, 0, len(list))
	for _, a := range list {
		parts = append(parts, AmountText(a))
	}
	return strings.Join(parts, " + ")
}

// ReasonText explains a flagged count, or returns "" for an unflagged one.
func ReasonText(c PackageCount, size *ingredients.Amount) string {
	switch c.Reason {
	case ReasonNoPackageSize:
		return "Check amount: no package size is saved for this product"
	case ReasonUnitNotConvertible:
		sizeText := ""
		if size != nil {
			sizeText = AmountText(*size)
		}
		return fmt.Sprintf("Check amount: %s doesn't convert to a %s package", amountList(c.Unconverted), sizeText)
	case ReasonCapped:
		return fmt.Sprintf("Check amount: capped at %d packages", MaxPackages)
	}
	return ""
}
