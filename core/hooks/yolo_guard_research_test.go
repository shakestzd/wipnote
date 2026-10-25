package hooks

import (
	"fmt"
	"testing"
)

// TestHasRecentResearch_FailsOpenOnRecordingGap verifies that when the
// session has no canonical tool log yet, the function returns true
// (fail-open) instead of blocking.
func TestHasRecentResearch_FailsOpenOnRecordingGap(t *testing.T) {
	projectDir := canonGuardProject(t)

	const (
		sessID  = "gap-session-0001"
		agentID = "gap-agent"
	)

	// No tool log yet → recording gap → must fail-open.
	if !hasRecentResearch(sessID, agentID, projectDir) {
		t.Error("expected hasRecentResearch=true (fail-open): only a SessionStart event recorded, recording pipeline may be broken")
	}
}

// TestHasRecentResearch_BlocksWhenNoResearchButOtherToolsRan verifies the
// normal blocking case: tool_call events exist but none are research-y.
func TestHasRecentResearch_BlocksWhenNoResearchButOtherToolsRan(t *testing.T) {
	projectDir := canonGuardProject(t)

	const (
		sessID  = "noread-session-0001"
		agentID = "noread-agent"
	)

	// A non-research Bash command: echo hi is not on the read-only list.
	appendCanonicalToolEvent(projectDir, sessID, canonicalToolEvent{Tool: "Bash", Summary: "echo hi"})

	// tool_call events exist but none are research → must block.
	if hasRecentResearch(sessID, agentID, projectDir) {
		t.Error("expected hasRecentResearch=false: Bash 'echo hi' is not research")
	}
}

// TestHasRecentResearch_WebToolsSatisfyResearch verifies that Claude Code's
// web research tools (WebSearch / WebFetch) count as research — consulting
// official docs, GitHub issues, or the web is first-class research and must not
// be penalized by the guard. See cmd/wipnote/prompts/research-routing.md.
func TestHasRecentResearch_WebToolsSatisfyResearch(t *testing.T) {
	cases := []struct {
		name    string
		sessID  string
		agentID string
		evtID   string
		tool    string
	}{
		{"WebSearch", "websearch-session-0001", "websearch-agent", "evt-websearch", "WebSearch"},
		{"WebFetch", "webfetch-session-0001", "webfetch-agent", "evt-webfetch", "WebFetch"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			projectDir := canonGuardProject(t)

			appendCanonicalToolEvent(projectDir, tc.sessID, canonicalToolEvent{Tool: tc.tool, Summary: "https://example.com"})

			if !hasRecentResearch(tc.sessID, tc.agentID, projectDir) {
				t.Errorf("expected hasRecentResearch=true: %s should count as research", tc.tool)
			}
		})
	}
}

