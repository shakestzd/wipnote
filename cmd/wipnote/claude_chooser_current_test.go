package main

import (
	"bytes"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/agent"
	dbpkg "github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/internal/launcher"
)

// TestResolveCurrentSessionIDs_ExpandsFamily verifies the env+family expansion:
// the env session ID seeds the list, and every sibling sharing that session's
// family (including the long-running split-child) is included so the chooser can
// surface whichever member carries the live transcript.
func TestResolveCurrentSessionIDs_ExpandsFamily(t *testing.T) {
	root := t.TempDir()
	// Parent stub and a split-child share family "fam-1"; an unrelated session
	// belongs to a different family and must NOT be pulled in.
	if err := agent.RegisterSessionFamily(root, "sess-parent", "fam-1"); err != nil {
		t.Fatalf("register parent: %v", err)
	}
	if err := agent.RegisterSessionFamily(root, "sess-child", "fam-1"); err != nil {
		t.Fatalf("register child: %v", err)
	}
	if err := agent.RegisterSessionFamily(root, "sess-other", "fam-2"); err != nil {
		t.Fatalf("register other: %v", err)
	}

	// Only the parent stub is known from the harness env.
	t.Setenv("WIPNOTE_SESSION_ID", "sess-parent")
	t.Setenv("WIPNOTE_HARNESS", "")
	t.Setenv("CODEX_SESSION_ID", "")
	t.Setenv("CODEX_THREAD_ID", "")
	t.Setenv("WIPNOTE_OTEL_SESSION", "")
	t.Setenv("GEMINI_SESSION_ID", "")
	t.Setenv("ANTIGRAVITY_SESSION_ID", "")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("CLAUDE_SESSION_ID", "")
	t.Setenv("WIPNOTE_PARENT_SESSION", "")
	t.Setenv("WIPNOTE_SESSION_FAMILY_ID", "")

	ids := resolveCurrentSessionIDs(root)
	got := map[string]bool{}
	for _, id := range ids {
		got[id] = true
	}
	if !got["sess-parent"] {
		t.Errorf("missing env session sess-parent in %v", ids)
	}
	if !got["sess-child"] {
		t.Errorf("missing family sibling sess-child in %v", ids)
	}
	if got["sess-other"] {
		t.Errorf("unrelated-family session sess-other must not be included: %v", ids)
	}
}

func TestResolveCurrentSessionIDs_CodexThreadBeatsInheritedParent(t *testing.T) {
	root := t.TempDir()
	if err := agent.RegisterSessionFamily(root, "codex-thread", "codex-family"); err != nil {
		t.Fatalf("register codex: %v", err)
	}
	if err := agent.RegisterSessionFamily(root, "stale-claude-parent", "claude-family"); err != nil {
		t.Fatalf("register stale parent: %v", err)
	}

	t.Setenv("WIPNOTE_HARNESS", "codex")
	t.Setenv("CODEX_SESSION_ID", "")
	t.Setenv("CODEX_THREAD_ID", "codex-thread")
	t.Setenv("WIPNOTE_OTEL_SESSION", "")
	t.Setenv("WIPNOTE_SESSION_ID", "stale-claude-parent")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("CLAUDE_SESSION_ID", "")
	t.Setenv("WIPNOTE_PARENT_SESSION", "")
	t.Setenv("WIPNOTE_SESSION_FAMILY_ID", "")

	ids := resolveCurrentSessionIDs(root)
	got := map[string]bool{}
	for _, id := range ids {
		got[id] = true
	}
	if !got["codex-thread"] {
		t.Fatalf("missing codex native thread ID in %v", ids)
	}
	if got["stale-claude-parent"] {
		t.Fatalf("inherited WIPNOTE_SESSION_ID should not compete with codex native ID: %v", ids)
	}
}

