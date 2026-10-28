package main

import (
	"path/filepath"
	"strings"
	"testing"

	dbpkg "github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/core/hooks"
	"github.com/shakestzd/wipnote/internal/delegate/session"
)

// nextPromptTitle runs the UserPromptSubmit handler for sid in root and
// returns the sessionTitle it sets, or "".
func nextPromptTitle(t *testing.T, root, sid string) string {
	t.Helper()
	res, err := hooks.UserPrompt(&hooks.CloudEvent{SessionID: sid, CWD: root, Prompt: "keep going"})
	if err != nil {
		t.Fatalf("UserPrompt: %v", err)
	}
	if res.HookSpecificOutput == nil {
		return ""
	}
	return res.HookSpecificOutput.SessionTitle
}

// TestFeatureStart_RenamesSessionOnNextPrompt: a claim in a live session
// renames it "<item-id> <title>" on the next prompt, once (plan-28b4a7f6
// slice 15).
func TestFeatureStart_RenamesSessionOnNextPrompt(t *testing.T) {
	root, id := labelTestFeature(t, "Name sessions by work item")
	const sid = "aaaa4444-bbbb-cccc-dddd-eeee0000aaaa"
	if got := nextPromptTitle(t, root, sid); got != "" {
		t.Fatalf("renamed before any claim: %q", got)
	}
	if err := wiSetStatusWithAgent("feature", id, "in-progress", sid, dbpkg.AgentRootSentinel); err != nil {
		t.Fatalf("start: %v", err)
	}
	if got, want := nextPromptTitle(t, root, sid), id+" Name sessions by work item"; got != want {
		t.Fatalf("sessionTitle = %q, want %q", got, want)
	}
	if got := nextPromptTitle(t, root, sid); got != "" {
		t.Fatalf("renamed twice: %q", got)
	}
}

// TestFeatureStart_SubagentKeepsSessionName is the other side: a subagent's
// claim belongs to its parent's session (and, below, a delegate worker keeps
// the name its mailbox uses).
func TestFeatureStart_SubagentKeepsSessionName(t *testing.T) {
	root, id := labelTestFeature(t, "Name sessions by work item")
	const sid = "aaaa4444-bbbb-cccc-dddd-eeee0000bbbb"
	if err := wiSetStatusWithAgent("feature", id, "in-progress", sid, "sub-agent-1"); err != nil {
		t.Fatalf("subagent start: %v", err)
	}
	if got := nextPromptTitle(t, root, sid); got != "" {
		t.Fatalf("a subagent's claim renamed the session: %q", got)
	}
}

func TestFeatureStart_DelegateWorkerKeepsSessionName(t *testing.T) {
	root, id := labelTestFeature(t, "Name sessions by work item")
	t.Setenv(session.RunEnv, "dlg-0123456789ab")
	const wsid = "aaaa4444-bbbb-cccc-dddd-eeee0000cccc"
	if err := wiSetStatusWithAgent("feature", id, "in-progress", wsid, dbpkg.AgentRootSentinel); err != nil {
		t.Fatalf("worker start: %v", err)
	}
	if got := nextPromptTitle(t, root, wsid); got != "" {
		t.Fatalf("a delegate worker's claim renamed it: %q", got)
	}
}

// TestResolveSessionName_WorkItem: a fresh launch for a work item is named
// <item-id>-<slug>; explicit --name, --resume and --continue keep theirs, and
// no item gives the old <project>-<timestamp> name.
func TestResolveSessionName_WorkItem(t *testing.T) {
	root, id := labelTestFeature(t, "Name sessions by work item")
	want := id + "-name-sessions-by"
	if got := resolveSessionName("", "", false, root, id); got != want {
		t.Fatalf("work item name = %q, want %q", got, want)
	}
	if got := resolveSessionName("mine", "", false, root, id); got != "mine" {
		t.Fatalf("explicit --name = %q", got)
	}
	if got := resolveSessionName("", "sess-abc", false, root, id); got != "" {
		t.Fatalf("resume renamed: %q", got)
	}
	if got := resolveSessionName("", "", true, root, id); got != "" {
		t.Fatalf("continue renamed: %q", got)
	}
	base := filepath.Base(root)
	if got := resolveSessionName("", "", false, root, ""); strings.HasPrefix(got, "feat-") || !strings.Contains(got, "-20") {
		t.Fatalf("no item = %q, want the <project>-<timestamp> name (project %s)", got, base)
	}
}

// TestDelegateSpawn_NameOptional: NAME may be left out; spawn then derives
// it from the work item and fails only for the usual reasons (here: not in
// herdr), never on the missing argument.
func TestDelegateSpawn_NameOptional(t *testing.T) {
	dir := delegateSessionProject(t)
	writeFeature(t, filepath.Join(dir, ".wipnote"), "feat-0a1b2c3d")
	t.Setenv("HERDR_ENV", "")
	_, err := execDelegate(t, "spawn", "--harness", "claude", "--work-item", "feat-0a1b2c3d")
	if err == nil || !strings.Contains(err.Error(), "herdr") {
		t.Fatalf("spawn without NAME: %v", err)
	}
}
