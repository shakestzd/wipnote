package oneshot

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/shakestzd/wipnote/internal/delegate/render"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// Job is what one run executes. The caller writes it to job.json, so a pane
// worker runs exactly what a headless run would (the mod's orig.sh and
// pane.sh, as data instead of a generated script).
type Job struct {
	Kind       Kind     `json:"kind"`
	Argv       []string `json:"argv"` // the final command (Recipe.Command)
	Cwd        string   `json:"cwd"`
	OwnLast    string   `json:"own_last,omitempty"` // codex exec's own -o file
	CcxRuns    string   `json:"ccx_runs,omitempty"`
	CcxWaitMS  int64    `json:"ccx_wait_ms,omitempty"`
	Pane       string   `json:"pane,omitempty"`
	Registry   string   `json:"registry,omitempty"` // tile registry the pane is listed in
	ClosePane  string   `json:"close_pane,omitempty"`
	CloseDelay int      `json:"close_delay,omitempty"`
	// Container runs the job in the project's devcontainer (slice 21).
	Container *ContainerJob `json:"container,omitempty"`
}

func writeJob(rf *runstore.RunFolder, j Job) error {
	data, err := json.MarshalIndent(j, "", "  ")
	if err != nil {
		return err
	}
	return rf.WriteFile(runstore.FileJob, append(data, '\n'))
}

func readJob(rf *runstore.RunFolder) (Job, error) {
	data, _, err := rf.ReadFrom(runstore.FileJob, 0)
	if err != nil {
		return Job{}, err
	}
	var j Job
	if err := json.Unmarshal(data, &j); err != nil || len(j.Argv) == 0 {
		return Job{}, fmt.Errorf("delegate run: unreadable %s: %v", runstore.FileJob, err)
	}
	return j, nil
}

// Execute runs the job's tool, writes the run folder as the mod's pane
// script does (start, raw.log, clean.log, clean.md, last.md, usage, then end
// and exit), and returns the tool's exit status. stdin is /dev/null, as for
// the mod's background run.
func Execute(ctx context.Context, rf *runstore.RunFolder, job Job) int {
	_ = rf.WriteStart(time.Now())
	_ = rf.WriteFile(runstore.FileCleanMD, nil)
	_ = rf.WriteFile(runstore.FileCleanLog, nil)
	if job.Container != nil {
		inside, ok := executeContainer(ctx, rf, job)
		if !ok {
			_ = rf.Finish(1, time.Now())
			return 1
		}
		job = inside
	}
	var exit int
	switch job.Kind {
	case KindCcx:
		exit = runCcx(ctx, rf, job)
	case KindCodexReview:
		exit = runReview(ctx, rf, job)
	default:
		exit = runMerged(ctx, rf, job)
	}
	_ = rf.Finish(exit, time.Now())
	return exit
}

// runLogs are the run folder's live logs, appended to by one writer.
type runLogs struct{ raw, log, md *os.File }

func openLogs(rf *runstore.RunFolder) (*runLogs, error) {
	var l runLogs
	for _, f := range []struct {
		dst  **os.File
		name string
	}{{&l.raw, runstore.FileRawLog}, {&l.log, runstore.FileCleanLog}, {&l.md, runstore.FileCleanMD}} {
		file, err := appendFile(rf, f.name)
		if err != nil {
			l.close()
			return nil, err
		}
		*f.dst = file
	}
	return &l, nil
}

func appendFile(rf *runstore.RunFolder, name string) (*os.File, error) {
	return os.OpenFile(filepath.Join(rf.Dir(), name), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
}

func (l *runLogs) close() {
	for _, f := range []*os.File{l.raw, l.log, l.md} {
		if f != nil {
			_ = f.Close()
		}
	}
}

// note is the mod's note(): one italic line in clean.log and clean.md.
func (l *runLogs) note(s string) {
	_, _ = l.log.WriteString("*" + s + "*\n")
	_, _ = l.md.WriteString("*" + s + "*\n")
}

// lineFilter is a render filter fed one raw line at a time.
type lineFilter interface {
	Line(raw string) (log, md string)
	flush() (log, md string)
}

type codexLines struct{ *render.CodexFilter }

func (c codexLines) flush() (string, string) { return c.Close() }

type streamLines struct{ *render.StreamFilter }

func (s streamLines) flush() (string, string) { return "", s.Close() }

// pump copies r into raw.log untouched and its lines through lf into
// clean.log and clean.md; a last line without a newline is taken too.
func pump(r io.Reader, l *runLogs, lf lineFilter) {
	br := bufio.NewReader(r)
	for {
		line, err := br.ReadString('\n')
		if line != "" {
			_, _ = l.raw.WriteString(line)
			log, md := lf.Line(strings.TrimSuffix(line, "\n"))
			_, _ = l.log.WriteString(log)
			_, _ = l.md.WriteString(md)
		}
		if err != nil {
			break
		}
	}
	log, md := lf.flush()
	_, _ = l.log.WriteString(log)
	_, _ = l.md.WriteString(md)
}

func command(ctx context.Context, job Job) *exec.Cmd {
	cmd := exec.CommandContext(ctx, job.Argv[0], job.Argv[1:]...)
	cmd.Dir = job.Cwd
	cmd.WaitDelay = 2 * time.Second
	return cmd
}

// startFailed says in the logs why the tool did not start and returns the
// exit a shell gives: 127 when it is not found, else 126.
func startFailed(l *runLogs, err error) int {
	msg := fmt.Sprintf("delegate run: %v\n", err)
	_, _ = l.raw.WriteString(msg)
	_, _ = l.log.WriteString(msg)
	if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
		return 127
	}
	return 126
}

