package runledger

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestStore_AppendIsAppendOnlyAndRoundTrips(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".wipnote")
	s := NewStore(dir)
	start := startEvent("s1", "tu1", KindShell, `gate "all" <modules> & more`)
	if err := s.Append(start); err != nil {
		t.Fatalf("append start: %v", err)
	}
	before, _ := os.ReadFile(s.ShardPath("s1"))
	if err := s.Append(endEvent("s1", "tu1", StatusCompleted, intp(0), t0.Add(1))); err != nil {
		t.Fatalf("append end: %v", err)
	}
	after, _ := os.ReadFile(s.ShardPath("s1"))
	// Every earlier row line survives byte for byte: an append never rewrites.
	rows := strings.Split(string(before), "\n")
	for _, line := range rows {
		if strings.HasPrefix(line, "<tr") && !bytes.Contains(after, []byte(line)) {
			t.Fatalf("earlier row rewritten: %s", line)
		}
	}
	events, err := s.ReadAll()
	if err != nil || len(events) != 2 {
		t.Fatalf("ReadAll = %d events, err %v", len(events), err)
	}
	if events[0].Description != start.Description || events[0].Exit != nil {
		t.Errorf("start round trip = %+v", events[0])
	}
	if events[1].Exit == nil || *events[1].Exit != 0 || events[1].Phase != PhaseEnd {
		t.Errorf("end round trip = %+v", events[1])
	}
}

func TestStore_OneShardPerSession(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), ".wipnote"))
	s.Append(startEvent("s1", "tu1", KindShell, "a"))
	s.Append(startEvent("s2", "tu2", KindShell, "b"))
	if s.ShardPath("s1") == s.ShardPath("s2") {
		t.Fatal("two sessions share a shard")
	}
	if s.ShardPath("../x") != filepath.Join(s.Dir(), "sess----x.html") {
		t.Errorf("unsafe session id not folded: %s", s.ShardPath("../x"))
	}
	recs, err := s.Records(never)
	if err != nil || len(recs) != 2 {
		t.Fatalf("Records = %d, err %v", len(recs), err)
	}
}

func TestStore_MissingLedgerReadsEmpty(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), ".wipnote"))
	if ev, err := s.ReadAll(); err != nil || len(ev) != 0 {
		t.Fatalf("ReadAll on empty project = %v, %v", ev, err)
	}
	if _, err := os.Stat(s.Dir()); !os.IsNotExist(err) {
		t.Error("a read must not create the runs directory")
	}
}

func TestStore_RefusesHostPathsAndBadRows(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), ".wipnote"))
	bad := startEvent("s1", "tu1", KindShell, "x")
	bad.Command = "cd /home/alice/proj && make"
	if err := s.Append(bad); err == nil {
		t.Error("host path in command was accepted")
	}
	bad = startEvent("s1", "tu1", "daemon", "x")
	if err := s.Append(bad); err == nil {
		t.Error("unknown kind was accepted")
	}
	bad = startEvent("s1", "tu1", KindShell, "x")
	bad.RunID = "dlg-123"
	if err := s.Append(bad); err == nil {
		t.Error("bad run id was accepted")
	}
	if ev, _ := s.ReadAll(); len(ev) != 0 {
		t.Errorf("refused rows were written: %d", len(ev))
	}
}

func TestClean_RelativizesAndRedacts(t *testing.T) {
	root := "/srv/checkout/proj"
	got := Clean("cd /srv/checkout/proj/core && go test; cat /home/alice/notes.txt", root)
	if strings.Contains(got, "/srv/checkout") || strings.Contains(got, "/home/alice") {
		t.Errorf("Clean left a host path: %q", got)
	}
	if !strings.Contains(got, "cd ./core && go test") {
		t.Errorf("Clean did not relativize the project root: %q", got)
	}
	if Clean("go vet ./...", root) != "go vet ./..." {
		t.Error("Clean changed a command with no paths")
	}
}

func TestStoredPath(t *testing.T) {
	root := "/srv/proj"
	if got := StoredPath("/srv/proj/.wipnote/logs/runs/r.log", root); got != ".wipnote/logs/runs/r.log" {
		t.Errorf("inside project = %q", got)
	}
	if got := StoredPath("/tmp/h-x/-srv-proj/abc/tasks/b1.output", root); got != "…/tasks/b1.output" {
		t.Errorf("outside project = %q", got)
	}
	if got := Folder("/srv/proj", root); got != "." {
		t.Errorf("folder of root = %q", got)
	}
	if got := Folder("/srv/wt-1", root); got != "../wt-1" {
		t.Errorf("sibling worktree folder = %q", got)
	}
}

func TestHeadCommit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := t.TempDir()
	git := func(args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", repo, "-c", "user.email=t@t", "-c", "user.name=t"}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	if HeadCommit(repo) != "" {
		t.Error("non-repo must have no commit")
	}
	git("init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(repo, "f"), []byte("x"), 0o644)
	git("add", "f")
	git("commit", "-q", "-m", "one")
	want := git("rev-parse", "HEAD")
	sub := filepath.Join(repo, "sub")
	os.MkdirAll(sub, 0o755)
	if got := HeadCommit(sub); got != want {
		t.Errorf("loose ref HEAD from a subfolder = %q, want %q", got, want)
	}
	git("pack-refs", "--all")
	if got := HeadCommit(repo); got != want {
		t.Errorf("packed ref HEAD = %q, want %q", got, want)
	}
	wt := filepath.Join(t.TempDir(), "wt")
	git("worktree", "add", "-q", "-b", "side", wt)
	if got := HeadCommit(wt); got != want {
		t.Errorf("worktree HEAD = %q, want %q", got, want)
	}
}
