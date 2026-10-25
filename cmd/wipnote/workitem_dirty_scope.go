package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Item-scoped dirty-source completion gate (bug-34c27c22, GH-#261).
//
// Completion used to refuse on ANY tracked file with uncommitted changes, so an
// unrelated edit in the same checkout blocked every item. The gate now blocks
// only on files that belong to the completing item: the files of its linked
// commits, plus (in a linked worktree) the branch diff the GH-#161 scope
// already computes. Everything else that is dirty is reported in a note and
// never blocks. The check is pure git, so it stays project-type agnostic
// (GH-#114).

// dirtyFile is one uncommitted path in the scanned worktree.
type dirtyFile struct {
	Path      string
	Untracked bool
}

func (d dirtyFile) label() string {
	if d.Untracked {
		return d.Path + " (untracked)"
	}
	return d.Path
}

// dirtySourceFiles lists tracked-modified and untracked (non-ignored) files in
// root, minus .wipnote/ and the ephemeral .claude/settings.local.json.
func dirtySourceFiles(root string) ([]dirtyFile, error) {
	out, err := exec.Command(
		"git", "-C", root, "status", "--porcelain=v1", "-z",
		// =all reports a new directory as its files, not one "dir/" entry.
		"--untracked-files=all",
	).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git status: %s: %w", strings.TrimSpace(string(out)), err)
	}
	var files []dirtyFile
	for _, e := range parsePorcelainZ(out) {
		if e.Path == ".claude/settings.local.json" || isWipnotePath(e.Path) {
			continue
		}
		files = append(files, dirtyFile{Path: e.Path, Untracked: e.Status == "??"})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

// ownItemPaths is the set of repo-relative paths that belong to the item.
func ownItemPaths(repoRoot string, commits []string, scope completionScope) map[string]bool {
	own := map[string]bool{}
	for _, files := range commitFilesByHash(repoRoot, commits) {
		for _, f := range files {
			own[filepath.ToSlash(f)] = true
		}
	}
	for _, f := range branchTouchedPaths(scope) {
		own[f] = true
	}
	return own
}

func partitionDirty(files []dirtyFile, own map[string]bool) (mine, other []dirtyFile) {
	for _, f := range files {
		if own[f.Path] {
			mine = append(mine, f)
		} else {
			other = append(other, f)
		}
	}
	return mine, other
}

func dirtyPaths(files []dirtyFile) []string {
	paths := make([]string, 0, len(files))
	for _, f := range files {
		paths = append(paths, f.Path)
	}
	return paths
}

func formatDirtyList(files []dirtyFile) string {
	labels := make([]string, 0, len(files))
	for _, f := range files {
		labels = append(labels, f.label())
	}
	return formatPathList(labels)
}

// unrelatedFilesNote renders the non-blocking note; "" when nothing is listed.
// noCommits switches the wording: with no linked commits nothing can be
// attributed to the item, so every dirty file is listed as unattributed.
func unrelatedFilesNote(id string, files []dirtyFile, noCommits bool) string {
	if len(files) == 0 {
		return ""
	}
	if noCommits {
		return fmt.Sprintf("note: %d modified file(s) left uncommitted (not attributed to %s: it has no linked commits):\n%s",
			len(files), id, formatDirtyList(files))
	}
	return fmt.Sprintf("note: %d unrelated modified file(s) left uncommitted (not part of %s):\n%s",
		len(files), id, formatDirtyList(files))
}

func ownDirtyError(id string, scope completionScope, own []dirtyFile) error {
	paths := dirtyPaths(own)
	return fmt.Errorf(
		"refusing to complete %s: files from this item have uncommitted changes (scanned %s):\n%s\n\nCommit them first, for example:\n  git add %s && git commit -m %q\n\nTo bypass intentionally, rerun with --allow-dirty",
		id, scope.describe(), formatDirtyList(own),
		strings.Join(shellQuotePaths(paths), " "), id+": commit implementation",
	)
}

// checkUncommittedSourceCompleteGate refuses completion when files that belong
// to the item have uncommitted changes. Unrelated dirty files and items with no
// linked commits never block; the returned note (also written to stderr)
// lists them so the caller can repeat it in the completion output.
//
// The scan is scoped to the completing agent's own worktree (bug-6c953712):
// a file dirty in the .wipnote owner's checkout or a sibling tree is never read.
func checkUncommittedSourceCompleteGate(ev *completionEvidence, allowDirty bool) (string, error) {
	repoRoot, id := ev.repoRoot, ev.id
	if !isGitRepo(repoRoot) {
		return "", nil
	}
	scope := resolveCompletionWorktree(repoRoot)
	dirty, err := dirtySourceFiles(scope.Root)
	if err != nil || len(dirty) == 0 {
		return "", err
	}

	commits := ev.linkedCommits()
	own, other := partitionDirty(dirty, ownItemPaths(repoRoot, commits, scope))
	noCommits := len(commits) == 0
	if noCommits {
		own, other = nil, dirty
	}
	if len(own) > 0 && !allowDirty {
		return "", ownDirtyError(id, scope, own)
	}
	if len(own) > 0 {
		fmt.Fprintf(os.Stderr, "allow-dirty warning: completing %s with uncommitted changes to its own files:\n%s\n", id, formatDirtyList(own))
	}
	note := unrelatedFilesNote(id, other, noCommits)
	if note != "" {
		fmt.Fprintln(os.Stderr, note)
	}
	return note, nil
}
