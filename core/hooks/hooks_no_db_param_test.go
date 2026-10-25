package hooks

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// TestHooksSource_NoFunctionTakesASQLDB pins feat-726637b8: no hook opens a
// database (feat-174dc958), so no function in the hooks package takes a
// *sql.DB. A nil handle threaded through every handler only kept dead guard
// branches alive.
func TestHooksSource_NoFunctionTakesASQLDB(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if ft, ok := n.(*ast.FuncType); ok && takesSQLDB(ft) {
				t.Errorf("%s: a function takes a *sql.DB", fset.Position(ft.Pos()))
			}
			return true
		})
	}
}

func takesSQLDB(ft *ast.FuncType) bool {
	for _, p := range ft.Params.List {
		star, ok := p.Type.(*ast.StarExpr)
		if !ok {
			continue
		}
		if sel, ok := star.X.(*ast.SelectorExpr); ok && sel.Sel.Name == "DB" {
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == "sql" {
				return true
			}
		}
	}
	return false
}
