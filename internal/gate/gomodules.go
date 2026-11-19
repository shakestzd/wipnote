package gate

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// skippedModuleDirs never hold a module the gate checks: vendored or
// installed code, tool state and test fixtures.
var skippedModuleDirs = map[string]bool{
	"vendor": true, "node_modules": true, ".git": true, ".claude": true,
	".gotmp-exec": true, "testdata": true,
}

// FindGoModules returns the folder of every go.mod under root, root first
// when it has one, the rest sorted. Hidden folders and skippedModuleDirs are
// not entered (bug-9f3b580f: the gate checked one module and missed the rest).
func FindGoModules(root string) []string {
	var dirs []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if path != root && (skippedModuleDirs[name] || strings.HasPrefix(name, ".")) {
				return filepath.SkipDir
			}
			if _, statErr := os.Stat(filepath.Join(path, "go.mod")); statErr == nil {
				dirs = append(dirs, path)
			}
		}
		return nil
	})
	sort.Strings(dirs) // a parent folder sorts before its children
	return dirs
}

// goModuleCommands is the Go build, vet and test run for one module. rel is
// the module folder under the code root, "" for a plain single-module plan.
func goModuleCommands(rel, dir string) []Command {
	label := func(n string) string {
		if rel == "" {
			return n
		}
		return n + " (" + rel + ")"
	}
	// The `go test ... -short ./...` step covers the whole module, which
	// includes the always-on durability contention fixture
	// (DurabilityContentionFixtureTest in DurabilityContentionFixturePkg).
	// That fixture is intentionally NOT skipped under -short, so a
	// regression in the migrated hot hooks (plan-2390966a, feat-bbb80917)
	// fails THIS gate. GoGateRunsDurabilityFixtureUnderShort and the
	// gate_durability_test.go assertion guard that this command keeps both
	// the -short flag and the module-wide ./... scope.
	return []Command{
		{Name: label("go build"), Dir: dir, Args: []string{"go", "build", "-buildvcs=false", "./..."}},
		{Name: label("go vet"), Dir: dir, Args: []string{"go", "vet", "./..."}},
		{Name: label("go test"), Dir: dir, Args: []string{"go", "test", "-buildvcs=false", "-short", GoTestTimeoutArg, "./..."}},
	}
}

// goGateCommands is the Go plan: one module as before, else every module.
func goGateCommands(codeRoot string) []Command {
	mods := FindGoModules(codeRoot)
	if len(mods) <= 1 {
		return goModuleCommands("", "")
	}
	var cmds []Command
	for _, m := range mods {
		rel, err := filepath.Rel(codeRoot, m)
		if err != nil || rel == "." {
			cmds = append(cmds, goModuleCommands("", m)...)
			continue
		}
		cmds = append(cmds, goModuleCommands(filepath.ToSlash(rel), m)...)
	}
	return cmds
}
