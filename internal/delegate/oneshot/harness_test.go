package oneshot

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/delegationledger"
	"github.com/shakestzd/wipnote/internal/delegate/herdr"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// paneWorkerEnv makes the test binary the pane worker: the fake client runs
// each pane's shell line with it set, so the line's "<exe> --project-dir D
// <run-id>" lands in paneWorkerMain.
const paneWorkerEnv = "ONESHOT_TEST_PANE_WORKER"

// TestMain doubles as the fake herdr (for the pane worker's own herdr calls)
// and as the pane worker. No test may reach a real herdr or a real project.
func TestMain(m *testing.M) {
	if filepath.Base(os.Args[0]) == "herdr" {
		os.Exit(herdr.FakeMain(os.Args[1:], os.Stdout, os.Stderr))
	}
	if os.Getenv(paneWorkerEnv) == "1" {
		os.Exit(paneWorkerMain(os.Args[1:]))
	}
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, "HERDR_") || strings.HasPrefix(name, "STUB_") {
			_ = os.Unsetenv(name)
		}
	}
	for _, name := range []string{"WIPNOTE_PROJECT_DIR", "CLAUDE_PROJECT_DIR", "CCX_RUNS_DIR", "CODEX_HOME"} {
		_ = os.Unsetenv(name)
	}
	os.Exit(m.Run())
}

// paneWorkerMain is what `wipnote delegate run-pane` does, for the test
// binary: --project-dir D <run-id>.
func paneWorkerMain(args []string) int {
	if len(args) != 3 || args[0] != "--project-dir" {
		os.Stderr.WriteString("pane worker: bad args " + strings.Join(args, " ") + "\n")
		return 2
	}
	store, err := runstore.New(args[1])
	if err != nil {
		return 2
	}
	return RunPane(context.Background(), PaneOptions{
		Store: store, RunID: args[2], Herdr: herdr.New(herdr.EnvFrom(os.Getenv)), Out: os.Stdout,
	})
}

// testProject is a temp project: its run store, its ledger and a run
// options base with stubs for codex, claude and ccx first on PATH.
type testProject struct {
	t      *testing.T
	dir    string
	store  *runstore.Store
	ledger *delegationledger.Store
	stdout bytes.Buffer
	stderr bytes.Buffer
}

func newProject(t *testing.T) *testProject {
	t.Helper()
	dir := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(dir, ".wipnote"), 0o755))
	store, err := runstore.New(dir)
	must(t, err)
	stubs, err := filepath.Abs(filepath.Join("testdata", "stubs"))
	must(t, err)
	t.Setenv("PATH", stubs+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	return &testProject{t: t, dir: dir, store: store, ledger: delegationledger.NewStore(filepath.Join(dir, ".wipnote"))}
}

// options is a headless run of argv with fast polls and the mod's defaults.
func (p *testProject) options(argv ...string) Options {
	s := DefaultSettings()
	s.CloseDelay = 0
	return Options{
		Argv: argv, Cwd: p.dir, Settings: s, Store: p.store, Ledger: p.ledger,
		Stdout: &p.stdout, Stderr: &p.stderr, Poll: 20 * time.Millisecond,
		PaneLine: func(runID string) string {
			exe, _ := os.Executable()
			return PaneLine(exe, nil, p.dir, runID)
		},
	}
}

func (p *testProject) run(o Options) Result {
	p.t.Helper()
	res, err := Run(context.Background(), o)
	if err != nil {
		p.t.Fatalf("Run: %v\nstdout: %s\nstderr: %s", err, p.stdout.String(), p.stderr.String())
	}
	return res
}

func (p *testProject) row(id string) delegationledger.Record {
	p.t.Helper()
	r, ok, err := p.ledger.Get(id)
	if err != nil || !ok {
		p.t.Fatalf("ledger row %s: ok=%v err=%v", id, ok, err)
	}
	return r
}

func (p *testProject) file(id, name string) string {
	p.t.Helper()
	b, err := os.ReadFile(filepath.Join(p.store.Root(), "runs", id, name))
	if err != nil {
		p.t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

func (p *testProject) runDir(id string) string { return filepath.Join(p.store.Root(), "runs", id) }

func fixture(t *testing.T, name string) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("testdata", name))
	must(t, err)
	return path
}

