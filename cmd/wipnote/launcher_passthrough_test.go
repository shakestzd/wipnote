package main

import (
	"bytes"
	"slices"
	"testing"
)

// Flags wipnote does not define reach Codex in their original order; wipnote's
// own flags stay in front with their values (bug-fc843d71).
func TestForwardHarnessFlags(t *testing.T) {
	root := buildRoot()
	for _, tt := range []struct {
		name     string
		in, want []string
	}{
		{"unknown bool flag", []string{"codex", "--approve-for-me"}, []string{"codex", "--", "--approve-for-me"}},
		{"unknown flag with value", []string{"codex", "--sandbox", "read-only"}, []string{"codex", "--", "--sandbox", "read-only"}},
		{"own flags stay in front", []string{"codex", "--sandbox", "read-only", "--yolo", "--work-item", "feat-1a2b3c4d", "-m", "o3"},
			[]string{"codex", "--yolo", "--work-item", "feat-1a2b3c4d", "--", "--sandbox", "read-only", "-m", "o3"}},
		{"own flag with inline value", []string{"codex", "--resume=abc", "--search"}, []string{"codex", "--resume=abc", "--", "--search"}},
		{"words after a typed -- all go to codex", []string{"codex", "--dev", "--", "--yolo", "--worktree"}, []string{"codex", "--dev", "--", "--yolo", "--worktree"}},
		{"prompt and subcommand words", []string{"codex", "resume", "--last"}, []string{"codex", "--", "resume", "--last"}},
		{"flags after a codex subcommand are codex's", []string{"codex", "--dev", "-m", "o3", "review", "--base", "main"},
			[]string{"codex", "--dev", "--", "-m", "o3", "review", "--base", "main"}},
		{"a session id after fork is not a worktree", []string{"codex", "fork", "--worktree", "abc"}, []string{"codex", "--", "fork", "--worktree", "abc"}},
		{"help after a subcommand is codex's", []string{"codex", "exec", "--help"}, []string{"codex", "--", "exec", "--help"}},
		{"a codex flag's value is not a wipnote flag", []string{"codex", "--add-dir", "--yolo", "-c", "--dev"}, []string{"codex", "--", "--add-dir", "--yolo", "-c", "--dev"}},
		{"a value named like a subcommand is a value", []string{"codex", "-m", "review", "--yolo"}, []string{"codex", "--yolo", "--", "-m", "review"}},
		{"a second -- keeps a dash prompt a prompt", []string{"codex", "--sandbox", "read-only", "--", "--", "--help me"},
			[]string{"codex", "--", "--sandbox", "read-only", "--", "--help me"}},
		{"version goes to codex", []string{"codex", "--version"}, []string{"codex", "--", "--version"}},
		{"project dir before the launcher", []string{"--project-dir", "/p", "codex", "--search", "--project-dir=/q"},
			[]string{"--project-dir", "/p", "codex", "--project-dir=/q", "--", "--search"}},
		{"help stays with wipnote", []string{"codex", "--help"}, []string{"codex", "--help"}},
		{"nothing to forward", []string{"codex", "--dev"}, []string{"codex", "--dev"}},
		{"other commands are untouched", []string{"feature", "show", "--bogus"}, []string{"feature", "show", "--bogus"}},
		{"a later word named codex is not the launcher", []string{"relevant", "codex", "--bogus"}, []string{"relevant", "codex", "--bogus"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := forwardHarnessFlags(root, tt.in); !slices.Equal(got, tt.want) {
				t.Errorf("forwardHarnessFlags(%q)\n got %q\nwant %q", tt.in, got, tt.want)
			}
		})
	}
}

// The rewritten line parses: cobra accepts it and hands the forwarded words
// to the launcher as arguments.
func TestForwardHarnessFlags_CobraAcceptsTheResult(t *testing.T) {
	root := buildRoot()
	cmd, rest, err := root.Find(forwardHarnessFlags(root, []string{"codex", "--approve-for-me", "--yolo", "--sandbox", "read-only"}))
	if err != nil || cmd.Name() != "codex" {
		t.Fatalf("find: %v %v", cmd, err)
	}
	if err := cmd.ParseFlags(rest); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got, want := cmd.Flags().Args(), []string{"--approve-for-me", "--sandbox", "read-only"}; !slices.Equal(got, want) {
		t.Errorf("forwarded args = %q, want %q", got, want)
	}
	if yolo, _ := cmd.Flags().GetBool("yolo"); !yolo {
		t.Error("--yolo was not parsed as wipnote's flag")
	}
}

// The entry point applies the rewrite. Without it cobra stops on the unknown
// flag before it reaches --help.
func TestBuildRootFor_AppliesTheRewrite(t *testing.T) {
	root := buildRootFor([]string{"codex", "--approve-for-me", "--help"})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v\n%s", err, out.String())
	}
}

// `codex --version` and a subcommand's help only print, so the launcher runs
// them with no launch marker, daemon or collector.
func TestCodexInfoOnlyLinesSkipTheLauncher(t *testing.T) {
	for _, tt := range []struct {
		args []string
		want bool
	}{
		{[]string{"--version"}, true},
		{[]string{"-V"}, true},
		{[]string{"exec", "--help"}, true},
		{[]string{"review", "-h"}, true},
		{[]string{"--search"}, false},
		{[]string{"fix it", "--version"}, false},
		{[]string{"--", "--help me"}, false},
		{nil, false},
	} {
		if got := codexInfoOnly(tt.args); got != tt.want {
			t.Errorf("codexInfoOnly(%q) = %v, want %v", tt.args, got, tt.want)
		}
	}

	var ran [][]string
	prev, prevExec := runCodexPlainFn, execCodexFn
	runCodexPlainFn = func(args []string) error { ran = append(ran, args); return nil }
	execCodexFn = func(codexLaunchOpts) error { t.Error("the full launcher ran"); return nil }
	t.Cleanup(func() { runCodexPlainFn, execCodexFn = prev, prevExec })
	if err := runCodexCmd(&codexFlags{}, []string{"--version"}); err != nil {
		t.Fatal(err)
	}
	if len(ran) != 1 || !slices.Equal(ran[0], []string{"--version"}) {
		t.Errorf("plain runs = %q", ran)
	}
}
