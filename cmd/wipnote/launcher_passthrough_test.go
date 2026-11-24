package main

import (
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
