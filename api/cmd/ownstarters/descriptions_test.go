package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/Linesmerrill/DinnerOS/api/internal/catalog"
	"github.com/Linesmerrill/DinnerOS/api/internal/platform/mongodb/mongotest"
)

// The meal kit's old words for Pork Sausage & Roasted Pepper Pasta, as the
// Shared Recipes page showed them in build 123 (Hark's review).
const porkSausageMealKit = "Are you the type of person who only goes to baseball games for that huge hoagie stuffed with sausage and peppers? We’re with you. This week, our chefs took the beloved dish from stadium fare to elegant weeknight dinner with a few delicious twists. It’s safe to say this dish knocks it out of the park."

func loadAllDescriptions(t *testing.T) []Description {
	t.Helper()
	files, err := filepath.Glob("descriptions/*.json")
	if err != nil || len(files) == 0 {
		t.Fatalf("no description files: %v", err)
	}
	var all []Description
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var list []Description
		if err := json.Unmarshal(raw, &list); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		all = append(all, list...)
	}
	return all
}

// starterNames is every starter in the production starter library, from the
// photo manifest (one entry per starter id).
func starterNames(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile("../../../docs/recipe-images/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Recipes []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"recipes"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, r := range m.Recipes {
		if r.Title != "" {
			names = append(names, r.Title)
		}
	}
	if len(names) < 400 {
		t.Fatalf("manifest lists %d starters, want the whole library", len(names))
	}
	return names
}

// Every phrase in the table is caught, and ordinary food words are not.
func TestMealKitPhrasesTable(t *testing.T) {
	bad := []struct{ phrase, text string }{
		{"our chefs", "Our chefs took the dish uptown."},
		{"chef", "A chef-approved favorite."},
		{"this week", "This week, it's tacos."},
		{"shipping", "Ships with everything."},
		{"delivery", "Delivered fresh."},
		{"box", "Everything in the box."},
		{"meal kit", "A meal kit classic."},
		{"HelloFresh", "A HelloFresh favorite."},
		{"em dash", "Chicken — with rice."},
		{"en dash", "Chicken – with rice."},
		{"hyphen used as a dash", "Chicken - with rice."},
		{"Are you the type", "Are you the type who loves tacos?"},
		{"we or our", "We think it works."},
		{"you or your", "You'll want seconds."},
		{"exclamation", "So good!"},
		{"subscription", "For subscribers only."},
		{"pre-portioned", "Pre-portioned spices."},
		{"recipe card", "Check the recipe card."},
		{"on the menu", "Back on the menu."},
		{"fan favorite", "A fan favorite."},
		{"knocks it out of the park", "It knocks it out of the park."},
		{"taste buds", "Wakes up the taste buds."},
		{"to your door", "Right to the door."},
		{"unboxing", "Unbox and cook."},
	}
	names := map[string]bool{}
	for _, p := range mealKitPhrases {
		names[p.Name] = true
	}
	for _, b := range bad {
		if !names[b.phrase] {
			t.Errorf("table has no %q row", b.phrase)
		}
		hit := false
		for _, p := range mealKitPhrases {
			if p.Name == b.phrase && p.Pattern.MatchString(b.text) {
				hit = true
			}
		}
		if !hit {
			t.Errorf("%q row misses %q", b.phrase, b.text)
		}
	}
	if len(bad) != len(mealKitPhrases) {
		t.Errorf("%d samples for %d rows: give every row a sample", len(bad), len(mealKitPhrases))
	}
	if got := mealKitPhrase(porkSausageMealKit); got == "" {
		t.Error("the old Pork Sausage pasta words pass")
	}
	for _, ok := range []string{
		"Flour tortillas with sour cream, Tex-Mex spices and one-pan rice.",
		"Sweet-and-sour pork with bok choy, a boxty-free plate.",
		"Cavatappi in a creamy garlic tomato sauce with browned pork sausage and roasted red pepper, finished with Parmesan.",
	} {
		if p := mealKitPhrase(ok); p != "" {
			t.Errorf("%q flagged as %q", ok, p)
		}
	}
}

