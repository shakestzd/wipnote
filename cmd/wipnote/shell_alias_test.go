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
	if got := shellRCPath("/h", "/bin/zsh", "darwin"); got != "/h/.zshrc" {
		t.Errorf("zsh: got %q", got)
	}
	if got := shellRCPath("/h", "/usr/bin/bash", "linux"); got != "/h/.bashrc" {
		t.Errorf("bash on linux: got %q", got)
	}
	if got := shellRCPath("/h", "/bin/bash", "darwin"); got != "/h/.bash_profile" {
		t.Errorf("bash on macOS: got %q", got)
	}
	if got := shellRCPath("/h", "", "linux"); got != "/h/.bashrc" {
		t.Errorf("unset: got %q", got)
	}
}

// A macOS bash login shell reads only the first startup file that exists, so
// the block joins that one instead of creating a ~/.bash_profile that would
// hide it.
func TestShellRCPathKeepsTheMacOSLoginFileInUse(t *testing.T) {
	home := t.TempDir()
	write := func(name string) {
		if err := os.WriteFile(filepath.Join(home, name), []byte("export PATH=x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".profile")
	if got := shellRCPath(home, "/bin/bash", "darwin"); got != filepath.Join(home, ".profile") {
		t.Errorf("only ~/.profile: got %q", got)
	}
	write(".bash_login")
	if got := shellRCPath(home, "/bin/bash", "darwin"); got != filepath.Join(home, ".bash_login") {
		t.Errorf("~/.bash_login and ~/.profile: got %q", got)
	}
	write(".bash_profile")
	if got := shellRCPath(home, "/bin/bash", "darwin"); got != filepath.Join(home, ".bash_profile") {
		t.Errorf("all three: got %q", got)
	}
}

func runShellAlias(t *testing.T, args ...string) string {
	t.Helper()
	cmd := shellAliasCmd()
	cmd.SetArgs(args)
	var out strings.Builder
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("shell-alias %v: %v", args, err)
	}
	return out.String()
}

func TestShellAliasInstallWritesOneManagedBlock(t *testing.T) {
	rc := filepath.Join(t.TempDir(), ".zshrc")
	if err := os.WriteFile(rc, []byte("export X=1\nexport Y=2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runShellAlias(t, "--short", "--install", "--rc", rc)
	if out := runShellAlias(t, "--short", "--install", "--rc", rc); !strings.Contains(out, "nothing changed") {
		t.Errorf("a second install must change nothing. Said: %s", out)
	}
	data, _ := os.ReadFile(rc)
	want := "export X=1\nexport Y=2\n\n" + aliasBlock(true)
	if string(data) != want {
		t.Errorf("rc file:\n%s\nwant:\n%s", data, want)
	}
	if !strings.Contains(string(data), `eval "$(wipnote shell-alias --short)"`) {
		t.Errorf("the block must load the short aliases from wipnote. Got:\n%s", data)
	}
	if info, _ := os.Stat(rc); info.Mode().Perm() != 0o600 {
		t.Errorf("the rc file's mode must be kept, got %v", info.Mode().Perm())
	}

	// Installing the other set replaces the block in place.
	if err := os.WriteFile(rc, append(data, []byte("alias ll='ls -l'\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	runShellAlias(t, "--install", "--rc", rc)
	data, _ = os.ReadFile(rc)
	want = "export X=1\nexport Y=2\n\n" + aliasBlock(false) + "alias ll='ls -l'\n"
	if string(data) != want {
		t.Errorf("after replacing:\n%s\nwant:\n%s", data, want)
	}

	runShellAlias(t, "--uninstall", "--rc", rc)
	data, _ = os.ReadFile(rc)
	if string(data) != "export X=1\nexport Y=2\nalias ll='ls -l'\n" {
		t.Errorf("after uninstall:\n%q", data)
	}
	if out := runShellAlias(t, "--uninstall", "--rc", rc); !strings.Contains(out, "nothing removed") {
		t.Errorf("a second uninstall must remove nothing. Said: %s", out)
	}
}

func TestShellAliasInstallCreatesTheRCFile(t *testing.T) {
	rc := filepath.Join(t.TempDir(), ".bashrc")
	runShellAlias(t, "--short", "--install", "--rc", rc)
	data, err := os.ReadFile(rc)
	if err != nil || string(data) != aliasBlock(true) {
		t.Errorf("new rc file = %q, %v", data, err)
	}
}

func TestShellAliasInstallRefusesAnOpenBlock(t *testing.T) {
	rc := filepath.Join(t.TempDir(), ".zshrc")
	if err := os.WriteFile(rc, []byte(aliasBlockBegin+"\nwhatever\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := shellAliasCmd()
	cmd.SetArgs([]string{"--install", "--rc", rc})
	cmd.SetOut(&strings.Builder{})
	cmd.SetErr(&strings.Builder{})
	if err := cmd.Execute(); err == nil {
		t.Error("a begin marker with no end marker must be refused, not overwritten")
	}
}
