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
		{ToolName: "SendMessage"},
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
		// A compound command starting with "wipnote" is not whitelisted just
		// because its first word is — every segment must be a wipnote
		// invocation (isWipnoteCLICommand), or a direct-execution segment
		// like the gofmt call here would slip through uncounted.
		bashEvent("wipnote status && gofmt -w main.go"),
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
		if !strings.Contains(advisory, "/3") {
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
		if i == 0 && advisory == "" {
			t.Fatal("guidance mode should advise on the first call")
		}
		if i > 0 && advisory != "" {
			t.Fatalf("advisory must show once per session, repeated on call %d: %q", i, advisory)
		}
		if strings.Contains(advisory, "blocks at") {
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

// TestOrchestratorStrictGuard_ShortReads pins feat-0e560d63: the orchestrator
// may Read a short file (or a short limit range) directly, while long or
// uninspectable reads remain violations.
func TestOrchestratorStrictGuard_ShortReads(t *testing.T) {
	t.Setenv(OrchestratorRescueEnv, "")
	cases := []struct {
		name    string
		lines   int // 0 = do not create the file
		limit   any
		allowed bool
	}{
		{name: "short file", lines: 200, allowed: true},
		{name: "long file", lines: 201, allowed: false},
		{name: "limit within range on long file", lines: 5000, limit: float64(200), allowed: true},
		{name: "missing file", lines: 0, allowed: false},
		{name: "limit over range on long file", lines: 5000, limit: float64(201), allowed: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			projectDir := orchestratorFixture(t, OrchestratorConfig{
				Enabled: true, Mode: OrchestratorModeStrict, MaxViolations: 3,
			})
			path := filepath.Join(projectDir, "target.go")
			if tc.lines > 0 {
				body := strings.Repeat("line\n", tc.lines)
				if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
					t.Fatalf("write target: %v", err)
				}
			}
			input := map[string]any{"file_path": path}
			if tc.limit != nil {
				input["limit"] = tc.limit
			}
			event := &CloudEvent{ToolName: "Read", ToolInput: input}
			advisory, block := checkOrchestratorStrictGuard(event, orchestratorCtx(projectDir, false))
			cfg, _ := LoadOrchestratorConfig(filepath.Join(projectDir, ".wipnote"))
			if tc.allowed {
				if advisory != "" || block != "" || cfg.Violations != 0 {
					t.Errorf("want allowed, got advisory=%q block=%q violations=%d",
						advisory, block, cfg.Violations)
				}
				return
			}
			if cfg.Violations != 1 || block != "" {
				t.Errorf("want one counted violation, got violations=%d block=%q", cfg.Violations, block)
			}
			if !strings.Contains(advisory, "Read short files") {
				t.Errorf("advisory should carry short-read guidance, got %q", advisory)
			}
		})
	}
}

