package registry

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/core/paths"
	_ "modernc.org/sqlite"
)

// effectiveTempDirs returns the set of candidate temp-root directories to
// check when deciding whether a path is a Go test temp dir. It always includes
// os.TempDir() (which honours TMPDIR on Unix), and also GOTMPDIR when set —
// that environment variable redirects where 'go test' places t.TempDir()
// subtrees (e.g. /home/vscode/.gotest-tmp in devcontainer setups).
//
// Each candidate is resolved with paths.EvalSymlinksBestEffort so that
// platforms where the temp root is itself a symlink (macOS: os.TempDir()
// returns /var/folders/... which resolves to /private/var/folders/...)
// compare correctly against paths checked by isGoTestTempDirPath /
// pathInsideTempDir, which resolve the same way.
func effectiveTempDirs() []string {
	seen := map[string]bool{}
	var dirs []string
	add := func(raw string) {
		if raw == "" {
			return
		}
		resolved := filepath.Clean(paths.EvalSymlinksBestEffort(raw))
		if !seen[resolved] {
			seen[resolved] = true
			dirs = append(dirs, resolved)
		}
	}
	add(os.TempDir())
	add(os.Getenv("GOTMPDIR"))
	return dirs
}

// isGoTestTempDirPath returns true when projectDir is inside any known temp
// root (os.TempDir() or GOTMPDIR) and matches Go's t.TempDir() naming
// convention: projectDir is DIRECTLY under (or IS ITSELF) a directory named
// TestXXX... (e.g. /tmp/TestFoo1234 or /tmp/TestFoo1234/sub, but not
// /tmp/TestFoo1234/realproject/sub).
//
// GOTMPDIR is checked alongside os.TempDir() to handle devcontainer setups
// where 'go test' writes t.TempDir() subtrees into a non-standard location
// (e.g. /home/vscode/.gotest-tmp) that differs from the system /tmp.
func isGoTestTempDirPath(projectDir string) bool {
	abs, err := filepath.Abs(projectDir)
	if err != nil {
		return false
	}
	// Resolve symlinks best-effort so this compares in the same address
	// space as effectiveTempDirs' resolved candidates (see its doc comment
	// for why: macOS os.TempDir()/GOTMPDIR values are symlinks, and
	// projectDir here is frequently a synthetic or not-yet-created path).
	abs = filepath.Clean(paths.EvalSymlinksBestEffort(abs))
	for _, tempDir := range effectiveTempDirs() {
		if matchesTestTempDirUnder(abs, tempDir) {
			return true
		}
	}
	return false
}

// matchesTestTempDirUnder returns true when abs is inside tempDir and matches
// Go's t.TempDir() path shape (Test* component directly under tempDir).
func matchesTestTempDirUnder(abs, tempDir string) bool {
	if !strings.HasPrefix(abs, tempDir+string(filepath.Separator)) {
		return false
	}

	// Check if abs itself or its direct parent is a Test* directory.
	// For <tempDir>/TestFoo1234, return true
	// For <tempDir>/TestFoo1234/sub, return true
	// For <tempDir>/TestFoo1234/sub/subsub, return false
	parent := filepath.Dir(abs)
	if parent == abs {
		return false
	}

	// Check direct parent
	parentBase := filepath.Base(parent)
	if strings.HasPrefix(parentBase, "Test") && parent != tempDir {
		return true
	}

	// Check if abs is itself a Test* dir (directly under tempDir)
	absBase := filepath.Base(abs)
	if strings.HasPrefix(absBase, "Test") {
		relPath, _ := filepath.Rel(tempDir, abs)
		return !strings.Contains(relPath, string(filepath.Separator))
	}

	return false
}

// IsGoTestTempDirPath reports whether projectDir points inside a known temp
// root (os.TempDir() or GOTMPDIR) and contains a Test* path component,
// matching Go's t.TempDir() naming convention.
func IsGoTestTempDirPath(projectDir string) bool {
	return isGoTestTempDirPath(projectDir)
}

// pathInsideTempDir reports whether path lives under any known temp root
// (os.TempDir() or GOTMPDIR). Test suites that redirect the registry file
// into a temp location should still be able to exercise tempdir projects
// without polluting the user's persistent registry.
func pathInsideTempDir(path string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	abs = filepath.Clean(paths.EvalSymlinksBestEffort(abs))
	for _, tempDir := range effectiveTempDirs() {
		if abs == tempDir || strings.HasPrefix(abs, tempDir+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// PruneStale removes entries whose LastSeen timestamp is older than ttl.
// It returns the number of removed entries. The in-memory registry is
// mutated; call Save to persist.
func PruneStale(reg *Registry, ttl time.Duration) int {
	cutoff := time.Now().Add(-ttl)
	var removed int
	kept := reg.entries[:0]
	for _, p := range reg.entries {
		t, err := time.Parse(time.RFC3339, p.LastSeen)
		if err != nil || t.Before(cutoff) {
			removed++
			continue
		}
		kept = append(kept, p)
	}
	reg.entries = kept
	return removed
}

// PruneTempdirEntries removes entries whose ProjectDir is inside os.TempDir()
// and has a Test* component (i.e. paths that match Go test temp dirs).
// Returns the number removed.
func PruneTempdirEntries(reg *Registry) int {
	var removed int
	kept := reg.entries[:0]
	for _, e := range reg.entries {
		if IsGoTestTempDirPath(e.ProjectDir) {
			removed++
			continue
		}
		kept = append(kept, e)
	}
	reg.entries = kept
	return removed
}

// looksLikeRealProject returns true when dir contains a .wipnote/
// subdirectory. That is the sole signal: wipnote projects are not
// required to be Git repositories, so a `.git` ancestor is NOT part of
// the gate (review #55 F1).
//
// Registration hardening lives in Upsert, not here: a real project is still
// defined solely by the presence of .wipnote/. The tempdir guard decides
// whether that real project should be written to the persistent registry.
func looksLikeRealProject(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".wipnote"))
	return err == nil
}
