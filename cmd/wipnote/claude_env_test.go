package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func assertEnvContains(t *testing.T, env []string, key, want string) {
	t.Helper()
	prefix := key + "="
	for _, kv := range env {
		if after, ok := strings.CutPrefix(kv, prefix); ok {
			if after != want {
				t.Errorf("%s = %q, want %q", key, after, want)
			}
			return
		}
	}
	t.Errorf("%s not set; want %q", key, want)
}

func assertEnvEmptyOrUnset(t *testing.T, env []string, key string) {
	t.Helper()
	prefix := key + "="
	for _, kv := range env {
		if after, ok := strings.CutPrefix(kv, prefix); ok {
			if after != "" {
				t.Errorf("%s = %q, want empty or unset", key, after)
			}
			return
		}
	}
}

func clearOtelEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"WIPNOTE_OTEL_ENABLED",
		"WIPNOTE_OTEL_HTTP_PORT",
		"WIPNOTE_OTEL_BIND",
		"WIPNOTE_OTEL_SESSION_ID",
		"WIPNOTE_SESSION_ID",
		"WIPNOTE_PROJECT_DIR",
		"CLAUDE_PROJECT_DIR",
		"CLAUDE_CODE_ENABLE_TELEMETRY",
		"CLAUDE_CODE_ENHANCED_TELEMETRY_BETA",
		"CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS",
		"OTEL_METRICS_EXPORTER",
		"OTEL_LOGS_EXPORTER",
		"OTEL_TRACES_EXPORTER",
		"OTEL_EXPORTER_OTLP_PROTOCOL",
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_LOG_TOOL_DETAILS",
		"OTEL_LOG_USER_PROMPTS",
		"OTEL_LOG_TOOL_CONTENT",
	} {
		t.Setenv(key, "")
	}
}

func testOverrides(port int) *otelEnvOverrides {
	return &otelEnvOverrides{CollectorPort: port, SessionID: "test-session"}
}

func TestBuildClaudeLaunchEnv_ExplicitOptOut(t *testing.T) {
	clearOtelEnv(t)
	t.Setenv("WIPNOTE_OTEL_ENABLED", "0")
	env := buildClaudeLaunchEnv("", nil)
	for _, key := range []string{
		"CLAUDE_CODE_ENABLE_TELEMETRY",
		"OTEL_METRICS_EXPORTER",
		"OTEL_EXPORTER_OTLP_ENDPOINT",
	} {
		assertEnvEmptyOrUnset(t, env, key)
	}

	for _, val := range []string{"false", "no", "off"} {
		t.Setenv("WIPNOTE_OTEL_ENABLED", val)
		env = buildClaudeLaunchEnv("", nil)
		assertEnvEmptyOrUnset(t, env, "CLAUDE_CODE_ENABLE_TELEMETRY")
	}
}

func TestBuildClaudeLaunchEnv_NoCollectorSkipsOTelInjection(t *testing.T) {
	clearOtelEnv(t)
	env := buildClaudeLaunchEnv("", nil)
	assertEnvEmptyOrUnset(t, env, "OTEL_EXPORTER_OTLP_ENDPOINT")
}

func TestBuildClaudeLaunchEnv_WithCollector(t *testing.T) {
	clearOtelEnv(t)
	env := buildClaudeLaunchEnv("", testOverrides(12345))
	assertEnvContains(t, env, "CLAUDE_CODE_ENABLE_TELEMETRY", "1")
	assertEnvContains(t, env, "OTEL_TRACES_EXPORTER", "otlp")
	assertEnvContains(t, env, "OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:12345")
	// OTel session ID goes to its own var; WIPNOTE_SESSION_ID must NOT carry it.
	assertEnvContains(t, env, "WIPNOTE_OTEL_SESSION_ID", "test-session")
	assertEnvEmptyOrUnset(t, env, "WIPNOTE_SESSION_ID")
}