// TestFileHasAtMostLines_LargeFileIsLong pins that a file at the byte cap
// counts as long without its lines being counted.
func TestFileHasAtMostLines_LargeFileIsLong(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.txt")
	if err := os.WriteFile(path, make([]byte, orchestratorShortReadMaxBytes), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if fileHasAtMostLines(path, orchestratorShortReadLines) {
		t.Error("a file at the byte cap must count as long")
	}
}

// TestIsOrchestratorWhitelistedTool_AntigravityReadFile guards the Codex
// review finding on #209: Antigravity sends reads as read_file (with
// absolute_path), which must get the same short-read allowance as Read.
func TestIsOrchestratorWhitelistedTool_AntigravityReadFile(t *testing.T) {
	dir := t.TempDir()
	short := filepath.Join(dir, "short.go")
	long := filepath.Join(dir, "long.go")
	if err := os.WriteFile(short, []byte(strings.Repeat("x\n", 10)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(long, []byte(strings.Repeat("x\n", 500)), 0o644); err != nil {
		t.Fatal(err)
	}
	if !isOrchestratorWhitelistedTool("read_file", map[string]any{"absolute_path": short}, dir) {
		t.Error("short read_file must be allowed")
	}
	if isOrchestratorWhitelistedTool("read_file", map[string]any{"absolute_path": long}, dir) {
		t.Error("long read_file must not be allowed")
	}
	if strings.Contains(orchestratorReadGuidance, "(Explore for code)") || !strings.Contains(orchestratorReadGuidance, "explorer on Codex") {
		t.Errorf("guidance must name each harness's explorer: %q", orchestratorReadGuidance)
	}
}

// TestIsShortShellRead guards the Codex review finding on #209: Codex reads
// files through its shell tool ("Bash"), so plain short cat/head/tail/sed
// reads must count as short reads, while anything chained, redirected or long
// must not.
func TestIsShortShellRead(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "short.go"), []byte(strings.Repeat("x\n", 20)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "long.go"), []byte(strings.Repeat("x\n", 500)), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := map[string]bool{
		"cat short.go":               true,
		"cat -n short.go":            true,
		"cat long.go":                false,
		"cat missing.go":             false,
		"head long.go":               true,
		"head -n 50 long.go":         true,
		"head -n50 long.go":          true,
		"tail -40 long.go":           true,
		"head -n 500 long.go":        false,
		"tail -n +5 long.go":         false,
		"tail -n +5 short.go":        true,
		"sed -n '1,200p' long.go":    true,
		"sed -n '1,500p' long.go":    false,
		"sed -n '1,500p' short.go":   true,
		"sed -n '1w out' short.go":   false,
		"sed -i 's/x/y/' short.go":   false,
		"cat short.go | sh":          false,
		"cat short.go; rm -rf /":     false,
		"cat short.go > copy.go":     false,
		"cat $(echo short.go)":       false,
		"cat *.go":                   false,
		"cat short.go long.go":       false,
		"grep -n x short.go":         false,
		"head -n 5 short.go long.go": false,
	}
	for cmd, want := range cases {
		if got := isShortShellRead(cmd, dir); got != want {
			t.Errorf("isShortShellRead(%q) = %v, want %v", cmd, got, want)
		}
	}
	if !isOrchestratorWhitelistedTool("Bash", map[string]any{"command": "sed -n '1,80p' long.go"}, dir) {
		t.Error("a bounded Codex shell read must be whitelisted")
	}
}

// TestCheckOrchestratorStrictGuard_ReadResolvesAgainstCWD guards the Codex
// review finding on #209: in a managed worktree ProjectDir is the main
// checkout, so a relative read must resolve against the event's cwd.
func TestCheckOrchestratorStrictGuard_ReadResolvesAgainstCWD(t *testing.T) {
	t.Setenv(OrchestratorRescueEnv, "")
	worktree := t.TempDir()
	if err := os.WriteFile(filepath.Join(worktree, "f.go"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name     string
		mainBody string
		cwd      string
		wantPass bool
	}{
		// f.go is short in the worktree but long on main: the worktree copy decides.
		{"short in worktree", strings.Repeat("x\n", 500), worktree, true},
		// No cwd: fall back to the canonical project dir (long there).
		{"no cwd falls back", strings.Repeat("x\n", 500), "", false},
	} {
		mainDir := orchestratorFixture(t, OrchestratorConfig{Enabled: true, Mode: OrchestratorModeStrict, MaxViolations: 3})
		if err := os.WriteFile(filepath.Join(mainDir, "f.go"), []byte(tc.mainBody), 0o644); err != nil {
			t.Fatal(err)
		}
		event := &CloudEvent{ToolName: "Read", CWD: tc.cwd, ToolInput: map[string]any{"file_path": "f.go"}}
		checkOrchestratorStrictGuard(event, orchestratorCtx(mainDir, false))
		cfg, _ := LoadOrchestratorConfig(filepath.Join(mainDir, ".wipnote"))
		if got := cfg.Violations == 0; got != tc.wantPass {
			t.Errorf("%s: allowed=%v, want %v (violations=%d)", tc.name, got, tc.wantPass, cfg.Violations)
		}
	}
}
