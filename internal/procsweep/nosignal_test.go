package procsweep

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// forbiddenCalls are selector names that send, or can send, a signal to a
// process. CommandContext is here because exec kills the child when its
// context ends; the sweep bounds ps by abandoning it instead.
var forbiddenCalls = map[string]bool{
	"Kill": true, "Signal": true, "FindProcess": true, "Tgkill": true,
	"Tkill": true, "PidfdSendSignal": true, "Killpg": true, "CommandContext": true,
}

var forbiddenImports = map[string]bool{"os/signal": true}

var forbiddenExecs = map[string]bool{"kill": true, "pkill": true, "killall": true}

// TestPackageNeverSignals enforces structurally that wipnote never stops a
// process: no non-test file of this package (any GOOS) may call a signal API
// or exec a kill binary.
func TestPackageNeverSignals(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("glob package sources: %v", err)
	}
	scanned := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		scanned++
		for _, v := range signalViolations(t, f) {
			t.Errorf("%s: %s", f, v)
		}
	}
	if scanned == 0 {
		t.Fatal("no package sources scanned")
	}
}

func signalViolations(t *testing.T, path string) []string {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), path, src, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, imp := range file.Imports {
		if p, _ := strconv.Unquote(imp.Path.Value); forbiddenImports[p] {
			out = append(out, "imports "+p)
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
			if forbiddenCalls[sel.Sel.Name] {
				out = append(out, "calls "+sel.Sel.Name)
			}
			if sel.Sel.Name == "Command" && len(call.Args) > 0 && execsKill(call.Args[0]) {
				out = append(out, "execs a kill binary")
			}
		}
		return true
	})
	return out
}

func execsKill(arg ast.Expr) bool {
	lit, ok := arg.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return false
	}
	v, _ := strconv.Unquote(lit.Value)
	return forbiddenExecs[filepath.Base(v)]
}

// TestSignalScanCatchesViolations proves the scan is not vacuous.
func TestSignalScanCatchesViolations(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "bad.go")
	src := "package x\nimport (\"os\"; \"os/exec\"; _ \"os/signal\")\n" +
		"func f(p *os.Process) { p.Kill(); exec.Command(\"/bin/kill\", \"1\") }\n"
	if err := os.WriteFile(bad, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := signalViolations(t, bad); len(got) != 3 {
		t.Fatalf("scan should flag import, Kill and kill exec; got %v", got)
	}
}
