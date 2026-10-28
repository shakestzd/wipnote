package session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/delegationledger"
	"github.com/shakestzd/wipnote/internal/delegate/herdr"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// fakeHerdr is an in-memory herdr: tabs, panes with a scripted foreground,
// screens and styled reads, and a call log in order. Every pane starts at a
// bare shell (pid 900+n); running the boot line starts a harness in it.
type fakeHerdr struct {
	mu       sync.Mutex
	calls    []string
	n        int
	panes    map[herdr.PaneID]*fpane
	tabs     map[herdr.TabID]bool
	agents   []herdr.Agent
	procsErr error
	// shellAfter: Processes reports an unsettled shell (Known=false) this
	// many times for a new pane before the bare shell.
	shellAfter int
	// exitOnEnter: an enter after the exit command leaves a bare shell.
	exitOnEnter bool
}

type fpane struct {
	tab      herdr.TabID
	terminal string
	gone     bool
	shellPID int
	fg       []herdr.Proc
	known    bool
	unsettle int
	screen   string
	styled   string
	styleErr error
	typed    []string
	pending  string
}

func newFakeHerdr() *fakeHerdr {
	// Pane numbers start at 11 so no worker pane shares the caller's id (w1:p1).
	return &fakeHerdr{n: 10, panes: map[herdr.PaneID]*fpane{}, tabs: map[herdr.TabID]bool{}}
}

func (f *fakeHerdr) log(format string, a ...any) {
	f.calls = append(f.calls, fmt.Sprintf(format, a...))
}

func (f *fakeHerdr) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeHerdr) newPane(tab herdr.TabID) herdr.PaneID {
	f.n++
	id := herdr.PaneID(fmt.Sprintf("w1:p%d", f.n))
	f.panes[id] = &fpane{tab: tab, terminal: fmt.Sprintf("term-%d", f.n), shellPID: 900 + f.n, known: true, unsettle: f.shellAfter}
	return id
}

func (f *fakeHerdr) pane(id herdr.PaneID) *fpane {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.panes[id]
}

var errAbsent = &herdr.Error{Exit: 1, Code: "pane_not_found", Message: "gone"}

func (f *fakeHerdr) TabCreate(_ context.Context, ws, label, cwd string) (herdr.TabID, herdr.PaneID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	tab := herdr.TabID(fmt.Sprintf("%s:t%d", ws, len(f.tabs)+2))
	f.tabs[tab] = true
	p := f.newPane(tab)
	f.log("tab create %s %s -> %s %s", ws, label, tab, p)
	return tab, p, nil
}

func (f *fakeHerdr) TabClose(_ context.Context, tab herdr.TabID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log("tab close %s", tab)
	delete(f.tabs, tab)
	return nil
}

func (f *fakeHerdr) Split(_ context.Context, target herdr.PaneID, dir herdr.Direction, cwd string) (herdr.PaneID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := f.panes[target]
	if t == nil || t.gone {
		return "", errAbsent
	}
	p := f.newPane(t.tab)
	f.log("split %s %s -> %s", target, dir, p)
	return p, nil
}

func (f *fakeHerdr) Run(_ context.Context, pane herdr.PaneID, shell string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log("run %s %s", pane, shell)
	if p := f.panes[pane]; p != nil {
		p.typed = append(p.typed, shell)
		// The boot line starts the harness, as the real one does.
		if len(p.fg) == 0 {
			p.fg = []herdr.Proc{{PID: p.shellPID, Name: "zsh"}, {PID: p.shellPID + 1000, Name: "claude"}}
		}
	}
	return nil
}

func (f *fakeHerdr) Get(_ context.Context, pane herdr.PaneID) (herdr.Pane, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := f.panes[pane]
	if p == nil || p.gone {
		return herdr.Pane{}, errAbsent
	}
	return herdr.Pane{ID: pane, TerminalID: p.terminal, TabID: p.tab, WorkspaceID: "w1"}, nil
}

func (f *fakeHerdr) Close(_ context.Context, pane herdr.PaneID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log("close %s", pane)
	if p := f.panes[pane]; p != nil {
		p.gone = true
	}
	return nil
}

func (f *fakeHerdr) Rename(_ context.Context, pane herdr.PaneID, title string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log("rename %s %s", pane, title)
	return nil
}

func (f *fakeHerdr) Processes(_ context.Context, pane herdr.PaneID) (herdr.ProcessInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log("process-info %s", pane)
	p := f.panes[pane]
	if f.procsErr != nil {
		return herdr.ProcessInfo{}, f.procsErr
	}
	if p == nil || p.gone {
		return herdr.ProcessInfo{}, errAbsent
	}
	if p.unsettle > 0 {
		p.unsettle--
		return herdr.ProcessInfo{}, nil
	}
	if !p.known {
		return herdr.ProcessInfo{}, nil
	}
	fg := []herdr.Proc{{PID: p.shellPID, Name: "zsh"}}
	if len(p.fg) > 0 {
		fg = append([]herdr.Proc(nil), p.fg...)
	}
	return herdr.ProcessInfo{Known: true, ShellPID: p.shellPID, Foreground: fg}, nil
}

func (f *fakeHerdr) Layout(context.Context, herdr.PaneID) ([]herdr.PaneRect, error) { return nil, nil }

func (f *fakeHerdr) PaneRead(_ context.Context, pane herdr.PaneID, lines int) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log("pane read %s", pane)
	if p := f.panes[pane]; p != nil {
		return p.screen, nil
	}
	return "", errAbsent
}

// PaneReadStyled makes the fake a StyledReader.
func (f *fakeHerdr) PaneReadStyled(_ context.Context, pane herdr.PaneID, lines int) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log("pane read --format ansi %s", pane)
	p := f.panes[pane]
	if p == nil {
		return "", errAbsent
	}
	return p.styled, p.styleErr
}

