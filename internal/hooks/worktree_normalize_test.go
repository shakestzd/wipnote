package hooks

import (
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/internal/paths"
)

// TestWorktreeNormalize_AbsoluteInsideRepo_StoredRelative verifies that a
// WorktreeCreate event with an absolute path inside the repo stores the
// normalized relative path in input_summary.
func TestWorktreeNormalize_AbsoluteInsideRepo_StoredRelative(t *testing.T) {
	td, sessionID := setupMissingEventsDB(t)

	// Override the package-level resolver so the test does not shell to git.
	// The resolver returns the repo root for any path under /repo/.
	paths.ResetNormalizeCacheForTesting()
	old := worktreePathResolver
	worktreePathResolver = func(dir string) string {
		if strings.HasPrefix(dir, "/repo") {
			return "/repo"
		}
		return ""
	}
	t.Cleanup(func() {
		worktreePathResolver = old
		paths.ResetNormalizeCacheForTesting()
	})

	event := &CloudEvent{
		SessionID:    sessionID,
		CWD:          t.TempDir(),
		WorktreePath: "/repo/.claude/worktrees/foo-12345",
	}

	result, err := WorktreeCreate(event, td.DB)
	if err != nil {
		t.Fatalf("WorktreeCreate: %v", err)
	}
	if result == nil || !result.Continue {
		t.Error("expected Continue=true")
	}

	var inputSummary string
	if err := td.DB.QueryRow(
		`SELECT input_summary FROM agent_events WHERE session_id = ? AND tool_name = 'WorktreeCreate'`,
		sessionID,
	).Scan(&inputSummary); err != nil {
		t.Fatalf("query agent_events: %v", err)
	}
	want := "Worktree created: .claude/worktrees/foo-12345"
	if inputSummary != want {
		t.Errorf("input_summary = %q, want %q", inputSummary, want)
	}
}

// TestWorktreeNormalize_AlreadyRelative_Unchanged verifies that a
// WorktreePath that is already relative is stored unchanged.
func TestWorktreeNormalize_AlreadyRelative_Unchanged(t *testing.T) {
	td, sessionID := setupMissingEventsDB(t)

	paths.ResetNormalizeCacheForTesting()
	old := worktreePathResolver
	worktreePathResolver = func(dir string) string { return "/repo" }
	t.Cleanup(func() {
		worktreePathResolver = old
		paths.ResetNormalizeCacheForTesting()
	})

	event := &CloudEvent{
		SessionID:    sessionID,
		CWD:          t.TempDir(),
		WorktreePath: ".claude/worktrees/feat-already",
	}

	_, err := WorktreeCreate(event, td.DB)
	if err != nil {
		t.Fatalf("WorktreeCreate: %v", err)
	}

	var inputSummary string
	if err := td.DB.QueryRow(
		`SELECT input_summary FROM agent_events WHERE session_id = ? AND tool_name = 'WorktreeCreate'`,
		sessionID,
	).Scan(&inputSummary); err != nil {
		t.Fatalf("query agent_events: %v", err)
	}
	want := "Worktree created: .claude/worktrees/feat-already"
	if inputSummary != want {
		t.Errorf("input_summary = %q, want %q", inputSummary, want)
	}
}

