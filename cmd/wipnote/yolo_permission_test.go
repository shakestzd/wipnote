package main

import (
	"strings"
	"testing"
)

// TestYoloPermissionModeFlagDefaultsToAuto pins that yolo launches in auto
// mode unless the user opts in to bypassPermissions explicitly.
func TestYoloPermissionModeFlagDefaultsToAuto(t *testing.T) {
	flag := yoloCmd().Flags().Lookup("permission-mode")
	if flag == nil {
		t.Fatal("yolo must expose --permission-mode")
	}
	if flag.DefValue != "auto" {
		t.Fatalf("--permission-mode default = %q, want auto", flag.DefValue)
	}
	for _, mode := range []string{"acceptEdits", "auto", "bypassPermissions", "default", "manual", "dontAsk", "plan"} {
		if !strings.Contains(flag.Usage, mode) {
			t.Errorf("--permission-mode help should list %q, got %q", mode, flag.Usage)
		}
	}
}

func TestValidateYoloPermissionMode(t *testing.T) {
	for _, mode := range yoloPermissionModes {
		if err := validateYoloPermissionMode(mode); err != nil {
			t.Errorf("%s: unexpected error %v", mode, err)
		}
	}
	// "default" is the canonical key in the Claude Code permissions docs (Codex review, #209).
	if err := validateYoloPermissionMode("default"); err != nil {
		t.Errorf("default: unexpected error %v", err)
	}
	for _, mode := range []string{"", "bypass", "Default", "Auto"} {
		if err := validateYoloPermissionMode(mode); err == nil {
			t.Errorf("%q: want an error", mode)
		}
	}
}

func TestYoloPermissionWarning(t *testing.T) {
	warn := yoloPermissionWarning("auto")
	for _, want := range []string{"manual mode", "--permission-mode bypassPermissions"} {
		if !strings.Contains(warn, want) {
			t.Errorf("auto warning should mention %q, got %q", want, warn)
		}
	}
	if strings.Contains(warn, "\n") {
		t.Errorf("auto warning must be one line, got %q", warn)
	}
	if got := yoloPermissionWarning("bypassPermissions"); got != "" {
		t.Errorf("bypassPermissions should carry no warning, got %q", got)
	}
}

func TestJoinYoloWarnings(t *testing.T) {
	if got := joinYoloWarnings("", ""); got != "" {
		t.Errorf("empty warnings joined to %q", got)
	}
	if got := joinYoloWarnings("a", "", "b"); got != "a b" {
		t.Errorf("joinYoloWarnings = %q, want %q", got, "a b")
	}
}

// TestYoloLaunchEnvMarksYolo pins that yolo sessions announce themselves to
// hooks via WIPNOTE_YOLO=1, since an auto-mode session never reports
// permission_mode=bypassPermissions and the guardrails would otherwise stay off.
func TestYoloLaunchEnvMarksYolo(t *testing.T) {
	env := yoloLaunchEnv()
	if len(env) != 1 || env[0] != "WIPNOTE_YOLO=1" {
		t.Fatalf("yoloLaunchEnv = %v, want [WIPNOTE_YOLO=1]", env)
	}
}

// TestBuildYoloSystemPrompt_AcknowledgesAutoFallback guards the Codex review
// finding on #209: yolo defaults to auto mode, which can fall back to manual,
// so the injected prompt must not claim permission prompts are disabled.
func TestBuildYoloSystemPrompt_AcknowledgesAutoFallback(t *testing.T) {
	prompt := buildYoloSystemPrompt("", "")
	if strings.Contains(prompt, "permission prompts disabled") || strings.Contains(prompt, "YOLO removes prompts") {
		t.Fatal("yolo prompt must not claim prompts are disabled: auto mode can fall back to manual")
	}
	if !strings.Contains(prompt, "falls back to asking") {
		t.Fatal("yolo prompt must tell the agent a permission prompt can still appear")
	}
}
