package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/internal/delegate/codex"
)

// threadFixture is a temp HOME and project with every part a project
// thread needs present: the binary on PATH, the plugin installed, its
// hooks having run (WIPNOTE_SESSION_ID exported) and a rule naming wipnote.
type threadFixture struct {
	home, project string
	env           map[string]string
	binary        bool
}

func newThreadFixture(t *testing.T) *threadFixture {
	t.Helper()
	unsetProjectEnv(t)
	f := &threadFixture{home: t.TempDir(), project: t.TempDir(), env: map[string]string{"WIPNOTE_SESSION_ID": "sess-thread"}, binary: true}
	installTestPlugin(t, f.home, true)
	writeTestFile(t, filepath.Join(f.project, ".claude", "rules", "tracking.md"), "Track work with wipnote.\n")
	return f
}

func (f *threadFixture) probe() projectThreadProbe {
	return projectThreadProbe{
		LookPath: func(name string) (string, error) {
			if f.binary {
				return "/opt/bin/" + name, nil
			}
			return "", errors.New("executable file not found in $PATH")
		},
		Getenv:     func(k string) string { return f.env[k] },
		Home:       f.home,
		ProjectDir: f.project,
	}
}

func unsetProjectEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"WIPNOTE_PROJECT_DIR", "CLAUDE_PROJECT_DIR"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
}

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// statusOf returns the check named name, failing the test when absent.
func statusOf(t *testing.T, checks []codex.Check, name string) codex.Check {
	t.Helper()
	for _, c := range checks {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no %q check in %+v", name, checks)
	return codex.Check{}
}

func failedNames(checks []codex.Check) []string {
	var out []string
	for _, c := range checks {
		if c.Status == codex.CheckFail {
			out = append(out, c.Name)
		}
	}
	return out
}

func TestDoctorProjectThread_AllPresent(t *testing.T) {
	f := newThreadFixture(t)
	checks := projectThreadChecks(f.probe())
	for _, name := range []string{"binary", "plugin", "hooks", "rules"} {
		if c := statusOf(t, checks, name); c.Status != codex.CheckPass {
			t.Fatalf("%s: %+v", name, c)
		}
	}
	var out bytes.Buffer
	if n := codex.WriteChecks(&out, checks); n != 0 {
		t.Fatalf("%d failed:\n%s", n, out.String())
	}
}

func TestDoctorProjectThread_NoHooksNamesFix(t *testing.T) {
	f := newThreadFixture(t)
	delete(f.env, "WIPNOTE_SESSION_ID")
	checks := projectThreadChecks(f.probe())
	if got := failedNames(checks); len(got) != 1 || got[0] != "hooks" {
		t.Fatalf("failed %v, want only hooks", got)
	}
	if d := statusOf(t, checks, "hooks").Detail; !strings.Contains(d, "fix:") || !strings.Contains(d, "Project settings > Plugins") {
		t.Fatalf("hooks detail names no fix: %q", d)
	}
	// This session's SessionStart having written .active-session for the
	// harness's own session id also counts as the hooks having run.
	writeTestFile(t, filepath.Join(f.project, ".wipnote", ".active-session"), `{"session_id": "native-1"}`)
	p := f.probe()
	p.NativeSession = "native-1"
	if c := statusOf(t, projectThreadChecks(p), "hooks"); c.Status != codex.CheckPass {
		t.Fatalf("active session for this session: %+v", c)
	}
	p.NativeSession = "another-session" // a stale file from another session does not
	if c := statusOf(t, projectThreadChecks(p), "hooks"); c.Status != codex.CheckFail {
		t.Fatalf("stale active session: %+v", c)
	}
}

func TestDoctorProjectThread_NoBinary(t *testing.T) {
	f := newThreadFixture(t)
	f.binary = false
	checks := projectThreadChecks(f.probe())
	if got := failedNames(checks); len(got) != 1 || got[0] != "binary" {
		t.Fatalf("failed %v, want only binary", got)
	}
	if d := statusOf(t, checks, "binary").Detail; !strings.Contains(d, "fix:") || !strings.Contains(d, "scripts/cloud-env-setup.sh") {
		t.Fatalf("binary detail names no fix: %q", d)
	}
}

func TestDoctorProjectThread_NoPluginNoRulesNameFixes(t *testing.T) {
	f := newThreadFixture(t)
	delete(f.env, "WIPNOTE_SESSION_ID")
	if err := os.RemoveAll(filepath.Join(f.home, ".claude")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(f.project, ".claude")); err != nil {
		t.Fatal(err)
	}
	checks := projectThreadChecks(f.probe())
	if got := strings.Join(failedNames(checks), ","); got != "plugin,hooks,rules" {
		t.Fatalf("failed %q", got)
	}
	if d := statusOf(t, checks, "plugin").Detail; !strings.Contains(d, "fix:") || !strings.Contains(d, "Project settings > Plugins") {
		t.Fatalf("plugin detail: %q", d)
	}
	if d := statusOf(t, checks, "rules").Detail; !strings.Contains(d, "fix:") || !strings.Contains(d, ".claude/rules/") {
		t.Fatalf("rules detail: %q", d)
	}
	// CLAUDE.md naming wipnote is enough for the rules part.
	writeTestFile(t, filepath.Join(f.project, "CLAUDE.md"), "# Project\nUse WIPNOTE for tracking.\n")
	if c := statusOf(t, projectThreadChecks(f.probe()), "rules"); c.Status != codex.CheckPass {
		t.Fatalf("CLAUDE.md: %+v", c)
	}
}

// The command exits non-zero (returns an error) when a part is missing, and
// zero when all are present.
func TestDoctorProjectThread_CommandExit(t *testing.T) {
	f := newThreadFixture(t)
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	t.Setenv("HOME", f.home)
	t.Setenv("WIPNOTE_SESSION_ID", "sess-thread")
	t.Chdir(f.project)
	run := func() (string, error) {
		var out bytes.Buffer
		cmd := doctorCmd()
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs([]string{"--project-thread"})
		err := cmd.Execute()
		return out.String(), err
	}
	out, err := run()
	if err == nil || !strings.Contains(out, "fail  binary") {
		t.Fatalf("no binary on PATH: err %v\n%s", err, out)
	}
	writeTestFile(t, filepath.Join(bin, "wipnote"), "#!/bin/sh\n")
	if err := os.Chmod(filepath.Join(bin, "wipnote"), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := run(); err != nil {
		t.Fatalf("all present: err %v\n%s", err, out)
	}
}

// installTestPlugin records wipnote@wipnote as Claude Code does: the files
// in the cache, a row in installed_plugins.json, and enabledPlugins in the
// user settings (only when enabled).
func installTestPlugin(t *testing.T, home string, enabled bool) {
	t.Helper()
	dir := filepath.Join(home, ".claude", "plugins", "cache", "wipnote", "wipnote", "0.84.0")
	writeTestFile(t, filepath.Join(dir, ".claude-plugin", "plugin.json"), `{"name": "wipnote"}`)
	writeTestFile(t, filepath.Join(home, ".claude", "plugins", "installed_plugins.json"),
		`{"version": 2, "plugins": {"wipnote@wipnote": [{"scope": "user", "installPath": "`+dir+`", "version": "0.84.0"}]}}`)
	if enabled {
		writeTestFile(t, filepath.Join(home, ".claude", "settings.json"), `{"enabledPlugins": {"wipnote@wipnote": true}}`)
	}
}

// The plugin part passes only for a plugin Claude Code installed and
// enabled (or synced from claude.ai and not turned off); a marketplace
// clone or a cached copy with no enabled install does not count.
func TestDoctorProjectThread_PluginMustBeInstalledAndEnabled(t *testing.T) {
	f := newThreadFixture(t)
	delete(f.env, "WIPNOTE_SESSION_ID") // no hooks, so no inference from them
	reset := func() {
		if err := os.RemoveAll(filepath.Join(f.home, ".claude")); err != nil {
			t.Fatal(err)
		}
	}
	plugin := func() codex.Check { return statusOf(t, projectThreadChecks(f.probe()), "plugin") }
	settings := filepath.Join(f.home, ".claude", "settings.json")
	cases := []struct {
		name  string
		setup func()
		want  string
	}{
		{"marketplace clone only", func() {
			writeTestFile(t, filepath.Join(f.home, ".claude", "plugins", "marketplaces", "wipnote", "plugin", ".claude-plugin", "plugin.json"), `{"name": "wipnote"}`)
		}, codex.CheckFail},
		{"cached copy, no install record", func() {
			writeTestFile(t, filepath.Join(f.home, ".claude", "plugins", "cache", "wipnote", "wipnote", "0.84.0", ".claude-plugin", "plugin.json"), `{"name": "wipnote"}`)
		}, codex.CheckFail},
		{"installed, not enabled", func() { installTestPlugin(t, f.home, false) }, codex.CheckFail},
		{"installed, disabled", func() {
			installTestPlugin(t, f.home, false)
			writeTestFile(t, settings, `{"enabledPlugins": {"wipnote@wipnote": false}}`)
		}, codex.CheckFail},
		{"installed and enabled", func() { installTestPlugin(t, f.home, true) }, codex.CheckPass},
		{"synced from claude.ai", func() {
			writeTestFile(t, filepath.Join(f.home, ".claude", "plugins", "synced", "wipnote", ".claude-plugin", "plugin.json"), `{"name": "wipnote"}`)
		}, codex.CheckPass},
		{"synced, turned off", func() {
			writeTestFile(t, filepath.Join(f.home, ".claude", "plugins", "synced", "wipnote", ".claude-plugin", "plugin.json"), `{"name": "wipnote"}`)
			writeTestFile(t, settings, `{"enabledPlugins": {"wipnote@synced": false}}`)
		}, codex.CheckFail},
	}
	for _, c := range cases {
		reset()
		c.setup()
		if got := plugin(); got.Status != c.want {
			t.Errorf("%s: %+v, want %s", c.name, got, c.want)
		}
	}
}

// Claude Code discovers .claude/rules/**/*.md recursively, so a rule in a
// subdirectory counts; a non-.md file there does not.
func TestDoctorProjectThread_RuleInSubdirectory(t *testing.T) {
	f := newThreadFixture(t)
	if err := os.RemoveAll(filepath.Join(f.project, ".claude")); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(f.project, ".claude", "rules", "tools", "notes.txt"), "wipnote\n")
	if c := statusOf(t, projectThreadChecks(f.probe()), "rules"); c.Status != codex.CheckFail {
		t.Fatalf("a .txt counted: %+v", c)
	}
	writeTestFile(t, filepath.Join(f.project, ".claude", "rules", "tools", "tracking.md"), "Use wipnote.\n")
	if c := statusOf(t, projectThreadChecks(f.probe()), "rules"); c.Status != codex.CheckPass || !strings.Contains(c.Detail, "tools") {
		t.Fatalf("nested rule: %+v", c)
	}
}
