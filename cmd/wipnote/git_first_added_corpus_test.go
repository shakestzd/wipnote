package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestRealCorpusFirstAdded compares the one-walk "created" lookup with the old
// per-file `git log --follow` lookup on a real repository, and times both. It
// is read-only and runs only when TIMESTAMP_CORPUS_REPO names a repo root:
//
//	TIMESTAMP_CORPUS_REPO=/path/to/repo go test ./cmd/wipnote -run TestRealCorpusFirstAdded -v
//
// A disagreement is acceptable only when --follow jumped to a DIFFERENT work
// item through git's copy detection. A disagreement where both sides name the
// same item is a lost rename and fails the test.
func TestRealCorpusFirstAdded(t *testing.T) {
	repo := os.Getenv("TIMESTAMP_CORPUS_REPO")
	if repo == "" {
		t.Skip("set TIMESTAMP_CORPUS_REPO to a repo root to run")
	}
	files := corpusWorkItemFiles(t, repo)

	start := time.Now()
	batch := batchGitFileTimestamps(repo, files)
	newDur := time.Since(start)

	start = time.Now()
	follow := perFileFirstAdded(repo, files)
	oldDur := time.Since(start)

	t.Logf("files=%d one-walk=%v per-file-follow=%v (workers=%d)", len(files), newDur, oldDur, runtime.NumCPU())
	copies, lost := 0, 0
	for _, f := range files {
		want, got := follow[f], batch[f].created
		if want.IsZero() || got.Equal(want) {
			continue
		}
		origin := followOrigin(t, repo, f)
		if itemIdentity(origin) != itemIdentity(f) {
			copies++
			continue
		}
		lost++
		t.Errorf("%s: one-walk=%v follow=%v origin=%s (same item: rename lost)", f, got, want, origin)
	}
	t.Logf("disagreements: copy-from-other-item=%d same-item=%d", copies, lost)
}

func corpusWorkItemFiles(t *testing.T, repo string) []string {
	t.Helper()
	var files []string
	for _, pat := range []string{"tracks/*.html", "tracks/*/index.html", "features/*.html", "bugs/*.html", "spikes/*.html", "recaps/*.html"} {
		m, _ := filepath.Glob(filepath.Join(repo, ".wipnote", pat))
		files = append(files, m...)
	}
	if len(files) == 0 {
		t.Fatalf("no work-item files under %s/.wipnote", repo)
	}
	return files
}

// perFileFirstAdded is the old lookup: one --follow process per file, run on
// every core, exactly as batchGitFileTimestamps did before bug-06d2c91d.
func perFileFirstAdded(repo string, files []string) map[string]time.Time {
	out := make(map[string]time.Time, len(files))
	var mu sync.Mutex
	jobs := make(chan string, len(files))
	for _, f := range files {
		jobs <- f
	}
	close(jobs)
	var wg sync.WaitGroup
	for i := 0; i < runtime.NumCPU(); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for f := range jobs {
				ts, err := gitFirstAdded(repo, f)
				if err == nil {
					mu.Lock()
					out[f] = ts
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()
	return out
}

// followOrigin returns the oldest path `git log --follow` reaches for file.
func followOrigin(t *testing.T, repo, file string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", repo, "log", "--follow", "--diff-filter=A",
		"--name-only", "--format=", "--", file).Output()
	if err != nil {
		t.Fatalf("follow origin %s: %v", file, err)
	}
	lines := strings.Fields(string(out))
	if len(lines) == 0 {
		return ""
	}
	return lines[len(lines)-1]
}

// itemIdentity is the work-item name a path carries: the file name, or the
// directory name for a track's index.html.
func itemIdentity(p string) string {
	base := filepath.Base(p)
	if base == "index.html" {
		return filepath.Base(filepath.Dir(p))
	}
	return base
}
