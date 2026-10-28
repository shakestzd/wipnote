package container

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// A container worker's hooks can write after the last fold of its run (a
// SessionEnd that finishes after stop folded and closed the row). Such a
// folder is unfolded: the host folds it at list and before a sweep
// (FoldPending), and a sweep keeps it (Unfolded) until it is folded.

// foldNow is the fold's clock; tests set it.
var foldNow = time.Now

// mtimeSlack absorbs clock and mtime differences across the bind mount: a
// file this close before the last fold still counts as unfolded, which only
// costs an idempotent fold more.
const mtimeSlack = 2 * time.Second

var runFolderRe = regexp.MustCompile(`^dlg-[0-9a-f]{12,64}$`)

// Unfolded reports whether runDir holds a container worker's own file (its
// session, gate, pending-close or claim-shard files) written after the last
// fold that folded it, or never folded.
func Unfolded(runDir string) bool {
	return unfoldedSince(runDir, readFolded(filepath.Join(runDir, FoldedFile)).FoldedAt)
}

func unfoldedSince(runDir string, foldedAt time.Time) bool {
	for _, path := range ownFiles(runDir) {
		fi, err := os.Stat(path)
		if err != nil {
			continue
		}
		if foldedAt.IsZero() || !fi.ModTime().Before(foldedAt.Add(-mtimeSlack)) {
			return true
		}
	}
	return false
}

func ownFiles(runDir string) []string {
	var out []string
	entries, _ := os.ReadDir(runDir)
	for _, e := range entries {
		n := e.Name()
		if e.Type().IsRegular() && strings.HasSuffix(n, ".json") &&
			(strings.HasPrefix(n, "session-") || strings.HasPrefix(n, "gate-") || strings.HasPrefix(n, "claim-close-")) {
			out = append(out, filepath.Join(runDir, n))
		}
	}
	shards, _ := os.ReadDir(filepath.Join(runDir, "claims"))
	for _, e := range shards {
		if e.Type().IsRegular() && strings.HasSuffix(e.Name(), ".html") {
			out = append(out, filepath.Join(runDir, "claims", e.Name()))
		}
	}
	return out
}

// FoldPending folds every run folder under the project's run store that is
// unfolded, and returns how many it folded. A folder that fails to fold is
// left for the next pass; the first error is returned.
func FoldPending(wipnoteDir string) (int, error) {
	runs := filepath.Join(wipnoteDir, "delegate", "runs")
	entries, err := os.ReadDir(runs)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	n := 0
	var first error
	for _, e := range entries {
		dir := filepath.Join(runs, e.Name())
		if !e.IsDir() || !runFolderRe.MatchString(e.Name()) || !Unfolded(dir) {
			continue
		}
		if _, err := Fold(wipnoteDir, dir); err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		n++
	}
	return n, first
}
