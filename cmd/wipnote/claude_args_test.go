package main

import (
	"os"
	"runtime"
	"slices"
	"testing"
)

// TestBuildClaudeArgsAutoModeOmitsRemovedFlag pins that auto mode is driven by
// --permission-mode auto only. --enable-auto-mode was removed upstream in
// Claude Code 2.1.111 and must not be emitted.
func TestBuildClaudeArgsAutoModeOmitsRemovedFlag(t *testing.T) {
	args := buildClaudeArgs(LaunchOpts{PermissionMode: autoPermissionMode(true)}, "")
	if slices.Contains(args, "--enable-auto-mode") {
		t.Errorf("--enable-auto-mode must not be passed: %v", args)
	}
	want := []string{"--permission-mode", "auto"}
	if !slices.Equal(args, want) {
		t.Errorf("args = %v, want %v", args, want)
	}
}

// TestBuildClaudeArgsSystemPromptFile pins that the prompt is passed by path
// via --append-system-prompt-file (large prompts fail as argv), never as
// inline --append-system-prompt text, and that --resume handling is unchanged.
func TestBuildClaudeArgsSystemPromptFile(t *testing.T) {
	args := buildClaudeArgs(LaunchOpts{ResumeID: "sid", ExtraArgs: []string{"--x"}}, "/tmp/p.md")
	want := []string{"--resume", "sid", "--append-system-prompt-file", "/tmp/p.md", "--x"}
	if !slices.Equal(args, want) {
		t.Errorf("args = %v, want %v", args, want)
	}
	if slices.Contains(args, "--append-system-prompt") {
		t.Errorf("inline --append-system-prompt must not be passed: %v", args)
	}
	if args := buildClaudeArgs(LaunchOpts{}, ""); len(args) != 0 {
		t.Errorf("no prompt file must add no flag: %v", args)
	}
}

func TestWriteClaudeSystemPromptFile(t *testing.T) {
	const prompt = "orchestrator prompt\nwith 'quotes' and $VARS\n"
	path, err := writeClaudeSystemPromptFile(prompt)
	if err != nil {
		t.Fatalf("writeClaudeSystemPromptFile: %v", err)
	}
	t.Cleanup(func() { os.Remove(path) })
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read prompt file: %v", err)
	}
	if string(data) != prompt {
		t.Errorf("prompt file content = %q, want %q", data, prompt)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat prompt file: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("prompt file mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestBuildClaudeArgsOrder(t *testing.T) {
	args := buildClaudeArgs(LaunchOpts{
		ResumeID:  "abc",
		Resume:    true,
		PluginDir: "/p",
		Name:      "n",
		ExtraArgs: []string{"--x"},
	}, "")
	want := []string{"--resume", "abc", "--plugin-dir", "/p", "--name", "n", "--x"}
	if !slices.Equal(args, want) {
		t.Errorf("args = %v, want %v", args, want)
	}
}