// exitOf is the tool's status as a shell reports it (128+n for a signal).
func exitOf(err error, state *os.ProcessState) int {
	if state == nil {
		return 1
	}
	if ws, ok := state.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return 128 + int(ws.Signal())
	}
	if err != nil && state.ExitCode() == 0 {
		return 1
	}
	return state.ExitCode()
}

// runMerged runs codex exec or claude -p with stderr merged into stdout
// (2>&1), then renders the whole raw.log once for clean.md, usage and, for
// claude, last.md.
func runMerged(ctx context.Context, rf *runstore.RunFolder, job Job) int {
	l, err := openLogs(rf)
	if err != nil {
		return 1
	}
	defer l.close()
	var lf lineFilter = codexLines{render.NewCodexFilter()}
	if job.Kind == KindClaude {
		lf = streamLines{render.NewClaudeFilter()}
	}
	cmd := command(ctx, job)
	pr, pw, err := os.Pipe()
	if err != nil {
		return startFailed(l, err)
	}
	cmd.Stdout, cmd.Stderr = pw, pw
	if err := cmd.Start(); err != nil {
		pr.Close()
		pw.Close()
		return startFailed(l, err)
	}
	pw.Close()
	pump(pr, l, lf)
	pr.Close()
	exit := exitOf(cmd.Wait(), cmd.ProcessState)
	finishMerged(rf, job)
	return exit
}

func finishMerged(rf *runstore.RunFolder, job Job) {
	raw, _, _ := rf.ReadFrom(runstore.FileRawLog, 0)
	if job.Kind == KindClaude {
		out := render.Claude(raw)
		_ = rf.WriteFile(runstore.FileCleanMD, []byte(out.CleanMD))
		writeUsage(rf, out.Usage)
		if last, _, _ := rf.ReadFrom(runstore.FileLastMD, 0); len(last) == 0 {
			_ = rf.WriteFile(runstore.FileLastMD, []byte(out.Last))
		}
		return
	}
	out := render.Codex(raw)
	_ = rf.WriteFile(runstore.FileCleanMD, []byte(out.CleanMD))
	cleanLog, _, _ := rf.ReadFrom(runstore.FileCleanLog, 0)
	writeUsage(rf, render.CodexUsage(raw, string(cleanLog)))
	copyOwnLast(rf, job)
}

func writeUsage(rf *runstore.RunFolder, usage string) {
	if usage == "" {
		_ = rf.WriteFile(runstore.FileUsage, nil)
		return
	}
	_ = rf.WriteUsage(usage)
}

// copyOwnLast: a caller's own -o file is written by codex as asked and
// copied to last.md, so the report and the page still have the answer.
func copyOwnLast(rf *runstore.RunFolder, job Job) {
	own := job.OwnLast
	if own == "" {
		return
	}
	if !filepath.IsAbs(own) {
		own = filepath.Join(job.Cwd, own)
	}
	if own == filepath.Join(rf.Dir(), runstore.FileLastMD) {
		return
	}
	if data, err := os.ReadFile(own); err == nil {
		_ = rf.WriteFile(runstore.FileLastMD, data)
	}
}

// runReview runs codex review: its final message (stdout) is the answer
// file and its progress (stderr) the log, as the mod's shim does for a
// standalone review.
func runReview(ctx context.Context, rf *runstore.RunFolder, job Job) int {
	l, err := openLogs(rf)
	if err != nil {
		return 1
	}
	defer l.close()
	last, err := os.OpenFile(filepath.Join(rf.Dir(), runstore.FileLastMD), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return startFailed(l, err)
	}
	defer last.Close()
	cmd := command(ctx, job)
	cmd.Stdout = last
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return startFailed(l, err)
	}
	if err := cmd.Start(); err != nil {
		return startFailed(l, err)
	}
	pump(stderr, l, codexLines{render.NewCodexFilter()})
	exit := exitOf(cmd.Wait(), cmd.ProcessState)
	raw, _, _ := rf.ReadFrom(runstore.FileRawLog, 0)
	_ = rf.WriteFile(runstore.FileCleanMD, []byte(render.Codex(raw).CleanMD))
	cleanLog, _, _ := rf.ReadFrom(runstore.FileCleanLog, 0)
	writeUsage(rf, render.CodexUsage(raw, string(cleanLog)))
	return exit
}
