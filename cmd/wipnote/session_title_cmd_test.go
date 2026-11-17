package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/core/hooks"
)

// TestLaunchNameEnv_OnlyDefaultName: the hooks learn the launch name only for
// an item-less default name; any other name clears the variable.
func TestLaunchNameEnv_OnlyDefaultName(t *testing.T) {
	for _, root := range []string{"/x/wipnote", "/x/My Project", ""} {
		name := defaultSessionName(root)
		if got := launchNameEnv(name); got != hooks.LaunchNameEnv+"="+name {
			t.Errorf("default name %q: env %q", name, got)
		}
	}
	for _, name := range []string{"", "my-session", "feat-00e8112d-name-top-level", "wipnote-20261116-22.59.16 · fix"} {
		if got := launchNameEnv(name); got != hooks.LaunchNameEnv+"=" {
			t.Errorf("name %q: env %q, want it cleared", name, got)
		}
	}
}

// TestSessionTitleArgs_NoToolsNoPlugins: the title call is one turn on the
// small model with no tools; it never takes --effort (not every small model
// supports it).
func TestSessionTitleArgs_NoToolsNoPlugins(t *testing.T) {
	prev := claudeHelpText
	claudeHelpText = func(context.Context) string { return "--safe-mode --no-session-persistence" }
	t.Cleanup(func() { claudeHelpText = prev })
	args := sessionTitleArgs(context.Background(), "SYS")
	joined := strings.Join(args, " ")
	for _, want := range []string{"-p", "--model haiku", "--max-turns 1", "--system-prompt SYS"} {
		if !strings.Contains(joined, want) {
			t.Errorf("args %q lack %q", joined, want)
		}
	}
	if i := slices.Index(args, "--tools"); i < 0 || args[i+1] != "" {
		t.Errorf("args %q do not turn tools off", joined)
	}
	if slices.Contains(args, "--effort") {
		t.Errorf("args %q carry --effort", joined)
	}
}

// TestSessionTitleCmd_WritesPendingTitle: the command runs the (mocked)
// model and leaves its title for the next prompt; a failing model leaves
// nothing and the command still exits 0.
func TestSessionTitleCmd_WritesPendingTitle(t *testing.T) {
	project := t.TempDir()
	const sid = "aaaa4444-bbbb-cccc-dddd-eeee0000c001"
	dir := filepath.Join(project, ".wipnote", "sessions", sid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	pending := filepath.Join(dir, ".pending-generated-title")
	run := func(model hooks.TitleModel) error {
		_ = os.WriteFile(filepath.Join(dir, ".title-prompt"), []byte("fix the stop drafts"), 0o644)
		prev := sessionTitleModelCall
		sessionTitleModelCall = model
		defer func() { sessionTitleModelCall = prev }()
		cmd := sessionTitleCmd()
		cmd.SetArgs([]string{"--project", project, "--session", sid, "--from", "prompt"})
		return cmd.Execute()
	}
	fail := func(context.Context, string, string) (string, error) { return "", context.DeadlineExceeded }
	if err := run(fail); err != nil {
		t.Fatalf("a failed model call failed the command: %v", err)
	}
	if _, err := os.Stat(pending); err == nil {
		t.Fatal("a failed model call wrote a title")
	}
	ok := func(context.Context, string, string) (string, error) { return "Fix stop drafts", nil }
	if err := run(ok); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(pending); strings.TrimSpace(string(data)) != "Fix stop drafts" {
		t.Fatalf("pending title = %q", data)
	}
}
