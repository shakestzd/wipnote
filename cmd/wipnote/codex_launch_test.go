package main

import (
	"os"
	"path/filepath"
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

func TestBuildCodexOtelConfigArgs_ConfiguresLogsTracesAndMetrics(t *testing.T) {
	got := buildCodexOtelConfigArgs(43189)
	joined := ""
	for _, arg := range got {
		joined += arg + "\n"
	}
	for _, want := range []string{
		"otel.log_user_prompt=true",
		`otel.exporter={ otlp-http = { endpoint = "http://127.0.0.1:43189/v1/logs", protocol = "http/protobuf" } }`,
		`otel.trace_exporter={ otlp-http = { endpoint = "http://127.0.0.1:43189/v1/traces", protocol = "http/protobuf" } }`,
		`otel.metrics_exporter={ otlp-http = { endpoint = "http://127.0.0.1:43189/v1/metrics", protocol = "http/protobuf" } }`,
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
	}, 0, nil)

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
	}, 43189, nil)

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

func TestBuildCodexArgs_IncludesInstructionOverrideBeforeResume(t *testing.T) {
	got := buildCodexArgs(codexLaunchOpts{
		ResumeLast: true,
	}, 0, []string{"-c", `model_instructions_file="/tmp/wipnote-codex.md"`})

	resumeIdx := indexOf(got, "resume")
	if resumeIdx < 0 {
		t.Fatalf("expected resume subcommand in %v", got)
	}
	if got[0] != "-c" || got[1] != `model_instructions_file="/tmp/wipnote-codex.md"` {
		t.Fatalf("expected instruction override before resume, got %v", got)
	}
	if resumeIdx < 2 {
		t.Fatalf("expected resume after instruction override, got %v", got)
	}
}

func TestBuildCodexArgs_PutsWritableRootsBeforeResume(t *testing.T) {
	got := buildCodexArgs(codexLaunchOpts{
		ResumeLast:    true,
		WritableRoots: []string{"/tmp/wipnote-cache"},
		ExtraArgs:     []string{"--sandbox", "workspace-write"},
	}, 0, nil)

	addDirIdx := indexOf(got, "--add-dir")
	resumeIdx := indexOf(got, "resume")
	if addDirIdx < 0 {
		t.Fatalf("expected --add-dir in %v", got)
	}
	if addDirIdx+1 >= len(got) || got[addDirIdx+1] != "/tmp/wipnote-cache" {
		t.Fatalf("expected writable root after --add-dir, got %v", got)
	}
	if resumeIdx < 0 {
		t.Fatalf("expected resume subcommand in %v", got)
	}
	if addDirIdx > resumeIdx {
		t.Fatalf("expected --add-dir before resume subcommand, got %v", got)
	}
	if got[len(got)-2] != "--sandbox" || got[len(got)-1] != "workspace-write" {
		t.Fatalf("expected forwarded extra args after resume args, got %v", got)
	}
}

func TestPrepareCodexWritableDBCreatesParent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cache", "wipnote.db")
	t.Setenv("WIPNOTE_DB_PATH", dbPath)

	gotPath, gotDir, err := prepareCodexWritableDB(t.TempDir())
	if err != nil {
		t.Fatalf("prepareCodexWritableDB: %v", err)
	}
	if gotPath != dbPath {
		t.Fatalf("db path = %q, want %q", gotPath, dbPath)
	}
	if gotDir != filepath.Dir(dbPath) {
		t.Fatalf("db dir = %q, want %q", gotDir, filepath.Dir(dbPath))
	}
	if info, err := os.Stat(gotDir); err != nil {
		t.Fatalf("expected db parent to exist: %v", err)
	} else if !info.IsDir() {
		t.Fatalf("expected db parent to be a directory")
	}
}

func TestAppendUniqueCodexWritableRootDedupesCleanPaths(t *testing.T) {
	got := appendUniqueCodexWritableRoot([]string{"/tmp/wipnote-cache"}, "/tmp/wipnote-cache/.")
	if len(got) != 1 {
		t.Fatalf("expected duplicate clean path to be ignored, got %v", got)
	}
	got = appendUniqueCodexWritableRoot(got, "/tmp/other-cache")
	if len(got) != 2 || got[1] != "/tmp/other-cache" {
		t.Fatalf("expected distinct root to be appended, got %v", got)
	}
}

