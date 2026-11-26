package oneshot

import (
	"context"
	"os"
	"path/filepath"

	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// plainLines passes a tool's progress lines through unchanged: Antigravity's
// stderr is plain text, not a Codex or Claude event stream.
type plainLines struct{}

func (plainLines) Line(raw string) (string, string) { return raw + "\n", raw + "\n" }
func (plainLines) flush() (string, string)          { return "", "" }

// runAnswerOnStdout runs agy --print (feat-e014b05b). In text mode its stdout
// is the answer, which becomes last.md, and its stderr is progress, which
// becomes the logs. Antigravity prints no token usage, so none is recorded.
func runAnswerOnStdout(ctx context.Context, rf *runstore.RunFolder, job Job) int {
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
	pump(stderr, l, plainLines{})
	return exitOf(cmd.Wait(), cmd.ProcessState)
}