// TestWorktreeNormalize_InputSummaryContainsAbsPath_Normalized verifies that
// when WorktreeRemove is fired, any absolute path embedding in input_summary
// is replaced with the relative form.
func TestWorktreeNormalize_InputSummaryContainsAbsPath_Normalized(t *testing.T) {
	td, sessionID := setupMissingEventsDB(t)

	paths.ResetNormalizeCacheForTesting()
	old := worktreePathResolver
	worktreePathResolver = func(dir string) string {
		if strings.HasPrefix(dir, "/workspaces/repo") {
			return "/workspaces/repo"
		}
		return ""
	}
	t.Cleanup(func() {
		worktreePathResolver = old
		paths.ResetNormalizeCacheForTesting()
	})

	event := &CloudEvent{
		SessionID:    sessionID,
		CWD:          t.TempDir(),
		WorktreePath: "/workspaces/repo/.claude/worktrees/trk-abc12345",
	}

	_, err := WorktreeRemove(event, td.DB)
	if err != nil {
		t.Fatalf("WorktreeRemove: %v", err)
	}

	var inputSummary string
	if err := td.DB.QueryRow(
		`SELECT input_summary FROM agent_events WHERE session_id = ? AND tool_name = 'WorktreeRemove'`,
		sessionID,
	).Scan(&inputSummary); err != nil {
		t.Fatalf("query agent_events: %v", err)
	}
	want := "Worktree removed: .claude/worktrees/trk-abc12345"
	if inputSummary != want {
		t.Errorf("input_summary = %q, want %q", inputSummary, want)
	}
}

// TestWorktreeNormalize_ForeignPath_MarkedUnresolved verifies that a
// WorktreePath outside the repo is stored with "unresolved:" prefix so it
// is queryable.
func TestWorktreeNormalize_ForeignPath_MarkedUnresolved(t *testing.T) {
	td, sessionID := setupMissingEventsDB(t)

	// Resolver returns "" — path is outside any known repo.
	paths.ResetNormalizeCacheForTesting()
	old := worktreePathResolver
	worktreePathResolver = func(dir string) string { return "" }
	t.Cleanup(func() {
		worktreePathResolver = old
		paths.ResetNormalizeCacheForTesting()
	})

	event := &CloudEvent{
		SessionID:    sessionID,
		CWD:          t.TempDir(),
		WorktreePath: "/home/otheruser/foreign-repo/.claude/worktrees/feat-xyz",
	}

	_, err := WorktreeCreate(event, td.DB)
	if err != nil {
		t.Fatalf("WorktreeCreate: %v", err)
	}

	var inputSummary string
	if err := td.DB.QueryRow(
		`SELECT input_summary FROM agent_events WHERE session_id = ? AND tool_name = 'WorktreeCreate'`,
		sessionID,
	).Scan(&inputSummary); err != nil {
		t.Fatalf("query agent_events: %v", err)
	}
	// HostPathPattern matches /home/... so it receives "unresolved:" prefix.
	want := "Worktree created: unresolved:/home/otheruser/foreign-repo/.claude/worktrees/feat-xyz"
	if inputSummary != want {
		t.Errorf("input_summary = %q, want %q", inputSummary, want)
	}
}

// TestWorktreeNormalize_EmptyPath_NoOp verifies that an empty WorktreePath
// is handled gracefully — no panic, summary falls back to generic text.
func TestWorktreeNormalize_EmptyPath_NoOp(t *testing.T) {
	td, sessionID := setupMissingEventsDB(t)

	paths.ResetNormalizeCacheForTesting()
	old := worktreePathResolver
	worktreePathResolver = func(dir string) string { return "/repo" }
	t.Cleanup(func() {
		worktreePathResolver = old
		paths.ResetNormalizeCacheForTesting()
	})

	event := &CloudEvent{
		SessionID:    sessionID,
		CWD:          t.TempDir(),
		WorktreePath: "",
	}

	result, err := WorktreeCreate(event, td.DB)
	if err != nil {
		t.Fatalf("WorktreeCreate panicked or errored: %v", err)
	}
	if result == nil || !result.Continue {
		t.Error("expected Continue=true")
	}

	var inputSummary string
	if err := td.DB.QueryRow(
		`SELECT input_summary FROM agent_events WHERE session_id = ? AND tool_name = 'WorktreeCreate'`,
		sessionID,
	).Scan(&inputSummary); err != nil {
		t.Fatalf("query agent_events: %v", err)
	}
	if inputSummary != "Worktree created" {
		t.Errorf("input_summary = %q, want %q", inputSummary, "Worktree created")
	}
}