func TestBuildClaudeLaunchEnv_InjectsWhenCollectorActive(t *testing.T) {
	clearOtelEnv(t)
	t.Setenv("WIPNOTE_OTEL_ENABLED", "1")

	env := buildClaudeLaunchEnv("", testOverrides(9999))
	assertEnvContains(t, env, "CLAUDE_CODE_ENABLE_TELEMETRY", "1")
	assertEnvContains(t, env, "CLAUDE_CODE_ENHANCED_TELEMETRY_BETA", "1")
	assertEnvContains(t, env, "OTEL_METRICS_EXPORTER", "otlp")
	assertEnvContains(t, env, "OTEL_LOGS_EXPORTER", "otlp")
	assertEnvContains(t, env, "OTEL_TRACES_EXPORTER", "otlp")
	assertEnvContains(t, env, "OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf")
	assertEnvContains(t, env, "OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:9999")
	assertEnvContains(t, env, "OTEL_LOG_TOOL_DETAILS", "1")
	// Prompt text and tool payloads are private by default.
	assertEnvContains(t, env, "OTEL_LOG_USER_PROMPTS", "0")
	assertEnvContains(t, env, "OTEL_LOG_TOOL_CONTENT", "0")
}

func TestBuildClaudeLaunchEnv_ContentLoggingIsOptIn(t *testing.T) {
	clearOtelEnv(t)
	t.Setenv("WIPNOTE_OTEL_ENABLED", "1")
	t.Setenv("OTEL_LOG_USER_PROMPTS", "1")
	t.Setenv("OTEL_LOG_TOOL_CONTENT", "1")

	env := buildClaudeLaunchEnv("", testOverrides(9999))
	assertEnvContains(t, env, "OTEL_LOG_USER_PROMPTS", "1")
	assertEnvContains(t, env, "OTEL_LOG_TOOL_CONTENT", "1")
}

func TestBuildClaudeLaunchEnv_RespectsUserOverrides(t *testing.T) {
	clearOtelEnv(t)
	t.Setenv("WIPNOTE_OTEL_ENABLED", "1")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "https://custom.example.com:4318")
	t.Setenv("OTEL_METRICS_EXPORTER", "console")
	t.Setenv("OTEL_LOG_TOOL_DETAILS", "0")

	env := buildClaudeLaunchEnv("", testOverrides(7777))
	assertEnvContains(t, env, "OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:7777")
	assertEnvContains(t, env, "OTEL_METRICS_EXPORTER", "console")
	assertEnvContains(t, env, "OTEL_LOG_TOOL_DETAILS", "0")
	assertEnvContains(t, env, "CLAUDE_CODE_ENABLE_TELEMETRY", "1")
}

func TestBuildClaudeLaunchEnv_WorktreeProjectDir(t *testing.T) {
	clearOtelEnv(t)
	t.Setenv("WIPNOTE_PROJECT_DIR", "/old/value")
	env := buildClaudeLaunchEnv("/worktree/main/.wipnote", nil)
	assertEnvContains(t, env, "WIPNOTE_PROJECT_DIR", "/worktree/main/.wipnote")
}

