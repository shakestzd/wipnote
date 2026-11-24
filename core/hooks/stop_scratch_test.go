package hooks

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/claimledger"
	"github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/core/scratch"
)

const scratchTestSession = "test-sess"

// scratchProject is a temp project with a store and a scratchpad folder.
func scratchProject(t *testing.T) (projectDir, pad string) {
	t.Helper()
	clearNestedEnv(t)
	unsetenv(t, "WIPNOTE_PROJECT_DIR")
	unsetenv(t, "CLAUDE_PROJECT_DIR")
	setupTestDB(t)
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	projectDir = filepath.Join(base, "project")
	pad = filepath.Join(base, "scratchpad")
	for _, d := range []string{filepath.Join(projectDir, ".wipnote"), pad} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	gitInitRepo(t, projectDir)
	return projectDir, pad
}

func writeScratch(t *testing.T, pad, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(pad, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func scratchRow(t *testing.T, projectDir, sessionID, path string) scratch.Row {
	t.Helper()
	m, err := scratch.Load(filepath.Join(projectDir, ".wipnote"), sessionID)
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	for _, r := range m.Files {
		if r.Path == path {
			return r
		}
	}
	t.Fatalf("no manifest row for %q in %+v", path, m)
	return scratch.Row{}
}

// quietStderr runs fn and returns what it wrote to stderr.
func quietStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	prev := os.Stderr
	os.Stderr = w
	done := make(chan string, 1)
	go func() {
		out, _ := io.ReadAll(r)
		done <- string(out)
	}()
	fn()
	os.Stderr = prev
	_ = w.Close()
	return <-done
}

// The sanitized SessionEnd payload from Claude Code 2.1.331 proves the field
// name: scratchpad_dir, at the top level, beside session_id.
func TestClaudePayload_ScratchpadDirFieldName(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "claude-2.1.331-session-end.json"))
	if err != nil {
		t.Fatal(err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatal(err)
	}
	var got []string
	for k := range keys {
		got = append(got, k)
	}
	sort.Strings(got)
	want := []string{"cwd", "hook_event_name", "prompt_id", "reason", "scratchpad_dir", "session_id", "transcript_path"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("payload keys = %v, want %v", got, want)
	}

	event, err := ParseEventForHarness(HarnessClaude, raw)
	if err != nil {
		t.Fatal(err)
	}
	if want := "<temp-root>/claude-<uid>/-<encoded-cwd>/<session-id>/scratchpad"; event.ScratchpadDir != want {
		t.Errorf("ScratchpadDir = %q, want %q", event.ScratchpadDir, want)
	}
	if event.SessionID != "<session-id>" || event.Reason != "other" {
		t.Errorf("event = %+v, want the payload's session id and reason", event)
	}
}

// Done-when 1: a Stop event with scratchpad_dir copies a new top-level .md
// draft into the store, in the folder resolveStopSessionID names.
func TestStop_ScratchpadDraftSavedToStore(t *testing.T) {
	projectDir, pad := scratchProject(t)
	writeScratch(t, pad, "draft.md", "# Draft\n")
	event := &CloudEvent{SessionID: scratchTestSession, CWD: projectDir, ScratchpadDir: pad}

	result, err := Stop(event)
	if err != nil || result == nil || !result.Continue {
		t.Fatalf("Stop = %+v, %v, want a normal result", result, err)
	}

	sessionID := resolveStopSessionID(event)
	if r := scratchRow(t, projectDir, sessionID, "draft.md"); !r.Saved || r.State != scratch.StatePresent {
		t.Errorf("row = %+v, want saved and present", r)
	}
	saved := filepath.Join(projectDir, ".wipnote", "scratch", sessionID, "files", "draft.md")
	if got, err := os.ReadFile(saved); err != nil || string(got) != "# Draft\n" {
		t.Errorf("saved copy = %q, %v", got, err)
	}
	m, _ := scratch.Load(filepath.Join(projectDir, ".wipnote"), sessionID)
	if m.ScratchpadDir != pad {
		t.Errorf("scratchpad_dir = %q, want %q", m.ScratchpadDir, pad)
	}
}

