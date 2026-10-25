package render

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// lockedBuffer is a bytes.Buffer safe to read while View writes to it.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func newRun(t *testing.T) *runstore.RunFolder {
	t.Helper()
	store, err := runstore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rf, err := store.CreateRun("dlg-0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	return rf
}

func write(t *testing.T, rf *runstore.RunFolder, name, text string) {
	t.Helper()
	if err := rf.WriteFile(name, []byte(text)); err != nil {
		t.Fatal(err)
	}
}

// waitFor polls cond for up to two seconds.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

// finishRun ends a run as the pane script does: answer, usage, then exit.
func finishRun(t *testing.T, rf *runstore.RunFolder, answer, usage string) {
	t.Helper()
	write(t, rf, runstore.FileLastMD, answer)
	if err := rf.WriteUsage(usage); err != nil {
		t.Fatal(err)
	}
	if err := rf.Finish(0, time.Now()); err != nil {
		t.Fatal(err)
	}
}

// The viewer prints clean.md as it grows, keeps following while the run is
// live, and returns once the exit file appears, with the status and answer.
func TestView_FollowsToExit(t *testing.T) {
	rf := newRun(t)
	write(t, rf, runstore.FileLabel, "cdx: list the files")
	if err := rf.WriteStart(time.Now()); err != nil {
		t.Fatal(err)
	}
	md := "### Assistant\n\nI will list the files.\n\n"
	write(t, rf, runstore.FileCleanMD, md)
	out := &lockedBuffer{}
	done := make(chan error, 1)
	go func() { done <- View(context.Background(), rf, out, ViewOptions{Poll: 5 * time.Millisecond}) }()

	waitFor(t, "the first prose", func() bool { return strings.Contains(out.String(), "I will list the files.") })
	md += "<!-- fold closed: Bash: ls · exit 0 · 2 lines -->\n**Bash** `ls`\n\n```\na\nb\n```\n<!-- /fold -->\n\npartial"
	write(t, rf, runstore.FileCleanMD, md)
	waitFor(t, "the fold", func() bool { return strings.Contains(out.String(), "✓ Bash: ls · exit 0 · 2 lines") })
	time.Sleep(30 * time.Millisecond)
	select {
	case err := <-done:
		t.Fatalf("View returned before the exit file: %v", err)
	default:
	}
	if strings.Contains(out.String(), "partial") {
		t.Error("a partial line was printed before its newline")
	}
	finishRun(t, rf, "the answer", "input 5 (cached 0), output 1")
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("View did not return after the exit file appeared")
	}
	got := out.String()
	for _, want := range []string{"cdx: list the files", "partial", "finished in", "input 5 (cached 0), output 1", "--- answer ---\nthe answer"} {
		if !strings.Contains(got, want) {
			t.Errorf("view output lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "<!--") {
		t.Errorf("fold markers reached the terminal:\n%s", got)
	}
}

// Without clean.md the viewer follows clean.log; without an answer it shows
// the end of clean.log. A failed fold shows its output.
func TestView_FallbacksAndFailedFold(t *testing.T) {
	rf := newRun(t)
	write(t, rf, runstore.FileCleanLog, "> ls\n  = exit 0: a b\n")
	if err := rf.Finish(1, time.Now()); err != nil {
		t.Fatal(err)
	}
	out := &lockedBuffer{}
	if err := View(context.Background(), rf, out, ViewOptions{Poll: time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "> ls") || !strings.Contains(got, "failed with exit 1") || !strings.Contains(got, "--- end of clean.log ---") {
		t.Errorf("fallback output:\n%s", got)
	}

	rf2 := newRun(t)
	write(t, rf2, runstore.FileCleanMD, "<!-- fold open: Bash: cat x · exit 1 · 1 line -->\n**Bash** `cat x`\n\n```\nnope\n```\n<!-- /fold -->\n")
	if err := rf2.Finish(1, time.Now()); err != nil {
		t.Fatal(err)
	}
	out2 := &lockedBuffer{}
	if err := View(context.Background(), rf2, out2, ViewOptions{Poll: time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out2.String(), "✗ Bash: cat x · exit 1 · 1 line\n    nope") {
		t.Errorf("failed fold output:\n%s", out2.String())
	}
}

// Cancelling the context stops a viewer of a run that never ends.
func TestView_StopsOnCancel(t *testing.T) {
	rf := newRun(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- View(ctx, rf, &lockedBuffer{}, ViewOptions{Poll: time.Millisecond}) }()
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Error("want the context error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("View ignored the cancelled context")
	}
}
