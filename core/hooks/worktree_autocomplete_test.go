package hooks

import (
	"os"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/core/db"
)

// TestExtractBranchFromWorktreePath verifies the worktree path → branch name extraction.
func TestExtractBranchFromWorktreePath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/repo/.claude/worktrees/trk-abc12345", "trk-abc12345"},
		{"/repo/.claude/worktrees/feat-abc12345", "feat-abc12345"},
		{"/repo/.claude/worktrees/bug-abc12345", "bug-abc12345"},
		{"/repo/worktrees/my-branch", "my-branch"},
		{"trk-abc12345", "trk-abc12345"},
		{"", ""},
	}
	for _, tt := range tests {
		got := extractBranchFromWorktreePath(tt.path)
		if got != tt.want {
			t.Errorf("extractBranchFromWorktreePath(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

// TestLooksLikeGitMerge checks the merge command detector.
func TestLooksLikeGitMerge(t *testing.T) {
	tests := []struct {
		cmd  string
		want bool
	}{
		{"git merge trk-abc12345", true},
		{"git merge --no-ff feat-abc12345", true},
		{"git-merge main", true},
		{"git commit -m msg", false},
		{"git log", false},
	}
	for _, tt := range tests {
		if got := looksLikeGitMerge(tt.cmd); got != tt.want {
			t.Errorf("looksLikeGitMerge(%q) = %v, want %v", tt.cmd, got, tt.want)
		}
	}
}

// TestExtractMergeBranch verifies branch extraction from merge commands.
func TestExtractMergeBranch(t *testing.T) {
	tests := []struct {
		cmd  string
		want string
	}{
		{"git merge trk-abc12345", "trk-abc12345"},
		{"git merge --no-ff trk-abc12345", "trk-abc12345"},
		{"git merge feat-abc12345", "feat-abc12345"},
		{"git merge main", "main"},
		{"git merge --squash --no-ff feat-abc12345", "feat-abc12345"},
		{"git log", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := extractMergeBranch(tt.cmd)
		if got != tt.want {
			t.Errorf("extractMergeBranch(%q) = %q, want %q", tt.cmd, got, tt.want)
		}
	}
}

// TestWorkItemBranchRe verifies the regex that identifies direct work item branches.
func TestWorkItemBranchRe(t *testing.T) {
	tests := []struct {
		branch string
		want   bool
	}{
		{"feat-aabbccdd", true},
		{"bug-12345678", true},
		{"spk-abcdef01", true},
		{"trk-abc12345", false}, // track, not direct work item
		{"main", false},
		{"feature/my-feature", false},
		{"feat-abc", false}, // too short
	}
	for _, tt := range tests {
		got := workItemBranchRe.MatchString(tt.branch)
		if got != tt.want {
			t.Errorf("workItemBranchRe.MatchString(%q) = %v, want %v", tt.branch, got, tt.want)
		}
	}
}

// TestTrackBranchRe verifies the regex that identifies track branches.
func TestTrackBranchRe(t *testing.T) {
	tests := []struct {
		branch string
		want   bool
	}{
		{"trk-abc12345", true},
		{"trk-deadbeef", true},
		{"feat-abc12345", false},
		{"main", false},
		{"trk-abc", false}, // too short
	}
	for _, tt := range tests {
		got := trackBranchRe.MatchString(tt.branch)
		if got != tt.want {
			t.Errorf("trackBranchRe.MatchString(%q) = %v, want %v", tt.branch, got, tt.want)
		}
	}
}

// TestCompleteInProgressByTrack_Query verifies that the SQL query used by
// completeInProgressByTrack selects the correct set of in-progress items.
func TestCompleteInProgressByTrack_Query(t *testing.T) {
	td := setupTestDB(t)
	td.addTrack("trk-abc12345", "Auth Track")
	td.addTrack("trk-xxxxxxxx", "Other Track")

	insertWithTrack := func(id, status, trackID string) {
		t.Helper()
		feat := &db.Feature{
			ID:       id,
			Type:     "feature",
			Title:    id,
			Status:   status,
			Priority: "medium",
			TrackID:  trackID,
		}
		if err := db.UpsertFeature(td.DB, feat); err != nil {
			t.Fatalf("UpsertFeature(%s): %v", id, err)
		}
	}

	insertWithTrack("feat-11111111", "in-progress", "trk-abc12345")
	insertWithTrack("feat-22222222", "in-progress", "trk-abc12345")
	insertWithTrack("feat-33333333", "todo", "trk-abc12345")        // not in-progress
	insertWithTrack("feat-44444444", "in-progress", "trk-xxxxxxxx") // different track

	// Query the same SQL used by completeInProgressByTrack.
	rows, err := td.DB.Query(
		`SELECT id FROM features WHERE track_id = ? AND status = 'in-progress'`,
		"trk-abc12345",
	)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}

	if len(ids) != 2 {
		t.Errorf("expected 2 in-progress items for track, got %d: %v", len(ids), ids)
	}
	for _, id := range ids {
		if id != "feat-11111111" && id != "feat-22222222" {
			t.Errorf("unexpected id in track query result: %q", id)
		}
	}
}

// TestWorktreeRemove_UnknownBranch verifies WorktreeRemove handles a non-work-item
// branch without error and still provides the standard relocation context.
func TestWorktreeRemove_UnknownBranch(t *testing.T) {
	database, projectDir := setupLifecycleDB(t)

	sessionID := "wt-remove-unknown-001"
	t.Setenv("WIPNOTE_SESSION_ID", sessionID)
	t.Setenv("WIPNOTE_PROJECT_DIR", projectDir)

	_, err := database.Exec(
		`INSERT INTO sessions (session_id, agent_assigned, status, created_at) VALUES (?, ?, ?, datetime('now'))`,
		sessionID, "claude-code", "active",
	)
	if err != nil {
		t.Fatalf("insert session: %v", err)
	}

	event := &CloudEvent{
		SessionID:    sessionID,
		CWD:          projectDir,
		WorktreePath: "/repo/.claude/worktrees/some-feature-branch",
	}

	result, err := WorktreeRemove(event, database)
	if err != nil {
		t.Fatalf("WorktreeRemove: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	// CC ignores stdout for the Observational WorktreeRemove event; the handler
	// no longer emits an additionalContext (feat-cd937fc9 cleanup).
	if result.AdditionalContext != "" {
		t.Errorf("expected no additionalContext, got: %s", result.AdditionalContext)
	}
}

// stubCompleteWorkItemCLI records the items the auto-complete path would
// complete through the CLI.
func stubCompleteWorkItemCLI(t *testing.T) *[]string {
	t.Helper()
	var completed []string
	prev := completeWorkItemCLIFn
	completeWorkItemCLIFn = func(_, id string) error {
		completed = append(completed, id)
		return nil
	}
	t.Cleanup(func() { completeWorkItemCLIFn = prev })
	return &completed
}

// TestWorktreeRemove_AutoCompletesFromCanonicalHTML pins R12 (feat-174dc958):
// with no launcher daemon, WorktreeRemove reads the work items from the
// canonical HTML, not the hook database. Removing a track worktree completes
// that track's in-progress items and nothing else: not a finished item on the
// track, not an in-progress item on another track. A branch that names no
// work item or track completes nothing.
func TestWorktreeRemove_AutoCompletesFromCanonicalHTML(t *testing.T) {
	unsetenv(t, DaemonSocketEnv)
	unsetenv(t, "WIPNOTE_PROJECT_DIR")
	unsetenv(t, "CLAUDE_PROJECT_DIR")
	projectDir := newDaemonProject(t)
	writeTrackedItem(t, projectDir, "feat-cafebabe", "in-progress", "trk-deadbeef")
	writeTrackedItem(t, projectDir, "feat-cafed00d", "done", "trk-deadbeef")
	writeTrackedItem(t, projectDir, "feat-0ddba11a", "in-progress", "trk-0ther000")
	completed := stubCompleteWorkItemCLI(t)

	for _, wt := range []string{"/repo/.claude/worktrees/main", ""} {
		if _, err := WorktreeRemove(&CloudEvent{SessionID: "wt-rm-1", CWD: projectDir, WorktreePath: wt}, nil); err != nil {
			t.Fatalf("WorktreeRemove(%q): %v", wt, err)
		}
	}
	if len(*completed) != 0 {
		t.Fatalf("a non-work-item branch completed %v", *completed)
	}
	ev := &CloudEvent{SessionID: "wt-rm-1", CWD: projectDir, WorktreePath: "/repo/.claude/worktrees/trk-deadbeef"}
	if _, err := WorktreeRemove(ev, nil); err != nil {
		t.Fatalf("WorktreeRemove: %v", err)
	}
	if got := strings.Join(*completed, ","); got != "feat-cafebabe" {
		t.Errorf("completed %q, want only feat-cafebabe", got)
	}
}

// writeTrackedItem writes a canonical feature HTML file on trackID.
func writeTrackedItem(t *testing.T, projectDir, id, status, trackID string) {
	t.Helper()
	path := writeCanonicalItem(t, projectDir, "features", id, "feature", status, "Item "+id)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	tagged := strings.Replace(string(raw), `data-priority="high"`, `data-priority="high" data-track-id="`+trackID+`"`, 1)
	if err := os.WriteFile(path, []byte(tagged), 0o644); err != nil {
		t.Fatal(err)
	}
}
