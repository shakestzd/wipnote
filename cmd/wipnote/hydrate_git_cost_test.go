package main

import (
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/db/dbtest"
)

// hydrateGitWalks hydrates a fresh repo of items work items, each rewritten
// in history commits, and returns how many git timestamp processes the whole
// hydrate ran.
func hydrateGitWalks(t *testing.T, items, history int) int64 {
	t.Helper()
	repo := setupGitRepo(t)
	for h := 0; h < history; h++ {
		for i := 0; i < items; i++ {
			rel := filepath.Join(".wipnote", "features", fmt.Sprintf("feat-%04d%04d.html", i, items))
			writeRepoFile(t, repo, rel, fmt.Sprintf(`<!DOCTYPE html><html><body>
<article id="feat-%04d%04d" data-type="feature" data-status="todo" data-priority="medium">
<header><h1>Item %d rev %d</h1></header></article></body></html>`, i, items, i, h))
		}
		when := time.Date(2026, 1, 1+h, 10, 0, 0, 0, time.UTC)
		gitAt(t, repo, when, "add", "-A")
		gitAt(t, repo, when, "commit", "-m", fmt.Sprintf("rev %d", h))
	}

	database, err := dbtest.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	var calls atomic.Int64
	old := gitTimestampCmd
	gitTimestampCmd = func(dir string, args ...string) ([]byte, error) {
		calls.Add(1)
		return old(dir, args...)
	}
	defer func() { gitTimestampCmd = old }()

	if err := hydrateCompatibilityDB(database, filepath.Join(repo, ".wipnote")); err != nil {
		t.Fatal(err)
	}
	return calls.Load()
}

// Issue #183 named an O(N*H) git cost in hydrate: git work per item per
// history entry. Hydrate must run a constant number of git timestamp walks,
// whatever the item count N and history depth H.
func TestHydrate_GitCostIndependentOfItemsAndHistory(t *testing.T) {
	small := hydrateGitWalks(t, 3, 2)
	large := hydrateGitWalks(t, 12, 6)
	if large != small {
		t.Fatalf("git walks grew with N and H: %d for 3x2, %d for 12x6", small, large)
	}
	if large < 1 || large > 2 {
		t.Fatalf("hydrate ran %d git timestamp walks, want 1-2 (last-modified + first-added)", large)
	}
}
