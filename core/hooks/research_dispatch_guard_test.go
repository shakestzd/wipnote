package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/core/research"
	"github.com/shakestzd/wipnote/core/workitem"
)

// seedResearchProject writes one feature of change kind ui (optionally with a
// research record) and the research config, returning the project root and id.
func seedResearchProject(t *testing.T, mode string, rec *research.Record) (string, string) {
	t.Helper()
	root := t.TempDir()
	hgDir := filepath.Join(root, ".wipnote")
	if err := os.MkdirAll(hgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hgDir, "config.json"),
		[]byte(`{"research":{"mode":"`+mode+`"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := workitem.Open(hgDir, "claude-code")
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	n, err := p.Features.Create("Header toolbar overflow menu")
	if err != nil {
		t.Fatal(err)
	}
	edit := p.Features.Edit(n.ID).SetProperty(research.PropKind, research.KindUI)
	if rec != nil {
		edit = edit.SetProperty(research.PropRecord, rec.Property())
	}
	if err := edit.Save(); err != nil {
		t.Fatal(err)
	}
	return root, n.ID
}

func coderDispatch(id string) *CloudEvent {
	return &CloudEvent{ToolName: "Task", ToolInput: map[string]any{
		"subagent_type": "wipnote:feature-coder",
		"prompt":        "Implement " + id + " per the plan.",
	}}
}

func TestResearchDispatchGuard_WarnAdvises(t *testing.T) {
	root, id := seedResearchProject(t, "warn", nil)
	msg, deny := checkResearchDispatchGuard(coderDispatch(id), &toolUseContext{ProjectDir: root})
	if deny || !strings.Contains(msg, "wipnote research start") {
		t.Fatalf("warn mode must advise with the fix, got deny=%v msg=%q", deny, msg)
	}
}

func TestResearchDispatchGuard_BlockDenies(t *testing.T) {
	root, id := seedResearchProject(t, "block", nil)
	msg, deny := checkResearchDispatchGuard(coderDispatch(id), &toolUseContext{ProjectDir: root})
	if !deny || msg == "" {
		t.Fatalf("block mode must deny, got deny=%v msg=%q", deny, msg)
	}
	// End to end through PreToolUse: the deny reaches the hook result.
	event := coderDispatch(id)
	event.SessionID = "sess-research-dispatch"
	event.CWD = root
	t.Setenv("CLAUDE_PROJECT_DIR", root)
	t.Setenv("WIPNOTE_PROJECT_DIR", root)
	database := makeSessionDB(t, event.SessionID, root)
	res, err := PreToolUse(event, database)
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || res.Decision != "block" || !strings.Contains(res.Reason, "research") {
		t.Fatalf("PreToolUse must deny the dispatch, got %+v", res)
	}
}

func TestResearchDispatchGuard_AllowsWhenRecordedOrNotImplementer(t *testing.T) {
	root, id := seedResearchProject(t, "block", &research.Record{Waiver: "copy change only"})
	if msg, deny := checkResearchDispatchGuard(coderDispatch(id), &toolUseContext{ProjectDir: root}); deny || msg != "" {
		t.Fatalf("waived item must dispatch freely, got deny=%v msg=%q", deny, msg)
	}
	root2, id2 := seedResearchProject(t, "block", nil)
	researcher := &CloudEvent{ToolName: "Task", ToolInput: map[string]any{
		"subagent_type": "wipnote:researcher", "prompt": "Research " + id2,
	}}
	if msg, deny := checkResearchDispatchGuard(researcher, &toolUseContext{ProjectDir: root2}); deny || msg != "" {
		t.Fatal("researcher dispatch must never be gated")
	}
	off, id3 := seedResearchProject(t, "off", nil)
	if msg, _ := checkResearchDispatchGuard(coderDispatch(id3), &toolUseContext{ProjectDir: off}); msg != "" {
		t.Fatal("off mode must be silent")
	}
}

func TestDispatchWorkItem_FallsBackToClaim(t *testing.T) {
	ev := &CloudEvent{ToolName: "Task", ToolInput: map[string]any{"subagent_type": "patch-coder", "prompt": "fix it"}}
	if got := dispatchWorkItem(ev, &toolUseContext{ClaimedItem: "feat-0000000a", FeatureID: "feat-0000000b"}); got != "feat-0000000a" {
		t.Fatalf("got %q", got)
	}
}

func TestResearchDispatchGuard_WarnReachesAdditionalContext(t *testing.T) {
	root, id := seedResearchProject(t, "warn", nil)
	event := coderDispatch(id)
	event.SessionID = "sess-research-dispatch-warn"
	event.CWD = root
	t.Setenv("CLAUDE_PROJECT_DIR", root)
	t.Setenv("WIPNOTE_PROJECT_DIR", root)
	res, err := PreToolUse(event, makeSessionDB(t, event.SessionID, root))
	if err != nil {
		t.Fatal(err)
	}
	if res == nil || res.Decision == "block" || !strings.Contains(res.AdditionalContext, "wipnote research start") {
		t.Fatalf("warn mode must allow with the advisory in additionalContext, got %+v", res)
	}
}
