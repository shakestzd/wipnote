package oneshot

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/internal/delegate/herdr"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
	"github.com/shakestzd/wipnote/internal/delegate/session"
)

// stubClaude is what the fake worker pane does once its boot line runs, in
// place of wipnote claude: it writes what the worker's hooks and status
// line command would write. The zero value binds, feeds the status line,
// answers "the answer" and ends its turn.
type stubClaude struct {
	noBind, noStatus, noAnswer, noStop bool
	stopFailure                        string  // write an error event instead of stop
	turns                              int     // assistant messages in the transcript
	cost                               float64 // the status line's total cost
	ignoreExit                         bool    // /exit does nothing; only a signal ends it
}

// workerHerdr is an in-memory herdr whose panes run a stubClaude when the
// boot line is typed. It records every call, the launch argv the boot would
// exec, and each signalled pid.
type workerHerdr struct {
	t       *testing.T
	store   *runstore.Store
	stub    stubClaude
	mu      sync.Mutex
	calls   []string
	n       int
	panes   map[herdr.PaneID]*wpane
	argv    []string
	signals []int
}

type wpane struct {
	running bool
	pid     int
	pending string
	gone    bool
	run     string
}

func newWorkerHerdr(t *testing.T, store *runstore.Store, stub stubClaude) *workerHerdr {
	return &workerHerdr{t: t, store: store, stub: stub, n: 10, panes: map[herdr.PaneID]*wpane{}}
}

func (f *workerHerdr) log(format string, a ...any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fmt.Sprintf(format, a...))
}

func (f *workerHerdr) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *workerHerdr) launchArgv() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.argv...)
}

func (f *workerHerdr) signalled() []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int(nil), f.signals...)
}

func (f *workerHerdr) newPane() herdr.PaneID {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n++
	id := herdr.PaneID(fmt.Sprintf("w1:p%d", f.n))
	f.panes[id] = &wpane{pid: 5000 + f.n}
	return id
}

func (f *workerHerdr) pane(id herdr.PaneID) *wpane {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.panes[id]
}

func (f *workerHerdr) TabCreate(_ context.Context, ws, label, cwd string) (herdr.TabID, herdr.PaneID, error) {
	p := f.newPane()
	f.log("tab create %s", p)
	return "w1:t2", p, nil
}

func (f *workerHerdr) TabClose(_ context.Context, tab herdr.TabID) error {
	f.log("tab close %s", tab)
	return nil
}

func (f *workerHerdr) Split(_ context.Context, target herdr.PaneID, dir herdr.Direction, cwd string) (herdr.PaneID, error) {
	p := f.newPane()
	f.log("split %s -> %s", target, p)
	return p, nil
}

func (f *workerHerdr) Get(_ context.Context, pane herdr.PaneID) (herdr.Pane, error) {
	p := f.pane(pane)
	if p == nil || p.gone {
		return herdr.Pane{}, &herdr.Error{Exit: 1, Code: "pane_not_found", Message: "gone"}
	}
	return herdr.Pane{ID: pane, TerminalID: "term-" + string(pane), TabID: "w1:t2", WorkspaceID: "w1"}, nil
}

func (f *workerHerdr) Close(_ context.Context, pane herdr.PaneID) error {
	f.log("close %s", pane)
	f.mu.Lock()
	defer f.mu.Unlock()
	if p := f.panes[pane]; p != nil {
		p.gone, p.running = true, false
	}
	return nil
}

func (f *workerHerdr) Rename(_ context.Context, pane herdr.PaneID, title string) error { return nil }

func (f *workerHerdr) Processes(_ context.Context, pane herdr.PaneID) (herdr.ProcessInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := f.panes[pane]
	if p == nil || p.gone {
		return herdr.ProcessInfo{}, &herdr.Error{Exit: 1, Code: "pane_not_found", Message: "gone"}
	}
	fg := []herdr.Proc{{PID: p.pid, Name: "zsh"}}
	if p.running {
		fg = append(fg, herdr.Proc{PID: p.pid + 1000, Name: "claude"})
	}
	return herdr.ProcessInfo{Known: true, ShellPID: p.pid, Foreground: fg}, nil
}

func (f *workerHerdr) Layout(context.Context, herdr.PaneID) ([]herdr.PaneRect, error) {
	return nil, nil
}

func (f *workerHerdr) PaneRead(context.Context, herdr.PaneID, int) (string, error) { return "", nil }

// PaneReadStyled: an empty Claude input line, never a draft.
func (f *workerHerdr) PaneReadStyled(context.Context, herdr.PaneID, int) (string, error) {
	return "❯ \n", nil
}

func (f *workerHerdr) SendText(_ context.Context, pane herdr.PaneID, text string) error {
	f.log("send-text %s %s", pane, text)
	f.mu.Lock()
	defer f.mu.Unlock()
	if p := f.panes[pane]; p != nil {
		p.pending += text
	}
	return nil
}

