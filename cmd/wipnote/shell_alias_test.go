package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShellAliasSnippet_ContainsExpectedAliases(t *testing.T) {
	out := shellAliasSnippet()
	for _, want := range []string{
		"alias claude='wipnote claude'",
		"alias codex='wipnote codex'",
		"alias agy='wipnote antigravity'",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("shellAliasSnippet missing %q. Got:\n%s", want, out)
		}
	}
}

func TestShellAliasCmd_Help(t *testing.T) {
	cmd := shellAliasCmd()
	if cmd.Use != "shell-alias" {
		t.Errorf("Use = %q, want %q", cmd.Use, "shell-alias")
	}
	if cmd.Short == "" {
		t.Error("Short is empty")
	}
}

func TestShortAliasSnippet_NamesTheLaunchers(t *testing.T) {
	out := shortAliasSnippet()
	for _, want := range []string{
		"alias wcc='wipnote claude'",
		"alias wcx='wipnote codex'",
		"alias wag='wipnote antigravity'",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("shortAliasSnippet missing %q. Got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "alias claude=") {
		t.Errorf("--short must leave the bare commands alone. Got:\n%s", out)
	}
}

func TestShellRCPath(t *testing.T) {
	if got := shellRCPath("/h", "/bin/zsh"); got != "/h/.zshrc" {
		t.Errorf("zsh: got %q", got)
	}
	if got := shellRCPath("/h", "/usr/bin/bash"); got != "/h/.bashrc" {
		t.Errorf("bash: got %q", got)
	}
	if got := shellRCPath("/h", ""); got != "/h/.bashrc" {
		t.Errorf("unset: got %q", got)
	}
}

func TestShellAliasInstallAppendsOnce(t *testing.T) {
	rc := filepath.Join(t.TempDir(), ".zshrc")
	if err := os.WriteFile(rc, []byte("export X=1"), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		cmd := shellAliasCmd()
		cmd.SetArgs([]string{"--short", "--install", "--rc", rc})
		cmd.SetOut(&strings.Builder{})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	data, err := os.ReadFile(rc)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if !strings.HasPrefix(got, "export X=1\n\n# wipnote shell-alias --short") {
		t.Errorf("the aliases must follow the existing text after a blank line. Got:\n%s", got)
	}
	if n := strings.Count(got, "alias wcc='wipnote claude'"); n != 1 {
		t.Errorf("wcc alias written %d times, want 1. Got:\n%s", n, got)
	}
	// The other snippet has its own marker, so it still goes in.
	if added, err := appendAliasesOnce(rc, shellAliasSnippet()); err != nil || !added {
		t.Errorf("the default snippet beside the short one: added=%v err=%v", added, err)
	}
}