func TestIsTruthy(t *testing.T) {
	for _, s := range []string{"1", "true", "TRUE", "yes", "on"} {
		if !isTruthy(s) {
			t.Errorf("isTruthy(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"", "0", "false", "no", "off", "maybe"} {
		if isTruthy(s) {
			t.Errorf("isTruthy(%q) = true, want false", s)
		}
	}
}

func TestIsExplicitlyDisabled(t *testing.T) {
	for _, s := range []string{"0", "false", "FALSE", "no", "off"} {
		if !isExplicitlyDisabled(s) {
			t.Errorf("isExplicitlyDisabled(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"", "1", "true", "yes", "random"} {
		if isExplicitlyDisabled(s) {
			t.Errorf("isExplicitlyDisabled(%q) = true, want false", s)
		}
	}
	for _, s := range []string{" 0", "false ", "  no  ", "\toff\t"} {
		if !isExplicitlyDisabled(s) {
			t.Errorf("isExplicitlyDisabled(%q) = false, want true (whitespace)", s)
		}
	}
}

// TestBuildClaudeLaunchEnv_InjectsAgentTeamsByDefault verifies that
// CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1 is set in the returned env
// even when no OTel collector is active.
func TestBuildClaudeLaunchEnv_InjectsAgentTeamsByDefault(t *testing.T) {
	clearOtelEnv(t)
	t.Setenv("CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS", "")

	env := buildClaudeLaunchEnv("", nil)
	assertEnvContains(t, env, "CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS", "1")
}

// TestBuildClaudeLaunchEnv_UserOverrideOfAgentTeamsWins verifies that a
// user-set value for CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS is not overwritten
// by the registry default (addIfUnset semantics).
func TestBuildClaudeLaunchEnv_UserOverrideOfAgentTeamsWins(t *testing.T) {
	clearOtelEnv(t)
	t.Setenv("CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS", "0")

	env := buildClaudeLaunchEnv("", nil)
	assertEnvContains(t, env, "CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS", "0")
}

// TestBuildClaudeLaunchEnv_StripsStaleInheritedSessionVars verifies that a
// parent shell's stale WIPNOTE_SESSION_ID / WIPNOTE_OTEL_SESSION_ID are not
// passed through to the child Claude process (bug-b262d303, roborev #416).
func TestBuildClaudeLaunchEnv_StripsStaleInheritedSessionVars(t *testing.T) {
	clearOtelEnv(t)
	// Simulate a parent Claude shell exporting stale launcher session vars.
	t.Setenv("WIPNOTE_SESSION_ID", "stale-parent")
	t.Setenv("WIPNOTE_OTEL_SESSION_ID", "stale-otel")

	// No collector: neither inherited var should survive into the child env.
	env := buildClaudeLaunchEnv("", nil)
	assertEnvEmptyOrUnset(t, env, "WIPNOTE_SESSION_ID")
	assertEnvEmptyOrUnset(t, env, "WIPNOTE_OTEL_SESSION_ID")

	// With a fresh collector session ID: only the fresh OTel var survives, and
	// the stale inherited WIPNOTE_SESSION_ID is still stripped.
	env = buildClaudeLaunchEnv("", testOverrides(12345))
	assertEnvContains(t, env, "WIPNOTE_OTEL_SESSION_ID", "test-session")
	assertEnvEmptyOrUnset(t, env, "WIPNOTE_SESSION_ID")
}

// TestClaudeLaunchEnv_FromLinkedWorktreeMatchesClaudeDir is the regression
// test for issue #202: `wipnote claude` run from a linked worktree resolves the
// main checkout as the project (so Claude runs there), and the exported
// WIPNOTE_PROJECT_DIR must name that same directory — not the launcher's cwd.
func TestClaudeLaunchEnv_FromLinkedWorktreeMatchesClaudeDir(t *testing.T) {
	clearOtelEnv(t)
	base := t.TempDir()
	mainRepo := filepath.Join(base, "repo")
	wt := filepath.Join(base, "repo-wt-feature")
	if err := os.MkdirAll(filepath.Join(mainRepo, ".wipnote"), 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, mainRepo)
	if err := os.WriteFile(filepath.Join(mainRepo, ".wipnote", "keep"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, mainRepo, "add", ".")
	mustGit(t, mainRepo, "commit", "-qm", "init")
	mustGit(t, mainRepo, "worktree", "add", "-q", wt)

	// Resolve from the worktree exactly as the launcher does, with no ambient
	// project vars (TestMain points them at its sandbox).
	t.Setenv("WIPNOTE_PROJECT_DIR", "")
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	t.Chdir(wt)
	projectRoot, err := resolveProjectRoot()
	if err != nil {
		t.Fatalf("resolveProjectRoot: %v", err)
	}
	opts := LaunchOpts{ProjectRoot: projectRoot, WipnoteRoot: canonicalProjectRoot(projectRoot)}

	env := buildClaudeLaunchEnv(claudeEnvProjectDir(opts), nil)
	got := resolveForCompare(envValue(env, "WIPNOTE_PROJECT_DIR"))
	if want := resolveForCompare(opts.ProjectRoot); got != want {
		t.Fatalf("WIPNOTE_PROJECT_DIR = %q, but Claude runs in %q (c.Dir)", got, want)
	}
	if got == resolveForCompare(wt) {
		t.Fatalf("WIPNOTE_PROJECT_DIR exported the launcher's worktree cwd %q", wt)
	}
}

func envValue(env []string, key string) string {
	for _, kv := range env {
		if strings.HasPrefix(kv, key+"=") {
			return strings.TrimPrefix(kv, key+"=")
		}
	}
	return ""
}
