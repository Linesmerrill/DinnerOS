package shopping

import (
	"errors"
	"strings"
	"testing"

	"github.com/Linesmerrill/DinnerOS/api/internal/households"
)

// These tests cover newStoreRequest, which is pure: no store, no Mongo.

func requestActor() households.Membership {
	return households.Membership{HouseholdID: "66e5a1f2c3b4a5d6e7f80a01", UserID: "u-1", Role: households.RoleMember}
}

// wantInvalid asserts err is a ValidationError whose message contains want.
func wantInvalid(t *testing.T, err error, want string) {
	t.Helper()
	var v *ValidationError
	if !errors.As(err, &v) {
		t.Fatalf("error = %v, want a ValidationError containing %q", err, want)
	}
	if !strings.Contains(v.Message, want) {
		t.Errorf("message = %q, want it to contain %q", v.Message, want)
	}
}

// TestNewStoreRequestRejectsSupportedStore covers both ways a household can
// name a store DinnerOS already supports: the catalog key, and a typed name
// or alias that resolves onto the same entry. Recording either would be
// demand for finished work.
func TestNewStoreRequestRejectsSupportedStore(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   StoreRequestInput
	}{
		{"explicit catalog key", StoreRequestInput{Key: "walmart"}},
		{"typed name", StoreRequestInput{Name: "Walmart"}},
		{"typed name, different case and spacing", StoreRequestInput{Name: "  wal-mart  "}},
		{"typed alias", StoreRequestInput{Name: "Walmart Supercenter"}},
		{"typed alias, Walmart+", StoreRequestInput{Name: "walmart plus"}},
		{"with a note", StoreRequestInput{Key: "walmart", Note: "we shop here"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := newStoreRequest(requestActor(), tc.in)
			wantInvalid(t, err, "already supported")
		})
	}
}

// TestNewStoreRequestAcceptsUnsupportedStores keeps the rejection narrow: it
// must not catch a store that genuinely needs the demand signal.
func TestNewStoreRequestAcceptsUnsupportedStores(t *testing.T) {
	for _, tc := range []struct {
		in        StoreRequestInput
		key, name string
	}{
		{StoreRequestInput{Key: "kroger"}, "kroger", "Kroger"},
		{StoreRequestInput{Name: "Kroger"}, "kroger", "Kroger"},
		{StoreRequestInput{Name: "krogers"}, "kroger", "Kroger"},
		{StoreRequestInput{Name: "some local market"}, "some-local-market", "Some Local Market"},
	} {
		req, err := newStoreRequest(requestActor(), tc.in)
		if err != nil {
			t.Errorf("newStoreRequest(%+v) = %v", tc.in, err)
			continue
		}
		if req.Key != tc.key || req.Name != tc.name {
			t.Errorf("newStoreRequest(%+v) = %s/%s, want %s/%s", tc.in, req.Key, req.Name, tc.key, tc.name)
		}
	}
}

// TestMatchCatalogNameNewEntries covers the spellings people actually type
// for entries the expanded catalog added.
func TestMatchCatalogNameNewEntries(t *testing.T) {
	for in, want := range map[string]string{
		"acme":            "acme",
		"Acme Markets":    "acme",
		"Shaw's":          "shaws",
		"shaws":           "shaws",
		"tjs":             "trader-joes",
		"TJ's":            "trader-joes",
		"aldis":           "aldi",
		"meijers":         "meijer",
		"stop n shop":     "stop-and-shop",
		"Pick 'n Save":    "pick-n-save",
		"picknsave":       "pick-n-save",
		"food 4 less":     "food-4-less",
		"ShopRite":        "wakefern",
		"shop rite":       "wakefern",
		"Smart & Final":   "smart-and-final",
		"fresh direct":    "freshdirect",
		"Weee!":           "weee",
		"market district": "giant-eagle",
		"Tom Thumb":       "tom-thumb",
		"hannafords":      "hannaford",
	} {
		e, ok := matchCatalogName(in)
		if !ok || e.Key != want {
			t.Errorf("matchCatalogName(%q) = %+v, %v; want %s", in, e, ok, want)
		}
	}
}
