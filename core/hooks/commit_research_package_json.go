package hooks

import (
	"path/filepath"

	"github.com/shakestzd/wipnote/core/paths"
)

// stagedDependencyChanges drops each staged package.json whose dependency
// fields the staged change leaves alone, comparing HEAD with the index. An
// edit to scripts, name or version adds no third-party code, so it must not
// demand web research (GitHub #334). The completion gate applies the same
// rule to an item's commits. Other manifests are kept as is.
func stagedDependencyChanges(repoRoot string, manifests []string) []string {
	var out []string
	for _, m := range manifests {
		if filepath.Base(m) != "package.json" || stagedPackageJSONDepsChanged(repoRoot, m) {
			out = append(out, m)
		}
	}
	return out
}

// stagedPackageJSONDepsChanged reports whether the staged package.json at
// path installs something different from HEAD's. A side it cannot read
// counts as a change, so the guard stays on when the answer is unknown.
func stagedPackageJSONDepsChanged(repoRoot, path string) bool {
	before, ok1 := paths.GitShow(repoRoot, "HEAD:"+filepath.ToSlash(path))
	after, ok2 := paths.GitShow(repoRoot, ":"+filepath.ToSlash(path))
	if !ok1 || !ok2 {
		return true
	}
	return paths.PackageJSONDependenciesChanged(before, after)
}
