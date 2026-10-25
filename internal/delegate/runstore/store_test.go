package runstore_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/worktree"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

const testRun = "dlg-0123456789abcdef"

// newProject returns a temp project with a .wipnote/ folder and the
// environment variables that could point a store elsewhere cleared.
func newProject(t *testing.T) string {
	t.Helper()
	for _, k := range []string{"WIPNOTE_PROJECT_DIR", "CLAUDE_PROJECT_DIR"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "HERDR_") {
			k := kv[:strings.IndexByte(kv, '=')]
			t.Setenv(k, "")
			os.Unsetenv(k)
		}
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".wipnote"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func newRun(t *testing.T) (*runstore.Store, *runstore.RunFolder) {
	t.Helper()
	s, err := runstore.New(newProject(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	rf, err := s.CreateRun(testRun)
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	return s, rf
}

func TestStore_RootAndIDs(t *testing.T) {
	dir := newProject(t)
	s, err := runstore.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, ".wipnote", "delegate"); s.Root() != want {
		t.Errorf("Root = %q, want %q", s.Root(), want)
	}
	if _, err := runstore.New(""); err == nil {
		t.Error("New(\"\") must refuse")
	}
	for _, bad := range []string{"", "dlg-abc", "dlg-0123456789ab/..", "../dlg-0123456789ab", "dlg-0123456789AB"} {
		if _, err := s.CreateRun(bad); err == nil {
			t.Errorf("CreateRun(%q) must refuse", bad)
		}
	}
	id := runstore.NewMsgID()
	if !runstore.ValidMsgID(id) || len(id) < len("msg-")+12 {
		t.Errorf("NewMsgID = %q: want msg- plus 12 or more hex", id)
	}
	if runstore.ValidMsgID("msg-deadbeef") {
		t.Error("an 8-hex id must be refused (P-6)")
	}
}

// TestRunStore_OneWriterPerFile: two processes writing events for one run
// each own one file, and the reader merges them in seq order.
func TestRunStore_OneWriterPerFile(t *testing.T) {
	s, rf := newRun(t)
	var wg sync.WaitGroup
	for _, w := range []string{"caller", "sess-a"} {
		wg.Add(1)
		go func(w string) {
			defer wg.Done()
			cmd := exec.Command(os.Args[0], "-test.run=^TestHelperEventWriter$")
			cmd.Env = append(os.Environ(), "RUNSTORE_HELPER_PROJECT="+filepath.Dir(filepath.Dir(s.Root())), "RUNSTORE_HELPER_WRITER="+w)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("writer process %s: %v\n%s", w, err, out)
			}
		}(w)
	}
	wg.Wait()
	files, _ := filepath.Glob(filepath.Join(rf.Dir(), "events-*.ndjson"))
	if len(files) != 2 {
		t.Fatalf("want one events file per writer, got %v", files)
	}
	for _, f := range files {
		assertSingleWriter(t, f)
	}
	evs, err := rf.ReadEvents()
	if err != nil || len(evs) != 100 {
		t.Fatalf("ReadEvents = %d events, %v; want 100", len(evs), err)
	}
	for i := 1; i < len(evs); i++ {
		if evs[i].Seq < evs[i-1].Seq {
			t.Fatalf("events not in seq order at %d: %d after %d", i, evs[i].Seq, evs[i-1].Seq)
		}
	}
	if _, err := rf.OpenEvents("../x"); err == nil {
		t.Error("a writer name with a path separator must be refused")
	}
}

// TestHelperEventWriter is the child process of TestRunStore_OneWriterPerFile.
func TestHelperEventWriter(t *testing.T) {
	project, w := os.Getenv("RUNSTORE_HELPER_PROJECT"), os.Getenv("RUNSTORE_HELPER_WRITER")
	if project == "" {
		t.Skip("helper process only")
	}
	s, _ := runstore.New(project)
	rf, err := s.Run(testRun)
	if err != nil {
		t.Fatal(err)
	}
	ew, err := rf.OpenEvents(w)
	if err != nil {
		t.Fatal(err)
	}
	defer ew.Close()
	for i := 0; i < 50; i++ {
		if err := ew.Append(runstore.Event{Kind: runstore.KindToolStart, SID: w}); err != nil {
			t.Fatal(err)
		}
	}
}

// assertSingleWriter checks every line of an events file came from the writer
// its name names, with a strictly rising seq.
func assertSingleWriter(t *testing.T, path string) {
	t.Helper()
	want := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(path), "events-"), ".ndjson")
	data, _ := os.ReadFile(path)
	last := int64(0)
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var ev runstore.Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("%s: bad line %q", path, line)
		}
		if ev.SID != want || ev.Seq <= last || ev.Run != testRun || ev.V != 1 {
			t.Fatalf("%s: line %q (want sid %s, seq > %d)", path, line, want, last)
		}
		last = ev.Seq
	}
}