// Done-when 3, through the hook: the row names the item the session's open
// claim holds, and a later change under a new claim adds the new item.
func TestStop_ScratchRowNamesActiveWorkItems(t *testing.T) {
	projectDir, pad := scratchProject(t)
	wipnoteDir := filepath.Join(projectDir, ".wipnote")
	event := &CloudEvent{SessionID: scratchTestSession, CWD: projectDir, ScratchpadDir: pad}
	sessionID := resolveStopSessionID(event)

	openCanonicalClaim(t, wipnoteDir, sessionID, sessionID, db.AgentRootSentinel, "feat-aaaa0001")
	writeScratch(t, pad, "draft.md", "v1\n")
	if _, err := Stop(event); err != nil {
		t.Fatal(err)
	}
	if got := scratchRow(t, projectDir, sessionID, "draft.md").WorkItems; !reflect.DeepEqual(got, []string{"feat-aaaa0001"}) {
		t.Fatalf("work_items = %v, want [feat-aaaa0001]", got)
	}

	if _, err := claimledger.NewStore(wipnoteDir).Close(sessionID, sessionID, db.AgentRootSentinel, "feat-aaaa0001",
		claimledger.OutcomeCompleted, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	openCanonicalClaim(t, wipnoteDir, sessionID, sessionID, db.AgentRootSentinel, "feat-bbbb0002")
	writeScratch(t, pad, "draft.md", "v2 is longer\n")
	if _, err := Stop(event); err != nil {
		t.Fatal(err)
	}
	if got := scratchRow(t, projectDir, sessionID, "draft.md").WorkItems; !reflect.DeepEqual(got, []string{"feat-aaaa0001", "feat-bbbb0002"}) {
		t.Errorf("work_items = %v, want both items", got)
	}
}

// Done-when 13: a Stop event with no scratchpad_dir, a missing folder or an
// unreadable file returns a normal hook result, and capture prints nothing.
func TestStop_ScratchCaptureNeverFailsTheHook(t *testing.T) {
	projectDir, pad := scratchProject(t)
	writeScratch(t, pad, "locked.md", "cannot read\n")
	if err := os.Chmod(filepath.Join(pad, "locked.md"), 0); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"no scratchpad_dir": "",
		"missing folder":    filepath.Join(pad, "nope"),
		"unreadable file":   pad,
		"a file, not a dir": filepath.Join(pad, "locked.md"),
	}
	for name, dir := range cases {
		event := &CloudEvent{SessionID: scratchTestSession, CWD: projectDir, ScratchpadDir: dir}
		baseline := &CloudEvent{SessionID: scratchTestSession, CWD: projectDir}
		var result *HookResult
		var err error
		noise := quietStderr(t, func() { _, _ = Stop(baseline) })
		out := quietStderr(t, func() { result, err = Stop(event) })
		if err != nil || result == nil || !result.Continue {
			t.Errorf("%s: Stop = %+v, %v, want a normal result", name, result, err)
		}
		if out != noise {
			t.Errorf("%s: capture wrote to stderr: %q (Stop alone wrote %q)", name, out, noise)
		}
		if direct := quietStderr(t, func() { captureScratch(event, projectDir, scratchTestSession, "") }); direct != "" {
			t.Errorf("%s: captureScratch wrote to stderr: %q", name, direct)
		}
	}
}

// A session id that is not a safe folder name stops the pass: nothing is
// written inside or outside the store's scratch folder.
func TestCaptureScratch_RefusesUnsafeSessionID(t *testing.T) {
	projectDir, pad := scratchProject(t)
	writeScratch(t, pad, "draft.md", "x\n")
	event := &CloudEvent{CWD: projectDir, ScratchpadDir: pad}

	for _, id := range []string{"", "../escape", "a/b", ".."} {
		if out := quietStderr(t, func() { captureScratch(event, projectDir, id, "") }); out != "" {
			t.Errorf("session id %q: stderr = %q", id, out)
		}
	}

	wipnoteDir := filepath.Join(projectDir, ".wipnote")
	for _, p := range []string{filepath.Join(wipnoteDir, "scratch"), filepath.Join(wipnoteDir, "escape"), filepath.Join(wipnoteDir, "files")} {
		if _, err := os.Lstat(p); err == nil {
			t.Errorf("an unsafe session id created %s", p)
		}
	}
}

// Capture never creates a store in a project that has none.
func TestCaptureScratch_NoStoreNoWrite(t *testing.T) {
	_, pad := scratchProject(t)
	writeScratch(t, pad, "draft.md", "x\n")
	bare := t.TempDir()

	captureScratch(&CloudEvent{CWD: bare, ScratchpadDir: pad}, bare, scratchTestSession, "")

	if _, err := os.Lstat(filepath.Join(bare, ".wipnote")); err == nil {
		t.Error("capture created a store")
	}
}

// Done-when 17, through the hook: the first capture makes the store's ignore
// file list scratch/, even when an older release wrote that file.
func TestCaptureScratch_OldIgnoreFileGainsScratch(t *testing.T) {
	projectDir, pad := scratchProject(t)
	ignore := filepath.Join(projectDir, ".wipnote", ".gitignore")
	if err := os.WriteFile(ignore, []byte("sessions/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeScratch(t, pad, "draft.md", "x\n")

	captureScratch(&CloudEvent{CWD: projectDir, ScratchpadDir: pad}, projectDir, scratchTestSession, "")

	got, _ := os.ReadFile(ignore)
	if !containsLine(string(got), "scratch/") {
		t.Errorf("ignore file does not list scratch/:\n%s", got)
	}
}

func containsLine(text, line string) bool {
	for _, l := range strings.Split(text, "\n") {
		if l == line {
			return true
		}
	}
	return false
}
