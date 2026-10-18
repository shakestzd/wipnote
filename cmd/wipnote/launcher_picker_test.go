package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	dbpkg "github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/internal/launcher"
)

// Tests for the short start-or-continue picker (feat-02c939ed): the last-session
// slot read from Claude's transcripts, the in-progress shortlist, the browse row,
// and the legacy "claude-code" harness string.

func writeTranscript(t *testing.T, dir, id string, lines []string, mod time.Time) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, id+".jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatal(err)
	}
}

func TestClaudeProjectDirName(t *testing.T) {
	if got := claudeProjectDirName("/home/user/wip_note.v2"); got != "-home-user-wip-note-v2" {
		t.Fatalf("claudeProjectDirName = %q", got)
	}
}

func TestLatestClaudeTranscriptSession_NewestAcrossRootAndWorktrees(t *testing.T) {
	projects := t.TempDir()
	root := "/repo/app"
	now := time.Now()
	rootDir := filepath.Join(projects, claudeProjectDirName(root))
	wtDir := filepath.Join(projects, claudeProjectDirName(root+"/.claude/worktrees/feat-x"))
	otherDir := filepath.Join(projects, claudeProjectDirName("/repo/application")) // shares a prefix, not a project

	writeTranscript(t, rootDir, "old", []string{`{"type":"user","cwd":"/repo/app","message":{"content":"older work"}}`}, now.Add(-3*time.Hour))
	// Newer than everything else, but unusable: a sibling repository whose folder
	// shares the prefix, and an aborted session with no prompt.
	siblingDir := filepath.Join(projects, claudeProjectDirName("/repo/app-copy"))
	writeTranscript(t, siblingDir, "sibling", []string{`{"type":"user","cwd":"/repo/app-copy","message":{"content":"other repo"}}`}, now.Add(time.Minute))
	writeTranscript(t, rootDir, "aborted", []string{`{"type":"attachment","cwd":"/repo/app"}`}, now.Add(2*time.Minute))
	writeTranscript(t, wtDir, "wt", []string{
		`{"type":"attachment","cwd":"/repo/app/.claude/worktrees/feat-x"}`,
		`{"type":"user","isMeta":true,"message":{"content":"meta text"}}`,
		`{"type":"user","message":{"content":"<command-name>/clear</command-name>"}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","content":"x"}]}}`,
		`{"type":"user","message":{"content":[{"type":"text","text":"fix the   herdr\nLive tab"}]}}`,
	}, now.Add(-time.Hour))
	writeTranscript(t, otherDir, "other", []string{`{"type":"user","message":{"content":"not this project"}}`}, now)
	writeTranscript(t, rootDir, "agent-sub", []string{`{"type":"user","message":{"content":"subagent"}}`}, now)

	got := latestClaudeTranscriptSession(projects, root)
	if got == nil {
		t.Fatal("no session found")
	}
	if got.ID != "wt" || got.Label != "fix the herdr Live tab" || got.Cwd != "/repo/app/.claude/worktrees/feat-x" {
		t.Fatalf("got %+v, want the worktree session with its first typed prompt", got)
	}
}

func TestLatestClaudeTranscriptSession_FallsBackPastUnusable(t *testing.T) {
	projects := t.TempDir()
	rootDir := filepath.Join(projects, claudeProjectDirName("/repo/app"))
	now := time.Now()
	writeTranscript(t, rootDir, "good", []string{`{"type":"user","cwd":"/repo/app/sub","message":{"content":"real work"}}`}, now.Add(-time.Hour))
	writeTranscript(t, rootDir, "nocwd", []string{`{"type":"user","message":{"content":"no cwd"}}`}, now)
	got := latestClaudeTranscriptSession(projects, "/repo/app")
	if got == nil || got.ID != "good" {
		t.Fatalf("got %+v, want the older session with a cwd under the root", got)
	}
}

func TestReadClaudeTranscriptSession_LabelPrecedence(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeTranscript(t, dir, "titled", []string{
		`{"type":"user","message":{"content":"first prompt"}}`,
		`{"type":"summary","summary":"A summary"}`,
		`{"type":"custom-title","customTitle":"My title"}`,
	}, now)
	writeTranscript(t, dir, "summarised", []string{
		`{"type":"summary","summary":"A summary"}`,
		`{"type":"user","message":{"content":"first prompt"}}`,
	}, now)
	writeTranscript(t, dir, "last-only", []string{`{"type":"last-prompt","lastPrompt":"the last prompt"}`}, now)
	writeTranscript(t, dir, "empty", []string{`{"type":"attachment","cwd":"/x"}`}, now)

	for id, want := range map[string]string{"titled": "My title", "summarised": "A summary", "last-only": "the last prompt"} {
		s := readClaudeTranscriptSession(filepath.Join(dir, id+".jsonl"))
		if s == nil || s.Label != want {
			t.Fatalf("%s: got %+v, want label %q", id, s, want)
		}
	}
	if s := readClaudeTranscriptSession(filepath.Join(dir, "empty.jsonl")); s != nil {
		t.Fatalf("a session with no prompt should not be offered: %+v", s)
	}
}

func TestTranscriptResumable_KeepsWorktreeOnly(t *testing.T) {
	ts := claudeTranscriptSession{ID: "abc", Label: "x", Cwd: "/repo", ModTime: time.Now().Add(-time.Hour)}
	if row := transcriptResumable(ts, "/repo"); row.ExecWorktreePath != "" || row.Live || row.Harness != "claude" || row.LastSessionID != "abc" {
		t.Fatalf("root session row = %+v", row)
	}
	ts.Cwd, ts.ModTime = "/repo/.claude/worktrees/w", time.Now()
	if row := transcriptResumable(ts, "/repo"); row.ExecWorktreePath != ts.Cwd || !row.Live {
		t.Fatalf("worktree session row = %+v", row)
	}
}

