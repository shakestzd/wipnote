package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"time"
)

// gitFileTimestamps returns (created, updated) for a file from git history.
//
//   - created = timestamp of the first commit that added the file
//     (via git log --diff-filter=A --follow, oldest entry wins).
//   - updated = timestamp of the most recent commit touching the file
//     (via git log -1).
//
// Falls back to (zero, zero) when git is unavailable or the file is untracked.
// Callers should use the HTML-attribute timestamps as a fallback when both
// returned values are zero.
func gitFileTimestamps(projectDir, filePath string) (created, updated time.Time, err error) {
	updated, err = gitLastModified(projectDir, filePath)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}

	created, err = gitFirstAdded(projectDir, filePath)
	if err != nil {
		// If we can't get the creation time, still return updated.
		return time.Time{}, updated, err
	}

	// If file has only one commit, created == updated, which is correct.
	if created.IsZero() {
		created = updated
	}

	return created, updated, nil
}

// gitLastModified returns the author timestamp of the most recent commit
// that touched filePath. Returns zero time when the file is untracked.
func gitLastModified(projectDir, filePath string) (time.Time, error) {
	out, err := gitTimestampCmd(projectDir, "log", "-1", "--format=%aI", "--", filePath)
	if err != nil {
		return time.Time{}, err
	}
	line := strings.TrimSpace(string(out))
	if line == "" {
		return time.Time{}, nil // untracked
	}
	return parseGitTimestamp(line)
}

// gitFirstAdded returns the author timestamp of the oldest commit that
// introduced filePath (following renames via --follow --diff-filter=A).
// Returns zero time when the file is untracked.
//
// This single-file lookup serves `wipnote recap` and the batch fallback. Note
// that --follow also follows COPIES, so for a work item written from the
// shared template it can return an older, unrelated item's creation time. The
// batch path (bulkFirstAdded) follows renames only; see git_bulk_walks.go.
func gitFirstAdded(projectDir, filePath string) (time.Time, error) {
	out, err := gitTimestampCmd(projectDir, "log", "--diff-filter=A", "--follow", "--format=%aI", "--", filePath)
	if err != nil {
		return time.Time{}, err
	}

	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return time.Time{}, nil // untracked
	}

	// git log outputs newest-first; we want the oldest (last line).
	lines := bytes.Split([]byte(raw), []byte("\n"))
	last := strings.TrimSpace(string(lines[len(lines)-1]))
	if last == "" {
		last = strings.TrimSpace(string(lines[0]))
	}
	return parseGitTimestamp(last)
}

// parseGitTimestamp parses an ISO 8601 timestamp produced by git --format=%aI.
func parseGitTimestamp(s string) (time.Time, error) {
	// git %aI produces RFC3339 with timezone offset e.g. "2024-01-15T10:30:00+05:30"
	return time.Parse(time.RFC3339, s)
}

// applyGitTimestamps overrides node timestamps with git history when available.
// If git has no record of the file (untracked/not committed), the provided
// htmlCreated and htmlUpdated values are returned unchanged.
//
// This is the primary integration point between git history and reindex.
func applyGitTimestamps(
	projectDir, filePath string,
	htmlCreated, htmlUpdated time.Time,
) (created, updated time.Time) {
	gitCreated, gitUpdated, err := gitFileTimestamps(projectDir, filePath)
	if err != nil {
		// git unavailable — use HTML attributes as-is.
		return htmlCreated, htmlUpdated
	}
	return resolveTimestamps(gitCreated, gitUpdated, htmlCreated, htmlUpdated)
}

// resolveTimestamps applies the html-attribute-fallback rule to a
// (gitCreated, gitUpdated) pair, however that pair was obtained. Both
// applyGitTimestamps (one file, its own `git log` calls) and the batch
// lookup path (batchGitFileTimestamps, one `git log` walk for many files)
// route through this single function so the two stay behaviorally
// identical by construction rather than by keeping two copies of the same
// zero-handling rules in sync by hand.
func resolveTimestamps(gitCreated, gitUpdated, htmlCreated, htmlUpdated time.Time) (created, updated time.Time) {
	if gitCreated.IsZero() && gitUpdated.IsZero() {
		// git has no record of the file (untracked/not committed) — use
		// HTML attributes as-is.
		return htmlCreated, htmlUpdated
	}

	created = gitCreated
	updated = gitUpdated

	// Sanity: if git only returned updated (no --diff-filter=A hit), fall back
	// to HTML created or updated as the creation timestamp.
	if created.IsZero() {
		if !htmlCreated.IsZero() {
			created = htmlCreated
		} else {
			created = updated
		}
	}

	return created, updated
}

// fileTimestamps holds the git-derived (created, updated) pair for one file,
// as produced by batchGitFileTimestamps.
type fileTimestamps struct {
	created time.Time
	updated time.Time
}

// batchGitFileTimestamps resolves raw (created, updated) git timestamps for
// every path in filePaths with two bulk `git log` walks, whatever the number
// of files (bug-06d2c91d). It used to run one `git log --follow` process per
// file for "created"; on this repo that was about 1,650 processes and took
// over 15 minutes at startup on a loaded 2-core machine.
//
// filePaths must be absolute paths (or relative to the current working
// directory) the same way callers already pass them to applyGitTimestamps.
// projectDir is the git repo root.
//
// "updated" comes from bulkLastModified and "created" from bulkFirstAdded;
// see git_bulk_walks.go for what each walk follows and why. A nil result means
// a walk failed: every caller then falls back to applyGitTimestamps per file.
//
// Returns raw (created, updated) pairs, not yet reconciled against HTML
// attribute fallbacks — callers pass the result through resolveTimestamps
// (see timestampsFromBatch) exactly as applyGitTimestamps does for the
// single-file path.
func batchGitFileTimestamps(projectDir string, filePaths []string) map[string]fileTimestamps {
	relToAbs := make(map[string]string, len(filePaths))
	for _, fp := range filePaths {
		rel, err := filepath.Rel(projectDir, fp)
		if err != nil {
			continue
		}
		relToAbs[filepath.ToSlash(rel)] = fp
	}
	if len(relToAbs) == 0 {
		return nil
	}

	walks := bulkWalks(projectDir, relToAbs)
	if walks.lastModified == nil || walks.firstAdded == nil {
		return nil
	}
	result := make(map[string]fileTimestamps, len(relToAbs))
	for rel, abs := range relToAbs {
		u := walks.lastModified[rel] // zero if untracked, like gitLastModified
		c := walks.firstAdded[rel]
		if c.IsZero() {
			c = u // same rule as gitFileTimestamps
		}
		result[abs] = fileTimestamps{created: c, updated: u}
	}
	return result
}

// timestampsFromBatch looks up filePath in a map produced by
// batchGitFileTimestamps and applies the same html-attribute-fallback rule
// applyGitTimestamps applies to a single-file lookup. ok is false when
// filePath has no entry in batch (including when batch is nil), signaling
// the caller to fall back to applyGitTimestamps for that file.
func timestampsFromBatch(batch map[string]fileTimestamps, filePath string, htmlCreated, htmlUpdated time.Time) (created, updated time.Time, ok bool) {
	ts, found := batch[filePath]
	if !found {
		return time.Time{}, time.Time{}, false
	}
	c, u := resolveTimestamps(ts.created, ts.updated, htmlCreated, htmlUpdated)
	return c, u, true
}
