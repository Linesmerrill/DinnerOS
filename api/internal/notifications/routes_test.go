package notifications

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// route is one row of testdata/routes.json, the fixture the iOS tests read
// too: every notification the server creates, and the screen tapping it opens.
type route struct {
	Type        string `json:"type"`
	SubjectKind string `json:"subjectKind"`
	Destination string `json:"destination"`
}

func loadRoutes(t *testing.T) []route {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "routes.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct{ Routes []route }
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc.Routes
}

// declaredConstants returns the string constants of typeName declared in
// model.go, by constant name.
func declaredConstants(t *testing.T, typeName string) map[string]string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "model.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs := spec.(*ast.ValueSpec)
			if id, ok := vs.Type.(*ast.Ident); !ok || id.Name != typeName {
				continue
			}
			for i, name := range vs.Names {
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok {
					t.Fatalf("%s is not a string literal", name.Name)
				}
				v, _ := strconv.Unquote(lit.Value)
				out[name.Name] = v
			}
		}
	}
	return out
}

// TestEveryTypeHasARoute fails when a notification type or subject kind is
// added without a row saying where the app opens it. Add the row, then the
// iOS route (NotificationRouting); the iOS test reads the same file.
func TestEveryTypeHasARoute(t *testing.T) {
	routes := loadRoutes(t)
	types := declaredConstants(t, "Type")
	kinds := declaredConstants(t, "SubjectKind")
	if len(types) == 0 || len(kinds) == 0 {
		t.Fatal("found no Type or SubjectKind constants in model.go")
	}
	declaredTypes, declaredKinds := map[string]bool{}, map[string]bool{}
	for _, v := range types {
		declaredTypes[v] = true
	}
	for _, v := range kinds {
		declaredKinds[v] = true
	}
	routed, usedKinds := map[string]bool{}, map[string]bool{}
	for _, r := range routes {
		if routed[r.Type] {
			t.Errorf("routes.json lists %q twice", r.Type)
		}
		routed[r.Type], usedKinds[r.SubjectKind] = true, true
		if r.Destination == "" {
			t.Errorf("routes.json: %q has no destination", r.Type)
		}
		if !declaredTypes[r.Type] {
			t.Errorf("routes.json lists %q, which model.go doesn't declare", r.Type)
		}
		if !declaredKinds[r.SubjectKind] {
			t.Errorf("routes.json: %q uses subject kind %q, which model.go doesn't declare", r.Type, r.SubjectKind)
		}
	}
	for name, v := range types {
		if !routed[v] {
			t.Errorf("%s (%q) has no row in testdata/routes.json", name, v)
		}
	}
	for name, v := range kinds {
		if !usedKinds[v] {
			t.Errorf("%s (%q) is not the subject of any route", name, v)
		}
	}
}

// TestProducersMatchRoutes reads every notifications.New literal in the API
// and checks its type and subject kind are the pair routes.json names, so a
// producer can't point a type at a record the app doesn't open for it.
func TestProducersMatchRoutes(t *testing.T) {
	want := map[string]string{}
	for _, r := range loadRoutes(t) {
		want[r.Type] = r.SubjectKind
	}
	types := declaredConstants(t, "Type")
	kinds := declaredConstants(t, "SubjectKind")
	created := map[string]bool{}
	err := filepath.WalkDir(filepath.Join("..", ".."), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == "testdata" {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok || selectorName(lit.Type) != "New" {
				return true
			}
			typ := types[selectorName(field(lit, "Type"))]
			var kind string
			if subject, ok := field(lit, "Subject").(*ast.CompositeLit); ok {
				kind = kinds[selectorName(field(subject, "Kind"))]
			}
			if typ == "" || kind == "" {
				t.Errorf("%s: a notification whose type or subject kind isn't a model.go constant", path)
				return true
			}
			created[typ] = true
			if got, ok := want[typ]; !ok || got != kind {
				t.Errorf("%s: %q points at %q; routes.json says %q", path, typ, kind, got)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for typ := range want {
		if !created[typ] {
			t.Errorf("no producer creates %q; remove its row, or the scan missed a producer", typ)
		}
	}
}

// field is the value of the keyed element name in a composite literal.
func field(lit *ast.CompositeLit, name string) ast.Expr {
	for _, el := range lit.Elts {
		if kv, ok := el.(*ast.KeyValueExpr); ok {
			if id, ok := kv.Key.(*ast.Ident); ok && id.Name == name {
				return kv.Value
			}
		}
	}
	return nil
}

// selectorName is Name for notifications.Name, and "" otherwise.
func selectorName(e ast.Expr) string {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	if id, ok := sel.X.(*ast.Ident); !ok || id.Name != "notifications" {
		return ""
	}
	return sel.Sel.Name
}
