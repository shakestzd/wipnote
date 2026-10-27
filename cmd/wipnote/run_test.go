package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/claimledger"
	"github.com/shakestzd/wipnote/core/runledger"
)

const runTestSession = "sess-run-wrapper-test"

func runWrapperProject(t *testing.T) string {
	t.Helper()
	dir := delegateTestProject(t)
	t.Setenv("WIPNOTE_SESSION_ID", runTestSession)
	projectDirFlag = dir
	t.Cleanup(func() { projectDirFlag = "" })
	t.Chdir(dir)
	return dir
}

func runWrapper(t *testing.T, args ...string) (string, int) {
	t.Helper()
	code := 0
	runCmdExit = func(c int) { code = c }
	t.Cleanup(func() { runCmdExit = os.Exit })
	var out bytes.Buffer
	cmd := wrapperRunCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("run %q: %v\n%s", args, err, out.String())
	}
	return out.String(), code
}

func onlyRun(t *testing.T, dir string) runledger.Record {
	t.Helper()
	recs, err := runledger.NewStore(filepath.Join(dir, ".wipnote")).Records(func(string) bool { return false })
	if err != nil || len(recs) != 1 {
		t.Fatalf("records %+v err %v, want exactly one", recs, err)
	}
	return recs[0]
}

func TestRunWrapper_ExitZeroRecordsCompletedRun(t *testing.T) {
	dir := runWrapperProject(t)
	out, code := runWrapper(t, "--work-item", "feat-0a1b2c3d", "--description", "gate",
		"--", "sh", "-c", "echo building; echo gate done")
	if code != 0 {
		t.Fatalf("exit %d, want 0\n%s", code, out)
	}
	if !strings.Contains(out, "building\ngate done\n") {
		t.Errorf("output not passed through: %q", out)
	}
	r := onlyRun(t, dir)
	if r.Kind != runledger.KindWrapper || r.Status != runledger.StatusCompleted || r.Exit == nil || *r.Exit != 0 {
		t.Errorf("run = %+v", r)
	}
	if r.WorkItem != "feat-0a1b2c3d" || r.Session != runTestSession || r.Description != "gate" {
		t.Errorf("links = %+v", r)
	}
	if r.LastLine != "gate done" || r.FirstError != "" || r.Folder != "." {
		t.Errorf("summary = %q / %q folder %q", r.LastLine, r.FirstError, r.Folder)
	}
	if r.Command != "sh -c 'echo building; echo gate done'" {
		t.Errorf("command = %q", r.Command)
	}
	log, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(r.LogPath)))
	if err != nil || string(log) != "building\ngate done\n" {
		t.Errorf("log %q (%v) at %s", log, err, r.LogPath)
	}
}

func TestRunWrapper_NonzeroExitReturnedUnchanged(t *testing.T) {
	dir := runWrapperProject(t)
	_, code := runWrapper(t, "--", "sh", "-c", "echo 'error: boom'; echo tail; echo 'stderr note' >&2; exit 3")
	if code != 3 {
		t.Fatalf("exit %d, want the command's 3", code)
	}
	r := onlyRun(t, dir)
	if r.Status != runledger.StatusFailed || r.Exit == nil || *r.Exit != 3 {
		t.Errorf("run = %s exit %v", r.Status, r.Exit)
	}
	if r.FirstError != "error: boom" {
		t.Errorf("first error = %q", r.FirstError)
	}
	// stderr is kept in the log too (its order against stdout is not fixed).
	if log, _ := os.ReadFile(filepath.Join(dir, filepath.FromSlash(r.LogPath))); !strings.Contains(string(log), "stderr note") {
		t.Errorf("stderr missing from log: %q", log)
	}
	// No --work-item and no claim: linked to the session only.
	if r.WorkItem != "" || r.Session != runTestSession {
		t.Errorf("links = item %q session %q", r.WorkItem, r.Session)
	}
}

func TestRunWrapper_DefaultsToActiveClaim(t *testing.T) {
	dir := runWrapperProject(t)
	_, _, err := claimledger.NewStore(filepath.Join(dir, ".wipnote")).Open(runTestSession, claimledger.Episode{
		WorkItemID: "feat-0a1b2c3d", SessionID: runTestSession, AgentID: "__root__", StartedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	runWrapper(t, "--", "true")
	if r := onlyRun(t, dir); r.WorkItem != "feat-0a1b2c3d" {
		t.Errorf("work item = %q, want the active claim", r.WorkItem)
	}
}

func TestRunWrapper_MissingCommandFails127(t *testing.T) {
	dir := runWrapperProject(t)
	_, code := runWrapper(t, "--", "definitely-not-a-command-xyz")
	if code != 127 {
		t.Fatalf("exit %d, want 127", code)
	}
	if r := onlyRun(t, dir); r.Status != runledger.StatusFailed || r.FirstError == "" {
		t.Errorf("run = %+v", r)
	}
}

func TestRunWrapper_RefusesBadWorkItemBeforeRunning(t *testing.T) {
	dir := runWrapperProject(t)
	marker := filepath.Join(dir, "ran")
	var out bytes.Buffer
	cmd := wrapperRunCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--work-item", "not-an-id", "--", "touch", marker})
	if err := cmd.Execute(); err == nil {
		t.Fatal("bad work item accepted")
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("command ran despite the refused record")
	}
}