// fakeClient is herdr for the caller: panes are bash processes running the
// line herdr would type. Methods the runner must not call panic (the nil
// embedded Client).
type fakeClient struct {
	herdr.Client
	mu         sync.Mutex
	calls      []string
	n          int
	closed     map[herdr.PaneID]bool
	procs      map[herdr.PaneID]*exec.Cmd
	output     map[herdr.PaneID]*syncBuffer
	closeAfter time.Duration // close each pane this long after it runs
	splitFails bool
}

func newFake(t *testing.T) *fakeClient {
	f := &fakeClient{closed: map[herdr.PaneID]bool{}, procs: map[herdr.PaneID]*exec.Cmd{}, output: map[herdr.PaneID]*syncBuffer{}}
	t.Cleanup(f.killAll)
	return f
}

func (f *fakeClient) record(s string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, s)
}

func (f *fakeClient) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeClient) Split(_ context.Context, target herdr.PaneID, dir herdr.Direction, cwd string) (herdr.PaneID, error) {
	f.record("split " + string(target) + " " + string(dir))
	if f.splitFails {
		return "", &herdr.Error{Exit: 1, Code: "herdr_error", Message: "no room"}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n++
	return herdr.PaneID("w1:p" + string(rune('0'+f.n))), nil
}

func (f *fakeClient) Run(_ context.Context, pane herdr.PaneID, shell string) error {
	f.record("run " + string(pane))
	cmd := exec.Command("bash", "-c", shell)
	cmd.Env = append(os.Environ(), paneWorkerEnv+"=1", "HERDR_ENV=1", "HERDR_PANE_ID="+string(pane))
	out := &syncBuffer{}
	cmd.Stdout, cmd.Stderr = out, out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	f.mu.Lock()
	f.procs[pane], f.output[pane] = cmd, out
	f.mu.Unlock()
	go func() { _ = cmd.Wait() }()
	if f.closeAfter > 0 {
		time.AfterFunc(f.closeAfter, func() { _ = f.Close(context.Background(), pane) })
	}
	return nil
}

func (f *fakeClient) Get(_ context.Context, pane herdr.PaneID) (herdr.Pane, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed[pane] {
		return herdr.Pane{}, &herdr.Error{Exit: 1, Code: "pane_not_found", Message: "gone"}
	}
	return herdr.Pane{ID: pane}, nil
}

func (f *fakeClient) Close(_ context.Context, pane herdr.PaneID) error {
	f.record("close " + string(pane))
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed[pane] = true
	if c := f.procs[pane]; c != nil && c.Process != nil {
		_ = syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
	}
	return nil
}

func (f *fakeClient) Rename(_ context.Context, pane herdr.PaneID, title string) error {
	f.record("rename " + string(pane) + " " + title)
	return nil
}

func (f *fakeClient) paneOutput(pane herdr.PaneID) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if b := f.output[pane]; b != nil {
		return b.String()
	}
	return ""
}

func (f *fakeClient) killAll() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.procs {
		if c.Process != nil {
			_ = syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
		}
	}
}

// inHerdr makes o a run inside herdr with client f.
func inHerdr(o Options, f herdr.Client) Options {
	o.Herdr = f
	o.HerdrEnv = herdr.Env{InHerdr: true, CallerPane: "w1:p0"}
	return o
}

// installFakeHerdr puts the fake herdr binary first on PATH for the pane
// worker's own calls (pane close) and returns its call log.
func installFakeHerdr(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	exe, err := os.Executable()
	must(t, err)
	must(t, os.Symlink(exe, filepath.Join(dir, "herdr")))
	script := filepath.Join(dir, "script.json")
	must(t, os.WriteFile(script, []byte(`{"rules":[{"match":["pane","close","*"],"replies":[{"result":{"type":"ok"}}]}]}`), 0o600))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(herdr.FakeScriptEnv, script)
	return script + ".log.ndjson"
}

// waitFor polls cond for up to 10 s.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for end := time.Now().Add(10 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

// syncBuffer is a pane's terminal: exec's copier writes it while a test reads.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
