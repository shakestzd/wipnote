package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// optInThisHost writes the user config (~/.wipnote/config.json under the
// test HOME) that lists this host in delegate.codex.dangerFullAccessHosts.
func optInThisHost(t *testing.T) {
	t.Helper()
	host, err := os.Hostname()
	if err != nil || host == "" {
		t.Skip("no host name")
	}
	dir := filepath.Join(os.Getenv("HOME"), ".wipnote")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := `{"delegate":{"codex":{"dangerFullAccessHosts":["` + host + `"]}}}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func runDoctor(t *testing.T) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd := delegateCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"doctor"})
	err := cmd.Execute()
	return out.String(), err
}

// delegate doctor names every check; a failed bwrap fails it on a host
// that did not opt in and warns on one that did.
func TestDelegateDoctor_NamesEachCheck(t *testing.T) {
	delegateRunProject(t)
	t.Setenv("STUB_BWRAP_EXIT", "1")
	t.Setenv("STUB_BWRAP_OUT", "bwrap: No permissions to create new namespace")
	out, err := runDoctor(t)
	for _, name := range []string{"codex:", "bwrap sandbox:", "danger-full-access opt-in:", "sandbox ladder:", "herdr:"} {
		if !strings.Contains(out, name) {
			t.Fatalf("doctor lacks %q:\n%s", name, out)
		}
	}
	if err == nil || !strings.Contains(out, "No permissions to create new namespace") {
		t.Fatalf("failed probe: err %v\n%s", err, out)
	}
	optInThisHost(t)
	if out, err := runDoctor(t); err != nil || !strings.Contains(out, "warn  bwrap sandbox") {
		t.Fatalf("opted host: err %v\n%s", err, out)
	}
}

// delegate run refuses a codex exec when bwrap fails, with the probe
// output, and runs it once the user config opts this host in.
func TestDelegateRun_SandboxPreflight(t *testing.T) {
	dir := delegateRunProject(t)
	argv := filepath.Join(t.TempDir(), "argv")
	t.Setenv("STUB_ARGV", argv)
	t.Setenv("STUB_BWRAP_EXIT", "1")
	t.Setenv("STUB_BWRAP_OUT", "bwrap: setting up uid map: Permission denied")
	cmd := delegateCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"run", "--", "codex", "exec", "x"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "uid map: Permission denied") {
		t.Fatalf("err %v, want the probe output", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".wipnote", "delegations-ledger.html")); !os.IsNotExist(err) {
		t.Fatalf("a refused run wrote the ledger: %v", err)
	}
	optInThisHost(t)
	out, code := runDelegate(t, "--", "codex", "exec", "x")
	got, _ := os.ReadFile(argv)
	if code != 0 || !strings.Contains(string(got), "--sandbox\ndanger-full-access\n") || !strings.Contains(out, "warning") {
		t.Fatalf("opted host: exit %d argv %q\n%s", code, got, out)
	}
}

// --write gives workspace-write; without it the run is read-only.
func TestDelegateRun_WriteFlag(t *testing.T) {
	delegateRunProject(t)
	argv := filepath.Join(t.TempDir(), "argv")
	t.Setenv("STUB_ARGV", argv)
	for args, want := range map[string]string{"": "read-only", "--write": "workspace-write"} {
		call := []string{"--", "codex", "exec", "x"}
		if args != "" {
			call = append([]string{args}, call...)
		}
		runDelegate(t, call...)
		if got, _ := os.ReadFile(argv); !strings.Contains(string(got), "--sandbox\n"+want+"\n") {
			t.Fatalf("%q: argv %q, want --sandbox %s", args, got, want)
		}
	}
}
