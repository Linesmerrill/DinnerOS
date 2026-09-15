package autopilot

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestPackageStaysIndependent keeps Autopilot extractable: the contract and
// its baseline import only the standard library and each other, and never
// mention a recipe source.
func TestPackageStaysIndependent(t *testing.T) {
	const self = "github.com/Linesmerrill/DinnerOS/api/internal/autopilot"
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(strings.ToLower(string(src)), "hello"+"fresh") {
			t.Errorf("%s mentions a recipe source", path)
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, src, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if strings.Contains(p, ".") && p != self && !strings.HasPrefix(p, self+"/") {
				t.Errorf("%s imports %s; autopilot may only import the standard library", path, p)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestWeekHelpers(t *testing.T) {
	start, err := WeekStart("2026-W38")
	if err != nil || !start.Equal(time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("WeekStart(2026-W38) = %v, %v", start, err)
	}
	for _, bad := range []string{"2026-38", "2025-W53", "2026-W00"} {
		if _, err := WeekStart(bad); err == nil {
			t.Errorf("WeekStart(%q) error = nil", bad)
		}
	}
	if n, ok := WeeksBetween("2026-W52", "2027-W02"); !ok || n != 3 {
		t.Errorf("WeeksBetween() = %d, %v; want 3 across a 53-week year", n, ok)
	}
	if _, ok := WeeksBetween("bad", "2026-W02"); ok {
		t.Error("WeeksBetween(bad) ok")
	}
	bands := TimeBands{QuickMaxMinutes: 20, MediumMaxMinutes: 35}
	for minutes, want := range map[int]TimeBand{0: BandMedium, 5: BandQuick, 20: BandQuick, 21: BandMedium, 35: BandMedium, 36: BandLong} {
		if got := bands.Of(minutes); got != want {
			t.Errorf("Of(%d) = %s, want %s", minutes, got, want)
		}
	}
	if (TimeBands{}).Of(30) != BandMedium || Sunday.Index() != 6 || Day("x").Valid() || Tuesday.Plural() != "Tuesdays" {
		t.Error("day or band helpers are wrong")
	}
}
