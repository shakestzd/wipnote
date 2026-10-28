package oneshot

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/internal/delegate/codex"
	"github.com/shakestzd/wipnote/internal/delegate/container"
	"github.com/shakestzd/wipnote/internal/delegate/container/containertest"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// containerProject is a test project with the project's own devcontainer
// config, a stub devcontainer CLI and a remote workspace folder that is a
// symlink to the project, so mapped paths land in the host's run folder.
func containerProject(t *testing.T) (*testProject, *containertest.Stub, string) {
	t.Helper()
	p := newProject(t)
	must(t, os.MkdirAll(filepath.Join(p.dir, ".devcontainer"), 0o755))
	must(t, os.WriteFile(filepath.Join(p.dir, ".devcontainer", "devcontainer.json"), []byte(`{"name":"t"}`), 0o644))
	remote := filepath.Join(t.TempDir(), "workspaces")
	must(t, os.Symlink(p.dir, remote))
	return p, containertest.Install(t, remote), remote
}

func (p *testProject) containerOptions(argv ...string) Options {
	o := p.options(argv...)
	o.Container, o.Root, o.CallerSession = true, p.dir, "caller-sid"
	return o
}

// execCall is the stub's exec call; it fails the test when there is none.
func execCall(t *testing.T, s *containertest.Stub) []string {
	t.Helper()
	for _, c := range s.Calls(t) {
		if len(c) > 0 && c[0] == "exec" {
			return c
		}
	}
	t.Fatalf("no devcontainer exec call in %q", s.Calls(t))
	return nil
}

func TestRun_ContainerClaudePrint(t *testing.T) {
	p, stub, remote := containerProject(t)
	t.Setenv("STUB_STDOUT", fixture(t, "claude_print.jsonl"))
	argvFile := filepath.Join(t.TempDir(), "argv")
	t.Setenv("STUB_ARGV", argvFile)
	res := p.run(p.containerOptions("claude", "-p", "say ok"))
	if res.Exit != 0 {
		t.Fatalf("exit %d\n%s", res.Exit, p.stdout.String())
	}
	calls := stub.Calls(t)
	if len(calls) != 2 || !slices.Equal(calls[0], []string{"up", "--workspace-folder", p.dir}) {
		t.Fatalf("calls %q: want up --workspace-folder <root> first", calls)
	}
	ex := execCall(t, stub)
	head := []string{"exec", "--workspace-folder", p.dir,
		"--remote-env", "WIPNOTE_DELEGATE_PLACE=container", "--remote-env", "WIPNOTE_DELEGATE_RUN=" + res.RunID,
		"--remote-env", "WIPNOTE_DELEGATE_CALLER=caller-sid", "claude"}
	if !slices.Equal(ex[:len(head)], head) {
		t.Fatalf("exec call %q\nwant it to start %q", ex, head)
	}
	if got, _ := os.ReadFile(argvFile); !strings.Contains(string(got), "say ok") {
		t.Errorf("claude inside got %q", got)
	}
	r := p.row(res.RunID)
	if r.Host != "container" || r.Harness != "claude-print" {
		t.Errorf("row host %q harness %q, want container and claude-print", r.Host, r.Harness)
	}
	if last := p.file(res.RunID, runstore.FileLastMD); strings.TrimSpace(last) == "" {
		t.Error("no answer in the host's run folder")
	}
	if c := p.file(res.RunID, FileContainer); !strings.Contains(c, remote) || !strings.Contains(c, `"place": "container"`) {
		t.Errorf("container.json %q: want the place and the workspace folder", c)
	}
}

