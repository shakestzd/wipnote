package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/claimledger"
	"github.com/shakestzd/wipnote/core/delegationledger"
	"github.com/shakestzd/wipnote/core/hooks"
)

// delegateRunProject is a temp project with the oneshot stubs (codex,
// claude, ccx) first on PATH and the session id set.
func delegateRunProject(t *testing.T) string {
	t.Helper()
	dir := delegateTestProject(t)
	stubs, err := filepath.Abs(filepath.Join("..", "..", "internal", "delegate", "oneshot", "testdata", "stubs"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", stubs+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("WIPNOTE_SESSION_ID", "sess-delegate-run-test")
	t.Chdir(t.TempDir()) // the tool's cwd: a relative -o file lands here
	projectDirFlag = dir
	t.Cleanup(func() { projectDirFlag = "" })
	return dir
}

func runDelegate(t *testing.T, args ...string) (string, int) {
	t.Helper()
	code := 0
	delegateRunExit = func(c int) { code = c }
	t.Cleanup(func() { delegateRunExit = os.Exit })
	var out bytes.Buffer
	cmd := delegateCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(append([]string{"run"}, args...))
	if err := cmd.Execute(); err != nil {
		t.Fatalf("delegate run %q: %v\n%s", args, err, out.String())
	}
	return out.String(), code
}

func onlyRow(t *testing.T, dir string) delegationledger.Record {
	t.Helper()
	rows, err := delegationledger.NewStore(filepath.Join(dir, ".wipnote")).ReadAll()
	if err != nil || len(rows) == 0 {
		t.Fatalf("rows %v err %v", rows, err)
	}
	return rows[len(rows)-1]
}

// The row carries the caller's active work item when --work-item is absent;
// an explicit --work-item wins.
func TestDelegateRun_DefaultsToActiveWorkItem(t *testing.T) {
	dir := delegateRunProject(t)
	sid := hooks.EnvSessionID("")
	if sid == "" {
		t.Fatal("no session id resolved")
	}
	_, _, err := claimledger.NewStore(filepath.Join(dir, ".wipnote")).Open(sid, claimledger.Episode{
		WorkItemID: "feat-0a1b2c3d", SessionID: sid, AgentID: "__root__", StartedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	out, code := runDelegate(t, "--", "codex", "exec", "x")
	if code != 0 || !strings.HasPrefix(out, "codex exited 0 after ") {
		t.Fatalf("exit %d\n%s", code, out)
	}
	if r := onlyRow(t, dir); r.WorkItem != "feat-0a1b2c3d" || r.CallerSession != sid || r.Label != "" {
		t.Fatalf("row %+v", r)
	}
	runDelegate(t, "--work-item", "bug-0a1b2c3d", "--label", "explicit", "--", "codex", "exec", "x")
	if r := onlyRow(t, dir); r.WorkItem != "bug-0a1b2c3d" || r.Label != "explicit" {
		t.Fatalf("row %+v", r)
	}
}

// The process exits with the tool's status, and the tool's own flags are
// never read as wipnote's (no -- needed).
func TestDelegateRun_ExitIsTheTools(t *testing.T) {
	delegateRunProject(t)
	t.Setenv("STUB_EXIT", "4")
	out, code := runDelegate(t, "codex", "exec", "-o", "own.md", "x")
	if code != 4 || !strings.HasPrefix(out, "codex exited 4 after ") {
		t.Fatalf("exit %d\n%s", code, out)
	}
}

func TestDelegateRun_RefusesChained(t *testing.T) {
	delegateRunProject(t)
	cmd := delegateCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"run", "--", "codex", "exec", "x", "|", "sort"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "chained commands") {
		t.Fatalf("err %v", err)
	}
}
