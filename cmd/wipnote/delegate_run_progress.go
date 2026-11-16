package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/shakestzd/wipnote/internal/delegate/oneshot"
	"github.com/shakestzd/wipnote/internal/delegate/render"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// progressPoll is how often --progress reads the run's clean.md.
const progressPoll = time.Second

// runProgress is delegate run --progress: one key=value line when the run
// folder exists, one per completed tool step and one when the run ends, in
// place of the report, so Claude Code's Monitor tool can follow a long
// one-shot run step by step (feat-dd556710).
type runProgress struct {
	mu     sync.Mutex
	w      io.Writer
	rf     *runstore.RunFolder
	cancel context.CancelFunc
	done   chan struct{}
}

func newRunProgress(w io.Writer) *runProgress { return &runProgress{w: w} }

func (p *runProgress) emit(line string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, _ = io.WriteString(p.w, line+"\n")
}

// start is oneshot's OnRun: it prints the started line and follows the run.
func (p *runProgress) start(rf *runstore.RunFolder) {
	if p.rf != nil {
		return
	}
	p.rf = rf
	p.emit(fmt.Sprintf("run=%s state=started", rf.ID()))
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel, p.done = cancel, make(chan struct{})
	go func() {
		defer close(p.done)
		render.Progress(ctx, rf, progressPoll, func(part render.Part) { p.emit(render.StepLine(rf.ID(), part)) })
	}()
}

// finish stops the follower, which reports the last steps, then prints the
// done line, or a failed line when the run returned an error.
func (p *runProgress) finish(res oneshot.Result, err error) {
	if p.cancel != nil {
		p.cancel()
		<-p.done
	}
	id := res.RunID
	if p.rf != nil {
		id = p.rf.ID()
	}
	if id == "" {
		id = "-"
	}
	if err != nil {
		p.emit(fmt.Sprintf("run=%s state=failed error=%q", id, strings.Join(strings.Fields(err.Error()), " ")))
		return
	}
	line := fmt.Sprintf("run=%s state=done exit=%d end=%s", id, res.Exit, res.EndReason)
	if p.rf != nil {
		if answer := filepath.Join(p.rf.Dir(), runstore.FileLastMD); nonEmptyFile(answer) {
			line += " answer=" + answer
		}
		if page := filepath.Join(p.rf.Dir(), runstore.FileRunHTML); nonEmptyFile(page) {
			line += " page=" + page
		}
	}
	p.emit(line)
}

func nonEmptyFile(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Size() > 0
}
