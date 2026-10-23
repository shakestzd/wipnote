package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// buildTestRoot returns a minimal cobra root command that mirrors the kinds of
// commands the real CLI registers — visible, hidden, deprecated, grouped, and
// ungrouped — so tests can assert renderCompactHelp behaviour in isolation.
func findSubcommand(parent *cobra.Command, name string) *cobra.Command {
	for _, child := range parent.Commands() {
		if child.Name() == name {
			return child
		}
	}
	return nil
}

func buildTestRoot() *cobra.Command {
	root := &cobra.Command{Use: "wipnote"}

	addCmd := func(name, short string, subs ...string) *cobra.Command {
		c := &cobra.Command{Use: name, Short: short}
		for _, s := range subs {
			c.AddCommand(&cobra.Command{Use: s, Short: s + " sub"})
		}
		return c
	}

	// Register a group so group-based rendering is exercised.
	root.AddGroup(&cobra.Group{ID: "workitems", Title: "Work Items"})
	root.AddGroup(&cobra.Group{ID: "query", Title: "Query & Status"})

	feature := addCmd("feature", "Manage features", "create", "show", "list", "start", "complete", "delete")
	feature.GroupID = "workitems"
	root.AddCommand(feature)

	bug := addCmd("bug", "Bug tracking", "create", "show", "list", "start", "complete", "delete")
	bug.GroupID = "workitems"
	root.AddCommand(bug)

	status := addCmd("status", "Quick project status")
	status.GroupID = "query"
	root.AddCommand(status)

	find := addCmd("find", "Search work items")
	find.GroupID = "query"
	root.AddCommand(find)

	// Hidden command — must not appear in output.
	hidden := addCmd("serve-child", "internal child process")
	hidden.Hidden = true
	root.AddCommand(hidden)

	// Deprecated command — must not appear in output.
	deprecated := addCmd("old-cmd", "Old command")
	deprecated.Deprecated = "use new-cmd instead"
	root.AddCommand(deprecated)

	// Ungrouped visible command — omitted from compact output.
	root.AddCommand(addCmd("internal-plumbing", "internal only"))

	return root
}

func TestRenderCompactHelp_ContainsVisibleCommands(t *testing.T) {
	root := buildTestRoot()
	out := renderCompactHelp(root)

	for _, name := range []string{"feature", "bug", "status", "find"} {
		if !strings.Contains(out, name) {
			t.Errorf("expected output to contain command %q, but it did not\noutput:\n%s", name, out)
		}
	}
}

func TestRenderCompactHelp_ExcludesHiddenCommands(t *testing.T) {
	root := buildTestRoot()
	out := renderCompactHelp(root)

	if strings.Contains(out, "serve-child") {
		t.Errorf("output should NOT contain hidden command 'serve-child'\noutput:\n%s", out)
	}
}

func TestRenderCompactHelp_ExcludesDeprecatedCommands(t *testing.T) {
	root := buildTestRoot()
	out := renderCompactHelp(root)

	if strings.Contains(out, "old-cmd") {
		t.Errorf("output should NOT contain deprecated command 'old-cmd'\noutput:\n%s", out)
	}
}

func TestRenderCompactHelp_Under35Lines(t *testing.T) {
	root := buildTestRoot()
	out := renderCompactHelp(root)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) > 35 {
		t.Errorf("output exceeds 35 lines: got %d lines\noutput:\n%s", len(lines), out)
	}
}

func TestRenderCompactHelp_SubcommandsGrouped(t *testing.T) {
	root := buildTestRoot()
	out := renderCompactHelp(root)

	// feature has subcommands; they should appear in brackets on the same line.
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "feature") && strings.HasPrefix(strings.TrimSpace(line), "feature") {
			if !strings.Contains(line, "[") || !strings.Contains(line, "|") {
				t.Errorf("feature line should show grouped subcommands in [a|b|c] form, got: %q", line)
			}
			return
		}
	}
	t.Error("no line found starting with 'feature'")
}

func TestRenderCompactHelp_HasHeaderAndHint(t *testing.T) {
	root := buildTestRoot()
	out := renderCompactHelp(root)

	if !strings.Contains(out, "## CLI Quick Reference") {
		t.Error("output should contain '## CLI Quick Reference' header")
	}
	if !strings.Contains(out, "wipnote help --compact") {
		t.Error("output should contain the 'wipnote help --compact' reprint hint")
	}
}

func TestRenderCompactHelp_RealTree_Under70Lines(t *testing.T) {
	root := buildRoot()
	out := renderCompactHelp(root)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) > 70 {
		t.Errorf("real CLI output exceeds 70 lines: got %d lines\noutput:\n%s", len(lines), out)
	}
}

func TestRenderCompactHelp_RealTree_ContainsExpectedCommands(t *testing.T) {
	root := buildRoot()
	out := renderCompactHelp(root)

	mustContain := []string{
		"feature", "bug", "spike", "track", "plan",
		"status", "snapshot", "find", "yolo", "upgrade",
	}
	for _, name := range mustContain {
		if !strings.Contains(out, name) {
			t.Errorf("real CLI output should contain %q\noutput:\n%s", name, out)
		}
	}
}

