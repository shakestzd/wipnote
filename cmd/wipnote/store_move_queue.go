package main

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/shakestzd/wipnote/internal/commitqueue"
)

// dropMovedStoreIntents removes the queued commits whose paths all lie in the
// store that just moved out of the project at code (feat-b153fe64). The copy
// holds their files and the app store's own repository commits them, so
// draining them would put one more wipnote commit on the project's branch,
// with its hooks, also on a detached HEAD or in the middle of a rebase. An
// intent that names a path outside the store stays queued.
func dropMovedStoreIntents(w io.Writer, code, rel string) {
	outbox, err := openCommitOutbox(code)
	if err != nil {
		return
	}
	drop := func(commitqueue.Intent) error { return nil }
	res, err := outbox.FlushMatching(drop, commitqueue.MaxAttempts, func(i commitqueue.Intent) bool {
		return intentInsideStore(i, code, rel)
	})
	if err != nil {
		fmt.Fprintf(w, "Could not clear the project's queued store commits (%v). Do not run `wipnote commit-queue flush` for them: the app store already holds their files.\n", err)
		return
	}
	if n := res.Committed + res.DeadLettered; n > 0 {
		fmt.Fprintf(w, "Dropped %d queued store commit(s) for the project. The app store holds their files.\n", n)
	}
}

// intentInsideStore reports an intent for the repo at code whose every path
// lies in the store folder rel.
func intentInsideStore(i commitqueue.Intent, code, rel string) bool {
	if len(i.RelPaths) == 0 || !sameFolder(i.RepoRoot, code) {
		return false
	}
	store := filepath.Clean(rel)
	for _, p := range i.RelPaths {
		clean := filepath.Clean(filepath.FromSlash(p))
		if clean != store && !strings.HasPrefix(clean, store+string(filepath.Separator)) {
			return false
		}
	}
	return true
}

// sameFolder compares two folder paths after resolving symbolic links.
func sameFolder(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if ra, err := filepath.EvalSymlinks(a); err == nil {
		a = ra
	}
	if rb, err := filepath.EvalSymlinks(b); err == nil {
		b = rb
	}
	return filepath.Clean(a) == filepath.Clean(b)
}