// TestHasRecentResearch_CrossHarnessShellTools verifies that shell reads
// performed via non-Bash harness tool names (exec_command for Codex,
// run_shell_command for Gemini) are credited as research (issue #144).
// Also verifies that curl and wipnote sh/search count in all harnesses, while
// non-research commands (rm, echo) still do NOT count.
func TestHasRecentResearch_CrossHarnessShellTools(t *testing.T) {
	cases := []struct {
		name         string
		toolName     string
		inputSummary string
		wantResearch bool
	}{
		// Codex shell tool with a read-like command.
		{
			name:         "Codex exec_command cat counts as research",
			toolName:     "exec_command",
			inputSummary: "cat foo.md",
			wantResearch: true,
		},
		{
			name:         "Codex functions.exec_command cat counts as research",
			toolName:     "functions.exec_command",
			inputSummary: "cat README.md",
			wantResearch: true,
		},
		// Gemini shell tool with a read-like command.
		{
			name:         "Gemini run_shell_command grep counts as research",
			toolName:     "run_shell_command",
			inputSummary: "grep -rn foo /bar",
			wantResearch: true,
		},
		{
			name:         "Gemini run_shell_command ls counts as research",
			toolName:     "run_shell_command",
			inputSummary: "ls -la",
			wantResearch: true,
		},
		// curl and wipnote sh/search via standard Bash.
		{
			name:         "Bash curl counts as research",
			toolName:     "Bash",
			inputSummary: "curl https://example.com/api",
			wantResearch: true,
		},
		{
			name:         "Bash wipnote sh counts as research",
			toolName:     "Bash",
			inputSummary: `wipnote sh "grep -rn foo ."`,
			wantResearch: true,
		},
		{
			name:         "Bash wipnote search counts as research",
			toolName:     "Bash",
			inputSummary: "wipnote search 'fn foo'",
			wantResearch: true,
		},
		// Antigravity shell tool (run_command) with a read-like command.
		{
			name:         "Antigravity run_command cat counts as research",
			toolName:     "run_command",
			inputSummary: "cat AGENTS.md",
			wantResearch: true,
		},
		{
			name:         "Antigravity run_command grep counts as research",
			toolName:     "run_command",
			inputSummary: "grep -rn SessionID core/",
			wantResearch: true,
		},
		// Non-research shell commands must NOT count.
		{
			name:         "Bash rm does NOT count as research",
			toolName:     "Bash",
			inputSummary: "rm -rf /tmp/stuff",
			wantResearch: false,
		},
		{
			name:         "Bash echo does NOT count as research",
			toolName:     "Bash",
			inputSummary: "echo hello",
			wantResearch: false,
		},
		{
			name:         "exec_command rm does NOT count as research",
			toolName:     "exec_command",
			inputSummary: "rm file.txt",
			wantResearch: false,
		},
		{
			name:         "run_shell_command echo does NOT count as research",
			toolName:     "run_shell_command",
			inputSummary: "echo done",
			wantResearch: false,
		},
		{
			name:         "Antigravity run_command rm does NOT count as research",
			toolName:     "run_command",
			inputSummary: "rm -rf build/",
			wantResearch: false,
		},
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			projectDir := canonGuardProject(t)

			sessID := fmt.Sprintf("cross-harness-test-sess-%02d", i)
			agentID := fmt.Sprintf("cross-harness-agent-%02d", i)

			// Insert the shell read event.
			appendCanonicalToolEvent(projectDir, sessID, canonicalToolEvent{Tool: tc.toolName, Summary: tc.inputSummary})

			// When wantResearch=false we also need a second tool_call so
			// fail-open (no tool log) does not mask the false-positive.
			if !tc.wantResearch {
				appendCanonicalToolEvent(projectDir, sessID, canonicalToolEvent{Tool: "Write", Summary: "out.txt"})
			}

			got := hasRecentResearch(sessID, agentID, projectDir)
			if got != tc.wantResearch {
				t.Errorf("hasRecentResearch for tool=%q cmd=%q = %v, want %v",
					tc.toolName, tc.inputSummary, got, tc.wantResearch)
			}
		})
	}
}

// TestHasRecentResearch_SedReadOnlyCounts verifies that a read-only `sed`
// invocation (e.g. `sed -n '1,20p' file`) counts as research, but an in-place
// write form (`sed -i ...` / `sed --in-place ...`) does NOT — that is a file
// mutation, not a read, and crediting it would let the research guard be
// satisfied by the very write it exists to gate (issue #125).
func TestHasRecentResearch_SedReadOnlyCounts(t *testing.T) {
	cases := []struct {
		name         string
		inputSummary string
		wantResearch bool
	}{
		{"sed -n print range counts", "sed -n '1,20p' core/hooks/yolo_guard.go", true},
		{"plain sed substitution to stdout counts", "sed 's/foo/bar/' file.go", true},
		{"sed -i in-place does NOT count", "sed -i 's/foo/bar/' file.go", false},
		{"sed --in-place does NOT count", "sed --in-place 's/foo/bar/' file.go", false},
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			projectDir := canonGuardProject(t)

			sessID := fmt.Sprintf("sed-test-sess-%02d", i)
			agentID := fmt.Sprintf("sed-test-agent-%02d", i)

			appendCanonicalToolEvent(projectDir, sessID, canonicalToolEvent{Tool: "Bash", Summary: tc.inputSummary})

			// When wantResearch=false, add a second tool_call so the
			// no-tool-log fail-open path does not mask a false positive.
			if !tc.wantResearch {
				appendCanonicalToolEvent(projectDir, sessID, canonicalToolEvent{Tool: "Write", Summary: "out.txt"})
			}

			got := hasRecentResearch(sessID, agentID, projectDir)
			if got != tc.wantResearch {
				t.Errorf("hasRecentResearch for cmd=%q = %v, want %v", tc.inputSummary, got, tc.wantResearch)
			}
		})
	}
}
