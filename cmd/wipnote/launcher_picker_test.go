package main

import (
	"bytes"
	"fmt"
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
	for in, want := range map[string]string{
		"/home/user/wip_note.v2": "-home-user-wip-note-v2",
		"/home/café/日本":          "-home-caf----", // one '-' per character, not per byte
		"/x/a😀b":                 "-x-a--b",       // a character outside the BMP is two UTF-16 units
	} {
		if got := claudeProjectDirName(in); got != want {
			t.Fatalf("claudeProjectDirName(%q) = %q, want %q", in, got, want)
		}
	}
}

// userLine is a transcript user message recorded in cwd.
func userLine(cwd, prompt string) string {
	return fmt.Sprintf(`{"type":"user","cwd":%q,"message":{"content":%q}}`, cwd, prompt)
}

func TestLatestClaudeTranscriptSession_NewestAcrossRootAndWorktrees(t *testing.T) {
	projects := t.TempDir()
	base := t.TempDir()
	root := filepath.Join(base, "app")
	wt := filepath.Join(root, ".claude", "worktrees", "feat-x")
	sibling := filepath.Join(base, "app-copy") // its folder shares root's prefix
	for _, d := range []string{wt, sibling} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	rootDir := filepath.Join(projects, claudeProjectDirName(root))
	wtDir := filepath.Join(projects, claudeProjectDirName(wt))
	otherDir := filepath.Join(projects, claudeProjectDirName(base+"/application"))

	writeTranscript(t, rootDir, "old", []string{userLine(root, "older work")}, now.Add(-3*time.Hour))
	writeTranscript(t, wtDir, "wt", []string{
		fmt.Sprintf(`{"type":"attachment","cwd":%q}`, wt),
		`{"type":"user","isMeta":true,"message":{"content":"meta text"}}`,
		`{"type":"user","message":{"content":"<command-name>/clear</command-name>"}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","content":"x"}]}}`,
		`{"type":"user","message":{"content":[{"type":"text","text":"fix the   herdr\nLive tab"}]}}`,
	}, now.Add(-time.Hour))
	// Newer than the worktree session, but unusable: another project, a sibling
	// repository whose folder shares the prefix, a subagent file, and an
	// aborted session with no prompt.
	writeTranscript(t, otherDir, "other", []string{userLine(base+"/application", "not this project")}, now)
	writeTranscript(t, filepath.Join(projects, claudeProjectDirName(sibling)), "sibling", []string{userLine(sibling, "other repo")}, now.Add(time.Minute))
	writeTranscript(t, rootDir, "agent-sub", []string{userLine(root, "subagent")}, now)
	writeTranscript(t, rootDir, "aborted", []string{fmt.Sprintf(`{"type":"attachment","cwd":%q}`, root)}, now.Add(2*time.Minute))

	got := latestClaudeTranscriptSession(projects, root)
	if got == nil {
		t.Fatal("no session found")
	}
	if got.ID != "wt" || got.Label != "fix the herdr Live tab" || got.Cwd != wt {
		t.Fatalf("got %+v, want the worktree session with its first typed prompt", got)
	}
}

func TestLatestClaudeTranscriptSession_FallsBackPastUnusable(t *testing.T) {
	projects := t.TempDir()
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	gone := filepath.Join(root, ".claude", "worktrees", "removed") // never created
	now := time.Now()
	writeTranscript(t, filepath.Join(projects, claudeProjectDirName(root)), "good", []string{userLine(sub, "real work")}, now.Add(-time.Hour))
	writeTranscript(t, filepath.Join(projects, claudeProjectDirName(root)), "nocwd", []string{`{"type":"user","message":{"content":"no cwd"}}`}, now)
	writeTranscript(t, filepath.Join(projects, claudeProjectDirName(gone)), "gone", []string{userLine(gone, "worktree deleted")}, now.Add(time.Minute))
	got := latestClaudeTranscriptSession(projects, root)
	if got == nil || got.ID != "good" {
		t.Fatalf("got %+v, want the older session in a directory that still exists", got)
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

func TestPromptLaunchIntent_BrowseOfferedWithEmptyShortlist(t *testing.T) {
	var out bytes.Buffer
	intent, err := promptLaunchIntent(strings.NewReader("2\n"), &out, "claude", dbpkg.HarnessGroupedResumableSessions{})
	if err != nil || !intent.WantsBrowse() {
		t.Fatalf("choice 2 with no rows = %+v, %v; want browse\n%s", intent, err, out.String())
	}
	out.Reset()
	if intent, _ := promptLaunchIntent(strings.NewReader("2\n"), &out, "codex", dbpkg.HarnessGroupedResumableSessions{}); intent.Kind != launcher.LaunchIntentNew || out.Len() != 0 {
		t.Fatalf("codex with no rows should start new work without a menu: %+v\n%s", intent, out.String())
	}
}

func TestApplyClaudeLaunchIntent_DropsNonClaudeResumeID(t *testing.T) {
	legacy := launcher.ContinueWorkIntent("feat-a", "claude", "sess-legacy", "", true)
	if r := applyClaudeLaunchIntent("", "", legacy); r.resumeID != "" || r.workItem != "feat-a" || r.mode != "continue" {
		t.Fatalf("a legacy wipnote id must not reach --resume: %+v", r)
	}
	const uuid = "0f1e2d3c-4b5a-6978-8a9b-0c1d2e3f4a5b"
	real := launcher.ContinueWorkIntent("", "claude", uuid, "", true)
	if r := applyClaudeLaunchIntent("", "", real); r.resumeID != uuid {
		t.Fatalf("a Claude session id should resume directly: %+v", r)
	}
}

func TestPicksExistingSession(t *testing.T) {
	browse := applyClaudeLaunchIntent("", "", launcher.BrowseSessionsIntent())
	last := applyClaudeLaunchIntent("", "", launcher.ContinueWorkIntent("", "claude", "0f1e2d3c-4b5a-6978-8a9b-0c1d2e3f4a5b", "", true))
	work := applyClaudeLaunchIntent("", "", launcher.ContinueWorkIntent("feat-a", "claude", "", "", true))
	fresh := applyClaudeLaunchIntent("", "", launcher.NewWorkIntent())
	if !picksExistingSession(browse, browse.workItem, browse.resumeID) || !picksExistingSession(last, last.workItem, last.resumeID) {
		t.Fatal("browse and a last-session resume pick an existing session")
	}
	if picksExistingSession(work, work.workItem, work.resumeID) || picksExistingSession(fresh, fresh.workItem, fresh.resumeID) {
		t.Fatal("a work item or new work may still get its own worktree")
	}
}

func TestApplyLaunchPlanOpts_InPlacePlansNoAdhocWorktree(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".wipnote"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".wipnote", "config.json"), []byte(`{"launch_isolation":"auto"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var w bytes.Buffer
	if p := applyLaunchPlanOpts(root, root, "", true, false, &w); p.PlannedWorktreePath != "" || p.RefuseLaunch {
		t.Fatalf("in-place plan under auto isolation = %+v, want no worktree and no refusal", p)
	}
}
