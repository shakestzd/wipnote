package hooks

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shakestzd/wipnote/core/scratch"
)

// endScratchpad is a scratchpad folder with one draft, for a SessionEnd test.
func endScratchpad(t *testing.T) string {
	t.Helper()
	pad, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writeScratch(t, pad, "draft.md", "# Draft\n")
	return pad
}

// A print-mode session may end with no Stop. SessionEnd then saves the
// drafts, and the row still names the item the session held: the item is
// read before the claims close.
func TestSessionEnd_CapturesScratchWithActiveItem(t *testing.T) {
	f := newEndFixture(t, 0)
	pad := endScratchpad(t)
	event := &CloudEvent{SessionID: f.sessionID, CWD: f.projectDir, Reason: "other", ScratchpadDir: pad}

	var result *HookResult
	var err error
	out := quietStderr(t, func() { result, err = SessionEnd(event, f.projectDir) })

	if err != nil || result == nil || !result.Continue {
		t.Fatalf("SessionEnd = %+v, %v, want a normal result", result, err)
	}
	if out != "" {
		t.Errorf("SessionEnd wrote to stderr: %q", out)
	}
	r := scratchRow(t, f.projectDir, resolveStopSessionID(event), "draft.md")
	if !r.Saved || r.State != scratch.StatePresent {
		t.Errorf("row = %+v, want saved and present", r)
	}
	if len(r.WorkItems) != 1 || (r.WorkItems[0] != "feat-aaaa0001" && r.WorkItems[0] != "feat-aaaa0002") {
		t.Errorf("work_items = %v, want the item the session held", r.WorkItems)
	}
	if _, ok := ReadSessionEndNote(f.projectDir, f.sessionID); !ok {
		t.Error("the end note is missing")
	}
	for _, ep := range f.episodes(t, f.sessionID) {
		if ep.IsOpen() {
			t.Errorf("claim on %s is still open after SessionEnd", ep.WorkItemID)
		}
	}
}

// SessionEnd with no scratchpad_dir, or one that does not exist, returns a
// normal result and leaves no scratch folder.
func TestSessionEnd_NoScratchpadIsQuiet(t *testing.T) {
	f := newEndFixture(t, 0)
	for _, dir := range []string{"", filepath.Join(f.projectDir, "no-such-scratchpad")} {
		event := &CloudEvent{SessionID: f.sessionID, CWD: f.projectDir, Reason: "other", ScratchpadDir: dir}
		var result *HookResult
		var err error
		out := quietStderr(t, func() { result, err = SessionEnd(event, f.projectDir) })
		if err != nil || result == nil || !result.Continue || out != "" {
			t.Errorf("scratchpad_dir %q: SessionEnd = %+v, %v, stderr %q", dir, result, err, out)
		}
	}
	if _, err := os.Lstat(filepath.Join(f.wipnoteDir, "scratch")); err == nil {
		t.Error("SessionEnd left a scratch folder with nothing to capture")
	}
}
