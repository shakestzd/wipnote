package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/core/delegationledger"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
	"github.com/shakestzd/wipnote/internal/delegate/session"
)

func delegateSessionProject(t *testing.T) string {
	t.Helper()
	dir := delegateTestProject(t)
	t.Setenv("WIPNOTE_SESSION_ID", "sess-delegate-spawn-test")
	projectDirFlag = dir
	t.Cleanup(func() { projectDirFlag = "" })
	return dir
}

func execDelegate(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd := delegateCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestDelegateSpawn_Refusals(t *testing.T) {
	delegateSessionProject(t)
	if _, err := execDelegate(t, "spawn", "w1", "--harness", "claude"); err == nil || !strings.Contains(err.Error(), "no work item") {
		t.Fatalf("no work item: %v", err)
	}
	if _, err := execDelegate(t, "spawn", "w1", "--harness", "claude", "--work-item", "feat-0a1b2c3d"); err == nil ||
		!strings.Contains(err.Error(), "herdr") {
		t.Fatalf("outside herdr: %v", err)
	}
	if _, err := execDelegate(t, "spawn", "W1", "--harness", "claude", "--work-item", "feat-0a1b2c3d"); err == nil ||
		!strings.Contains(err.Error(), "invalid worker name") {
		t.Fatalf("bad name: %v", err)
	}
}

func TestDelegateListAndTree_Empty(t *testing.T) {
	delegateSessionProject(t)
	out, err := execDelegate(t, "list")
	if err != nil || !strings.Contains(out, "no open delegation runs") {
		t.Fatalf("list %q err %v", out, err)
	}
	out, err = execDelegate(t, "tree", "--json")
	if err != nil || strings.TrimSpace(out) != "[]" {
		t.Fatalf("tree %q err %v", out, err)
	}
}

// boot reads the run folder and execs the launcher with the brief as argv
// and the scrubbed environment; it never reads the brief from the line.
func TestDelegateBoot_ExecsLauncherWithBrief(t *testing.T) {
	dir := delegateSessionProject(t)
	t.Setenv("WIPNOTE_PARENT_SESSION", "sess-caller")
	t.Setenv("WIPNOTE_NESTING_DEPTH", "1")
	store, _ := runstore.New(dir)
	row, err := delegationledger.NewStore(filepath.Join(dir, ".wipnote")).Open(delegationledger.Record{
		Mode: delegationledger.ModeInteractive, Harness: "claude", Name: "w1", WorkItem: "feat-0a1b2c3d",
	})
	if err != nil {
		t.Fatal(err)
	}
	rf, _ := store.CreateRun(row.ID)
	if err := session.WriteInfo(rf, session.Info{Run: row.ID, Name: "w1", Harness: "claude", Cwd: dir}); err != nil {
		t.Fatal(err)
	}
	if err := rf.WriteFile(runstore.FileBrief, []byte("the brief\n")); err != nil {
		t.Fatal(err)
	}
	var gotArgv, gotEnv []string
	delegateBootExec = func(argv0 string, argv, env []string) error {
		gotArgv, gotEnv = argv, env
		return nil
	}
	t.Cleanup(func() { delegateBootExec = defaultBootExec })
	t.Setenv(session.CallerEnv, "sess-caller")
	t.Chdir(t.TempDir()) // boot changes to the worker's cwd; restore after
	if _, err := execDelegate(t, "boot", row.ID); err != nil {
		t.Fatal(err)
	}
	if len(gotArgv) < 2 || gotArgv[1] != "claude" || gotArgv[len(gotArgv)-1] != "the brief\n" {
		t.Fatalf("argv %q", gotArgv)
	}
	if slices.Contains(gotEnv, "WIPNOTE_PARENT_SESSION=sess-caller") || !slices.Contains(gotEnv, session.RunEnv+"="+row.ID) ||
		!slices.Contains(gotEnv, session.CallerEnv+"=sess-caller") {
		t.Fatalf("env %v", gotEnv)
	}
	if _, err := os.Stat(filepath.Join(dir, ".wipnote", "delegate")); err != nil {
		t.Fatal(err)
	}
}
