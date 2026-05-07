package main

import (
	"strings"
	"testing"
)

// TestAppendOrReplaceEnv_AppendsNew verifies that a key not present in env is appended.
func TestAppendOrReplaceEnv_AppendsNew(t *testing.T) {
	env := []string{"FOO=bar", "BAZ=qux"}
	got := appendOrReplaceEnv(env, "NEW_KEY=new_value")
	found := false
	for _, kv := range got {
		if kv == "NEW_KEY=new_value" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected NEW_KEY=new_value to be appended; got %v", got)
	}
	// Original keys must still be present.
	for _, orig := range env {
		present := false
		for _, kv := range got {
			if kv == orig {
				present = true
				break
			}
		}
		if !present {
			t.Errorf("original key %q was lost; got %v", orig, got)
		}
	}
}

// TestAppendOrReplaceEnv_OverridesExisting verifies that an existing key is replaced in-place.
func TestAppendOrReplaceEnv_OverridesExisting(t *testing.T) {
	env := []string{"FOO=old", "BAR=keep"}
	got := appendOrReplaceEnv(env, "FOO=new")

	// FOO must be overridden.
	fooCount := 0
	for _, kv := range got {
		if kv == "FOO=old" {
			t.Errorf("stale FOO=old still present in %v", got)
		}
		if kv == "FOO=new" {
			fooCount++
		}
	}
	if fooCount != 1 {
		t.Errorf("expected exactly one FOO=new entry, got %d in %v", fooCount, got)
	}

	// BAR must be unchanged.
	barFound := false
	for _, kv := range got {
		if kv == "BAR=keep" {
			barFound = true
		}
	}
	if !barFound {
		t.Errorf("BAR=keep was lost; got %v", got)
	}
}

// TestAppendOrReplaceEnv_Multiple verifies that multiple kv pairs are all applied.
func TestAppendOrReplaceEnv_Multiple(t *testing.T) {
	env := []string{"EXISTING=old"}
	got := appendOrReplaceEnv(env, "EXISTING=new", "BRAND=fresh")

	existingNew := false
	brandFresh := false
	for _, kv := range got {
		if kv == "EXISTING=new" {
			existingNew = true
		}
		if kv == "BRAND=fresh" {
			brandFresh = true
		}
		if kv == "EXISTING=old" {
			t.Errorf("stale EXISTING=old still present in %v", got)
		}
	}
	if !existingNew {
		t.Errorf("EXISTING=new not found in %v", got)
	}
	if !brandFresh {
		t.Errorf("BRAND=fresh not found in %v", got)
	}
}

// TestAppendOrReplaceEnv_Empty verifies that empty input env is handled correctly.
func TestAppendOrReplaceEnv_Empty(t *testing.T) {
	got := appendOrReplaceEnv(nil, "KEY=val")
	if len(got) != 1 || got[0] != "KEY=val" {
		t.Errorf("expected [KEY=val], got %v", got)
	}

	got2 := appendOrReplaceEnv([]string{}, "A=1", "B=2")
	if len(got2) != 2 {
		t.Errorf("expected 2 entries, got %v", got2)
	}
}

func TestBuildCodexAgentEnv_SetsCodexIdentity(t *testing.T) {
	got := buildCodexAgentEnv([]string{
		"WIPNOTE_AGENT_ID=previous",
		"WIPNOTE_AGENT_TYPE=previous",
		"KEEP=yes",
	})
	for _, want := range []string{
		"WIPNOTE_AGENT_ID=codex",
		"WIPNOTE_AGENT_TYPE=codex",
		"KEEP=yes",
	} {
		if indexOf(got, want) < 0 {
			t.Fatalf("expected %q in env %v", want, got)
		}
	}
	if indexOf(got, "WIPNOTE_AGENT_ID=previous") >= 0 || indexOf(got, "WIPNOTE_AGENT_TYPE=previous") >= 0 {
		t.Fatalf("stale Codex identity remained in env %v", got)
	}
}

func TestBuildCodexOtelConfigArgs_DisabledWithoutPort(t *testing.T) {
	if got := buildCodexOtelConfigArgs(0); len(got) != 0 {
		t.Fatalf("expected no config args without collector port, got %v", got)
	}
}

func TestBuildCodexOtelConfigArgs_ConfiguresLogsAndTraces(t *testing.T) {
	got := buildCodexOtelConfigArgs(43189)
	joined := ""
	for _, arg := range got {
		joined += arg + "\n"
	}
	for _, want := range []string{
		"otel.log_user_prompt=true",
		`otel.exporter={ otlp-http = { endpoint = "http://127.0.0.1:43189/v1/logs", protocol = "binary" } }`,
		`otel.trace_exporter={ otlp-http = { endpoint = "http://127.0.0.1:43189/v1/traces", protocol = "binary" } }`,
	} {
		if !containsLine(joined, want) {
			t.Errorf("Codex OTel config args missing %q in %v", want, got)
		}
	}
	for i := 0; i < len(got); i += 2 {
		if got[i] != "-c" {
			t.Fatalf("expected every config override to be prefixed by -c, got %v", got)
		}
	}
}

func TestBuildCodexArgs_PutsYoloBeforeResume(t *testing.T) {
	got := buildCodexArgs(codexLaunchOpts{
		ResumeLast: true,
		Yolo:       true,
	}, 0)

	yoloIdx := indexOf(got, "--dangerously-bypass-approvals-and-sandbox")
	resumeIdx := indexOf(got, "resume")
	if yoloIdx < 0 {
		t.Fatalf("expected Codex bypass flag in %v", got)
	}
	if resumeIdx < 0 {
		t.Fatalf("expected resume subcommand in %v", got)
	}
	if yoloIdx > resumeIdx {
		t.Fatalf("expected bypass flag before resume subcommand, got %v", got)
	}
}

func TestBuildCodexArgs_PutsOtelConfigBeforeResume(t *testing.T) {
	got := buildCodexArgs(codexLaunchOpts{
		ResumeLast: true,
		ExtraArgs:  []string{"--sandbox", "workspace-write"},
	}, 43189)

	resumeIdx := indexOf(got, "resume")
	if resumeIdx < 0 {
		t.Fatalf("expected resume subcommand in %v", got)
	}
	for i := 0; i < resumeIdx; i += 2 {
		if got[i] != "-c" {
			t.Fatalf("expected config overrides before resume, got %v", got)
		}
	}
	if !containsLine(strings.Join(got[:resumeIdx], "\n"), "otel.log_user_prompt=true") {
		t.Fatalf("expected OTel config before resume, got %v", got)
	}
	if got[resumeIdx+1] != "--last" {
		t.Fatalf("expected resume --last after config overrides, got %v", got)
	}
	if got[len(got)-2] != "--sandbox" || got[len(got)-1] != "workspace-write" {
		t.Fatalf("expected forwarded extra args after resume args, got %v", got)
	}
}

func containsLine(s, want string) bool {
	for _, line := range strings.Split(s, "\n") {
		if line == want {
			return true
		}
	}
	return false
}

func indexOf(values []string, want string) int {
	for i, value := range values {
		if value == want {
			return i
		}
	}
	return -1
}
