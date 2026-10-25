package main

import (
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// gitTimestampCmd runs `git -C dir args...` and returns stdout. It is a variable so a
// test can count git processes.
var gitTimestampCmd = func(dir string, args ...string) ([]byte, error) {
	return exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
}

// bulkWalkResult holds the two git-derived timestamp maps for one (repo, HEAD,
// scope) triple, keyed by repo-relative path.
type bulkWalkResult struct {
	lastModified map[string]time.Time
	firstAdded   map[string]time.Time
}

type bulkWalkKey struct {
	projectDir string
	head       string
	scope      string
}

var (
	bulkWalkMu    sync.Mutex
	bulkWalkCache = map[bulkWalkKey]*bulkWalkResult{}
)

// bulkWalks returns both timestamp maps for the paths in relToAbs. It runs the
// two walks at most once per (repo, HEAD, scope) per process, and runs them in
// parallel.
//
// Hydration calls batchGitFileTimestamps once per work-item directory (tracks,
// features, bugs, spikes, recaps). Widening the scope to the enclosing .wipnote
// directory lets all of them share one result. The cache is keyed by HEAD, so a
// commit invalidates it.
func bulkWalks(projectDir string, relToAbs map[string]string) *bulkWalkResult {
	scope := bulkWalkScope(relToAbs)
	key := bulkWalkKey{projectDir: projectDir, head: gitHead(projectDir), scope: scope}

	bulkWalkMu.Lock()
	defer bulkWalkMu.Unlock()
	if cached, ok := bulkWalkCache[key]; ok {
		return cached
	}
	res := &bulkWalkResult{}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		res.firstAdded = bulkFirstAdded(projectDir)
	}()
	res.lastModified = bulkLastModified(projectDir, scope)
	wg.Wait()
	if res.lastModified == nil || res.firstAdded == nil {
		// A FAILED walk (git missing, not a repo) is not cached: one transient
		// error must not poison every later hydration in this process.
		return res
	}
	bulkWalkCache[key] = res
	return res
}

// bulkWalkScope picks the pathspec for the last-modified walk. It returns the
// enclosing `.wipnote` directory when every path lives under one (the
// hydration case), otherwise the longest common directory prefix, otherwise ""
// for an unrestricted walk.
func bulkWalkScope(relToAbs map[string]string) string {
	var common []string
	first := true
	for rel := range relToAbs {
		slashed := filepath.ToSlash(rel)
		if slashed == ".." || strings.HasPrefix(slashed, "../") {
			// A ".." pathspec is rejected by git and would fail the whole walk.
			return ""
		}
		parts := strings.Split(path.Dir(slashed), "/")
		if first {
			common = append([]string(nil), parts...)
			first = false
			continue
		}
		n := 0
		for n < len(common) && n < len(parts) && common[n] == parts[n] {
			n++
		}
		common = common[:n]
		if len(common) == 0 {
			break
		}
	}
	for i, seg := range common {
		if seg == ".wipnote" {
			return strings.Join(common[:i+1], "/")
		}
	}
	if len(common) == 0 || (len(common) == 1 && (common[0] == "." || common[0] == "")) {
		return ""
	}
	return strings.Join(common, "/")
}

// gitHead returns HEAD's sha, or "" when it cannot be resolved (which makes
// the cache key weaker, never wrong for a single-shot CLI process).
func gitHead(projectDir string) string {
	out, err := gitTimestampCmd(projectDir, "rev-parse", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// bulkLastModified resolves the newest-commit timestamp for every path under
// scope with one `git log --name-only` walk (newest first; the first sighting
// of a path wins). It matches gitLastModified exactly: "last modified" is
// about the literal path, so no rename is followed. A path with no entry is
// untracked. A nil map means the walk failed.
func bulkLastModified(projectDir, scope string) map[string]time.Time {
	// The @@ prefix can never collide with a name-only path line.
	args := []string{"-c", "core.quotePath=false", "log", "--name-only", "--pretty=format:@@%aI"}
	if scope != "" {
		args = append(args, "--", scope)
	}
	out, err := gitTimestampCmd(projectDir, args...)
	if err != nil {
		return nil
	}
	return parseNameOnlyWalk(string(out))
}

// parseNameOnlyWalk parses `git log --name-only --pretty=format:@@%aI` output
// into path -> timestamp, keeping the FIRST sighting of each path.
func parseNameOnlyWalk(out string) map[string]time.Time {
	result := make(map[string]time.Time)
	var curTS time.Time
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "@@") {
			if ts, perr := parseGitTimestamp(strings.TrimPrefix(line, "@@")); perr == nil {
				curTS = ts
			}
			continue
		}
		if _, seen := result[line]; !seen {
			result[line] = curTS
		}
	}
	return result
}

// bulkFirstAdded resolves the first-add timestamp of every path in the repo
// with ONE oldest-first walk of add (A) and rename (R) events (bug-06d2c91d).
//
// # What it follows, and how that differs from `git log --follow`
//
// The walk is unrestricted, because a pathspec would hide renames whose source
// lies outside it. This repo's work items were moved twice, .htmlgraph ->
// .erinn -> .wipnote (385 files each), and the walk follows both moves.
//
// It does NOT follow copies. The old per-file `git log --follow` did: for a
// file that appears as added, --follow searches the WHOLE parent tree for a
// similar file (it turns on find-copies-harder) and, on a match, carries on
// with that file's history. Work items are written from one template, so a new
// item often matches an older, unrelated item. Example: feat-075c110d was
// created in 36a6164fd ("wipnote: create feat-075c110d", 11:43:34), but
// --follow reports C058 from feat-d2754049 and returns feat-d2754049's creation
// time (11:42:27). An earlier note here read that as a hidden rename; it is a
// different work item. So where the two disagree, the old value is the wrong
// one. TestRealCorpusFirstAdded classifies every disagreement on a real repo.
//
// A nil map means the walk failed.
func bulkFirstAdded(projectDir string) map[string]time.Time {
	out, err := gitTimestampCmd(projectDir, "-c", "core.quotePath=false",
		"log", "--reverse", "-M", "--diff-filter=AR", "--name-status", "--format=format:@@%aI")
	if err != nil {
		return nil
	}
	return parseFirstAddedWalk(string(out))
}

// parseFirstAddedWalk folds an oldest-first `--name-status` walk into
// path -> first-add time. An add keeps the first time seen for its path, as
// --follow keeps the oldest add. A rename gives the new path its source's
// origin (or the rename time when the source was never seen), replacing any
// earlier value: --follow stops following the new path at the rename too.
func parseFirstAddedWalk(out string) map[string]time.Time {
	result := make(map[string]time.Time)
	var cur time.Time
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "@@") {
			if ts, err := parseGitTimestamp(strings.TrimPrefix(line, "@@")); err == nil {
				cur = ts
			}
			continue
		}
		f := strings.Split(line, "\t")
		switch {
		case len(f) == 2 && f[0] == "A":
			if _, seen := result[f[1]]; !seen {
				result[f[1]] = cur
			}
		case len(f) == 3 && strings.HasPrefix(f[0], "R"):
			origin, ok := result[f[1]]
			if !ok {
				origin = cur
			}
			result[f[2]] = origin
		}
	}
	return result
}
