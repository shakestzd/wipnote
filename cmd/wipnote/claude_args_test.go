package main

import (
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