// TestRun_ContainerTaskArgsRunAsClaudePrint: the redirect's form
// (--container -- <ccx task args>) runs claude -p with those args inside.
func TestRun_ContainerTaskArgsRunAsClaudePrint(t *testing.T) {
	p, stub, _ := containerProject(t)
	t.Setenv("STUB_STDOUT", fixture(t, "claude_print.jsonl"))
	res := p.run(p.containerOptions("fix the build", "--resume", "abc"))
	ex := execCall(t, stub)
	i := slices.Index(ex, "claude")
	if i < 0 || !slices.Equal(ex[i:], []string{"claude", "--output-format", "stream-json", "--verbose", "-p", "fix the build", "--resume", "abc"}) {
		t.Fatalf("exec call %q: want claude -p with the task's args", ex)
	}
	if r := p.row(res.RunID); r.Harness != "claude-print" || r.Host != "container" {
		t.Errorf("row %+v", r)
	}
}

// TestRun_ContainerMapsPathsToRemoteWorkspace: the run folder's paths are
// given to the tool in the container's workspace folder, and the answer the
// tool writes there lands in the host's run folder.
func TestRun_ContainerMapsPathsToRemoteWorkspace(t *testing.T) {
	p, stub, remote := containerProject(t)
	t.Setenv("STUB_ANSWER", "mapped answer")
	o := p.containerOptions("codex", "exec", "x")
	o.Sandbox = SandboxOptions{Host: testHost, OptIn: []string{testHost}}
	res := p.run(o)
	ex := execCall(t, stub)
	want := filepath.Join(remote, ".wipnote", "delegate", "runs", res.RunID, runstore.FileLastMD)
	if i := slices.Index(ex, "--output-last-message"); i < 0 || ex[i+1] != want {
		t.Fatalf("exec call %q: want --output-last-message %s", ex, want)
	}
	if slices.Contains(ex, filepath.Join(p.runDir(res.RunID), runstore.FileLastMD)) {
		t.Errorf("a host path was given to the container: %q", ex)
	}
	if got := p.file(res.RunID, runstore.FileLastMD); got != "mapped answer" {
		t.Errorf("host last.md %q", got)
	}
	sub := filepath.Join(p.dir, "sub")
	must(t, os.MkdirAll(sub, 0o755))
	o = p.containerOptions("claude", "-p", "x")
	o.Cwd = sub
	p.run(o)
	ex = stub.Calls(t)[len(stub.Calls(t))-1]
	if !slices.Contains(ex, filepath.Join(remote, "sub")) || slices.Contains(ex, sub) {
		t.Errorf("exec call %q: want the cwd mapped to %s", ex, filepath.Join(remote, "sub"))
	}
}

// TestRun_ContainerCodexNeedsOptIn: bwrap fails inside a container, so
// slice 12's ladder refuses a Codex run there before anything is written,
// unless this host opted in; then it runs with danger-full-access and a
// warning.
func TestRun_ContainerCodexNeedsOptIn(t *testing.T) {
	p, stub, _ := containerProject(t)
	for _, argv := range [][]string{{"codex", "exec", "x"}, {"codex", "review"}} {
		o := p.containerOptions(argv...)
		o.Sandbox = SandboxOptions{Host: testHost}
		_, err := Run(context.Background(), o)
		if !errors.Is(err, codex.ErrRefused) {
			t.Errorf("%q: err %v, want the ladder's refusal", argv, err)
		}
	}
	if recs, _ := p.ledger.ReadAll(); len(recs) != 0 || len(stub.Calls(t)) != 0 {
		t.Fatalf("a refused run wrote %d rows and made calls %q", len(recs), stub.Calls(t))
	}
	o := p.containerOptions("codex", "exec", "x")
	o.Sandbox = SandboxOptions{Host: testHost, OptIn: []string{testHost}}
	p.run(o)
	if ex := execCall(t, stub); !slices.Contains(ex, "danger-full-access") {
		t.Errorf("opted-in exec call %q: want --sandbox danger-full-access", ex)
	}
	if !strings.Contains(p.stderr.String(), "danger-full-access") {
		t.Errorf("no widening warning on stderr: %q", p.stderr.String())
	}
}