// TestRunStore_SeqFollowsOtherWriters: a writer opened after another has
// written continues above that writer's seq, so the merge is causal.
func TestRunStore_SeqFollowsOtherWriters(t *testing.T) {
	_, rf := newRun(t)
	a, _ := rf.OpenEvents("a")
	for i := 0; i < 3; i++ {
		_ = a.Append(runstore.Event{Kind: runstore.KindPrompt})
	}
	a.Close()
	b, _ := rf.OpenEvents("b")
	_ = b.Append(runstore.Event{Kind: runstore.KindStop})
	b.Close()
	evs, _ := rf.ReadEvents()
	if len(evs) != 4 || evs[3].Kind != runstore.KindStop || evs[3].Seq != 4 {
		t.Fatalf("want b's stop last with seq 4, got %+v", evs)
	}
}

// TestRunStore_SkipsPartialLine: a torn last line is never parsed, a writer
// that reopens the file does not glue its next event onto it, and its seq
// continues from the last complete line.
func TestRunStore_SkipsPartialLine(t *testing.T) {
	_, rf := newRun(t)
	ew, _ := rf.OpenEvents("w")
	_ = ew.Append(runstore.Event{Kind: runstore.KindPrompt})
	_ = ew.Append(runstore.Event{Kind: runstore.KindToolStart})
	ew.Close()
	path := filepath.Join(rf.Dir(), "events-w.ndjson")
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	// A complete JSON object with no newline: still a torn write.
	_, _ = f.WriteString(`{"v":1,"seq":3,"kind":"stop"}`)
	f.Close()
	evs, _ := rf.ReadEvents()
	if len(evs) != 2 {
		t.Fatalf("a line with no newline must be skipped: got %+v", evs)
	}
	ew, err := rf.OpenEvents("w")
	if err != nil {
		t.Fatal(err)
	}
	_ = ew.Append(runstore.Event{Kind: runstore.KindStop})
	ew.Close()
	evs, _ = rf.ReadEvents()
	if len(evs) != 3 || evs[2].Kind != runstore.KindStop || evs[2].Seq != 3 {
		t.Fatalf("after reopen want 3 events ending in stop seq 3, got %+v", evs)
	}
}

// TestSubAgents_OnlyRealPairs (D4): only SubagentStart/SubagentStop events
// count as sub-agents; other events carrying an agent id do not.
func TestSubAgents_OnlyRealPairs(t *testing.T) {
	evs := []runstore.Event{
		{Kind: runstore.KindSubagentStart, SID: "s", AgentID: "a1", AgentType: "Explore"},
		{Kind: runstore.KindToolStart, SID: "s", AgentID: "plugin-hook"},
		{Kind: runstore.KindPrompt, SID: "s", AgentID: "recap"},
		{Kind: runstore.KindSubagentStop, SID: "s", AgentID: "orphan"},
		{Kind: runstore.KindSubagentStart, SID: "s", AgentID: "a2", AgentType: "general-purpose"},
		{Kind: runstore.KindSubagentStop, SID: "s", AgentID: "a1"},
		{Kind: runstore.KindSubagentStart, SID: "s", AgentID: ""},
	}
	got := runstore.SubAgents(evs)
	if len(got) != 2 || got[0].ID != "a1" || got[0].Open || got[1].ID != "a2" || !got[1].Open {
		t.Fatalf("SubAgents = %+v; want a1 closed and a2 open only", got)
	}
}

