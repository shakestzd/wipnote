package hooks

import (
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/containerwrite"
)

const containerRun = "dlg-00c0ffee0123"

// workerProject is a temp git project with a delegate run folder, and the
// worker's session env cleared.
func workerProject(t *testing.T) (string, string) {
	t.Helper()
	project := t.TempDir()
	runDir := filepath.Join(project, ".wipnote", "delegate", "runs", containerRun)
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", project, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	for _, k := range []string{"CLAUDE_SESSION_ID", "WIPNOTE_PARENT_SESSION", "WIPNOTE_NESTING_DEPTH",
		"WIPNOTE_SESSION_ID", "WIPNOTE_SESSION_FAMILY_ID", "CLAUDE_ENV_FILE"} {
		t.Setenv(k, "")
	}
	t.Setenv("CLAUDE_PROJECT_DIR", project)
	t.Setenv("WIPNOTE_HARNESS", "claude") // the launcher's stamp inside the container
	t.Setenv(DelegateRunEnv, containerRun)
	return project, runDir
}

// snapshot maps every file under .wipnote/ to its modification time and size.
func snapshot(t *testing.T, project string) map[string]string {
	t.Helper()
	out := map[string]string{}
	root := filepath.Join(project, ".wipnote")
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		fi, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		rel, _ := filepath.Rel(project, p)
		out[filepath.ToSlash(rel)] = fi.ModTime().String() + "/" + fi.Name() + "/" + string(rune(fi.Size()%1000))
		return nil
	})
	return out
}

func changed(before, after map[string]string) []string {
	var out []string
	for k, v := range after {
		if before[k] != v {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// runWorkerHooks fires one session's lifecycle through the real handlers.
func runWorkerHooks(t *testing.T, project, sid string) {
	t.Helper()
	ev := &CloudEvent{SessionID: sid, CWD: project, HookEventName: "SessionStart", Source: "startup"}
	if _, err := SessionStart(ev, project); err != nil {
		t.Fatalf("SessionStart: %v", err)
	}
	ev.HookEventName, ev.Prompt = "UserPromptSubmit", "hi"
	_, _ = UserPrompt(ev)
	ev.HookEventName = "Stop"
	_, _ = Stop(ev)
	ev.HookEventName = "SessionEnd"
	_, _ = SessionEnd(ev, project)
	time.Sleep(50 * time.Millisecond)
}

// ownFile reports whether rel is a file the worker session alone owns.
func ownFile(rel, sid string) bool {
	runPrefix := ".wipnote/delegate/runs/" + containerRun + "/"
	switch {
	case strings.HasPrefix(rel, runPrefix):
		name := strings.TrimPrefix(rel, runPrefix)
		return name == "events-"+sid+".ndjson" || name == "session-"+sid+".json"
	case rel == ".wipnote/sessions/"+sid+".html", strings.HasPrefix(rel, ".wipnote/sessions/"+sid+"/"):
		return true
	case strings.HasPrefix(rel, ".wipnote/claims/sess-"+sid+".html"):
		return true
	}
	return false
}

// TestContainerWorker_WritesOnlyOwnFiles: a container worker's hooks write
// only files it alone owns (its events, its own session row, its own session
// folder and claim shard), never a shared .wipnote/ file such as the
// sessions ledger, the family index or .active-session.
func TestContainerWorker_WritesOnlyOwnFiles(t *testing.T) {
	project, runDir := workerProject(t)
	t.Setenv(containerwrite.PlaceEnv, containerwrite.PlaceContainer)
	sid := "aaaaaaaa-1111-2222-3333-444444444444"
	before := snapshot(t, project)
	runWorkerHooks(t, project, sid)
	for _, rel := range changed(before, snapshot(t, project)) {
		if !ownFile(rel, sid) {
			t.Errorf("container worker wrote shared file %s", rel)
		}
	}
	sessions, err := containerwrite.ReadSessions(runDir)
	if err != nil || len(sessions) != 1 {
		t.Fatalf("own session rows %+v, %v; want one", sessions, err)
	}
	s := sessions[0]
	if s.SessionID != sid || s.Harness != "claude" || s.StartedAt.IsZero() || s.EndedAt.IsZero() || s.Family != sid {
		t.Errorf("own session row %+v: want id, the real harness, start, end and family", s)
	}
	data, err := os.ReadFile(filepath.Join(runDir, "events-"+sid+".ndjson"))
	if err != nil || !strings.Contains(string(data), `"session_start"`) || !strings.Contains(string(data), `"stop"`) {
		t.Errorf("events file %q, %v: want the bind and the stop", data, err)
	}
}

// TestHostWorker_StillWritesSharedLedger is the other side: the same hooks
// without the container place write the shared sessions ledger and no own
// session file.
func TestHostWorker_StillWritesSharedLedger(t *testing.T) {
	project, runDir := workerProject(t)
	t.Setenv(containerwrite.PlaceEnv, "")
	sid := "bbbbbbbb-1111-2222-3333-444444444444"
	runWorkerHooks(t, project, sid)
	ledger, err := os.ReadFile(filepath.Join(project, ".wipnote", "sessions-ledger.html"))
	if err != nil || !strings.Contains(string(ledger), sid) {
		t.Fatalf("host worker: sessions ledger %v, want its row", err)
	}
	if _, err := os.Stat(filepath.Join(runDir, "session-"+sid+".json")); !os.IsNotExist(err) {
		t.Errorf("host worker wrote an own session file (err %v)", err)
	}
	var fam map[string]any
	data, _ := os.ReadFile(filepath.Join(project, ".wipnote", "session-families.json"))
	if json.Unmarshal(data, &fam) != nil {
		t.Errorf("host worker: no family index written")
	}
}

func inode(t *testing.T, path string) uint64 {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return statIno(fi)
}

// TestContainerWorker_EventsReplacedByRename: in a container worker each
// event replaces the events file by rename (a new inode, every line kept);
// on the host the same events append in place (the inode stays).
func TestContainerWorker_EventsReplacedByRename(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("inodes are a unix check")
	}
	for _, place := range []string{containerwrite.PlaceContainer, ""} {
		project, runDir := workerProject(t)
		t.Setenv(containerwrite.PlaceEnv, place)
		path := filepath.Join(runDir, "events-sid-r.ndjson")
		RecordDelegateEvent(project, DelegateEvent{Kind: "session_start", SID: "sid-r"})
		first := inode(t, path)
		RecordDelegateEvent(project, DelegateEvent{Kind: "prompt", SID: "sid-r"})
		renamed := inode(t, path) != first
		if renamed != (place == containerwrite.PlaceContainer) {
			t.Errorf("place %q: replaced by rename = %v", place, renamed)
		}
		if lines := readDelegateLines(t, path); len(lines) != 2 {
			t.Errorf("place %q: %d lines, want 2", place, len(lines))
		}
	}
}
