package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Script tests for the before-and-after check. They put stub commands first on
// PATH so no model runs and no real build happens.

const promptABScript = "../../scripts/prompt-ab.sh"

type abStubs struct {
	goodRun    bool   // the stub worker writes a run record
	compareRC  string // exit code of the stubbed comparator
	failRemove bool   // the git wrapper fails `worktree remove`
}

func writeStub(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
}

// writeABStubs writes the go, wipnote, python3 and git stubs and returns the git log path.
func writeABStubs(t *testing.T, st abStubs, stubs, raw, realPy, realGit, work string) string {
	t.Helper()
	writeStub(t, stubs, "go", `[ "$1" = build ] && { shift; [ "$1" = -o ] && : > "$2"; }; exit 0`+"\n")
	writeStub(t, stubs, "wipnote", `case "$1 $2" in
"feature create") echo feat-aaaa1111 ;;
"delegate run") if [ -f "`+raw+`" ]; then mkdir -p .wipnote/delegate/runs/r1; cp "`+raw+`" .wipnote/delegate/runs/r1/raw.log; fi ;;
esac
exit 0
`)
	writeStub(t, stubs, "python3", `if [ "$2" = compare ]; then echo stub-compare; exit `+st.compareRC+`; fi
exec "`+realPy+`" "$@"
`)
	log := filepath.Join(work, "git.log")
	fail := ""
	if st.failRemove {
		fail = `case "$*" in *"worktree remove"*) exit 1 ;; esac` + "\n"
	}
	writeStub(t, stubs, "git", `echo "$*" >> "`+log+`"`+"\n"+fail+`exec "`+realGit+`" "$@"`+"\n")
	return log
}

// runABScript runs the script with stubs and returns exit code, stderr and the git call log.
func runABScript(t *testing.T, st abStubs, out string) (int, string, string) {
	t.Helper()
	needPython(t)
	realPy, _ := exec.LookPath("python3")
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	stubs, work := t.TempDir(), t.TempDir()
	raw := filepath.Join(work, "raw.log")
	if st.goodRun {
		line := tu("Bash", map[string]any{"command": "wipnote feature start feat-aaaa1111"})
		if err := os.WriteFile(raw, []byte(line+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	log := writeABStubs(t, st, stubs, raw, realPy, realGit, work)
	t.Cleanup(func() {
		for _, s := range []string{"wt-old", "wt-new"} {
			exec.Command(realGit, "worktree", "remove", "--force", filepath.Join(out, s)).Run()
		}
		exec.Command(realGit, "worktree", "prune").Run()
	})
	cmd := exec.Command("bash", promptABScript, "--old", "HEAD", "--new", "HEAD", "--runs", "1", "--only", "add-func", "--out", out)
	cmd.Env = append(os.Environ(), "PATH="+stubs+":"+os.Getenv("PATH"))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err = cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	logged, _ := os.ReadFile(log)
	return code, stderr.String(), string(logged)
}

// A failed run forces exit 2 even when the comparator returns 0.
func TestPromptABScriptFailedRunExits2(t *testing.T) {
	code, stderr, _ := runABScript(t, abStubs{goodRun: false, compareRC: "0"}, filepath.Join(t.TempDir(), "o"))
	if code != 2 {
		t.Errorf("exit = %d, want 2\n%s", code, stderr)
	}
}

// With every run finished the comparator's code stands, including 1 for a regression.
func TestPromptABScriptFinishedRunsKeepComparatorCode(t *testing.T) {
	for rc, want := range map[string]int{"0": 0, "1": 1} {
		code, stderr, _ := runABScript(t, abStubs{goodRun: true, compareRC: rc}, filepath.Join(t.TempDir(), "o"))
		if code != want {
			t.Errorf("comparator %s: exit = %d, want %d\n%s", rc, code, want, stderr)
		}
	}
}

// --out inside the source checkout is refused before anything is written.
func TestPromptABScriptRefusesOutInsideCheckout(t *testing.T) {
	root, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Skip("not in a git checkout")
	}
	inside := filepath.Join(strings.TrimSpace(string(root)), "prompt-ab-refuse-test", "deep")
	cmd := exec.Command("bash", promptABScript, "--old", "HEAD", "--new", "HEAD", "--out", inside, "--dry-run")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err = cmd.Run()
	ee, ok := err.(*exec.ExitError)
	if !ok || ee.ExitCode() != 2 {
		t.Errorf("err = %v, want exit 2\n%s", err, stderr.String())
	}
	if _, statErr := os.Stat(filepath.Dir(inside)); statErr == nil {
		t.Error("the refused --out folder was created")
	}
}

// A worktree that cleanup cannot remove is named on stderr, and prune runs at the end.
func TestPromptABScriptReportsCleanupFailure(t *testing.T) {
	out := filepath.Join(t.TempDir(), "o")
	_, stderr, log := runABScript(t, abStubs{goodRun: true, compareRC: "0", failRemove: true}, out)
	for _, s := range []string{"wt-old", "wt-new"} {
		if !strings.Contains(stderr, "warning") || !strings.Contains(stderr, filepath.Join(out, s)) {
			t.Errorf("stderr does not warn about %s:\n%s", s, stderr)
		}
	}
	if !strings.Contains(log, "worktree prune") {
		t.Errorf("git worktree prune did not run:\n%s", log)
	}
}
