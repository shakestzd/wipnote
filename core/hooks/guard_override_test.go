package hooks

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPreToolUse_GuardsOff_RecordsOverrideAndAllows verifies the kill switch
// still allows the call but is no longer silent: a stderr warning is written
// and a GuardOverride check_point agent_event is recorded (GH-#164).
func TestPreToolUse_GuardsOff_RecordsOverrideAndAllows(t *testing.T) {
	clearNestedEnv(t)
	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, ".wipnote"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WIPNOTE_PROJECT_DIR", projectDir)
	t.Setenv(guardOverrideEnvVar, "1")

	var stderr bytes.Buffer
	prev := guardOverrideStderr
	guardOverrideStderr = &stderr
	t.Cleanup(func() { guardOverrideStderr = prev })

	event := &CloudEvent{
		AgentID:   "claude-code",
		SessionID: "test-sess",
		CWD:       projectDir,
		ToolName:  "Bash",
		ToolInput: map[string]any{"command": "rm -rf .wipnote/features/feat-abc.html"},
		ToolUseID: "guards-off-call",
	}
	result, err := PreToolUse(event)
	if err != nil {
		t.Fatalf("PreToolUse: %v", err)
	}
	if result == nil || result.Decision == "block" {
		t.Fatalf("kill switch must allow the call, got %+v", result)
	}
	if !strings.Contains(stderr.String(), guardOverrideEnvVar) {
		t.Errorf("expected stderr warning naming %s, got %q", guardOverrideEnvVar, stderr.String())
	}

	events, _ := readCanonicalToolEvents(projectDir, "test-sess")
	var overrides []string
	for _, ev := range events {
		if ev.Tool == guardOverrideToolName {
			overrides = append(overrides, ev.Summary)
		}
	}
	if len(overrides) != 1 || !strings.Contains(overrides[0], "for Bash") {
		t.Errorf("expected one GuardOverride tool-log line naming Bash, got %q", overrides)
	}
}

// TestPreToolUse_GuardsOff_NilDB_DoesNotPanic verifies the override path
// degrades to stderr + debug log when no database is available.
func TestPreToolUse_GuardsOff_NilDB_DoesNotPanic(t *testing.T) {
	t.Setenv(guardOverrideEnvVar, "1")
	var stderr bytes.Buffer
	prev := guardOverrideStderr
	guardOverrideStderr = &stderr
	t.Cleanup(func() { guardOverrideStderr = prev })

	event := &CloudEvent{
		SessionID: "nil-db-sess",
		CWD:       t.TempDir(),
		ToolName:  "Write",
		ToolInput: map[string]any{"file_path": ".wipnote/features/feat-abc.html"},
	}
	result, err := PreToolUse(event)
	if err != nil || result == nil || result.Decision == "block" {
		t.Fatalf("expected allow with nil DB, got result=%+v err=%v", result, err)
	}
	if !strings.Contains(stderr.String(), "guards are disabled for Write") {
		t.Errorf("unexpected stderr: %q", stderr.String())
	}
}

// TestGuardBlockMessages_DoNotAdvertiseOverride pins the agent-visible block
// messages of the research guards: they explain the block and its remedy but
// never name the operator-only kill switch.
func TestGuardBlockMessages_DoNotAdvertiseOverride(t *testing.T) {
	root := "/proj"
	messages := map[string]string{
		"external tech":         checkExternalTechResearchGuard("Write", false, filepath.Join(root, "go.mod"), root),
		"harness contract":      checkHarnessContractResearchGuard("Write", false, filepath.Join(root, "plugin/agents/coder.md"), root),
		"bash research":         checkYoloBashResearchGuard(&CloudEvent{ToolName: "Bash", ToolInput: map[string]any{"command": "sed -i 's/a/b/' main.go"}}, true, false),
		"bash research relmove": checkYoloBashResearchGuard(&CloudEvent{ToolName: "Bash", ToolInput: map[string]any{"command": "cp x notes.html"}}, true, false),
	}
	for name, msg := range messages {
		if msg == "" {
			t.Errorf("%s: expected a block message", name)
			continue
		}
		if strings.Contains(msg, "GUARDS_OFF") {
			t.Errorf("%s: block message advertises the kill switch: %q", name, msg)
		}
	}
}

// TestNoStringLiteralAdvertisesGuardOverride scans every non-test source file
// of this package and fails if any string literal outside guard_override.go
// mentions the kill switch — the only place it may be spelled out is the
// override recorder itself (which writes to stderr/log, not to the agent).
func TestNoStringLiteralAdvertisesGuardOverride(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") || file == "guard_override.go" {
			continue
		}
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(fset, file, src, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if ok && lit.Kind == token.STRING && strings.Contains(lit.Value, "GUARDS_OFF") {
				t.Errorf("%s: string literal names the kill switch: %s", fset.Position(lit.Pos()), lit.Value)
			}
			return true
		})
	}
}
