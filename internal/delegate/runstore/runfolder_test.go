package runstore_test

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

func modDir(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "mods", "codex-herdr-pane")
}

// modReadNames lists the run-folder files the mod's run follower
// (hooks/runstate.ts) and pane viewer (viewer/codex_view.py) read.
func modReadNames(t *testing.T) []string {
	t.Helper()
	pats := map[string]*regexp.Regexp{
		"hooks/runstate.ts":    regexp.MustCompile(`\$\{r\.dir\}/([A-Za-z0-9._-]+)`),
		"viewer/codex_view.py": regexp.MustCompile(`(?:self\.dir|run_dir) / "([A-Za-z0-9._-]+)"`),
	}
	seen := map[string]bool{}
	var names []string
	for rel, re := range pats {
		src, err := os.ReadFile(filepath.Join(modDir(t), rel))
		if err != nil {
			t.Fatalf("read mod source %s: %v", rel, err)
		}
		for _, m := range re.FindAllStringSubmatch(string(src), -1) {
			if !seen[m[1]] {
				seen[m[1]] = true
				names = append(names, m[1])
			}
		}
	}
	if len(names) < 8 {
		t.Fatalf("found only %v in the mod sources; the patterns no longer match", names)
	}
	return names
}

// TestRunFolder_MatchesModContract: every file the mod reads is a contract
// file Go writes, and a Go-written folder parses the way the mod parses it.
func TestRunFolder_MatchesModContract(t *testing.T) {
	contract := map[string]bool{}
	for _, n := range runstore.ContractFiles {
		contract[n] = true
	}
	for _, n := range modReadNames(t) {
		if !contract[n] {
			t.Errorf("the mod reads %q, which is not in runstore.ContractFiles", n)
		}
	}
	for _, n := range []string{"raw.log", "clean.log", "clean.md", "last.md", "usage", "start", "end", "exit", "label", "host", "run.html"} {
		if !contract[n] {
			t.Errorf("feat-a1df60c2 file %q missing from ContractFiles", n)
		}
	}
	rf := writeFixtureRun(t)
	assertModParses(t, rf.Dir())
	info, err := rf.Read()
	if err != nil || !info.Finished || info.Exit != 0 || info.Host != runstore.HostHerdr || info.Label != "codex exec fixture" {
		t.Fatalf("Read = %+v, %v", info, err)
	}
}

func writeFixtureRun(t *testing.T) *runstore.RunFolder {
	t.Helper()
	_, rf := newRun(t)
	raw, err := os.ReadFile(filepath.Join(modDir(t), "test", "fixtures", "run1.raw.log"))
	if err != nil {
		t.Fatal(err)
	}
	start := time.Unix(1_760_000_000, 0)
	steps := []error{
		rf.WriteLabel("codex exec fixture"),
		rf.WriteHost(runstore.HostHerdr),
		rf.WriteStart(start),
		rf.WriteFile(runstore.FileRawLog, raw),
		rf.WriteFile(runstore.FileCleanLog, []byte("clean\n")),
		rf.WriteFile(runstore.FileCleanMD, []byte("# run\n")),
		rf.WriteFile(runstore.FileLastMD, []byte("the answer\n")),
		rf.WriteUsage("12 in · 3 out"),
		rf.Finish(0, start.Add(75*time.Second)),
	}
	for i, err := range steps {
		if err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}
	if err := rf.WriteHost("bogus"); err == nil {
		t.Error("WriteHost must refuse a host other than claude, herdr or none")
	}
	return rf
}

// assertModParses reads the folder as runstate.ts and codex_view.py do:
// trimmed text, Number()/float() for start and end, the exit code as text.
func assertModParses(t *testing.T, dir string) {
	t.Helper()
	read := func(n string) string {
		b, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			t.Fatalf("the mod reads %s: %v", n, err)
		}
		return strings.TrimSpace(string(b))
	}
	if read("exit") != "0" || read("host") != "herdr" || read("label") != "codex exec fixture" {
		t.Errorf("exit/host/label = %q/%q/%q", read("exit"), read("host"), read("label"))
	}
	s, err1 := strconv.ParseFloat(read("start"), 64)
	e, err2 := strconv.ParseFloat(read("end"), 64)
	if err1 != nil || err2 != nil || e-s != 75 {
		t.Errorf("start/end not unix seconds: %q %q", read("start"), read("end"))
	}
	if read("usage") != "12 in · 3 out" || read("last.md") != "the answer" {
		t.Errorf("usage/last.md = %q/%q", read("usage"), read("last.md"))
	}
	if _, err := os.Stat(filepath.Join(dir, "exit.tmp")); err == nil {
		t.Error("exit.tmp left behind")
	}
}

// TestRunFolder_ExitEndsTheRun: a run is live until exit exists, and end is
// written before exit so a reader that sees exit also sees end.
func TestRunFolder_ExitEndsTheRun(t *testing.T) {
	_, rf := newRun(t)
	_ = rf.WriteStart(time.Now())
	if info, _ := rf.Read(); info.Finished {
		t.Fatal("a run with no exit file is live")
	}
	if err := rf.Finish(3, time.Now()); err != nil {
		t.Fatal(err)
	}
	info, _ := rf.Read()
	if !info.Finished || info.Exit != 3 || info.End.IsZero() {
		t.Fatalf("after Finish: %+v", info)
	}
	if err := rf.WriteFile("../escape", []byte("x")); err == nil {
		t.Error("WriteFile must refuse a name outside the contract")
	}
}