// TestRun_ContainerRefusesBeforeWriting: no devcontainer config, no CLI, or
// ccx task under --container: refused with nothing written.
func TestRun_ContainerRefusesBeforeWriting(t *testing.T) {
	p, _, _ := containerProject(t)
	must(t, os.Remove(filepath.Join(p.dir, ".devcontainer", "devcontainer.json")))
	if _, err := Run(context.Background(), p.containerOptions("claude", "-p", "x")); !errors.Is(err, container.ErrNoConfig) {
		t.Errorf("no config: %v", err)
	}
	if _, err := Run(context.Background(), p.containerOptions("ccx", "task", "x")); err == nil {
		t.Error("ccx task under --container was accepted")
	}
	if recs, _ := p.ledger.ReadAll(); len(recs) != 0 {
		t.Errorf("refusals wrote %d rows", len(recs))
	}
}

// TestRun_ContainerUpFails: a failed devcontainer up ends the run with exit
// 1 and its message in the log; nothing runs inside.
func TestRun_ContainerUpFails(t *testing.T) {
	p, stub, _ := containerProject(t)
	t.Setenv("STUB_UP_FAIL", "1")
	res := p.run(p.containerOptions("claude", "-p", "x"))
	if res.Exit != 1 {
		t.Errorf("exit %d, want 1", res.Exit)
	}
	for _, c := range stub.Calls(t) {
		if c[0] == "exec" {
			t.Fatalf("exec ran after a failed up: %q", c)
		}
	}
	if log := p.file(res.RunID, runstore.FileCleanLog); !strings.Contains(log, "stub up failed") {
		t.Errorf("clean.log %q: want up's message", log)
	}
	if r := p.row(res.RunID); r.IsOpen() || r.Exit == nil || *r.Exit != 1 {
		t.Errorf("row %+v: want closed with exit 1", r)
	}
}

// TestRun_ContainerMapsPathsWithSpaces: a project whose path has a space
// (and a container workspace folder with one) still gets the answer file and
// the cwd mapped; the answer lands in the host's run folder and the tool runs
// in the mapped subfolder.
func TestRun_ContainerMapsPathsWithSpaces(t *testing.T) {
	base := newProject(t)
	dir := filepath.Join(t.TempDir(), "My Project")
	must(t, os.MkdirAll(filepath.Join(dir, ".wipnote"), 0o755))
	must(t, os.MkdirAll(filepath.Join(dir, ".devcontainer"), 0o755))
	must(t, os.WriteFile(filepath.Join(dir, ".devcontainer", "devcontainer.json"), []byte(`{"name":"t"}`), 0o644))
	store, err := runstore.New(dir)
	must(t, err)
	p := &testProject{t: t, dir: dir, store: store, ledger: base.ledger}
	remote := filepath.Join(t.TempDir(), "work spaces")
	must(t, os.Symlink(dir, remote))
	stub := containertest.Install(t, remote)
	sub := filepath.Join(dir, "sub dir")
	must(t, os.MkdirAll(sub, 0o755))
	t.Setenv("STUB_ANSWER", "spaced answer")
	o := p.containerOptions("codex", "exec", "x")
	o.Cwd = sub
	o.Sandbox = SandboxOptions{Host: testHost, OptIn: []string{testHost}}
	res := p.run(o)
	ex := execCall(t, stub)
	want := filepath.Join(remote, ".wipnote", "delegate", "runs", res.RunID, runstore.FileLastMD)
	if i := slices.Index(ex, "--output-last-message"); i < 0 || ex[i+1] != want {
		t.Fatalf("exec call %q: want --output-last-message %s", ex, want)
	}
	if !slices.Contains(ex, filepath.Join(remote, "sub dir")) || slices.Contains(ex, sub) {
		t.Errorf("exec call %q: want the cwd mapped to %s", ex, filepath.Join(remote, "sub dir"))
	}
	if got := p.file(res.RunID, runstore.FileLastMD); got != "spaced answer" {
		t.Errorf("host last.md %q (exit %d)", got, res.Exit)
	}
}