func TestCodexRequestedModel(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "short", args: []string{"-m", "gpt-5.4"}, want: "gpt-5.4"},
		{name: "long", args: []string{"--model", "gpt-5.5"}, want: "gpt-5.5"},
		{name: "equals", args: []string{"--model=gpt-5.3-codex"}, want: "gpt-5.3-codex"},
		{name: "absent", args: []string{"--sandbox", "workspace-write"}, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := codexRequestedModel(tt.args); got != tt.want {
				t.Fatalf("codexRequestedModel(%v) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

func TestSelectCodexBaseInstructions(t *testing.T) {
	data := []byte(`{"models":[{"slug":"gpt-a","base_instructions":"base a"},{"slug":"gpt-b","base_instructions":"base b"}]}`)

	if got, err := selectCodexBaseInstructions(data, "gpt-b"); err != nil || got != "base b" {
		t.Fatalf("select specific = %q, %v; want base b, nil", got, err)
	}
	if got, err := selectCodexBaseInstructions(data, ""); err != nil || got != "base a" {
		t.Fatalf("select default = %q, %v; want base a, nil", got, err)
	}
	if _, err := selectCodexBaseInstructions(data, "missing"); err == nil {
		t.Fatalf("expected missing model error")
	}
}

func TestWriteCodexInstructionsFileComposesBaseAndWipnotePrompt(t *testing.T) {
	path, err := writeCodexInstructionsFile("base instructions", "extra instructions", codexLaunchModeDefault)
	if err != nil {
		t.Fatalf("writeCodexInstructionsFile: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading generated instructions: %v", err)
	}
	content := string(data)
	for _, want := range []string{
		"base instructions",
		"# wipnote Orchestrator Addendum",
		"extra instructions",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("generated instructions missing %q:\n%s", want, content)
		}
	}
}

func TestCodexLaunchOptsEffectiveMode(t *testing.T) {
	tests := []struct {
		name string
		opts codexLaunchOpts
		want codexLaunchMode
	}{
		{name: "default", opts: codexLaunchOpts{}, want: codexLaunchModeDefault},
		{name: "continue", opts: codexLaunchOpts{ResumeLast: true}, want: codexLaunchModeContinue},
		{name: "dev", opts: codexLaunchOpts{Mode: codexLaunchModeDev}, want: codexLaunchModeDev},
		{name: "yolo", opts: codexLaunchOpts{Yolo: true}, want: codexLaunchModeYolo},
		{name: "yolo dev", opts: codexLaunchOpts{Mode: codexLaunchModeDev, Yolo: true}, want: codexLaunchModeYoloDev},
		{name: "yolo continue", opts: codexLaunchOpts{Mode: codexLaunchModeContinue, Yolo: true}, want: codexLaunchModeYoloCont},
		{name: "yolo resume", opts: codexLaunchOpts{ResumeLast: true, Yolo: true}, want: codexLaunchModeYoloCont},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.opts.effectiveMode(); got != tt.want {
				t.Fatalf("effectiveMode() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCodexInstructionAddendumByMode(t *testing.T) {
	tests := []struct {
		name string
		mode codexLaunchMode
		want []string
	}{
		{name: "default", mode: codexLaunchModeDefault, want: []string{"# wipnote Orchestrator"}},
		{name: "dev", mode: codexLaunchModeDev, want: []string{"# wipnote Orchestrator", "## Codex Dev Mode"}},
		{name: "continue", mode: codexLaunchModeContinue, want: []string{"# wipnote Orchestrator", "## Codex Continue Mode"}},
		{name: "yolo", mode: codexLaunchModeYolo, want: []string{"# YOLO Autonomous Development Mode", "## Codex YOLO Mode"}},
		{name: "yolo dev", mode: codexLaunchModeYoloDev, want: []string{"# YOLO Autonomous Development Mode", "## Codex Dev Mode", "## Codex YOLO Mode"}},
		{name: "yolo continue", mode: codexLaunchModeYoloCont, want: []string{"# YOLO Autonomous Development Mode", "## Codex Continue Mode", "## Codex YOLO Mode"}},
		{name: "init", mode: codexLaunchModeInit, want: []string{"## Codex Init Mode"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := codexInstructionAddendum(tt.mode)
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Fatalf("addendum for %s missing %q", tt.name, want)
				}
			}
		})
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