func (f *fakeHerdr) SendText(_ context.Context, pane herdr.PaneID, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log("send-text %s %s", pane, text)
	if p := f.panes[pane]; p != nil {
		p.pending += text
	}
	return nil
}

func (f *fakeHerdr) SendKeys(_ context.Context, pane herdr.PaneID, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log("send-keys %s %s", pane, key)
	p := f.panes[pane]
	if p != nil && key == "enter" && f.exitOnEnter && (p.pending == "/exit" || p.pending == "/quit") {
		p.fg = nil
	}
	if p != nil && key == "enter" {
		p.pending = ""
	}
	return nil
}

func (f *fakeHerdr) Prompt(context.Context, herdr.Pin, string) error { panic("Prompt not expected") }
func (f *fakeHerdr) Read(context.Context, herdr.Pin, int) (string, error) {
	panic("Read not expected")
}

func (f *fakeHerdr) State(_ context.Context, pin herdr.Pin) (herdr.Agent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, a := range f.agents {
		if a.Pane == pin.Pane {
			return a, nil
		}
	}
	return herdr.Agent{}, herdr.ErrPin
}

func (f *fakeHerdr) AgentRename(_ context.Context, pin herdr.Pin, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log("agent rename %s %s %s", pin.Pane, pin.TerminalID, name)
	return nil
}

func (f *fakeHerdr) AgentList(context.Context) ([]herdr.Agent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log("agent list")
	return append([]herdr.Agent(nil), f.agents...), nil
}

func (f *fakeHerdr) CallerWorkspace(context.Context) (string, error) { return "w1", nil }

var _ herdr.Client = (*fakeHerdr)(nil)
var _ StyledReader = (*fakeHerdr)(nil)

// setRunning puts a harness process in the pane's foreground.
func (f *fakeHerdr) setRunning(pane herdr.PaneID, procs ...herdr.Proc) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.panes[pane].fg = append([]herdr.Proc{{PID: f.panes[pane].shellPID, Name: "zsh"}}, procs...)
}

// env is a temp project with stores, a fake herdr and fixed deps. It never
// touches the real project's .wipnote/.
type env struct {
	t      *testing.T
	dir    string
	herdr  *fakeHerdr
	deps   Deps
	alive  map[int]string
	aliveM sync.Mutex
}

func newEnv(t *testing.T) *env {
	t.Helper()
	t.Setenv("WIPNOTE_PROJECT_DIR", "")
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	os.Unsetenv("WIPNOTE_PROJECT_DIR")
	os.Unsetenv("CLAUDE_PROJECT_DIR")
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".wipnote"), 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := runstore.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, dir: dir, herdr: newFakeHerdr(), alive: map[int]string{}}
	e.deps = Deps{
		Herdr: e.herdr, Store: store, Ledger: delegationledger.NewStore(filepath.Join(dir, ".wipnote")),
		ProcStart: e.procStart,
	}
	return e
}

func (e *env) procStart(pid int) (string, bool) {
	e.aliveM.Lock()
	defer e.aliveM.Unlock()
	s, ok := e.alive[pid]
	return s, ok
}

func (e *env) setAlive(pid int, start string) {
	e.aliveM.Lock()
	defer e.aliveM.Unlock()
	if start == "" {
		delete(e.alive, pid)
		return
	}
	e.alive[pid] = start
}

func (e *env) opts(name string) SpawnOptions {
	return SpawnOptions{
		Name: name, Harness: HarnessClaude, WorkItem: itemFor(name), CallerSession: "sess-caller",
		CallerPane: "w1:p1", Brief: "Do the thing.\nIn detail.", Cwd: e.dir,
		BootLine:  func(run string) string { return BootLine("/bin/wipnote", e.dir, run, "sess-caller") },
		ShellWait: time.Second, Poll: 5 * time.Millisecond,
	}
}

// itemFor gives each worker name its own work item: one live worker per item
// (slice 16). w1 keeps the base item.
func itemFor(name string) string {
	if name == "w1" {
		return "feat-0a1b2c3d"
	}
	return "feat-0a1b2c3d" + strings.ReplaceAll(name, "-", "")
}

func (e *env) spawn(name string) Spawned {
	e.t.Helper()
	s, err := Spawn(context.Background(), e.deps, e.opts(name))
	if err != nil {
		e.t.Fatalf("spawn %s: %v", name, err)
	}
	return s
}

func (e *env) row(run string) delegationledger.Record {
	e.t.Helper()
	r, ok, err := e.deps.Ledger.Get(run)
	if err != nil || !ok {
		e.t.Fatalf("row %s: ok=%v err=%v", run, ok, err)
	}
	return r
}

func (e *env) runFolder(run string) *runstore.RunFolder {
	e.t.Helper()
	rf, err := e.deps.Store.Run(run)
	if err != nil {
		e.t.Fatal(err)
	}
	return rf
}

// event appends one hook-style event to the run as writer sid.
func (e *env) event(run, sid, kind string, extra ...func(*runstore.Event)) {
	e.t.Helper()
	w, err := e.runFolder(run).OpenEvents(sid)
	if err != nil {
		e.t.Fatal(err)
	}
	defer w.Close()
	ev := runstore.Event{Kind: kind, SID: sid}
	for _, f := range extra {
		f(&ev)
	}
	if err := w.Append(ev); err != nil {
		e.t.Fatal(err)
	}
}

func hasCall(calls []string, prefix string) bool {
	for _, c := range calls {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

func indexOfCall(calls []string, prefix string) int {
	for i, c := range calls {
		if strings.HasPrefix(c, prefix) {
			return i
		}
	}
	return -1
}
