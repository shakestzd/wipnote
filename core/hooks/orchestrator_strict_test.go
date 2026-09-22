package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// orchestratorFixture writes an orchestrator.json and returns the project dir.
func orchestratorFixture(t *testing.T, cfg OrchestratorConfig) string {
	t.Helper()
	projectDir := t.TempDir()
	wipnoteDir := filepath.Join(projectDir, ".wipnote")
	if err := os.MkdirAll(wipnoteDir, 0o755); err != nil {
		t.Fatalf("mkdir .wipnote: %v", err)
	}
	if err := SaveOrchestratorConfig(wipnoteDir, cfg); err != nil {
		t.Fatalf("SaveOrchestratorConfig: %v", err)
	}
	return projectDir
}

func orchestratorCtx(projectDir string, isSubagent bool) *toolUseContext {
	return &toolUseContext{
		SessionID:  "orch-sess",
		AgentID:    "claude-code",
		IsSubagent: isSubagent,
		ProjectDir: projectDir,
		HgDir:      filepath.Join(projectDir, ".wipnote"),
	}
}

func bashEvent(command string) *CloudEvent {
	return &CloudEvent{ToolName: "Bash", ToolInput: map[string]any{"command": command}}
}

// TestOrchestratorStrictGuard_Whitelist pins which tools an orchestrator may
// run directly. Skill is deliberately NOT whitelisted — that is GH-#19.
func TestOrchestratorStrictGuard_Whitelist(t *testing.T) {
	t.Setenv(OrchestratorRescueEnv, "")
	allowed := []*CloudEvent{
		{ToolName: "Task"},
		{ToolName: "Agent"},
		{ToolName: "AskUserQuestion"},
		{ToolName: "TodoWrite"},
		{ToolName: "TaskCreate"},
		{ToolName: "TaskUpdate"},
		{ToolName: "TaskList"},
		{ToolName: "TaskGet"},
		bashEvent("wipnote feature start feat-1"),
		bashEvent("  wipnote snapshot --summary"),
	}
	for _, event := range allowed {
		// A fresh config per case so an earlier violation cannot escalate.
		projectDir := orchestratorFixture(t, OrchestratorConfig{
			Enabled: true, Mode: OrchestratorModeStrict, MaxViolations: 3,
		})
		advisory, block := checkOrchestratorStrictGuard(event, orchestratorCtx(projectDir, false))
		if advisory != "" || block != "" {
			t.Errorf("%s: want no guard output, got advisory=%q block=%q", event.ToolName, advisory, block)
		}
		cfg, _ := LoadOrchestratorConfig(filepath.Join(projectDir, ".wipnote"))
		if cfg.Violations != 0 {
			t.Errorf("%s: whitelisted call counted a violation", event.ToolName)
		}
	}

	denied := []*CloudEvent{
		{ToolName: "Skill", ToolInput: map[string]any{"skill": "frontend-design:frontend-design"}},
		{ToolName: "Read", ToolInput: map[string]any{"file_path": "main.go"}},
		{ToolName: "Edit", ToolInput: map[string]any{"file_path": "main.go"}},
		bashEvent("gh issue create --title x"),
		bashEvent("grep -rn github ."),
	}
	for _, event := range denied {
		projectDir := orchestratorFixture(t, OrchestratorConfig{
			Enabled: true, Mode: OrchestratorModeStrict, MaxViolations: 3,
		})
		advisory, block := checkOrchestratorStrictGuard(event, orchestratorCtx(projectDir, false))
		if advisory == "" || block != "" {
			t.Errorf("%s: want a first-violation advisory, got advisory=%q block=%q",
				event.ToolName, advisory, block)
		}
		cfg, _ := LoadOrchestratorConfig(filepath.Join(projectDir, ".wipnote"))
		if cfg.Violations != 1 {
			t.Errorf("%s: violations = %d, want 1", event.ToolName, cfg.Violations)
		}
	}
}

