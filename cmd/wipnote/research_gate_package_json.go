package main

import (
	"os"
	"path/filepath"

	"github.com/shakestzd/wipnote/core/paths"
)

// dependencyChangedManifests drops each package.json whose dependency fields
// the item did not change. A package.json edit that only touched scripts, name
// or version adds no third-party code, so it must not demand web research
// (GitHub #334). Every other manifest is kept as is.
func dependencyChangedManifests(ev *completionEvidence, manifests []string) []string {
	var out []string
	for _, m := range manifests {
		if filepath.Base(m) != "package.json" || packageJSONDepsChanged(ev, m) {
			out = append(out, m)
		}
	}
	return out
}

// packageJSONDepsChanged reports whether the item changed what path installs:
// in any linked commit, or, for an item with no commits, between the scanned
// worktree's base and its working tree. A version it cannot read counts as a
// change, so the gate stays on when the answer is unknown.
func packageJSONDepsChanged(ev *completionEvidence, path string) bool {
	if commits := ev.linkedCommits(); len(commits) > 0 {
		for _, c := range commits {
			before, ok1 := paths.GitShow(ev.repoRoot, c+"^:"+filepath.ToSlash(path))
			after, ok2 := paths.GitShow(ev.repoRoot, c+":"+filepath.ToSlash(path))
			if !ok1 || !ok2 || paths.PackageJSONDependenciesChanged(before, after) {
				return true
			}
		}
		return false
	}
	_, scope := ev.codeBearingPaths()
	root, base := scope.Root, "HEAD"
	if root == "" {
		root = ev.repoRoot
	}
	if scope.Linked && scope.BaseRef != "" {
		base = scope.BaseRef
	}
	before, ok := paths.GitShow(root, base+":"+filepath.ToSlash(path))
	if !ok {
		return true
	}
	after, err := os.ReadFile(filepath.Join(root, path))
	if err != nil && !os.IsNotExist(err) {
		return true
	}
	return paths.PackageJSONDependenciesChanged(before, after)
}
