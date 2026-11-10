package render

import (
	"bytes"
	"context"
	"github.com/charmbracelet/x/ansi"
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

// TestView_ShowsPendingAsks: an interactive run's view prints each pending
// ask once when it appears (D7), and prints nothing when there is none.
func TestView_ShowsPendingAsks(t *testing.T) {
	rf := newRun(t)
	var mu sync.Mutex
	asks := []string{}
	out := &lockedBuffer{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- View(ctx, rf, out, ViewOptions{Poll: 5 * time.Millisecond, Asks: func() []string {
			mu.Lock()
			defer mu.Unlock()
			return append([]string(nil), asks...)
		}})
	}()
	time.Sleep(30 * time.Millisecond)
	if strings.Contains(out.String(), "pending") {
		t.Fatalf("no ask, yet: %q", out.String())
	}
	mu.Lock()
	asks = []string{"msg-0123456789abcdef-q1: Which branch?"}
	mu.Unlock()
	waitFor(t, "the ask", func() bool { return strings.Contains(out.String(), "Which branch?") })
	time.Sleep(30 * time.Millisecond)
	if strings.Count(out.String(), "Which branch?") != 1 {
		t.Fatalf("ask printed more than once: %q", out.String())
	}
	cancel()
	<-done
}

func runStyled(t *testing.T, colour *bool) string {
	t.Helper()
	rf := newRun(t)
	if err := rf.WriteStart(time.Now()); err != nil {
		t.Fatal(err)
	}
	write(t, rf, runstore.FileCleanMD, "### Assistant\n\nsome **bold** text\n<!-- fold closed: Bash: ls · exit 0 · 1 lines -->\n**Bash** `ls`\n<!-- /fold -->\n```\n**raw**\n")
	finishRun(t, rf, "the answer", "input 1")
	var out bytes.Buffer
	opts := ViewOptions{Poll: time.Millisecond, Color: colour, Width: func() int { return 0 }}
	if err := View(context.Background(), rf, &out, opts); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

// Styling is off for a non-terminal writer and when forced off, and the
// output is then the raw Markdown; forced on it is styled.
func TestView_StyleOnlyWhenColour(t *testing.T) {
	off := false
	on := true
	plainOut, forcedOff, styled := runStyled(t, nil), runStyled(t, &off), runStyled(t, &on)
	if plainOut != forcedOff || strings.Contains(plainOut, "\x1b[") {
		t.Errorf("non-terminal output was styled:\n%q", plainOut)
	}
	for _, want := range []string{"### Assistant", "some **bold** text", "✓ Bash: ls"} {
		if !strings.Contains(plainOut, want) {
			t.Errorf("raw output lacks %q", want)
		}
	}
	if !strings.Contains(styled, "\x1b[") || strings.Contains(styled, "### ") ||
		!strings.Contains(ansi.Strip(styled), "✓ Bash: ls") || !strings.Contains(ansi.Strip(styled), "  **raw**") {
		t.Errorf("styled output wrong:\n%q", styled)
	}
}

func runAnswer(t *testing.T, colour bool, clean, answer string) string {
	t.Helper()
	rf := newRun(t)
	if err := rf.WriteStart(time.Now()); err != nil {
		t.Fatal(err)
	}
	if clean != "" {
		write(t, rf, runstore.FileCleanMD, clean)
	}
	finishRun(t, rf, answer, "input 1")
	var out bytes.Buffer
	opts := ViewOptions{Poll: time.Millisecond, Color: &colour, Width: func() int { return 0 }}
	if err := View(context.Background(), rf, &out, opts); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestView_AnswerStyledWithColour(t *testing.T) {
	got := runAnswer(t, true, "### Hi\n", "## Result\n\nsome **bold** text")
	i := strings.Index(got, "answer ---")
	if i < 0 || !strings.Contains(got[:i], "\x1b[") {
		t.Fatalf("divider not styled:\n%q", got)
	}
	ans := got[i:]
	if !strings.Contains(ans, "\x1b[") || strings.Contains(ans, "## ") || strings.Contains(ans, "**") {
		t.Errorf("answer not styled:\n%q", ans)
	}
}

func TestView_AnswerPassthroughWithoutColour(t *testing.T) {
	got := runAnswer(t, false, "", "## Result\n\nsome **bold** text")
	if !strings.Contains(got, "\n--- answer ---\n## Result\n\nsome **bold** text\n") || strings.Contains(got, "\x1b") {
		t.Errorf("answer changed:\n%q", got)
	}
}

func TestView_OpenFenceDoesNotLeakIntoAnswer(t *testing.T) {
	got := runAnswer(t, true, "```\ncode\n", "plain answer")
	ans := got[strings.Index(got, "answer ---"):]
	line := strings.Split(ans, "\n")[1]
	if strings.Contains(line, "\x1b[") || line != "plain answer" {
		t.Errorf("fence leaked into answer: %q", line)
	}
}

func TestView_StripsControlSequencesBothModes(t *testing.T) {
	evil := "a\x1b]0;title\x07b\x1b[31mc\u009b31md\x07e\tf\rg\u0085h"
	for _, colour := range []bool{false, true} {
		got := runAnswer(t, colour, evil+"\nsecond\n", evil)
		plain := ansi.Strip(got)
		if strings.Contains(got, "title") || strings.ContainsAny(plain, "\x07\r\u009b\u0085") {
			t.Errorf("colour=%v controls leaked:\n%q", colour, got)
		}
		if !strings.Contains(strings.ReplaceAll(plain, "    ", "\t"), "abcde\tfgh\nsecond") {
			t.Errorf("colour=%v text lost:\n%q", colour, plain)
		}
	}
}

func TestView_LogFallbackStyledOnlyIfMarkdown(t *testing.T) {
	for _, tc := range []struct {
		log    string
		styled bool
	}{{"> ls\n  = exit 0: a b\n", false}, {"## Notes\nsome **bold**\n", true}} {
		rf := newRun(t)
		write(t, rf, runstore.FileCleanLog, tc.log)
		if err := rf.Finish(0, time.Now()); err != nil {
			t.Fatal(err)
		}
		on := true
		var out bytes.Buffer
		if err := View(context.Background(), rf, &out, ViewOptions{Poll: time.Millisecond, Color: &on, Width: func() int { return 0 }}); err != nil {
			t.Fatal(err)
		}
		tail := out.String()[strings.Index(out.String(), "end of clean.log"):]
		if got := strings.Contains(tail, "\x1b["); got != tc.styled {
			t.Errorf("log %q: styled=%v want %v\n%q", tc.log, got, tc.styled, tail)
		}
	}
}
