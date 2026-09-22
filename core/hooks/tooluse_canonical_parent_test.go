package hooks

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/core/agent"
)

// initMainRepo creates a git repository checked out on `main` with one commit,
// so resolveCommitBranch reports a protected branch.
func initMainRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	run("add", "seed.txt")
	run("commit", "-q", "-m", "seed")
	if err := os.MkdirAll(filepath.Join(dir, ".wipnote"), 0o755); err != nil {
		t.Fatalf("mkdir .wipnote: %v", err)
	}
	return dir
}

// TestResolveToolUseContext_BackfillsCanonicalParent pins bug-2e5081b4 (GH-#88)
// at the source: with the REAL read-only hook open path (an empty tables-only
// projection, so db.GetToolUseContext can never return a parent_session_id),
// a sub-agent's context must still carry a parent session derived from the
// canonical session-family index.
func TestResolveToolUseContext_BackfillsCanonicalParent(t *testing.T) {
	clearNestedEnv(t)
	t.Setenv("WIPNOTE_NO_AUTO_WRITER", "1")
	stubRouteSQLAsync(t, true)
	featureIDCache = featureIDCacheEntry{}

	const rootSession = "019ee380-abcd-7000-8000-0000000088a0"
	const childSession = "019ee380-abcd-7000-8000-0000000088a1"
	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, ".wipnote"), 0o755); err != nil {
		t.Fatalf("mkdir .wipnote: %v", err)
	}
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	t.Setenv("WIPNOTE_PROJECT_DIR", projectDir)
	if err := agent.RegisterSessionFamily(projectDir, childSession, rootSession); err != nil {
		t.Fatalf("RegisterSessionFamily: %v", err)
	}

	database, reason := OpenHookDBReadOnly("pretooluse", childSession, ":memory:")
	if database == nil {
		t.Fatalf("OpenHookDBReadOnly returned nil (reason=%s)", reason)
	}
	t.Cleanup(func() { _ = database.Close() })

	// Sub-agent owning a distinct session ID: parent is the family root.
	ctx := resolveToolUseContext(&CloudEvent{
		SessionID: childSession,
		AgentID:   "worktree-coder-1",
		CWD:       projectDir,
		ToolName:  "Bash",
	}, database, false)
	if ctx == nil {
		t.Fatal("resolveToolUseContext returned nil")
	}
	if !ctx.IsSubagent {
		t.Fatal("expected IsSubagent for a named agent ID")
	}
	if ctx.ParentSessionID != rootSession {
		t.Errorf("ParentSessionID = %q, want %q", ctx.ParentSessionID, rootSession)
	}

	// Orchestrator: no parent, so the sub-agent commit guard keeps allowing it
	// to commit on main.
	rootCtx := resolveToolUseContext(&CloudEvent{
		SessionID: rootSession,
		AgentID:   "claude-code",
		CWD:       projectDir,
		ToolName:  "Bash",
	}, database, false)
	if rootCtx == nil {
		t.Fatal("resolveToolUseContext(root) returned nil")
	}
	if rootCtx.IsSubagent {
		t.Fatal("orchestrator misclassified as subagent")
	}
	if rootCtx.ParentSessionID != "" {
		t.Errorf("orchestrator ParentSessionID = %q, want empty", rootCtx.ParentSessionID)
	}
}

// TestPreToolUse_SubagentCommitOnMainBlocked is the end-to-end proof for
// GH-#88: a dispatched sub-agent running `git commit` on main is blocked, and
// the orchestrator in the same repo is not. Before the backfill the empty
// projection left ParentSessionID == "", which checkSubagentCommitGuard reads
// as "orchestrator — allow", so sub-agent commits landed on main unchallenged.
func TestPreToolUse_SubagentCommitOnMainBlocked(t *testing.T) {
	clearNestedEnv(t)
	t.Setenv("WIPNOTE_NO_AUTO_WRITER", "1")
	t.Setenv("WIPNOTE_AGENT_BRANCH", "")
	stubRouteSQLAsync(t, true)
	featureIDCache = featureIDCacheEntry{}

	const rootSession = "019ee380-abcd-7000-8000-0000000088b0"
	const childSession = "019ee380-abcd-7000-8000-0000000088b1"
	projectDir := initMainRepo(t)
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	t.Setenv("WIPNOTE_PROJECT_DIR", projectDir)
	if err := agent.RegisterSessionFamily(projectDir, childSession, rootSession); err != nil {
		t.Fatalf("RegisterSessionFamily: %v", err)
	}

	database, _ := OpenHookDBReadOnly("pretooluse", childSession, ":memory:")
	if database == nil {
		t.Fatal("OpenHookDBReadOnly returned nil handle")
	}
	t.Cleanup(func() { _ = database.Close() })

	commitEvent := func(sessionID, agentID string) *CloudEvent {
		return &CloudEvent{
			SessionID: sessionID,
			AgentID:   agentID,
			CWD:       projectDir,
			ToolName:  "Bash",
			ToolInput: map[string]any{"command": `git commit -m "agent work"`},
		}
	}

	result, err := PreToolUse(commitEvent(childSession, "worktree-coder-1"), database)
	if err != nil {
		t.Fatalf("PreToolUse(subagent): %v", err)
	}
	if result == nil || result.Decision != "block" ||
		!strings.Contains(result.Reason, "Sub-agent git commit blocked") {
		t.Fatalf("expected the sub-agent commit block on main, got %+v", result)
	}

	// The orchestrator keeps its permission to commit on main.
	result, err = PreToolUse(commitEvent(rootSession, "claude-code"), database)
	if err != nil {
		t.Fatalf("PreToolUse(orchestrator): %v", err)
	}
	if result != nil && result.Decision == "block" &&
		strings.Contains(result.Reason, "Sub-agent git commit blocked") {
		t.Fatalf("orchestrator commit on main wrongly blocked as a sub-agent: %+v", result)
	}
}