func (f *workerHerdr) SendKeys(_ context.Context, pane herdr.PaneID, key string) error {
	f.log("send-keys %s %s", pane, key)
	f.mu.Lock()
	p := f.panes[pane]
	exit := p != nil && key == "enter" && p.pending == "/exit" && !f.stub.ignoreExit && p.running
	if p != nil && key == "enter" {
		p.pending = ""
	}
	if exit {
		p.running = false
	}
	f.mu.Unlock()
	if exit {
		f.event(p.run, runstore.KindSessionEnd, "prompt_input_exit")
	}
	return nil
}

func (f *workerHerdr) Prompt(context.Context, herdr.Pin, string) error { panic("Prompt not expected") }
func (f *workerHerdr) Read(context.Context, herdr.Pin, int) (string, error) {
	panic("Read not expected")
}
func (f *workerHerdr) State(context.Context, herdr.Pin) (herdr.Agent, error) {
	return herdr.Agent{}, herdr.ErrPin
}
func (f *workerHerdr) AgentRename(context.Context, herdr.Pin, string) error { return nil }
func (f *workerHerdr) AgentList(context.Context) ([]herdr.Agent, error)     { return nil, nil }
func (f *workerHerdr) CallerWorkspace(context.Context) (string, error)      { return "w1", nil }

var _ herdr.Client = (*workerHerdr)(nil)
var _ session.StyledReader = (*workerHerdr)(nil)

// signal is the worker options' SIGTERM: the stub always dies of it.
func (f *workerHerdr) signal(pid int) error {
	f.mu.Lock()
	f.signals = append(f.signals, pid)
	var run string
	for _, p := range f.panes {
		if p.pid+1000 == pid && p.running {
			p.running, run = false, p.run
		}
	}
	f.mu.Unlock()
	if run != "" {
		f.event(run, runstore.KindSessionEnd, "other")
	}
	return nil
}

// procStart: a pane's claude runs while the pane says so.
func (f *workerHerdr) procStart(pid int) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.panes {
		if p.pid+1000 == pid && p.running {
			return "start-" + strconv.Itoa(pid), true
		}
	}
	return "", false
}

// Run is the boot line: the stub starts in the pane.
func (f *workerHerdr) Run(_ context.Context, pane herdr.PaneID, line string) error {
	f.log("run %s", pane)
	words := strings.Fields(line)
	run := strings.Trim(words[len(words)-1], "'")
	f.mu.Lock()
	p := f.panes[pane]
	p.running, p.run = true, run
	f.mu.Unlock()
	go f.play(run)
	return nil
}

// play is the stub claude: the launch argv boot would exec, then the
// hooks' and status line's writes.
func (f *workerHerdr) play(run string) {
	rf, err := f.store.Run(run)
	if err != nil {
		return
	}
	info, _ := session.ReadInfo(rf)
	brief, _, _ := rf.ReadFrom(runstore.FileBrief, 0)
	f.mu.Lock()
	f.argv = session.BootArgv("/bin/wipnote", info, string(brief))
	f.mu.Unlock()
	s := f.stub
	if !s.noStatus {
		in := fmt.Sprintf(`{"session_id":%q,"cost":{"total_cost_usd":%v},"context_window":{"total_input_tokens":120,"total_output_tokens":30}}`, info.SessionID, s.cost)
		_, _ = RecordStatusLine(rf, strings.NewReader(in))
	}
	if s.noBind {
		return
	}
	f.event(run, runstore.KindSessionStart, "")
	f.event(run, runstore.KindPrompt, "")
	mb, _ := f.store.Mailbox(info.Name)
	_ = mb.Ack(info.Task)
	f.transcript(info.Transcript, s.turns)
	f.finish(run, mb, info.Task)
}

func (f *workerHerdr) finish(run string, mb *runstore.Mailbox, task string) {
	s := f.stub
	if !s.noAnswer {
		_ = mb.WriteResult(task, runstore.Message{Body: "the answer"})
	}
	switch {
	case s.stopFailure != "":
		f.event(run, runstore.KindError, s.stopFailure)
	case !s.noStop:
		f.event(run, runstore.KindStop, "")
	}
}

func (f *workerHerdr) transcript(path string, turns int) {
	if path == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	var b strings.Builder
	for i := 0; i < turns; i++ {
		line, _ := json.Marshal(map[string]any{"type": "assistant", "message": map[string]any{"id": fmt.Sprintf("msg_%d", i), "role": "assistant"}})
		b.Write(append(line, '\n'))
	}
	_ = os.WriteFile(path, []byte(b.String()), 0o600)
}

// event appends one hook-style event as the worker's session.
func (f *workerHerdr) event(run, kind, summary string) {
	rf, err := f.store.Run(run)
	if err != nil {
		return
	}
	info, _ := session.ReadInfo(rf)
	sid := info.SessionID
	if sid == "" {
		sid = "sid-stub"
	}
	w, err := rf.OpenEvents(sid)
	if err != nil {
		return
	}
	defer w.Close()
	_ = w.Append(runstore.Event{Kind: kind, SID: sid, Summary: summary, TS: time.Now().UTC().Format(time.RFC3339Nano)})
}
