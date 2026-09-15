package ingredients

import (
	"errors"
	"testing"
)

func mustUnit(t *testing.T, code string) Unit {
	t.Helper()
	u, err := LookupUnit(code)
	if err != nil {
		t.Fatalf("LookupUnit(%q) error = %v", code, err)
	}
	return u
}

func TestQuantityArithmeticIsExact(t *testing.T) {
	third := NewQuantity(1, 3)
	if sum := third.Add(third).Add(third); !sum.Equal(NewQuantity(1, 1)) {
		t.Errorf("1/3+1/3+1/3 = %s, want 1", sum)
	}
	half := NewQuantity(1, 2)
	if got := half.Mul(NewQuantity(2, 1)); !got.Equal(NewQuantity(1, 1)) {
		t.Errorf("1/2×2 = %s", got)
	}
	if _, err := half.Div(Quantity{}); !errors.Is(err, ErrInvalidQuantity) {
		t.Errorf("divide by zero error = %v", err)
	}
	var zero Quantity
	if !zero.IsZero() || zero.String() != "0" {
		t.Errorf("zero value = %s", zero)
	}
	if got := zero.Add(half); !got.Equal(half) {
		t.Errorf("0+1/2 = %s", got)
	}
}

func TestQuantityFromFloat(t *testing.T) {
	tests := []struct {
		in   float64
		want string
	}{
		{0.25, "1/4"}, {0.5, "1/2"}, {1.5, "3/2"}, {0.333, "1/3"}, {0.3333333, "1/3"},
		{0.667, "2/3"}, {0.125, "1/8"}, {14, "14"}, {0.3, "3/10"}, {2.2, "11/5"},
	}
	for _, tt := range tests {
		q, err := QuantityFromFloat(tt.in)
		if err != nil {
			t.Fatalf("QuantityFromFloat(%v) error = %v", tt.in, err)
		}
		if q.String() != tt.want {
			t.Errorf("QuantityFromFloat(%v) = %s, want %s", tt.in, q, tt.want)
		}
	}
	for _, bad := range []float64{-1} {
		if _, err := QuantityFromFloat(bad); !errors.Is(err, ErrInvalidQuantity) {
			t.Errorf("QuantityFromFloat(%v) error = %v, want ErrInvalidQuantity", bad, err)
		}
	}
}

func TestParseQuantity(t *testing.T) {
	tests := map[string]string{
		"2": "2", "0.5": "1/2", "1/3": "1/3", "1 1/2": "3/2", "½": "1/2", "1½": "3/2", " 2 ¾ ": "11/4",
	}
	for in, want := range tests {
		q, err := ParseQuantity(in)
		if err != nil || q.String() != want {
			t.Errorf("ParseQuantity(%q) = %s, %v; want %s", in, q, err, want)
		}
	}
	for _, bad := range []string{"", "abc", "-1"} {
		if _, err := ParseQuantity(bad); !errors.Is(err, ErrInvalidQuantity) {
			t.Errorf("ParseQuantity(%q) error = %v", bad, err)
		}
	}
}

func TestQuantityFormat(t *testing.T) {
	tests := []struct {
		q    Quantity
		want string
	}{
		{NewQuantity(1, 1), "1"}, {NewQuantity(1, 2), "½"}, {NewQuantity(3, 2), "1 ½"},
		{NewQuantity(1, 3), "⅓"}, {NewQuantity(11, 4), "2 ¾"}, {NewQuantity(3, 10), "0.3"},
		{Quantity{}, "0"},
	}
	for _, tt := range tests {
		if got := tt.q.Format(); got != tt.want {
			t.Errorf("Format(%s) = %q, want %q", tt.q, got, tt.want)
		}
	}
}

