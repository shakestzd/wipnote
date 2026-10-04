package hooks

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCapContext(t *testing.T) {
	if got := capContext("short", 100); got != "short" {
		t.Errorf("got %q", got)
	}
	got := capContext(strings.Repeat("é", 2000), 800)
	if n := utf8.RuneCountInString(got); n != 800 {
		t.Errorf("runes = %d, want 800", n)
	}
	if !strings.HasSuffix(got, contextTruncMarker) {
		t.Error("missing truncation marker")
	}
}

func TestClaimSessionOnce(t *testing.T) {
	dir := t.TempDir()
	if !claimSessionOnce(dir, "s1", "k") {
		t.Error("first claim must succeed")
	}
	if claimSessionOnce(dir, "s1", "k") {
		t.Error("second claim in same session must fail")
	}
	if !claimSessionOnce(dir, "s2", "k") {
		t.Error("other session must claim independently")
	}
	if !claimSessionOnce(dir, "s1", "other-key") {
		t.Error("other key must claim independently")
	}
	if !claimSessionOnce("", "s1", "k") || !claimSessionOnce(dir, "", "k") {
		t.Error("no state to key on => show")
	}
}

func TestEmitClaudeResponseCapsContext(t *testing.T) {
	big := strings.Repeat("x", 5000)
	want := map[string]int{
		"PreToolUse":   toolEventContextCap,
		"PostToolUse":  toolEventContextCap,
		"SessionStart": 5000, // under the prompt-event cap: untouched
	}
	for event, n := range want {
		var buf bytes.Buffer
		if err := emitClaudeResponseForEvent(&buf, event, &HookResult{AdditionalContext: big}); err != nil {
			t.Fatal(err)
		}
		var got struct {
			H struct {
				C string `json:"additionalContext"`
			} `json:"hookSpecificOutput"`
		}
		if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if g := utf8.RuneCountInString(got.H.C); g != n {
			t.Errorf("%s: %d runes, want %d", event, g, n)
		}
	}
}

// TestOrchestratorAdvisory_OncePerSessionStrict pins the context-budget rule:
// a session sees the advisory once, a new session sees it again, and the last
// warning before the block is never suppressed.
func TestOrchestratorAdvisory_OncePerSessionStrict(t *testing.T) {
	t.Setenv(OrchestratorRescueEnv, "")
	projectDir := orchestratorFixture(t, OrchestratorConfig{
		Enabled: true, Mode: OrchestratorModeStrict, MaxViolations: 4,
	})
	ctx := orchestratorCtx(projectDir, false)
	event := &CloudEvent{ToolName: "Skill", ToolInput: map[string]any{"skill": "x"}}

	want := []bool{true, false, true} // 1st shown, 2nd suppressed, 3rd = last warning before block at 4
	for i, w := range want {
		adv, block := checkOrchestratorStrictGuard(event, ctx)
		if block != "" {
			t.Fatalf("unexpected block on call %d", i+1)
		}
		if (adv != "") != w {
			t.Errorf("call %d: advisory shown=%v, want %v (%q)", i+1, adv != "", w, adv)
		}
	}

	other := orchestratorCtx(projectDir, false)
	other.SessionID = "another-session"
	cfg, _ := LoadOrchestratorConfig(other.HgDir)
	cfg.Violations = 0
	_ = SaveOrchestratorConfig(other.HgDir, cfg)
	if adv, _ := checkOrchestratorStrictGuard(event, other); adv == "" {
		t.Error("a new session must see the advisory once")
	}
	if n := len(orchestratorAdvisory("Skill", 1, 3)); n > 120 {
		t.Errorf("advisory text too long: %d chars", n)
	}
}
