package db

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// TestPackageSource_NoFileBackedHookAPI pins feat-726637b8: nothing in
// production opens a file-backed database or writes hook rows any more, so
// the openers and hook-era writers are gone, not just unused. Tests open
// databases through dbtest.Open.
func TestPackageSource_NoFileBackedHookAPI(t *testing.T) {
	gone := map[string]bool{
		"Open": true, "OpenReadOnly": true, "OpenWritable": true, "OpenWithBusyTimeout": true,
		"OpenReadOnlyMigrated": true, "OpenReadOnlyFast": true, "BuildPragmas": true,
		"InsertEvent": true, "EnsureSession": true, "UpsertSession": true, "GetToolUseContext": true,
		"InsertLineageTraceStmt": true, "CompleteLineageTrace": true, "UpdateSessionHandoff": true,
		"HeartbeatClaimByWorkItem": true, "ReleaseAllClaimsForSession": true, "ReapExpiredClaimsStmt": true,
	}
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
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && gone[fd.Name.Name] {
				t.Errorf("%s still declares %s", file, fd.Name.Name)
			}
		}
	}
}