func TestResolveCurrentSessionIDs_UsesGenericSessionWhenNoHarnessNativeID(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WIPNOTE_HARNESS", "codex")
	t.Setenv("CODEX_SESSION_ID", "")
	t.Setenv("CODEX_THREAD_ID", "")
	t.Setenv("WIPNOTE_OTEL_SESSION", "")
	t.Setenv("WIPNOTE_SESSION_ID", "generic-session")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("CLAUDE_SESSION_ID", "")
	t.Setenv("WIPNOTE_PARENT_SESSION", "")
	t.Setenv("WIPNOTE_SESSION_FAMILY_ID", "")

	ids := resolveCurrentSessionIDs(root)
	if len(ids) == 0 || ids[0] != "generic-session" {
		t.Fatalf("generic session should remain fallback when no native ID exists: %v", ids)
	}
}

func TestShortSessionID(t *testing.T) {
	cases := map[string]string{
		"6ef24b5a-c9e2-4501-901e-b15de323782f": "6ef24b5a",
		"019ede12-3456":                        "019ede12",
		"abcdefghij":                           "abcdefgh", // no dash, first 8
		"short":                                "short",
		"":                                     "",
	}
	for in, want := range cases {
		if got := shortSessionID(in); got != want {
			t.Errorf("shortSessionID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTruncateTitle(t *testing.T) {
	if got := truncateTitle("short title", 44); got != "short title" {
		t.Errorf("short unchanged: got %q", got)
	}
	long := "Plan renderer strips literal angle-bracket placeholders and derives status"
	got := truncateTitle(long, 44)
	if n := len([]rune(got)); n > 44 {
		t.Errorf("truncated len = %d, want <= 44: %q", n, got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("truncated should end with ellipsis: %q", got)
	}
}

func TestRelativeTime(t *testing.T) {
	if got := relativeTime(""); got != "" {
		t.Errorf("empty: got %q, want empty", got)
	}
	if got := relativeTime("not-a-timestamp"); got == "" {
		t.Errorf("unparseable should fall back to raw, got empty")
	}
	// Far past → calendar date fallback.
	if got := relativeTime("2000-01-02T03:04:05Z"); got != "2000-01-02" {
		t.Errorf("old date: got %q, want 2000-01-02", got)
	}
	// Fractional-second RFC3339 (as messages.timestamp stores) must parse.
	old := time.Now().UTC().Add(-3 * time.Hour).Format("2006-01-02T15:04:05.000Z07:00")
	if got := relativeTime(old); got != "3h ago" {
		t.Errorf("3h fractional: got %q, want 3h ago", got)
	}
	// SQLite CURRENT_TIMESTAMP form ("2006-01-02 15:04:05", UTC, no zone) must
	// also render as a relative delta, not a bare date.
	sqliteNow := time.Now().UTC().Add(-5 * time.Minute).Format("2006-01-02 15:04:05")
	if got := relativeTime(sqliteNow); got != "5m ago" {
		t.Errorf("sqlite datetime: got %q, want 5m ago", got)
	}
}

// TestDescribeCurrentSession_ShowsSessionID is the direct fix for the "looks
// hardwired" feedback: the current-session slot must surface its short session
// ID so the user can see which session it resolves to.
func TestDescribeCurrentSession_ShowsSessionID(t *testing.T) {
	row := dbpkg.ResumableSession{
		Harness:       "claude",
		LastSessionID: "6ef24b5a-c9e2-4501-901e-b15de323782f",
		LastActivity:  "2000-01-02T03:04:05Z",
	}
	got := describeCurrentSession(row)
	if !strings.Contains(got, "6ef24b5a") {
		t.Fatalf("current-session description missing short session id: %q", got)
	}
	if strings.Contains(got, "6ef24b5a-c9e2-4501") {
		t.Fatalf("should show SHORT id, not the full UUID: %q", got)
	}
}

func TestDescribeCurrentSession_UsesSessionFactsNotWorkItemTitle(t *testing.T) {
	row := dbpkg.ResumableSession{
		WorkItemID:       "bug-stale",
		Title:            "Stale work item label",
		PromptLabel:      "review and fix this issue",
		Branch:           "main",
		ExecWorktreePath: ".wipnote/worktrees/bug-active",
		Harness:          "codex",
		LastSessionID:    "1e97ab5e-4af4-4f89-b428-f098dd05a57b",
		LastActivity:     "2000-01-02T03:04:05Z",
	}
	got := describeCurrentSession(row)
	for _, bad := range []string{"bug-stale", "Stale work item label"} {
		if strings.Contains(got, bad) {
			t.Fatalf("current-session description should not include work-item attribution %q: %q", bad, got)
		}
	}
	for _, want := range []string{`"review and fix this issue"`, "in bug-active"} {
		if !strings.Contains(got, want) {
			t.Fatalf("current-session description missing %q: %q", want, got)
		}
	}
	// With no prompt, the short session id says which session it is.
	row.PromptLabel = ""
	if got := describeCurrentSession(row); !strings.Contains(got, "1e97ab5e") {
		t.Fatalf("current-session description without a prompt should show the short id: %q", got)
	}
}

// A work-item row is about the item: it shows the id and title, and the session
// prompt only when the item has no title.
func TestDescribeResumableSession_UsesWorkItemTitleBeforePromptLabel(t *testing.T) {
	row := dbpkg.ResumableSession{
		WorkItemID:    "feat-abc123",
		Title:         "Work item title fallback",
		Type:          "feature",
		PromptLabel:   "last user prompt from the session",
		Harness:       "codex",
		LastSessionID: "sess-codex",
		LastActivity:  "2000-01-02T03:04:05Z",
	}
	got := describeResumableSession(row, true)
	if !strings.HasPrefix(got, "feat-abc123  Work item title fallback") {
		t.Fatalf("work-item row should lead with id and title: %q", got)
	}
	if strings.Contains(got, "last user prompt") || strings.Contains(got, "from Codex") {
		t.Fatalf("same-harness row with a title should show neither prompt nor source harness: %q", got)
	}
	row.Title = ""
	if got := describeResumableSession(row, false); !strings.Contains(got, "last user prompt") || !strings.Contains(got, "from Codex") {
		t.Fatalf("untitled cross-harness row should show the prompt and its harness: %q", got)
	}
}

func TestCurrentSessionActionLabel(t *testing.T) {
	row := &dbpkg.ResumableSession{Harness: "claude"}
	if got := currentSessionActionLabel(dbpkg.HarnessGroupedResumableSessions{Current: row}); got != "Resume this session" {
		t.Fatalf("launch-from session label = %q, want Resume this session", got)
	}
	if got := currentSessionActionLabel(dbpkg.HarnessGroupedResumableSessions{Current: row, CurrentIsLast: true}); got != "Continue last session" {
		t.Fatalf("last-session label = %q, want Continue last session", got)
	}
}

// TestIsActionableCurrentSession guards the slot from producing a degenerate
// continue intent. With no work item, only a same-harness session on a harness
// that resumes natively by session ID (claude/codex) is actionable. Any row with
// a work item is actionable. Cross-harness rows are not valid for the special
// current-session slot; those belong in the grouped "Continue from" list.
func TestIsActionableCurrentSession(t *testing.T) {
	cases := []struct {
		name    string
		row     dbpkg.ResumableSession
		harness string
		want    bool
	}{
		{
			name:    "same harness with resume id",
			row:     dbpkg.ResumableSession{Harness: "claude", LastSessionID: "sess-cur"},
			harness: "claude",
			want:    true,
		},
		{
			name:    "same harness without resume id",
			row:     dbpkg.ResumableSession{Harness: "claude", LastSessionID: ""},
			harness: "claude",
			want:    false,
		},
		{
			name:    "codex same harness with resume id (native resume)",
			row:     dbpkg.ResumableSession{Harness: "codex", LastSessionID: "sess-cur"},
			harness: "codex",
			want:    true,
		},
		{
			// Gemini resumes by numeric index, not the stored session ID — a
			// resume-ID-only slot is not actionable without a work item.
			name:    "gemini same harness resume-id-only not actionable",
			row:     dbpkg.ResumableSession{Harness: "gemini", LastSessionID: "sess-cur"},
			harness: "gemini",
			want:    false,
		},
		{
			name:    "gemini same harness with work item is actionable",
			row:     dbpkg.ResumableSession{Harness: "gemini", LastSessionID: "sess-cur", WorkItemID: "feat-a"},
			harness: "gemini",
			want:    true,
		},
		{
			name:    "antigravity same harness resume-id-only not actionable",
			row:     dbpkg.ResumableSession{Harness: "antigravity", LastSessionID: "sess-cur"},
			harness: "antigravity",
			want:    false,
		},
		{
			name:    "cross harness with no context",
			row:     dbpkg.ResumableSession{Harness: "codex", LastSessionID: "sess-x"},
			harness: "claude",
			want:    false,
		},
		{
			name:    "cross harness with work item is grouped, not current",
			row:     dbpkg.ResumableSession{Harness: "codex", WorkItemID: "feat-a", LastSessionID: "sess-x"},
			harness: "claude",
			want:    false,
		},
		{
			// Worktree without a work item is NOT actionable: the continue-context
			// path drops the worktree when WorkItemID is empty.
			name:    "cross harness with worktree but no work item",
			row:     dbpkg.ResumableSession{Harness: "codex", ExecWorktreePath: ".claude/worktrees/feat-a"},
			harness: "claude",
			want:    false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isActionableCurrentSession(tc.row, tc.harness); got != tc.want {
				t.Fatalf("isActionableCurrentSession() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestHasResumableOptions_CurrentOnlyCountsAsResumable is the regression guard
// for the chooseLaunchIntent gate: when the current-session slot is the ONLY
// resumable option (both harness groups empty — a split-child session with no or
// a completed work item), the chooser must NOT short-circuit to new work.
func TestHasResumableOptions_CurrentOnlyCountsAsResumable(t *testing.T) {
	cases := []struct {
		name    string
		grouped dbpkg.HarnessGroupedResumableSessions
		want    bool
	}{
		{
			name:    "nothing resumable",
			grouped: dbpkg.HarnessGroupedResumableSessions{},
			want:    false,
		},
		{
			name: "current slot only",
			grouped: dbpkg.HarnessGroupedResumableSessions{
				Current: &dbpkg.ResumableSession{Harness: "claude", LastSessionID: "sess-current"},
			},
			want: true,
		},
		{
			name: "same-harness only",
			grouped: dbpkg.HarnessGroupedResumableSessions{
				SameHarness: []dbpkg.ResumableSession{{WorkItemID: "feat-a", Harness: "claude", LastSessionID: "sess-a"}},
			},
			want: true,
		},
		{
			name: "cross-harness only",
			grouped: dbpkg.HarnessGroupedResumableSessions{
				CrossHarness: []dbpkg.ResumableSession{{WorkItemID: "feat-b", Harness: "codex", LastSessionID: "sess-b"}},
			},
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasResumableOptions(tc.grouped); got != tc.want {
				t.Fatalf("hasResumableOptions() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestPromptLaunchIntent_CurrentSessionSlotAtTop verifies the first-class
// current-session entry: it renders above the same/cross-harness groups
// (numeric option 2) and resolves to a continue intent that resumes the current
// session ID, even when that session carries no work item.
func TestPromptLaunchIntent_CurrentSessionSlotAtTop(t *testing.T) {
	var out bytes.Buffer
	grouped := dbpkg.HarnessGroupedResumableSessions{
		Current: &dbpkg.ResumableSession{
			WorkItemID:    "",
			Harness:       "claude",
			LastSessionID: "sess-current",
			LastActivity:  "2026-06-19T01:00:00Z",
		},
		SameHarness: []dbpkg.ResumableSession{{
			WorkItemID:    "feat-a",
			Title:         "Alpha",
			Type:          "feature",
			Harness:       "claude",
			LastSessionID: "sess-a",
			LastActivity:  "2026-06-16T12:00:00Z",
		}},
	}

	intent, err := promptLaunchIntent(strings.NewReader("2\n"), &out, "claude", grouped)
	if err != nil {
		t.Fatalf("promptLaunchIntent() error = %v", err)
	}
	rendered := out.String()
	if !strings.Contains(rendered, "Resume this session") {
		t.Fatalf("output missing current-session slot:\n%s", rendered)
	}
	if intent.Kind != launcher.LaunchIntentContinue {
		t.Fatalf("intent.Kind = %q, want continue", intent.Kind)
	}
	if intent.ResumeSessionID != "sess-current" {
		t.Fatalf("intent.ResumeSessionID = %q, want sess-current", intent.ResumeSessionID)
	}
}

func TestPromptLaunchIntent_CurrentSessionCrossHarnessRowNeverInserted(t *testing.T) {
	var out bytes.Buffer
	grouped := dbpkg.HarnessGroupedResumableSessions{
		CrossHarness: []dbpkg.ResumableSession{{
			WorkItemID:    "feat-a",
			Title:         "Alpha",
			Harness:       "claude",
			LastSessionID: "sess-claude",
			LastActivity:  "2026-06-19T01:00:00Z",
		}},
	}

	intent, err := promptLaunchIntent(strings.NewReader("2\n"), &out, "codex", grouped)
	if err != nil {
		t.Fatalf("promptLaunchIntent() error = %v", err)
	}
	rendered := out.String()
	if strings.Contains(rendered, "Resume this session") {
		t.Fatalf("cross-harness row should not be rendered as current session:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Continue feat-a  Alpha · from Claude") {
		t.Fatalf("cross-harness continuation should be a work-item row naming its harness:\n%s", rendered)
	}
	if intent.Kind != launcher.LaunchIntentContinue {
		t.Fatalf("intent.Kind = %q, want continue", intent.Kind)
	}
	if intent.WorkItemID != "feat-a" {
		t.Fatalf("intent.WorkItemID = %q, want feat-a", intent.WorkItemID)
	}
	if intent.ResumeSessionID != "" {
		t.Fatalf("intent.ResumeSessionID = %q, want empty for cross-harness continue", intent.ResumeSessionID)
	}
}

// TestPromptLaunchIntent_CurrentSlotOrdersBeforeGroups verifies the option
// ordering: current=2, then same-harness=3. Selecting 3 must resolve to the
// same-harness row, proving the current slot did not displace the groups.
func TestPromptLaunchIntent_CurrentSlotOrdersBeforeGroups(t *testing.T) {
	var out bytes.Buffer
	grouped := dbpkg.HarnessGroupedResumableSessions{
		Current: &dbpkg.ResumableSession{
			Harness: "claude", LastSessionID: "sess-current",
		},
		SameHarness: []dbpkg.ResumableSession{{
			WorkItemID: "feat-a", Harness: "claude", LastSessionID: "sess-a",
		}},
	}

	intent, err := promptLaunchIntent(strings.NewReader("3\n"), &out, "claude", grouped)
	if err != nil {
		t.Fatalf("promptLaunchIntent() error = %v", err)
	}
	if intent.WorkItemID != "feat-a" || intent.ResumeSessionID != "sess-a" {
		t.Fatalf("option 3 resolved to %+v, want same-harness feat-a/sess-a", intent)
	}
}

// TestPromptLaunchIntent_NoCurrentSlotWhenNil verifies clean degradation: with
// Current nil the menu is unchanged — option 2 is the first same-harness row and
// no "Resume this session" header is printed.
func TestPromptLaunchIntent_NoCurrentSlotWhenNil(t *testing.T) {
	var out bytes.Buffer
	grouped := dbpkg.HarnessGroupedResumableSessions{
		SameHarness: []dbpkg.ResumableSession{{
			WorkItemID: "feat-a", Harness: "claude", LastSessionID: "sess-a",
		}},
	}

	intent, err := promptLaunchIntent(strings.NewReader("2\n"), &out, "claude", grouped)
	if err != nil {
		t.Fatalf("promptLaunchIntent() error = %v", err)
	}
	if strings.Contains(out.String(), "Resume this session") {
		t.Fatalf("unexpected current-session slot when Current is nil:\n%s", out.String())
	}
	if intent.WorkItemID != "feat-a" {
		t.Fatalf("option 2 resolved to %q, want feat-a", intent.WorkItemID)
	}
}

// TestBuildSelectOptions_IncludesCurrentSlot verifies the TUI option list puts
// the current-session entry at index 1 (right after "Start something new"),
// ahead of the same-harness rows.
func TestBuildSelectOptions_IncludesCurrentSlot(t *testing.T) {
	grouped := dbpkg.HarnessGroupedResumableSessions{
		Current: &dbpkg.ResumableSession{
			Harness: "claude", LastSessionID: "sess-current",
		},
		SameHarness: []dbpkg.ResumableSession{{
			WorkItemID: "feat-a", Harness: "claude", LastSessionID: "sess-a",
		}},
	}
	opts := buildSelectOptions("claude", grouped)
	if len(opts) != 4 {
		t.Fatalf("len(opts) = %d, want 4 (new + current + same + browse)", len(opts))
	}
	if opts[3].Key != browseSessionsLabel || opts[3].Value != 3 {
		t.Fatalf("opts[3] = %q/%d, want the browse row last", opts[3].Key, opts[3].Value)
	}
	if !strings.Contains(opts[1].Key, "Resume this session") {
		t.Fatalf("opts[1] = %q, want current-session slot", opts[1].Key)
	}
	// Index value of the current slot must be 1 so mapIndexToIntent picks
	// orderedRows[0] (the current session).
	if opts[1].Value != 1 {
		t.Fatalf("opts[1].Value = %d, want 1", opts[1].Value)
	}
}

// TestResolveCurrentSessionIDs_CodexRootIsFallbackOnly pins CODEX_SESSION_ID
// (the root thread) as a fallback: with CODEX_THREAD_ID present the root must
// not be a candidate, or the chooser could offer the parent for resume.
func TestResolveCurrentSessionIDs_CodexRootIsFallbackOnly(t *testing.T) {
	root := t.TempDir()
	for _, k := range []string{"WIPNOTE_OTEL_SESSION", "WIPNOTE_SESSION_ID", "CLAUDE_CODE_SESSION_ID",
		"CLAUDE_SESSION_ID", "WIPNOTE_PARENT_SESSION", "WIPNOTE_SESSION_FAMILY_ID",
		"GEMINI_SESSION_ID", "ANTIGRAVITY_SESSION_ID"} {
		t.Setenv(k, "")
	}
	t.Setenv("WIPNOTE_HARNESS", "codex")
	t.Setenv("CODEX_SESSION_ID", "codex-root")

	t.Setenv("CODEX_THREAD_ID", "codex-subagent-thread")
	ids := resolveCurrentSessionIDs(root)
	if !slices.Contains(ids, "codex-subagent-thread") {
		t.Fatalf("missing current thread in %v", ids)
	}
	if slices.Contains(ids, "codex-root") {
		t.Fatalf("root CODEX_SESSION_ID must not compete with the thread id: %v", ids)
	}

	t.Setenv("CODEX_THREAD_ID", "")
	ids = resolveCurrentSessionIDs(root)
	if !slices.Contains(ids, "codex-root") {
		t.Fatalf("root CODEX_SESSION_ID should be the fallback without a thread id: %v", ids)
	}
}
