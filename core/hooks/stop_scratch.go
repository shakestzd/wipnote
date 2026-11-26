package hooks

import (
	"os"
	"path/filepath"

	"github.com/shakestzd/wipnote/core/scratch"
	"github.com/shakestzd/wipnote/core/worktree"
)

// captureScratch saves the session's authored scratch drafts into the store,
// under scratch/<sessionID>/. A draft then exists in the store as soon as the
// turn that wrote it ends, so a restart or the harness's retention sweep
// cannot lose it (feat-7884b1d0).
//
// It never fails the hook and prints nothing. The pass is bounded by the
// fixed limits in core/scratch. An error goes to the debug log, which is a
// file. A payload with no scratchpad_dir costs one string compare.
func captureScratch(event *CloudEvent, projectDir, sessionID, workItem string) {
	if event == nil || event.ScratchpadDir == "" || projectDir == "" {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			debugLog(projectDir, "scratch capture: panic: %v", r)
		}
	}()
	storeDir := filepath.Join(projectDir, ".wipnote")
	if fi, err := os.Stat(storeDir); err != nil || !fi.IsDir() {
		return // no store: capture never creates one
	}
	if !scratch.SafeSessionID(sessionID) {
		debugLog(projectDir, "scratch capture: session id is not a safe folder name")
		return
	}
	if _, err := os.Lstat(filepath.Join(storeDir, "scratch", sessionID)); err != nil {
		// First capture of the session: a store whose ignore file an older
		// release wrote must list scratch/ before any draft lands in it.
		worktree.EnsureWipnoteGitignore(projectDir)
	}
	_, err := scratch.Capture(scratch.Request{
		StoreDir:      storeDir,
		SessionID:     sessionID,
		ScratchpadDir: event.ScratchpadDir,
		WorkItem:      workItem,
	})
	if err != nil {
		debugLog(projectDir, "scratch capture: %v", err)
	}
}

// scratchWorkItem is the session's active work item for a capture pass. It is
// "" when the payload names no scratchpad, so such a hook reads no claim
// ledger.
func scratchWorkItem(event *CloudEvent, projectDir, sessionID string) string {
	if event == nil || event.ScratchpadDir == "" {
		return ""
	}
	return canonicalActiveWorkItem(projectDir, sessionID)
}