// TestRenderCompactHelp_GroupsFromMetadata verifies that renderCompactHelp
// uses cobra Group metadata — not any hand-maintained list — to decide which
// commands appear and in which section.
func TestRenderCompactHelp_GroupsFromMetadata(t *testing.T) {
	root := &cobra.Command{Use: "wipnote"}
	root.AddGroup(&cobra.Group{ID: "alpha", Title: "Alpha Group"})
	root.AddGroup(&cobra.Group{ID: "beta", Title: "Beta Group"})

	cmdA := &cobra.Command{Use: "aardvark", Short: "first alpha cmd"}
	cmdA.GroupID = "alpha"
	root.AddCommand(cmdA)

	cmdB := &cobra.Command{Use: "zebra", Short: "second alpha cmd"}
	cmdB.GroupID = "alpha"
	root.AddCommand(cmdB)

	cmdC := &cobra.Command{Use: "mango", Short: "only beta cmd"}
	cmdC.GroupID = "beta"
	root.AddCommand(cmdC)

	// Ungrouped command should be omitted.
	cmdD := &cobra.Command{Use: "plumbing", Short: "internal only"}
	root.AddCommand(cmdD)

	out := renderCompactHelp(root)

	// Group headers must appear.
	if !strings.Contains(out, "Alpha Group") {
		t.Errorf("expected 'Alpha Group' header in output\noutput:\n%s", out)
	}
	if !strings.Contains(out, "Beta Group") {
		t.Errorf("expected 'Beta Group' header in output\noutput:\n%s", out)
	}

	// Alpha group commands must appear.
	if !strings.Contains(out, "aardvark") {
		t.Errorf("expected 'aardvark' in output\noutput:\n%s", out)
	}
	if !strings.Contains(out, "zebra") {
		t.Errorf("expected 'zebra' in output\noutput:\n%s", out)
	}

	// Beta group command must appear.
	if !strings.Contains(out, "mango") {
		t.Errorf("expected 'mango' in output\noutput:\n%s", out)
	}

	// Ungrouped command must NOT appear.
	if strings.Contains(out, "plumbing") {
		t.Errorf("ungrouped command 'plumbing' should NOT appear in output\noutput:\n%s", out)
	}

	// Alpha group header must come before Beta group header (registration order).
	alphaIdx := strings.Index(out, "Alpha Group")
	betaIdx := strings.Index(out, "Beta Group")
	if alphaIdx >= betaIdx {
		t.Errorf("Alpha Group should appear before Beta Group in output\noutput:\n%s", out)
	}

	// Within Alpha group, aardvark must come before zebra (alphabetical).
	aardvarkIdx := strings.Index(out, "aardvark")
	zebraIdx := strings.Index(out, "zebra")
	if aardvarkIdx >= zebraIdx {
		t.Errorf("'aardvark' should appear before 'zebra' (alphabetical sort within group)\noutput:\n%s", out)
	}
}

// TestBuildRoot_RegistersAllCommands is a sanity check that buildRoot() returns
// a non-nil root with at least one command registered.
func TestBuildRoot_RegistersAllCommands(t *testing.T) {
	root := buildRoot()
	if root == nil {
		t.Fatal("buildRoot() returned nil")
	}
	cmds := root.Commands()
	if len(cmds) == 0 {
		t.Fatal("buildRoot() registered no commands")
	}
}

// internalPlumbingAllowlist lists command names that are intentionally left
// ungrouped (they are internal plumbing not meant for agent/user consumption).
// Any command NOT on this list and NOT hidden/deprecated MUST have a GroupID
// assigned, or TestBuildRoot_AllVisibleCommandsHaveGroup will catch the drift.
var internalPlumbingAllowlist = map[string]bool{
	"version":       true,
	"statusline":    true,
	"serve-child":   true,
	"hook":          true,
	"claude":        true,
	"codex":         true,
	"antigravity":   true,
	"orchestrator":  true,
	"install-hooks": true,
	"report":        true,
	"budget":        true,
	"ci":            true,
	"help":          true,
	"claim":         true,
	"purge-spikes":  true,
	"trace":         true,
	"graph":         true,
	"query":         true,
	"pricing":       true, // OTel model-pricing maintenance subcommand
	// Less frequently used — omitted from compact output to stay within line budget.
	"dev":         true,
	"plugin":      true,
	"projects":    true,
	"init":        true,
	"setup":       true,
	"setup-cli":   true,
	"harness":     true, // debugging/inspection tool for harness registry
	"shell-alias": true, // opt-in alias snippet printer; not a routine workflow command
}

// TestBuildRoot_AllVisibleCommandsHaveGroup is the drift guard: if someone adds
// a new top-level command to buildRoot() and forgets to assign it to a group,
// this test fails immediately rather than silently omitting the command from
// compact help.
func TestBuildRoot_AllVisibleCommandsHaveGroup(t *testing.T) {
	root := buildRoot()
	for _, c := range root.Commands() {
		if c.Hidden || c.Deprecated != "" {
			continue
		}
		if internalPlumbingAllowlist[c.Name()] {
			continue
		}
		if c.GroupID == "" {
			t.Errorf("command %q is visible and not in the plumbing allowlist, but has no GroupID — assign it to a group in buildRoot()", c.Name())
		}
	}
}
