package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// The status line command records cost and tokens into the run folder and
// prints the usage line (slice 22).
func TestDelegateStatusLine_RecordsCost(t *testing.T) {
	dir := delegateRunProject(t)
	store, err := runstore.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	run := "dlg-0123456789ab"
	if _, err := store.CreateRun(run); err != nil {
		t.Fatal(err)
	}
	out := statusLine(t, run, `{"cost":{"total_cost_usd":0.25},"context_window":{"total_input_tokens":1000,"total_output_tokens":20}}`)
	if out != "delegate 1000 in / 20 out tokens, $0.25\n" {
		t.Fatalf("out %q", out)
	}
	usage, err := os.ReadFile(filepath.Join(store.Root(), "runs", run, runstore.FileUsage))
	if err != nil || strings.TrimSpace(string(usage)) != "1000 in / 20 out tokens, $0.25" {
		t.Fatalf("usage %q err %v", usage, err)
	}
	// Bad input or an unknown run prints nothing and never fails.
	if out := statusLine(t, run, `not json`); out != "" {
		t.Fatalf("out %q", out)
	}
	if out := statusLine(t, "dlg-ffffffffffff", `{"cost":{"total_cost_usd":1}}`); out != "" {
		t.Fatalf("out %q", out)
	}
}

func statusLine(t *testing.T, run, stdin string) string {
	t.Helper()
	var out bytes.Buffer
	cmd := delegateCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs([]string{"status-line", run})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("status-line: %v", err)
	}
	return out.String()
}

func TestDelegateStatusLineCommand_Quoted(t *testing.T) {
	got := delegateStatusLineCommand("/opt/my bin/wipnote", "/p/x", "dlg-0123456789ab")
	if got != `'/opt/my bin/wipnote' delegate status-line --project-dir '/p/x' 'dlg-0123456789ab'` {
		t.Fatalf("%s", got)
	}
}

// A delegate worker's launcher runs claude in the run's own dir
// (--work-dir) while the wipnote root stays the run store's project, so no
// .wipnote is made under the work dir and the hooks find the run folder.
func TestClaudeLaunchDirs_WorkDir(t *testing.T) {
	dir := delegateRunProject(t) // --project-dir is this project
	sub := filepath.Join(dir, "a", "b")
	must(t, os.MkdirAll(sub, 0o755))
	child, root, err := claudeLaunchDirs(sub)
	if err != nil || child != sub || root != dir {
		t.Fatalf("child %q root %q err %v", child, root, err)
	}
	opts := LaunchOpts{ProjectRoot: child, WipnoteRoot: root}
	if claudeEnvProjectDir(opts) != dir || launchMarkerRoot(opts) != dir {
		t.Fatalf("env %q marker %q", claudeEnvProjectDir(opts), launchMarkerRoot(opts))
	}
	writeLaunchMarker("test", launchMarkerRoot(opts))
	if _, err := os.Stat(filepath.Join(sub, ".wipnote")); !os.IsNotExist(err) {
		t.Fatalf("a .wipnote was made under the work dir: %v", err)
	}
	// The work dir is the project itself: no separate root.
	if child, root, err := claudeLaunchDirs(dir); err != nil || child != dir || root != "" {
		t.Fatalf("child %q root %q err %v", child, root, err)
	}
	// Without --work-dir the project root, as before.
	if child, _, err := claudeLaunchDirs(""); err != nil || child != dir {
		t.Fatalf("child %q err %v", child, err)
	}
	if _, _, err := claudeLaunchDirs(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("a missing work dir is refused")
	}
	if f := claudeCmd().Flags().Lookup("work-dir"); f == nil || !f.Hidden {
		t.Fatal("claude has a hidden --work-dir flag")
	}
}

// A monorepo: the project (.wipnote) sits below the git root. Without
// --project-dir the root is the nearest .wipnote above the work dir, never
// the git root and never the work dir.
func TestClaudeLaunchDirs_MonorepoSubdir(t *testing.T) {
	delegateRunProject(t)
	projectDirFlag = ""
	repo := t.TempDir()
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	project := filepath.Join(repo, "services", "api")
	sub := filepath.Join(project, "pkg")
	must(t, os.MkdirAll(filepath.Join(project, ".wipnote"), 0o755))
	must(t, os.MkdirAll(sub, 0o755))
	child, root, err := claudeLaunchDirs(sub)
	if err != nil || child != sub || root != project {
		t.Fatalf("child %q root %q err %v, want root %s", child, root, err, project)
	}
	// No .wipnote anywhere above the work dir: refused, nothing made.
	lone := t.TempDir()
	if _, _, err := claudeLaunchDirs(lone); err == nil {
		t.Fatal("a work dir with no project is refused")
	}
	if _, err := os.Stat(filepath.Join(lone, ".wipnote")); !os.IsNotExist(err) {
		t.Fatal("a .wipnote was made")
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