// TestOrchestratorStrictGuard_Escalates pins the warn → block escalation and
// that the counter is durable in orchestrator.json across calls.
func TestOrchestratorStrictGuard_Escalates(t *testing.T) {
	t.Setenv(OrchestratorRescueEnv, "")
	projectDir := orchestratorFixture(t, OrchestratorConfig{
		Enabled: true, Mode: OrchestratorModeStrict, MaxViolations: 3,
	})
	ctx := orchestratorCtx(projectDir, false)
	event := &CloudEvent{ToolName: "Skill", ToolInput: map[string]any{"skill": "x"}}

	for i := 1; i <= 2; i++ {
		advisory, block := checkOrchestratorStrictGuard(event, ctx)
		if block != "" {
			t.Fatalf("violation %d blocked too early: %s", i, block)
		}
		if !strings.Contains(advisory, "of 3") {
			t.Errorf("violation %d advisory missing the escalation count: %q", i, advisory)
		}
	}
	advisory, block := checkOrchestratorStrictGuard(event, ctx)
	if block == "" {
		t.Fatal("third violation should block")
	}
	if advisory != "" {
		t.Errorf("a blocking decision should not also advise: %q", advisory)
	}
	if !strings.Contains(block, "Task(") {
		t.Errorf("block message must tell the orchestrator to delegate: %q", block)
	}
	if !strings.Contains(block, OrchestratorRescueEnv) {
		t.Errorf("block message must name the rescue escape hatch: %q", block)
	}
	cfg, ok := LoadOrchestratorConfig(filepath.Join(projectDir, ".wipnote"))
	if !ok || cfg.Violations != 3 {
		t.Errorf("violations = %d (ok=%v), want 3 persisted", cfg.Violations, ok)
	}
}

// TestOrchestratorStrictGuard_Exemptions covers every way the guard stands
// down: subagents, orchestrator mode off, no config, and the rescue hatch.
func TestOrchestratorStrictGuard_Exemptions(t *testing.T) {
	event := &CloudEvent{ToolName: "Skill", ToolInput: map[string]any{"skill": "x"}}
	strict := OrchestratorConfig{Enabled: true, Mode: OrchestratorModeStrict, MaxViolations: 1}

	t.Run("subagent", func(t *testing.T) {
		t.Setenv(OrchestratorRescueEnv, "")
		projectDir := orchestratorFixture(t, strict)
		advisory, block := checkOrchestratorStrictGuard(event, orchestratorCtx(projectDir, true))
		if advisory != "" || block != "" {
			t.Errorf("subagents are the delegate and must be exempt: advisory=%q block=%q", advisory, block)
		}
	})

	t.Run("disabled", func(t *testing.T) {
		t.Setenv(OrchestratorRescueEnv, "")
		projectDir := orchestratorFixture(t, OrchestratorConfig{Enabled: false, Mode: OrchestratorModeStrict})
		advisory, block := checkOrchestratorStrictGuard(event, orchestratorCtx(projectDir, false))
		if advisory != "" || block != "" {
			t.Errorf("disabled orchestrator mode must be inert: advisory=%q block=%q", advisory, block)
		}
	})

	t.Run("no config", func(t *testing.T) {
		t.Setenv(OrchestratorRescueEnv, "")
		projectDir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(projectDir, ".wipnote"), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		advisory, block := checkOrchestratorStrictGuard(event, orchestratorCtx(projectDir, false))
		if advisory != "" || block != "" {
			t.Errorf("no config must mean orchestrator mode off: advisory=%q block=%q", advisory, block)
		}
	})

	t.Run("rescue escape hatch", func(t *testing.T) {
		// bug-c8ac6a11: when subagents fail the orchestrator must be able to
		// take over, even at max_violations.
		t.Setenv(OrchestratorRescueEnv, "1")
		projectDir := orchestratorFixture(t, OrchestratorConfig{
			Enabled: true, Mode: OrchestratorModeStrict, Violations: 99, MaxViolations: 1,
		})
		advisory, block := checkOrchestratorStrictGuard(
			&CloudEvent{ToolName: "Edit", ToolInput: map[string]any{"file_path": "main.go"}},
			orchestratorCtx(projectDir, false))
		if advisory != "" || block != "" {
			t.Errorf("rescue mode must allow direct writes: advisory=%q block=%q", advisory, block)
		}
	})
}