func TestConvertWithinKind(t *testing.T) {
	tests := []struct {
		q        Quantity
		from, to string
		want     Quantity
	}{
		{NewQuantity(3, 1), "tsp", "tbsp", NewQuantity(1, 1)},
		{NewQuantity(1, 1), "cup", "tsp", NewQuantity(48, 1)},
		{NewQuantity(1, 1), "cup", "tbsp", NewQuantity(16, 1)},
		{NewQuantity(16, 1), "oz", "lb", NewQuantity(1, 1)},
		{NewQuantity(1, 1), "kg", "g", NewQuantity(1000, 1)},
		{NewQuantity(1, 1), "oz", "g", mustParse(t, "28.349523125")},
		{NewQuantity(1, 1), "floz", "ml", mustParse(t, "29.5735295625")},
		{NewQuantity(2, 1), "clove", "clove", NewQuantity(2, 1)},
	}
	for _, tt := range tests {
		got, err := Convert(tt.q, mustUnit(t, tt.from), mustUnit(t, tt.to))
		if err != nil {
			t.Errorf("Convert(%s %s → %s) error = %v", tt.q, tt.from, tt.to, err)
			continue
		}
		if !got.Equal(tt.want) {
			t.Errorf("Convert(%s %s → %s) = %s, want %s", tt.q, tt.from, tt.to, got, tt.want)
		}
	}
}

func TestConvertForbiddenAcrossKinds(t *testing.T) {
	pairs := [][2]string{
		{"oz", "cup"}, {"cup", "g"}, {"count", "oz"}, {"clove", "count"}, {"can", "package"}, {"pinch", "tsp"},
	}
	for _, p := range pairs {
		_, err := Convert(NewQuantity(1, 1), mustUnit(t, p[0]), mustUnit(t, p[1]))
		if !errors.Is(err, ErrIncompatibleUnits) {
			t.Errorf("Convert(%s → %s) error = %v, want ErrIncompatibleUnits", p[0], p[1], err)
		}
	}
}

func TestConvertRoundTripIsIdentity(t *testing.T) {
	q := NewQuantity(7, 3)
	for _, a := range UnitCodes() {
		for _, b := range UnitCodes() {
			ua, ub := mustUnit(t, a), mustUnit(t, b)
			if !ua.CanConvertTo(ub) {
				continue
			}
			there, err := Convert(q, ua, ub)
			if err != nil {
				t.Fatalf("%s→%s: %v", a, b, err)
			}
			back, err := Convert(there, ub, ua)
			if err != nil {
				t.Fatalf("%s→%s: %v", b, a, err)
			}
			if !back.Equal(q) {
				t.Errorf("round trip %s→%s→%s = %s, want %s", a, b, a, back, q)
			}
		}
	}
}

func TestAmountAddAndScale(t *testing.T) {
	half := Amount{Quantity: NewQuantity(1, 2), Unit: mustUnit(t, "count")}
	sum, err := half.Add(half)
	if err != nil || !sum.Quantity.Equal(NewQuantity(1, 1)) {
		t.Errorf("½ onion + ½ onion = %s, %v; want 1", sum.Quantity, err)
	}

	tbsp := Amount{Quantity: NewQuantity(1, 1), Unit: mustUnit(t, "tbsp")}
	tsp := Amount{Quantity: NewQuantity(3, 1), Unit: mustUnit(t, "tsp")}
	got, err := tbsp.Add(tsp)
	if err != nil || !got.Quantity.Equal(NewQuantity(2, 1)) || got.Unit.Code != "tbsp" {
		t.Errorf("1 tbsp + 3 tsp = %s %s, %v; want 2 tbsp", got.Quantity, got.Unit.Code, err)
	}

	onion := Amount{Quantity: NewQuantity(1, 1), Unit: mustUnit(t, "count")}
	onionOz := Amount{Quantity: NewQuantity(8, 1), Unit: mustUnit(t, "oz")}
	if _, err := onion.Add(onionOz); !errors.Is(err, ErrIncompatibleUnits) {
		t.Errorf("1 onion + 8 oz onion error = %v, want ErrIncompatibleUnits", err)
	}

	if scaled := tbsp.Scale(NewQuantity(2, 1)); !scaled.Quantity.Equal(NewQuantity(2, 1)) {
		t.Errorf("scale = %s", scaled.Quantity)
	}
}

func TestLookupUnknownUnit(t *testing.T) {
	if _, err := LookupUnit("dollop"); !errors.Is(err, ErrUnknownUnit) {
		t.Errorf("error = %v, want ErrUnknownUnit", err)
	}
}

func mustParse(t *testing.T, s string) Quantity {
	t.Helper()
	q, err := ParseQuantity(s)
	if err != nil {
		t.Fatal(err)
	}
	return q
}
