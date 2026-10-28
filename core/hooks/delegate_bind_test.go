package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func delegateRunDir(t *testing.T, project, run string) string {
	t.Helper()
	dir := filepath.Join(project, ".wipnote", "delegate", "runs", run)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func readDelegateLines(t *testing.T, path string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var m map[string]any
		if json.Unmarshal([]byte(l), &m) == nil {
			out = append(out, m)
		}
	}
	return out
}

func TestDelegateBind_WritesBindEvent(t *testing.T) {
	project := t.TempDir()
	run := "dlg-0123456789ab"
	dir := delegateRunDir(t, project, run)
	t.Setenv(DelegateRunEnv, run)
	RecordDelegateEvent(project, DelegateEvent{Kind: "session_start", SID: "sid-one"})
	RecordDelegateEvent(project, DelegateEvent{Kind: "prompt", SID: "sid-one"})
	RecordDelegateEvent(project, DelegateEvent{Kind: "session_start", SID: "sid-two"})
	one := readDelegateLines(t, filepath.Join(dir, "events-sid-one.ndjson"))
	two := readDelegateLines(t, filepath.Join(dir, "events-sid-two.ndjson"))
	if len(one) != 2 || len(two) != 1 || one[0]["kind"] != "session_start" || one[0]["run"] != run || one[0]["sid"] != "sid-one" {
		t.Fatalf("one %v two %v", one, two)
	}
	if one[0]["v"] != float64(1) || one[1]["seq"].(float64) <= one[0]["seq"].(float64) || two[0]["seq"].(float64) <= one[1]["seq"].(float64) {
		t.Fatalf("seq must rise across writers: %v %v", one, two)
	}
}

func TestDelegateBind_FastAndFailOpen(t *testing.T) {
	project := t.TempDir()
	run := "dlg-0123456789ab"
	dir := delegateRunDir(t, project, run)
	t.Setenv(DelegateRunEnv, run)
	var took []time.Duration
	for i := 0; i < 25; i++ {
		start := time.Now()
		RecordDelegateEvent(project, DelegateEvent{Kind: "session_start", SID: "sid-fast"})
		took = append(took, time.Since(start))
	}
	sort.Slice(took, func(i, j int) bool { return took[i] < took[j] })
	if median := took[len(took)/2]; median > 2*time.Millisecond {
		t.Fatalf("bind median %s, want under 2ms", median)
	}
	// Fail open: every bad input writes nothing, returns, and never panics.
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(project, ".wipnote", "delegate", "runs", "dlg-aaaaaaaaaaaa")); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ run, project, sid string }{
		{"", project, "sid"},                      // not a worker
		{"not-a-run", project, "sid"},             // bad run id
		{"dlg-bbbbbbbbbbbb", project, "sid"},      // no run folder
		{"dlg-aaaaaaaaaaaa", project, "sid"},      // a symlinked run folder
		{run, project, "../escape"},               // bad writer
		{run, project, ""},                        // no session
		{run, "", "sid"},                          // no project
		{run, filepath.Join(outside, "x"), "sid"}, // missing project
	} {
		t.Setenv(DelegateRunEnv, c.run)
		start := time.Now()
		RecordDelegateEvent(c.project, DelegateEvent{Kind: "session_start", SID: c.sid})
		if time.Since(start) > 100*time.Millisecond {
			t.Fatalf("%+v blocked", c)
		}
	}
	entries, _ := os.ReadDir(outside)
	files, _ := filepath.Glob(filepath.Join(dir, "events-*"))
	if len(entries) != 0 || len(files) != 1 {
		t.Fatalf("fail-open wrote: outside %v run %v", entries, files)
	}
	// Other side: the same call with a good run and sid does write.
	t.Setenv(DelegateRunEnv, run)
	RecordDelegateEvent(project, DelegateEvent{Kind: "stop", SID: "sid-good"})
	if _, err := os.Stat(filepath.Join(dir, "events-sid-good.ndjson")); err != nil {
		t.Fatal(err)
	}
}

// TestDelegateBind_ResumeSourceBindsSameSession (plan-28b4a7f6 slice 20): a
// resumed worker's SessionStart arrives with source "resume" and the session
// id the stopped run held. It binds that id to the NEW run the env names,
// and the old run's folder gets nothing.
func TestDelegateBind_ResumeSourceBindsSameSession(t *testing.T) {
	project := t.TempDir()
	oldDir := delegateRunDir(t, project, "dlg-0123456789ab")
	newRun := "dlg-ba9876543210"
	newDir := delegateRunDir(t, project, newRun)
	t.Setenv(DelegateRunEnv, newRun)
	ev := &CloudEvent{SessionID: "sid-kept", CWD: project, HookEventName: "SessionStart", Source: "resume"}
	if _, err := SessionStart(ev, project); err != nil {
		t.Fatalf("SessionStart: %v", err)
	}
	lines := readDelegateLines(t, filepath.Join(newDir, "events-sid-kept.ndjson"))
	if len(lines) != 1 || lines[0]["kind"] != "session_start" || lines[0]["sid"] != "sid-kept" || lines[0]["run"] != newRun {
		t.Fatalf("new run lines %v: want one session_start for sid-kept", lines)
	}
	if old, _ := filepath.Glob(filepath.Join(oldDir, "events-*")); len(old) != 0 {
		t.Fatalf("the old run folder was written: %v", old)
	}
	// Other side: the same resume start outside a worker binds nothing.
	t.Setenv(DelegateRunEnv, "")
	ev2 := &CloudEvent{SessionID: "sid-other", CWD: project, HookEventName: "SessionStart", Source: "resume"}
	if _, err := SessionStart(ev2, project); err != nil {
		t.Fatalf("SessionStart: %v", err)
	}
	if got, _ := filepath.Glob(filepath.Join(newDir, "events-sid-other*")); len(got) != 0 {
		t.Fatalf("a non-worker session bound: %v", got)
	}
}
