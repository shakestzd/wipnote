package hooks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRecentFilePointers_SurviveLogCap is the regression test for the Codex
// review finding on #208: past canonicalToolLogMaxBytes the log stops
// appending, so the handoff named the files touched before the cap instead of
// the newest ones.
func TestRecentFilePointers_SurviveLogCap(t *testing.T) {
	project := t.TempDir()
	const sid = "sess-cap"
	path := canonicalToolLogPath(project, sid)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	// Fill the log past its cap with an early edit plus padding.
	appendCanonicalToolEvent(project, sid, canonicalToolEvent{Tool: "Edit", Input: `{"file_path":"early.go"}`})
	pad := strings.Repeat("x", 300)
	for i := 0; ; i++ {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() >= canonicalToolLogMaxBytes {
			break
		}
		appendCanonicalToolEvent(project, sid, canonicalToolEvent{Tool: "Read", Summary: fmt.Sprint(i, pad)})
	}
	// Edits after the cap.
	for _, f := range []string{"late1.go", "late2.go"} {
		appendCanonicalToolEvent(project, sid, canonicalToolEvent{Tool: "Write", Input: `{"file_path":"` + f + `"}`})
	}
	// Non-edit tools never reach the ledger.
	appendCanonicalToolEvent(project, sid, canonicalToolEvent{Tool: "Bash", Summary: "ls"})

	// The newest edits must lead. (early.go may or may not follow: the log is
	// read from a bounded tail window, which a full log has scrolled past.)
	got := recentCanonicalFilePointers(project, sid, func(p string) string { return p })
	if len(got) < 2 || got[0] != "late2.go" || got[1] != "late1.go" {
		t.Fatalf("pointers = %v, want late2.go, late1.go first", got)
	}
	if n := len(readRecentEditOverflow(recentEditOverflowPath(path))); n != 2 {
		t.Fatalf("overflow ledger has %d events, want 2 (edits only)", n)
	}
}

func TestRecentEditOverflow_Bounded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tool-events.ndjson")
	for i := 0; i < recentEditOverflowMax+10; i++ {
		appendRecentEditOverflow(path, canonicalToolEvent{Tool: "Edit", Input: fmt.Sprintf(`{"file_path":"f%d"}`, i)})
	}
	events := readRecentEditOverflow(recentEditOverflowPath(path))
	if len(events) != recentEditOverflowMax {
		t.Fatalf("ledger len = %d, want %d", len(events), recentEditOverflowMax)
	}
	if !strings.Contains(events[len(events)-1].Input, fmt.Sprintf("f%d", recentEditOverflowMax+9)) {
		t.Fatalf("newest event must be kept last, got %q", events[len(events)-1].Input)
	}
}