// TestRedact_Linear: secrets are masked, text is clipped, and an 8000-char
// adversarial line (issue R) and a 1 MB one finish quickly.
func TestRedact_Linear(t *testing.T) {
	in := "run with token=abc123SECRET and sk-ant-api03-ABCDEFGHIJKLMNOPQRST\nsecond line"
	got := runstore.Short(in)
	if strings.Contains(got, "abc123SECRET") || strings.Contains(got, "ABCDEFGHIJKLMNOPQRST") || strings.Contains(got, "second") {
		t.Errorf("Short(%q) = %q: secret or second line kept", in, got)
	}
	if !strings.Contains(got, "run with") {
		t.Errorf("Short dropped plain text: %q", got)
	}
	if n := len([]rune(runstore.Short(strings.Repeat("x", 500)))); n > 200 {
		t.Errorf("Short must clip to 200 runes, got %d", n)
	}
	for _, n := range []int{8000, 1 << 20} {
		big := strings.Repeat("token=a", n/7) + strings.Repeat("A", n%7)
		start := time.Now()
		_ = runstore.Redact(big)
		if d := time.Since(start); d > 2*time.Second {
			t.Errorf("Redact of %d chars took %v", n, d)
		}
	}
}

// TestSweep_KeepsLiveRuns: finished runs older than the age go; live runs and
// fresh finished runs stay.
func TestSweep_KeepsLiveRuns(t *testing.T) {
	s, err := runstore.New(newProject(t))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	old := now.Add(-10 * 24 * time.Hour)
	mk := func(id string, end *time.Time) {
		rf, err := s.CreateRun(id)
		if err != nil {
			t.Fatal(err)
		}
		_ = rf.WriteStart(old)
		if end != nil {
			_ = rf.Finish(0, *end)
		}
	}
	mk("dlg-000000000001", &old) // finished long ago: swept
	mk("dlg-000000000002", nil)  // live, old start: kept
	fresh := now.Add(-time.Hour)
	mk("dlg-000000000003", &fresh) // finished recently: kept
	removed, err := s.Sweep(now, 7*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || removed[0] != "dlg-000000000001" {
		t.Fatalf("Sweep removed %v, want only the old finished run", removed)
	}
	for id, want := range map[string]bool{"dlg-000000000001": false, "dlg-000000000002": true, "dlg-000000000003": true} {
		_, err := os.Stat(filepath.Join(s.Root(), "runs", id))
		if (err == nil) != want {
			t.Errorf("%s present=%v, want %v", id, err == nil, want)
		}
	}
}

// TestGitignore_IgnoresDelegate: after a run, its mail and events, git status
// in the project shows nothing under .wipnote/delegate/, while a work item
// beside it still shows.
func TestGitignore_IgnoresDelegate(t *testing.T) {
	dir := newProject(t)
	git := func(args ...string) string {
		out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	git("init", "-q")
	worktree.EnsureWipnoteGitignore(dir)
	s, _ := runstore.New(dir)
	writeSampleRun(t, s)
	_ = os.MkdirAll(filepath.Join(dir, ".wipnote", "features"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, ".wipnote", "features", "feat-x.html"), []byte("<p>"), 0o644)
	status := git("status", "--porcelain", "--untracked-files=all")
	if strings.Contains(status, ".wipnote/delegate") {
		t.Errorf("git status lists delegate files:\n%s", status)
	}
	if !strings.Contains(status, "feat-x.html") {
		t.Errorf("a work item must stay visible:\n%s", status)
	}
}

func writeSampleRun(t *testing.T, s *runstore.Store) {
	t.Helper()
	rf, err := s.CreateRun(testRun)
	if err != nil {
		t.Fatal(err)
	}
	_ = rf.WriteStart(time.Now())
	_ = rf.WriteFile(runstore.FileRawLog, []byte("raw\n"))
	ew, _ := rf.OpenEvents("caller")
	_ = ew.Append(runstore.Event{Kind: runstore.KindStop})
	ew.Close()
	mb, err := s.Mailbox("worker-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mb.Post(runstore.Inbox, runstore.Message{From: "human", To: "worker-1", Body: "hi"}); err != nil {
		t.Fatal(err)
	}
	if err := rf.Finish(0, time.Now()); err != nil {
		t.Fatal(err)
	}
}