func TestShortlistWorkItemRows_InProgressRecentAndCapped(t *testing.T) {
	now := time.Now()
	at := func(d time.Duration) string { return now.Add(-d).UTC().Format(time.RFC3339) }
	grouped := dbpkg.HarnessGroupedResumableSessions{
		SameHarness: []dbpkg.ResumableSession{
			{WorkItemID: "s1", Status: "in-progress", LastActivity: at(1 * time.Hour)},
			{WorkItemID: "todo", Status: "todo", LastActivity: at(time.Minute)},
			{WorkItemID: "stale", Status: "in-progress", LastActivity: at(8 * 24 * time.Hour)},
			{WorkItemID: "s4", Status: "in-progress", LastActivity: at(4 * time.Hour)},
		},
		CrossHarness: []dbpkg.ResumableSession{
			{WorkItemID: "c2", Status: "in-progress", LastActivity: at(2 * time.Hour)},
			{WorkItemID: "c3", Status: "in-progress", LastActivity: at(3 * time.Hour)},
			{WorkItemID: "blocked", Status: "blocked", LastActivity: at(time.Minute)},
		},
	}
	got := shortlistWorkItemRows(grouped, now)
	ids := func(rows []dbpkg.ResumableSession) []string {
		var out []string
		for _, r := range rows {
			out = append(out, r.WorkItemID)
		}
		return out
	}
	if s, c := ids(got.SameHarness), ids(got.CrossHarness); !slices.Equal(s, []string{"s1"}) || !slices.Equal(c, []string{"c2", "c3"}) {
		t.Fatalf("shortlist same=%v cross=%v, want [s1] and [c2 c3]", s, c)
	}
}

func TestMapIndexToIntent_BrowseRowForClaudeOnly(t *testing.T) {
	rows := []dbpkg.ResumableSession{{WorkItemID: "feat-a", Harness: "claude", LastSessionID: "s"}}
	if got := mapIndexToIntent(2, rows, "claude"); !got.WantsBrowse() {
		t.Fatalf("index after the rows should browse for claude, got %+v", got)
	}
	if got := mapIndexToIntent(2, rows, "codex"); got.Kind != launcher.LaunchIntentNew {
		t.Fatalf("codex has no browse row, got %+v", got)
	}
	var out bytes.Buffer
	grouped := dbpkg.HarnessGroupedResumableSessions{SameHarness: rows}
	intent, err := promptLaunchIntentNumeric(strings.NewReader("3\n"), &out, "claude", grouped, rows)
	if err != nil || !intent.WantsBrowse() {
		t.Fatalf("numeric choice 3 = %+v, %v; want browse\n%s", intent, err, out.String())
	}
	if !strings.Contains(out.String(), "3. "+browseSessionsLabel) {
		t.Fatalf("numeric menu missing the browse row:\n%s", out.String())
	}
}

func TestApplyClaudeLaunchIntent_BrowseRunsBareResume(t *testing.T) {
	r := applyClaudeLaunchIntent("", "", launcher.BrowseSessionsIntent())
	if !r.browse || r.mode != "browse" || r.resumeID != "" || r.workItem != "" {
		t.Fatalf("browse result = %+v", r)
	}
	args := buildClaudeArgs(LaunchOpts{Resume: r.browse, PluginDir: "/p"}, "")
	if !slices.Equal(args[:3], []string{"--resume", "--plugin-dir", "/p"}) {
		t.Fatalf("args = %v, want a bare --resume before --plugin-dir", args)
	}
}

func TestListHarnessGroupedResumableSessions_ClaudeCodeGroupsWithClaude(t *testing.T) {
	database, err := dbpkg.Open(filepath.Join(t.TempDir(), "claude_code.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	if _, err := database.Exec(`INSERT INTO features (id, type, title, status) VALUES ('feat-a', 'feature', 'Alpha', 'in-progress')`); err != nil {
		t.Fatal(err)
	}
	insertResumableSession(t, database, "sess-legacy", "claude-code", "active", time.Now().Add(-time.Hour))
	if _, err := database.Exec(`UPDATE sessions SET active_feature_id = 'feat-a', harness = 'claude-code' WHERE session_id = 'sess-legacy'`); err != nil {
		t.Fatal(err)
	}
	grouped, err := dbpkg.ListHarnessGroupedResumableSessions(database, 2*time.Minute, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if len(grouped.SameHarness) != 1 || len(grouped.CrossHarness) != 0 {
		t.Fatalf("claude-code row should be same-harness for claude: %+v", grouped)
	}
	row := grouped.SameHarness[0]
	if row.Harness != "claude" || row.Status != "in-progress" {
		t.Fatalf("row harness/status = %q/%q, want claude/in-progress", row.Harness, row.Status)
	}
	if got := resumeSessionIDForHarness(row, "claude"); got != "sess-legacy" {
		t.Fatalf("claude-code session should resume natively, got %q", got)
	}
}

func TestRecentlyActive(t *testing.T) {
	now := time.Now()
	if !recentlyActive(dbpkg.ResumableSession{LastActivity: now.Add(-time.Hour).UTC().Format(time.RFC3339)}, now) {
		t.Fatal("an hour-old session is recent")
	}
	for _, at := range []string{now.Add(-8 * 24 * time.Hour).UTC().Format(time.RFC3339), "", "not a time"} {
		if recentlyActive(dbpkg.ResumableSession{LastActivity: at}, now) {
			t.Fatalf("%q should not count as recent", at)
		}
	}
}
