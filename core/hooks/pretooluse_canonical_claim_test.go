package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/agent"
	"github.com/shakestzd/wipnote/core/claimledger"
	"github.com/shakestzd/wipnote/core/db"
)

// openCanonicalClaim opens a real claim episode through the canonical writer,
// held by holderSession under rootSession's shard.
func openCanonicalClaim(t *testing.T, wipnoteDir, rootSession, holderSession, agentID, workItem string) {
	t.Helper()
	store := claimledger.NewStore(wipnoteDir)
	if _, _, err := store.Open(rootSession, claimledger.Episode{
		WorkItemID:    workItem,
		SessionID:     holderSession,
		RootSessionID: rootSession,
		AgentID:       agentID,
		StartedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("open claim episode: %v", err)
	}
}

// TestPreToolUse_SubagentWriteInheritsCanonicalClaim pins the fix for
// bug-7036b94f (GH-#87).
//
// It runs the REAL read-only hook open path: OpenHookDBReadOnly hands
// PreToolUse the empty tables-only projection, exactly as `wipnote hook
// pretooluse` does. With the orchestrator's claim recorded only in the
// canonical ledger (what `wipnote feature start` writes), a YOLO subagent's
// Write must be allowed — before the fix the parent-chain walk read the empty
// sessions/claims tables and blocked with "claim=none" regardless of the claim.
func TestPreToolUse_SubagentWriteInheritsCanonicalClaim(t *testing.T) {
	clearNestedEnv(t)
	t.Setenv("WIPNOTE_NO_AUTO_WRITER", "1")
	stubRouteSQLAsync(t, true)
	featureIDCache = featureIDCacheEntry{}

	const sessionID = "019ee378-abcd-7000-8000-00000000c1a1"
	projectDir := t.TempDir()
	wipnoteDir := filepath.Join(projectDir, ".wipnote")
	if err := os.MkdirAll(wipnoteDir, 0o755); err != nil {
		t.Fatalf("mkdir .wipnote: %v", err)
	}
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	t.Setenv("WIPNOTE_PROJECT_DIR", projectDir)

	database, reason := OpenHookDBReadOnly("pretooluse", sessionID, ":memory:")
	if database == nil {
		t.Fatalf("OpenHookDBReadOnly returned nil handle (reason=%s)", reason)
	}
	t.Cleanup(func() { _ = database.Close() })

	writeEvent := &CloudEvent{
		SessionID:      sessionID,
		AgentID:        "agent-worktree-coder-1", // a real per-subagent identity
		CWD:            projectDir,
		PermissionMode: "bypassPermissions",
		ToolName:       "Write",
		ToolInput: map[string]any{
			"file_path": filepath.Join(projectDir, "main.go"),
			"content":   "package main\n",
		},
	}

	// No claim anywhere: the subagent work-item guard must still BLOCK. A
	// missing shard is an authoritative "no claim", not "cannot verify".
	result, err := PreToolUse(writeEvent, database)
	if err != nil {
		t.Fatalf("PreToolUse (no claim): %v", err)
	}
	if result == nil || result.Decision != "block" || !strings.Contains(result.Reason, "no claimed work item") {
		t.Fatalf("expected the subagent work-item block without a canonical claim, got %+v", result)
	}

	// The orchestrator ran `wipnote feature start` — recorded ONLY canonically.
	openCanonicalClaim(t, wipnoteDir, sessionID, sessionID, db.AgentRootSentinel, "feat-orchestrated")

	result, err = PreToolUse(writeEvent, database)
	if err != nil {
		t.Fatalf("PreToolUse (canonical claim): %v", err)
	}
	if result == nil || result.Decision == "block" {
		t.Fatalf("subagent Write blocked despite the orchestrator's canonical claim: %+v", result)
	}
}

// TestPreToolUse_CodexSubagentInheritsFamilyRootClaim covers the lineage shape
// where the subagent owns a DISTINCT session ID registered under the
// orchestrator's family (Codex): the inherited claim is resolved through the
// session-family index to the root's shard.
func TestPreToolUse_CodexSubagentInheritsFamilyRootClaim(t *testing.T) {
	clearNestedEnv(t)
	t.Setenv("WIPNOTE_NO_AUTO_WRITER", "1")
	stubRouteSQLAsync(t, true)
	featureIDCache = featureIDCacheEntry{}

	const rootSession = "019ee378-abcd-7000-8000-00000000c1b0"
	const childSession = "019ee378-abcd-7000-8000-00000000c1b1"
	projectDir := t.TempDir()
	wipnoteDir := filepath.Join(projectDir, ".wipnote")
	if err := os.MkdirAll(wipnoteDir, 0o755); err != nil {
		t.Fatalf("mkdir .wipnote: %v", err)
	}
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	t.Setenv("WIPNOTE_PROJECT_DIR", projectDir)
	if err := agent.RegisterSessionFamily(projectDir, childSession, rootSession); err != nil {
		t.Fatalf("RegisterSessionFamily: %v", err)
	}
	openCanonicalClaim(t, wipnoteDir, rootSession, rootSession, "codex", "bug-family-root")

	database, _ := OpenHookDBReadOnly("pretooluse", childSession, ":memory:")
	if database == nil {
		t.Fatal("OpenHookDBReadOnly returned nil handle")
	}
	t.Cleanup(func() { _ = database.Close() })

	result, err := PreToolUse(&CloudEvent{
		SessionID:      childSession,
		AgentID:        "codex-subagent-7",
		CWD:            projectDir,
		PermissionMode: "bypassPermissions",
		ToolName:       "Edit",
		ToolInput:      map[string]any{"file_path": filepath.Join(projectDir, "main.go"), "old_string": "a", "new_string": "b"},
	}, database)
	if err != nil {
		t.Fatalf("PreToolUse: %v", err)
	}
	if result == nil || result.Decision == "block" {
		t.Fatalf("Codex subagent Edit blocked despite the family root's canonical claim: %+v", result)
	}
}

// TestCheckYoloSubagentGrace_Canonical pins the grace window's canonical
// inputs: the subagent's start time and the parent's OPEN ledger claim.
func TestCheckYoloSubagentGrace_Canonical(t *testing.T) {
	projectDir := t.TempDir()
	wipnoteDir := filepath.Join(projectDir, ".wipnote")
	if err := os.MkdirAll(wipnoteDir, 0o755); err != nil {
		t.Fatalf("mkdir .wipnote: %v", err)
	}
	const parent = "grace-parent"
	const claimedParent = "grace-parent-claimed"
	openCanonicalClaim(t, wipnoteDir, claimedParent, claimedParent, db.AgentRootSentinel, "feat-grace")
	fresh := time.Now().Add(-5 * time.Second)
	stale := time.Now().Add(-2 * yoloSubagentGracePeriod)

	cases := []struct {
		name      string
		yolo, sub bool
		startedAt time.Time
		parent    string
		want      bool
	}{
		{"not yolo", false, true, fresh, claimedParent, false},
		{"not a subagent", true, false, fresh, claimedParent, false},
		{"unknown start time", true, true, time.Time{}, claimedParent, false},
		{"started too long ago", true, true, stale, claimedParent, false},
		{"parent has no claim", true, true, fresh, parent, false},
		{"no parent", true, true, fresh, "", false},
		{"fresh subagent, parent holds canonical claim", true, true, fresh, claimedParent, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := checkYoloSubagentGrace(tc.yolo, tc.sub, tc.startedAt, tc.parent, wipnoteDir); got != tc.want {
				t.Fatalf("checkYoloSubagentGrace = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestCanonicalParentSessionID pins the parent derivation used by the subagent
// guards now that sessions.parent_session_id is never hydrated for them.
func TestCanonicalParentSessionID(t *testing.T) {
	clearNestedEnv(t)
	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, ".wipnote"), 0o755); err != nil {
		t.Fatalf("mkdir .wipnote: %v", err)
	}
	if err := agent.RegisterSessionFamily(projectDir, "child-own-session", "family-root"); err != nil {
		t.Fatalf("RegisterSessionFamily: %v", err)
	}

	// Root session, nothing recorded → no parent.
	if got := canonicalParentSessionID(projectDir, "root-session", false); got != "" {
		t.Errorf("root session: want no parent, got %q", got)
	}
	// Claude Code subagent sharing the orchestrator's session ID → that session.
	if got := canonicalParentSessionID(projectDir, "root-session", true); got != "root-session" {
		t.Errorf("shared-session subagent: want root-session, got %q", got)
	}
	// Subagent owning a distinct session registered under a family → the root.
	if got := canonicalParentSessionID(projectDir, "child-own-session", true); got != "family-root" {
		t.Errorf("family subagent: want family-root, got %q", got)
	}
	// A resumed ROOT that shares a family is still an orchestrator: no parent.
	if got := canonicalParentSessionID(projectDir, "child-own-session", false); got != "" {
		t.Errorf("resumed root: want no parent, got %q", got)
	}
	// Session-start exports the root's own ID at depth 0: not a parent.
	t.Setenv("WIPNOTE_PARENT_SESSION", "root-session")
	t.Setenv("WIPNOTE_NESTING_DEPTH", "0")
	if got := canonicalParentSessionID(projectDir, "root-session", false); got != "" {
		t.Errorf("depth-0 env: want no parent, got %q", got)
	}
	// A nested launch names a different parent at depth > 0: use it.
	t.Setenv("WIPNOTE_PARENT_SESSION", "outer-orchestrator")
	t.Setenv("WIPNOTE_NESTING_DEPTH", "1")
	if got := canonicalParentSessionID(projectDir, "nested-session", false); got != "outer-orchestrator" {
		t.Errorf("nested env: want outer-orchestrator, got %q", got)
	}
}