// Every starter description in git reads plainly: none uses a phrase from
// the table, each is one or two short sentences, and no two repeat.
func TestStarterDescriptionsArePlain(t *testing.T) {
	all := loadAllDescriptions(t)
	sentenceEnd := regexp.MustCompile(`[.?]\s+[A-Z]`)
	seenDesc := map[string]string{}
	seenSentence := map[string]string{}
	for _, d := range all {
		for _, p := range mealKitPhrases {
			if p.Pattern.MatchString(d.Description) {
				t.Errorf("%s: uses %q: %q", d.Name, p.Name, d.Description)
			}
		}
		if n := len(sentenceEnd.FindAllString(d.Description, -1)) + 1; n > 2 {
			t.Errorf("%s: %d sentences, want one or two", d.Name, n)
		}
		if w := len(strings.Fields(d.Description)); w < 5 || w > 35 {
			t.Errorf("%s: %d words, want 5 to 35", d.Name, w)
		}
		if !strings.HasSuffix(d.Description, ".") {
			t.Errorf("%s: doesn't end with a period", d.Name)
		}
		if strings.ContainsAny(d.Description, "&’") {
			t.Errorf("%s: write \"and\" and a straight apostrophe: %q", d.Name, d.Description)
		}
		if prev, ok := seenDesc[d.Description]; ok {
			t.Errorf("%s and %s have the same description", prev, d.Name)
		}
		seenDesc[d.Description] = d.Name
		for _, s := range regexp.MustCompile(`(?:[.?])\s+`).Split(strings.TrimSuffix(d.Description, "."), -1) {
			k := strings.ToLower(strings.TrimSpace(s))
			if prev, ok := seenSentence[k]; ok && k != "" {
				t.Errorf("%s repeats %s's sentence %q", d.Name, prev, s)
			}
			seenSentence[k] = d.Name
		}
	}
	for _, f := range []string{"descriptions/build-124.json"} {
		if _, err := loadDescriptions(f); err != nil {
			t.Errorf("loadDescriptions(%s): %v", f, err)
		}
	}
	// Fix descriptions are starter descriptions too.
	files, _ := filepath.Glob("fixes/*.json")
	for _, f := range files {
		fixes, err := loadFixes(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, fx := range fixes {
			if p := mealKitPhrase(fx.Description); p != "" {
				t.Errorf("%s %s: uses %q", f, fx.CatalogKey, p)
			}
		}
	}
}

// Every starter in the library has a description, so none is left with the
// meal kit's words, and the file names no starter that doesn't exist.
func TestEveryStarterHasADescription(t *testing.T) {
	have := map[string]bool{}
	for _, d := range loadAllDescriptions(t) {
		have[nameKey(d.Name)] = true
	}
	starters := map[string]bool{}
	for _, n := range starterNames(t) {
		starters[nameKey(n)] = true
		if !have[nameKey(n)] {
			t.Errorf("starter %q has no description", n)
		}
	}
	// Fixes may rename a starter (build 122); the new name is a starter too.
	starters[nameKey("One-Pan Cheesy Chicken Enchiladas")] = true
	for n := range have {
		if !starters[n] {
			t.Errorf("description for %q, which is no starter", n)
		}
	}
	want := map[string]string{
		"Pork Sausage & Roasted Pepper Pasta":    "Cavatappi in a creamy garlic tomato sauce with browned pork sausage and roasted red pepper, finished with Parmesan.",
		"Ginger Chicken & Rice Bowls":            "Chicken, snap peas and red pepper in a sticky, sweet, gently spicy ginger sauce over rice, topped with scallions, sesame seeds and chili flakes.",
		"One-Pan Cheesy Chicken Enchiladas":      "Chicken thighs and blistered green pepper simmered with salsa verde, rolled in warm tortillas, covered in Mexican cheese, broiled until bubbly and drizzled with a spicy sour cream crema.",
		"Cuban-Spiced Pork with Pineapple Salsa": "Seared pork with Cuban spices and a citrus onion sauce, served with cilantro-lime rice and a fresh pineapple salsa.",
	}
	got := map[string]string{}
	for _, d := range loadAllDescriptions(t) {
		got[d.Name] = d.Description
	}
	for n, w := range want {
		if got[n] != w {
			t.Errorf("%s = %q, want %q", n, got[n], w)
		}
	}
}

func TestLoadDescriptionsRefuses(t *testing.T) {
	dir := t.TempDir()
	for name, list := range map[string][]Description{
		"meal kit": {{Name: "Pork Sausage & Roasted Pepper Pasta", Description: porkSausageMealKit}},
		"twice":    {{Name: "Tacos", Description: "Beef tacos."}, {Name: " tacos ", Description: "Pork tacos."}},
		"empty":    {{Name: "Tacos"}},
	} {
		raw, _ := json.Marshal(list)
		p := filepath.Join(dir, strings.ReplaceAll(name, " ", "-")+".json")
		if err := os.WriteFile(p, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadDescriptions(p); err == nil {
			t.Errorf("%s: loaded", name)
		}
	}
}

func TestIntegrationStarterDescriptions(t *testing.T) {
	client := mongotest.Client(t)
	ctx := t.Context()
	db := client.Database()
	coll := db.Collection("recipes")
	starter, home, ordered, own, sharer := bson.NewObjectID(), bson.NewObjectID(), bson.NewObjectID(), bson.NewObjectID(), bson.NewObjectID()
	pasta, other := bson.NewObjectID(), bson.NewObjectID()
	const old = porkSausageMealKit
	const fresh = "Cavatappi in a creamy garlic tomato sauce with browned pork sausage and roasted red pepper, finished with Parmesan."
	rec := func(id, hh bson.ObjectID, source, srcID, name, desc string, extra ...bson.E) bson.D {
		d := bson.D{{Key: "_id", Value: id}, {Key: "householdId", Value: hh}, {Key: "source", Value: source},
			{Key: "sourceRecipeId", Value: srcID}, {Key: "catalogKey", Value: source + ":" + srcID},
			{Key: "name", Value: name}, {Key: "description", Value: desc}}
		return append(d, extra...)
	}
	homeCopy, orderedCopy, ownCopy, shared := bson.NewObjectID(), bson.NewObjectID(), bson.NewObjectID(), bson.NewObjectID()
	if _, err := coll.InsertMany(ctx, []any{
		rec(pasta, starter, "hellofresh", "p1", "Pork Sausage & Roasted Pepper Pasta", old),
		rec(other, starter, "hellofresh", "x1", "A Starter Not In The File", "Old words."),
		rec(bson.NewObjectID(), starter, "hellofresh", "x2", "A Deleted Starter", "Old words.", bson.E{Key: "deletedAt", Value: "2026-11-01"}),
		// A starter copy, as the starter library made it.
		rec(homeCopy, home, "hellofresh", "p1", "Pork Sausage & Roasted Pepper Pasta", old),
		// The household's own meal-kit import of the same dish: it has order history.
		rec(orderedCopy, ordered, "hellofresh", "p1", "Pork Sausage & Roasted Pepper Pasta", old,
			bson.E{Key: "orderWeeks", Value: bson.A{"2025-W10"}}, bson.E{Key: "timesOrdered", Value: 1}),
		// A copy whose description the household changed.
		rec(ownCopy, own, "hellofresh", "p1", "Pork Sausage & Roasted Pepper Pasta", "Our Friday pasta."),
		// Someone's link import of the same page, shared to Shared Recipes.
		rec(shared, sharer, "import", "neighbor-5", "Pork Sausage & Roasted Pepper Pasta", old),
	}); err != nil {
		t.Fatal(err)
	}
	entries := db.Collection(catalog.RecipesCollection)
	starterEntry, importEntry := bson.NewObjectID(), bson.NewObjectID()
	if _, err := entries.InsertMany(ctx, []any{
		bson.D{{Key: "_id", Value: starterEntry}, {Key: "catalogKey", Value: "hellofresh:p1"}, {Key: "description", Value: old}},
		bson.D{{Key: "_id", Value: importEntry}, {Key: "catalogKey", Value: "import:neighbor-5"}, {Key: "description", Value: old}},
	}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "d.json")
	raw, _ := json.Marshal([]Description{{Name: "pork sausage & roasted pepper pasta", Description: fresh}})
	if err := os.WriteFile(file, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"MONGODB_URI": os.Getenv("MONGODB_TEST_URI"), "MONGODB_DATABASE": db.Name(), "STARTER_RECIPES_HOUSEHOLD_ID": starter.Hex()}
	getenv := func(k string) string { return env[k] }
	args := []string{"-descriptions", file}

	var dry bytes.Buffer
	if err := run(ctx, args, getenv, &dry); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(dry.String(), "A Deleted Starter") {
		t.Errorf("dry run lists a deleted starter: %q", dry.String())
	}
	for _, want := range []string{
		"descriptions: would update 1 starters, 1 copies and 1 Shared Recipes entries; 1 starters have no description in the file",
		"no description: A Starter Not In The File",
	} {
		if !strings.Contains(dry.String(), want) {
			t.Errorf("dry run = %q, want %q", dry.String(), want)
		}
	}
	desc := func(c string, id bson.ObjectID) string {
		var d struct {
			Description string `bson:"description"`
		}
		if err := db.Collection(c).FindOne(ctx, bson.D{{Key: "_id", Value: id}}).Decode(&d); err != nil {
			t.Fatal(err)
		}
		return d.Description
	}
	if desc("recipes", pasta) != old {
		t.Fatal("the dry run wrote")
	}

	var out bytes.Buffer
	if err := run(ctx, append(args, "-apply"), getenv, &out); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		what, coll string
		id         bson.ObjectID
		want       string
	}{
		{"starter", "recipes", pasta, fresh},
		{"starter copy", "recipes", homeCopy, fresh},
		{"starter's Shared Recipes entry", catalog.RecipesCollection, starterEntry, fresh},
		{"household's own meal-kit import", "recipes", orderedCopy, old},
		{"copy the household rewrote", "recipes", ownCopy, "Our Friday pasta."},
		{"someone's shared import", "recipes", shared, old},
		{"shared import's entry", catalog.RecipesCollection, importEntry, old},
		{"starter not in the file", "recipes", other, "Old words."},
	} {
		if got := desc(c.coll, c.id); got != c.want {
			t.Errorf("%s = %q, want %q", c.what, got, c.want)
		}
	}
	var again bytes.Buffer
	if err := run(ctx, append(args, "-apply"), getenv, &again); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(again.String(), "descriptions: updated 0 starters, 0 copies and 0 Shared Recipes entries") {
		t.Errorf("second run = %q", again.String())
	}
}