// TestOrchestratorGuidanceMode_AdvisoryOnly pins that guidance mode never
// blocks and never counts — it only nudges.
func TestOrchestratorGuidanceMode_AdvisoryOnly(t *testing.T) {
	t.Setenv(OrchestratorRescueEnv, "")
	projectDir := orchestratorFixture(t, OrchestratorConfig{
		Enabled: true, Mode: OrchestratorModeGuidance, MaxViolations: 1,
	})
	ctx := orchestratorCtx(projectDir, false)
	event := &CloudEvent{ToolName: "Skill", ToolInput: map[string]any{"skill": "x"}}

	for i := 0; i < 5; i++ {
		advisory, block := checkOrchestratorStrictGuard(event, ctx)
		if block != "" {
			t.Fatalf("guidance mode must never block, got: %s", block)
		}
		if advisory == "" {
			t.Fatal("guidance mode should still advise")
		}
		if strings.Contains(advisory, "will block") {
			t.Errorf("guidance advisory must not threaten a block: %q", advisory)
		}
	}
	cfg, _ := LoadOrchestratorConfig(filepath.Join(projectDir, ".wipnote"))
	if cfg.Violations != 0 {
		t.Errorf("guidance mode counted %d violations, want 0", cfg.Violations)
	}
}

// TestPreToolUse_OrchestratorStrictBlocksSkill is the end-to-end proof through
// the real read-only hook open path: strict mode at its cap blocks a Skill
// call from the root session and tells it to delegate.
func TestPreToolUse_OrchestratorStrictBlocksSkill(t *testing.T) {
	clearNestedEnv(t)
	t.Setenv("WIPNOTE_NO_AUTO_WRITER", "1")
	t.Setenv(OrchestratorRescueEnv, "")
	stubRouteSQLAsync(t, true)
	featureIDCache = featureIDCacheEntry{}

	const sessionID = "019ee390-abcd-7000-8000-000000001920"
	projectDir := orchestratorFixture(t, OrchestratorConfig{
		Enabled: true, Mode: OrchestratorModeStrict, Violations: 2, MaxViolations: 3,
	})
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	t.Setenv("WIPNOTE_PROJECT_DIR", projectDir)

	database, _ := OpenHookDBReadOnly("pretooluse", sessionID, ":memory:")
	if database == nil {
		t.Fatal("OpenHookDBReadOnly returned nil handle")
	}
	t.Cleanup(func() { _ = database.Close() })

	result, err := PreToolUse(&CloudEvent{
		SessionID: sessionID,
		AgentID:   "claude-code",
		CWD:       projectDir,
		ToolName:  "Skill",
		ToolInput: map[string]any{"skill": "frontend-design:frontend-design"},
	}, database)
	if err != nil {
		t.Fatalf("PreToolUse: %v", err)
	}
	if result == nil || result.Decision != "block" ||
		!strings.Contains(result.Reason, "Orchestrator strict mode") {
		t.Fatalf("expected the orchestrator strict block, got %+v", result)
	}
}

// TestPreToolUse_OrchestratorStrictAdvisesBeforeBlocking pins that a
// below-cap violation surfaces as additionalContext and the tool still runs.
func TestPreToolUse_OrchestratorStrictAdvisesBeforeBlocking(t *testing.T) {
	clearNestedEnv(t)
	t.Setenv("WIPNOTE_NO_AUTO_WRITER", "1")
	t.Setenv(OrchestratorRescueEnv, "")
	stubRouteSQLAsync(t, true)
	featureIDCache = featureIDCacheEntry{}

	const sessionID = "019ee390-abcd-7000-8000-000000001921"
	projectDir := orchestratorFixture(t, OrchestratorConfig{
		Enabled: true, Mode: OrchestratorModeStrict, MaxViolations: 3,
	})
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	t.Setenv("WIPNOTE_PROJECT_DIR", projectDir)

	database, _ := OpenHookDBReadOnly("pretooluse", sessionID, ":memory:")
	if database == nil {
		t.Fatal("OpenHookDBReadOnly returned nil handle")
	}
	t.Cleanup(func() { _ = database.Close() })

	result, err := PreToolUse(&CloudEvent{
		SessionID: sessionID,
		AgentID:   "claude-code",
		CWD:       projectDir,
		ToolName:  "Skill",
		ToolInput: map[string]any{"skill": "frontend-design:frontend-design"},
	}, database)
	if err != nil {
		t.Fatalf("PreToolUse: %v", err)
	}
	if result == nil || result.Decision == "block" {
		t.Fatalf("first violation must not block: %+v", result)
	}
	if !strings.Contains(result.AdditionalContext, "orchestrator advisory") {
		t.Errorf("expected the advisory in additionalContext, got %q", result.AdditionalContext)
	}
}
