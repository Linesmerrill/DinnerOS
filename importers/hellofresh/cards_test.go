package main

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// Fixtures are synthetic; no real recipe card enters the repository.

// pdfText is one string placed on a synthetic page. TJ, when set, is shown as
// a TJ array instead of Text.
type pdfText struct {
	X, Y, Size float64
	Text       string
	TJ         string
}

// testWidth is the advance of every glyph in the synthetic font, in thousandths.
const testWidth = 500

// buildTestPDF writes a minimal PDF with one simple font (WinAnsi, fixed width)
// and one page per entry. Pages after the first are Flate-compressed.
func buildTestPDF(t *testing.T, pages ...[]pdfText) []byte {
	t.Helper()
	winAnsi := strings.NewReplacer("•", "\x95", "½", "\xbd", "°", "\xb0", "(", `\(`, ")", `\)`, `\`, `\\`)
	var objs []string
	add := func(s string) int { objs = append(objs, s); return len(objs) }

	catalog := add("") // filled in below
	pagesObj := add("")
	widths := strings.TrimSpace(strings.Repeat(fmt.Sprintf("%d ", testWidth), 224))
	font := add(fmt.Sprintf("<< /Type /Font /Subtype /Type1 /BaseFont /ABCDEF+TestSans /Encoding /WinAnsiEncoding /FirstChar 32 /LastChar 255 /Widths [%s] >>", widths))
	var kids []string
	for i, texts := range pages {
		var content strings.Builder
		for _, tx := range texts {
			show := fmt.Sprintf("(%s) Tj", winAnsi.Replace(tx.Text))
			if tx.TJ != "" {
				show = tx.TJ + " TJ"
			}
			fmt.Fprintf(&content, "BT /F1 %g Tf 1 0 0 1 %g %g Tm %s ET\n", tx.Size, tx.X, tx.Y, show)
		}
		stream, filter := content.String(), ""
		if i > 0 {
			var buf bytes.Buffer
			zw := zlib.NewWriter(&buf)
			_, _ = zw.Write([]byte(stream))
			_ = zw.Close()
			stream, filter = buf.String(), " /Filter /FlateDecode"
		}
		contents := add(fmt.Sprintf("<< /Length %d%s >>\nstream\n%s\nendstream", len(stream), filter, stream))
		page := add(fmt.Sprintf("<< /Type /Page /Parent %d 0 R /MediaBox [0 0 792 612] /Contents %d 0 R >>", pagesObj, contents))
		kids = append(kids, fmt.Sprintf("%d 0 R", page))
	}
	objs[catalog-1] = fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", pagesObj)
	objs[pagesObj-1] = fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d /Resources << /Font << /F1 %d 0 R >> >> >>", strings.Join(kids, " "), len(kids), font)

	var out bytes.Buffer
	out.WriteString("%PDF-1.5\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = out.Len()
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&out, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root %d 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, catalog, xref)
	return out.Bytes()
}

// syntheticCard lays text out the way a printed recipe card does: title and
// ingredient grid (with a HelloCustom swap box) on the front, "Bust out" list
// and step columns (with a call-out and a footnote) on the back.
func syntheticCard(t *testing.T) []byte {
	front := []pdfText{
		{X: 256, Y: 583, Size: 18, TJ: "[(SMOKY TEST) -250 (PORK CHILI)]"},
		{X: 256, Y: 565, Size: 12, Text: "with Synthetic Beans & Test Crema"},
		{X: 85, Y: 535, Size: 11, Text: "INGREDIENTS"},
		{X: 94, Y: 520, Size: 6, Text: "2 PERSON | 4 PERSON"},
		// Row 1: three cells, 2-person amount then 4-person amount.
		{X: 32.8, Y: 468, Size: 7, Text: "10 oz"}, {X: 56.8, Y: 468, Size: 7, Text: "20 oz"},
		{X: 30, Y: 460, Size: 7, Text: "Ground Pork*"},
		{X: 100, Y: 468, Size: 7, Text: "1½ TBSP"}, {X: 131, Y: 468, Size: 7, Text: "3 TBSP"},
		{X: 110, Y: 460, Size: 7, Text: "Test Crema"},
		{X: 105, Y: 453, Size: 6, Text: "Contains: Milk,"},
		{X: 120, Y: 446, Size: 6, Text: "Eggs"},
		{X: 190, Y: 468, Size: 7, Text: "1"}, {X: 199, Y: 468, Size: 7, Text: "2"},
		{X: 190, Y: 460, Size: 7, Text: "Lime"},
		// Row 2: one cell.
		{X: 28, Y: 410, Size: 7, Text: "2 cloves"}, {X: 62, Y: 410, Size: 7, Text: "4 cloves"},
		{X: 35, Y: 402, Size: 7, Text: "Garlic"},
		// Footnote, then the optional swap, which is not part of the meal.
		{X: 28, Y: 182, Size: 5.5, Text: "*The ingredient you received may differ."},
		{X: 88, Y: 126, Size: 11, Text: "HelloCustom"},
		{X: 66, Y: 52, Size: 7, Text: "10 oz"}, {X: 91, Y: 52, Size: 7, Text: "20 oz"},
		{X: 63, Y: 44, Size: 7, Text: "Ground Beef"},
		{X: 372, Y: 27, Size: 11, Text: "PREP: 10 MIN"},
		{X: 470, Y: 27, Size: 11, Text: "COOK: 30 MIN"},
		{X: 571, Y: 27, Size: 11, Text: "CALORIES: 650"},
	}
	back := []pdfText{
		{X: 76, Y: 352, Size: 11, Text: "BUST OUT"},
		{X: 29.5, Y: 335, Size: 9, Text: "•"}, {X: 36.5, Y: 335, Size: 9, Text: "Large pot"},
		{X: 29.5, Y: 320, Size: 9, Text: "•"}, {X: 36.5, Y: 320, Size: 9, Text: "Kosher salt"},
		{X: 29.5, Y: 305, Size: 9, Text: "•"}, {X: 36.5, Y: 305, Size: 9, Text: "Cooking oil (1 tsp | 2 tsp)"},
		{X: 113, Y: 335, Size: 9, Text: "•"}, {X: 120, Y: 335, Size: 9, Text: "Strainer"},
		{X: 113, Y: 320, Size: 9, Text: "•"}, {X: 120, Y: 320, Size: 9, Text: "Butter (1 TBSP | 2 TBSP)"},
		{X: 120, Y: 313, Size: 6, Text: "Contains: Milk"},

		{X: 219, Y: 492, Size: 10, Text: "1 PREP"},
		{X: 210, Y: 478, Size: 8, Text: "•"}, {X: 217, Y: 478, Size: 8, Text: "Wash and dry produce. Dice"},
		{X: 217, Y: 468.5, Size: 8, Text: "onion."},
		{X: 210, Y: 456, Size: 8, Text: "•"}, {X: 217, Y: 456, Size: 8, Text: "Zest lime."},
		{X: 226, Y: 440, Size: 8, Text: "Swap in beef if you chose it."},

		{X: 412, Y: 492, Size: 10, Text: "2 COOK CHILI"},
		{X: 403, Y: 478, Size: 8, Text: "•"}, {X: 410, Y: 478, Size: 8, Text: "Cook pork* in a large pot,"},
		{X: 410, Y: 468.5, Size: 8, Text: "4-5 minutes."},

		{X: 604, Y: 492, Size: 10, Text: "3 FINISH"},
		{X: 595, Y: 478, Size: 8, Text: "•"}, {X: 602, Y: 478, Size: 8, Text: "Serve with crema."},
		{X: 695, Y: 56, Size: 5.5, Text: "*Pork is cooked at 160°."},
		{X: 78, Y: 23, Size: 8, Text: "SHARE YOUR PICS"},
	}
	return buildTestPDF(t, front, back)
}

func TestExtractPDFTextPlacesRuns(t *testing.T) {
	runs, err := ExtractPDFText(syntheticCard(t))
	if err != nil {
		t.Fatalf("ExtractPDFText() error = %v", err)
	}
	var title, bullet, degrees *TextRun
	for i := range runs {
		switch r := &runs[i]; {
		case r.Page == 1 && r.Size == 18:
			title = r
		case r.Page == 2 && r.Text == "•" && r.Y == 335 && r.X == 29.5:
			bullet = r
		case strings.Contains(r.Text, "160"):
			degrees = r
		}
	}
	// The TJ gap becomes a space; widths advance the end position.
	if title == nil || title.Text != "SMOKY TEST PORK CHILI" || title.Font != "TestSans" || title.X != 256 || title.Y != 583 {
		t.Fatalf("title run = %+v", title)
	}
	if bullet == nil || bullet.EndX != 29.5+9*testWidth/1000.0 {
		t.Errorf("bullet run (compressed page) = %+v", bullet)
	}
	if degrees == nil || degrees.Text != "*Pork is cooked at 160°." {
		t.Errorf("WinAnsi text = %+v", degrees)
	}
}

func TestParseCard(t *testing.T) {
	card, err := ParseCard(syntheticCard(t))
	if err != nil {
		t.Fatalf("ParseCard() error = %v", err)
	}
	if card.Name != "SMOKY TEST PORK CHILI" || card.Headline != "with Synthetic Beans & Test Crema" {
		t.Errorf("title = %q / %q", card.Name, card.Headline)
	}
	if card.PrepMinutes != 10 || card.CookMinutes != 30 || card.Calories != 650 || !slices.Equal(card.Servings, []int{2, 4}) {
		t.Errorf("times %d/%d calories %d servings %v", card.PrepMinutes, card.CookMinutes, card.Calories, card.Servings)
	}

	type line struct {
		name      string
		pantry    bool
		amounts   string
		allergens string
	}
	var got []line
	for _, ing := range card.Ingredients {
		var amounts []string
		for _, a := range ing.Amounts {
			q := "-"
			if a.Quantity != nil {
				q = fmt.Sprint(*a.Quantity)
			}
			amounts = append(amounts, fmt.Sprintf("%d:%s %s", a.Servings, q, a.Unit))
		}
		got = append(got, line{ing.Name, ing.Pantry, strings.Join(amounts, ", "), strings.Join(ing.Allergens, ",")})
	}
	want := []line{
		{"Ground Pork", false, "2:10 oz, 4:20 oz", ""},
		{"Test Crema", false, "2:1.5 tbsp, 4:3 tbsp", "Milk,Eggs"},
		{"Lime", false, "2:1 unit, 4:2 unit", ""},
		{"Garlic", false, "2:2 cloves, 4:4 cloves", ""},
		{"Kosher salt", true, "", ""},
		{"Cooking oil", true, "2:1 tsp, 4:2 tsp", ""},
		{"Butter", true, "2:1 tbsp, 4:2 tbsp", "Milk"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("ingredients:\n got %+v\nwant %+v", got, want)
	}
	if !slices.Equal(card.Utensils, []string{"Large pot", "Strainer"}) {
		t.Errorf("utensils = %q", card.Utensils)
	}
	wantSteps := []string{
		"• Wash and dry produce. Dice\nonion.\n• Zest lime.",
		"• Cook pork* in a large pot,\n4-5 minutes.",
		"• Serve with crema.",
	}
	if !slices.Equal(card.Steps, wantSteps) {
		t.Errorf("steps:\n got %q\nwant %q", card.Steps, wantSteps)
	}

	r := card.hfRecipe("dddddddddddddddddddddddd")
	if r.Name != "Smoky Test Pork Chili" || r.PrepTime != "PT10M" || r.TotalTime != "PT30M" || len(r.Yields) != 2 || len(r.Yields[1].Ingredients) != len(card.Ingredients) {
		t.Errorf("hfRecipe() = %+v", r)
	}
}

func TestParseCardRejectsOtherLayouts(t *testing.T) {
	onlyTitle := buildTestPDF(t, []pdfText{{X: 100, Y: 500, Size: 18, Text: "A FLYER, NOT A CARD"}})
	if _, err := ParseCard(onlyTitle); !errors.Is(err, ErrCardLayout) {
		t.Errorf("ParseCard(flyer) error = %v, want ErrCardLayout", err)
	}
	// Amounts that don't pair up with the serving columns are not guessed at.
	lopsided := buildTestPDF(t, []pdfText{
		{X: 256, Y: 583, Size: 18, Text: "TEST TACOS"},
		{X: 85, Y: 535, Size: 11, Text: "INGREDIENTS"},
		{X: 94, Y: 520, Size: 6, Text: "2 PERSON | 4 PERSON"},
		{X: 30, Y: 468, Size: 7, Text: "10 oz"}, {X: 55, Y: 468, Size: 7, Text: "20 oz"}, {X: 110, Y: 468, Size: 7, Text: "1"},
		{X: 30, Y: 460, Size: 7, Text: "Pork"}, {X: 110, Y: 460, Size: 7, Text: "Lime"},
	})
	if _, err := ParseCard(lopsided); !errors.Is(err, ErrCardLayout) {
		t.Errorf("ParseCard(lopsided) error = %v, want ErrCardLayout", err)
	}
	if _, err := ParseCard([]byte("<Error>AccessDenied</Error>")); err == nil {
		t.Error("ParseCard(not a PDF) succeeded")
	}
}

func TestParseToUnicode(t *testing.T) {
	cmap := []byte(`begincmap
1 begincodespacerange <00> <FF> endcodespacerange
2 beginbfchar <01> <0041> <02> <00BD> endbfchar
1 beginbfrange <10> <12> <0061> endbfrange
1 beginbfrange <20> <21> [<0066006C> <2022>] endbfrange
endcmap`)
	m, lens := parseToUnicode(cmap)
	want := map[string]string{"\x01": "A", "\x02": "½", "\x10": "a", "\x11": "b", "\x12": "c", "\x20": "fl", "\x21": "•"}
	for code, s := range want {
		if m[code] != s {
			t.Errorf("code %x = %q, want %q", code, m[code], s)
		}
	}
	if !slices.Equal(lens, []int{1}) {
		t.Errorf("code lengths = %v", lens)
	}
}

func TestFetchCardsIsPoliteAndRecordsMisses(t *testing.T) {
	pdf := buildTestPDF(t, []pdfText{{X: 1, Y: 1, Size: 9, Text: "x"}})
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/card/aaaaaaaaaaaaaaaaaaaaaaaa-en-US.pdf":
			_, _ = w.Write(pdf)
		case "/card/cccccccccccccccccccccccc.pdf":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte("<html>blocked</html>"))
		default: // how the card bucket answers a missing key
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`<?xml version="1.0"?><Error><Code>AccessDenied</Code></Error>`))
		}
	}))
	defer srv.Close()

	f := testFetcher(srv.URL + "/recipes/")
	f.CardBaseURL = srv.URL + "/card/"
	var sleeps int
	f.Sleep = func(context.Context, time.Duration) error { sleeps++; return nil }
	dir := t.TempDir()
	targets := []CardTarget{
		{DeliveredID: "aaaaaaaaaaaaaaaaaaaaaaaa"},
		{DeliveredID: "bbbbbbbbbbbbbbbbbbbbbbbb", CardLink: srv.URL + "/card/bbbbbbbbbbbbbbbbbbbbbbbb-en-US-123-auto.pdf"},
	}
	stats, err := f.FetchCards(context.Background(), targets, dir, false)
	if err != nil || stats.Fetched != 1 || stats.Missing != 1 {
		t.Fatalf("FetchCards() = %+v, %v", stats, err)
	}
	wantPaths := []string{
		"/card/aaaaaaaaaaaaaaaaaaaaaaaa.pdf", "/card/aaaaaaaaaaaaaaaaaaaaaaaa-en-US.pdf",
		"/card/bbbbbbbbbbbbbbbbbbbbbbbb-en-US-123-auto.pdf", "/card/bbbbbbbbbbbbbbbbbbbbbbbb.pdf", "/card/bbbbbbbbbbbbbbbbbbbbbbbb-en-US.pdf",
	}
	if !slices.Equal(paths, wantPaths) || sleeps != len(wantPaths)-1 {
		t.Errorf("requests %v with %d delays, want %v one delay apart", paths, sleeps, wantPaths)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "cards", "aaaaaaaaaaaaaaaaaaaaaaaa.pdf")); !bytes.Equal(got, pdf) {
		t.Error("card not cached")
	}
	var miss cardMiss
	data, _ := os.ReadFile(filepath.Join(dir, "cards", "bbbbbbbbbbbbbbbbbbbbbbbb.missing.json"))
	if err := json.Unmarshal(data, &miss); err != nil || len(miss.Tried) != 3 {
		t.Errorf("miss record = %s (%v)", data, err)
	}

	// A re-run asks for nothing it already knows.
	paths = nil
	if stats, err := f.FetchCards(context.Background(), targets, dir, false); err != nil || stats.Cached != 2 || len(paths) != 0 {
		t.Errorf("re-run = %+v, %v, requests %v", stats, err, paths)
	}

	// A refusal that isn't a missing object stops the run.
	_, err = f.FetchCards(context.Background(), []CardTarget{{DeliveredID: "cccccccccccccccccccccccc"}}, dir, false)
	if !errors.Is(err, ErrBlocked) {
		t.Errorf("blocked error = %v, want ErrBlocked", err)
	}
}

func TestParseCachedCardsWritesOnlyParsedCards(t *testing.T) {
	dir := t.TempDir()
	cards := filepath.Join(dir, "cards")
	if err := os.MkdirAll(cards, 0o755); err != nil {
		t.Fatal(err)
	}
	good, bad := "aaaaaaaaaaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbbbbbbbbbb"
	_ = os.WriteFile(filepath.Join(cards, good+".pdf"), syntheticCard(t), 0o644)
	_ = os.WriteFile(filepath.Join(cards, bad+".pdf"), buildTestPDF(t, []pdfText{{X: 1, Y: 1, Size: 20, Text: "FLYER"}}), 0o644)
	_ = os.WriteFile(filepath.Join(cards, bad+".json"), []byte(`{"stale":true}`), 0o644)

	parsed, failed, err := ParseCachedCards(dir, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	if err != nil || parsed != 1 || len(failed) != 1 || failed[bad] == "" {
		t.Fatalf("ParseCachedCards() = %d, %v, %v", parsed, failed, err)
	}
	if _, err := os.Stat(filepath.Join(cards, bad+".json")); !errors.Is(err, os.ErrNotExist) {
		t.Error("stale JSON for an unparseable card was kept")
	}
	raws, err := LoadRawRecipes(dir)
	if err != nil || len(raws) != 1 || raws[0].Origin != OriginCard || raws[0].DeliveredID != good {
		t.Errorf("LoadRawRecipes() = %+v, %v", raws, err)
	}
}
