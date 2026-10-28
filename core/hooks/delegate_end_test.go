package hooks

import (
	"os"
	"path/filepath"
	"testing"
)

// delegateEndProject is a temp project with a delegate run folder; the real
// project's env is unset so nothing reaches it.
func delegateEndProject(t *testing.T, run string) (string, string) {
	t.Helper()
	t.Setenv("WIPNOTE_PROJECT_DIR", "")
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	os.Unsetenv("WIPNOTE_PROJECT_DIR")
	os.Unsetenv("CLAUDE_PROJECT_DIR")
	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, ".wipnote"), 0o755); err != nil {
		t.Fatal(err)
	}
	return project, delegateRunDir(t, project, run)
}

// TestDelegateEnd_StopFailureRecordsError: a worker's StopFailure writes an
// error event carrying the API error, so the one-shot run can fail with it.
func TestDelegateEnd_StopFailureRecordsError(t *testing.T) {
	run := "dlg-0123456789ab"
	project, dir := delegateEndProject(t, run)
	t.Setenv(DelegateRunEnv, run)
	if _, err := StopFailure(&CloudEvent{SessionID: "sid-one", CWD: project, StopError: "rate_limit"}); err != nil {
		t.Fatal(err)
	}
	lines := readDelegateLines(t, filepath.Join(dir, "events-sid-one.ndjson"))
	if len(lines) != 1 || lines[0]["kind"] != "error" || lines[0]["summary"] != "rate_limit" {
		t.Fatalf("want one error event with the error, got %v", lines)
	}
}

// TestDelegateEnd_StopFailureOutsideWorker: without the run env nothing is
// written to the run folder.
func TestDelegateEnd_StopFailureOutsideWorker(t *testing.T) {
	run := "dlg-0123456789ab"
	project, dir := delegateEndProject(t, run)
	t.Setenv(DelegateRunEnv, "")
	if _, err := StopFailure(&CloudEvent{SessionID: "sid-one", CWD: project, StopError: "rate_limit"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "events-sid-one.ndjson")); !os.IsNotExist(err) {
		t.Fatalf("no events file outside a worker, got err %v", err)
	}
}

// TestDelegateEnd_SessionEndRecordsReason: SessionEnd writes session_end
// with the end reason.
func TestDelegateEnd_SessionEndRecordsReason(t *testing.T) {
	run := "dlg-0123456789ab"
	project, dir := delegateEndProject(t, run)
	t.Setenv(DelegateRunEnv, run)
	if _, err := SessionEnd(&CloudEvent{SessionID: "sid-one", CWD: project, Reason: "prompt_input_exit"}, project); err != nil {
		t.Fatal(err)
	}
	lines := readDelegateLines(t, filepath.Join(dir, "events-sid-one.ndjson"))
	if len(lines) != 1 || lines[0]["kind"] != "session_end" || lines[0]["summary"] != "prompt_input_exit" {
		t.Fatalf("want one session_end event with the reason, got %v", lines)
	}
}

// TestDelegateEnd_SessionEndOutsideWorker: no run env, no event.
func TestDelegateEnd_SessionEndOutsideWorker(t *testing.T) {
	run := "dlg-0123456789ab"
	project, dir := delegateEndProject(t, run)
	t.Setenv(DelegateRunEnv, "")
	if _, err := SessionEnd(&CloudEvent{SessionID: "sid-one", CWD: project, Reason: "other"}, project); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "events-sid-one.ndjson")); !os.IsNotExist(err) {
		t.Fatalf("no events file outside a worker, got err %v", err)
	}
}
