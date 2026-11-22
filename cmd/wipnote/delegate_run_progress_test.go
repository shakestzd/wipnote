package main

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/internal/delegate/oneshot"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

func progressRun(t *testing.T) *runstore.RunFolder {
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

// --progress prints a started line, the steps, and a done line naming the
// exit status, the answer and the run page.
func TestRunProgress_StartedStepsDone(t *testing.T) {
	rf := progressRun(t)
	var out bytes.Buffer
	p := newRunProgress(&out)
	p.start(rf)
	if err := rf.WriteFile(runstore.FileCleanMD, []byte("<!-- fold closed: Bash: ls · exit 0 -->\n**Bash** `ls`\n<!-- /fold -->\n")); err != nil {
		t.Fatal(err)
	}
	if err := rf.WriteFile(runstore.FileLastMD, []byte("the answer\n")); err != nil {
		t.Fatal(err)
	}
	if err := rf.WriteFile(runstore.FileRunHTML, []byte("<html></html>")); err != nil {
		t.Fatal(err)
	}
	p.finish(oneshot.Result{RunID: rf.ID(), Exit: 2, EndReason: "exit"}, nil)
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("lines = %q", lines)
	}
	if lines[0] != "run=dlg-0123456789abcdef state=started" {
		t.Errorf("started = %s", lines[0])
	}
	if lines[1] != `run=dlg-0123456789abcdef state=step step=ok title="Bash: ls · exit 0"` {
		t.Errorf("step = %s", lines[1])
	}
	want := "run=dlg-0123456789abcdef state=done exit=2 end=exit answer=" + filepath.Join(rf.Dir(), runstore.FileLastMD) +
		" page=" + filepath.Join(rf.Dir(), runstore.FileRunHTML)
	if lines[2] != want {
		t.Errorf("done = %s\nwant %s", lines[2], want)
	}
}

// A run that never got a folder still ends with one failed line.
func TestRunProgress_FailedBeforeTheRunFolder(t *testing.T) {
	var out bytes.Buffer
	p := newRunProgress(&out)
	p.finish(oneshot.Result{}, errors.New("refused: \"rm\" is not a delegate tool"))
	if got := strings.TrimSpace(out.String()); got != `run=- state=failed error="refused: \"rm\" is not a delegate tool"` {
		t.Fatalf("line = %s", got)
	}
}

// A run with no answer leaves answer= out of the done line.
func TestRunProgress_DoneWithoutAnswer(t *testing.T) {
	rf := progressRun(t)
	var out bytes.Buffer
	p := newRunProgress(&out)
	p.start(rf)
	p.finish(oneshot.Result{RunID: rf.ID(), Exit: 1, EndReason: "exit"}, nil)
	if got := strings.TrimSpace(out.String()); !strings.HasSuffix(got, "state=done exit=1 end=exit") {
		t.Fatalf("out = %s", got)
	}
}
