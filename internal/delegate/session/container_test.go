package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/containerwrite"
	"github.com/shakestzd/wipnote/core/sessionledger"
	"github.com/shakestzd/wipnote/internal/delegate/codex"
	"github.com/shakestzd/wipnote/internal/delegate/container"
	"github.com/shakestzd/wipnote/internal/delegate/container/containertest"
	"github.com/shakestzd/wipnote/internal/delegate/herdr"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// containerEnv is newEnv with the project's devcontainer config, a stub
// devcontainer CLI and a remote workspace folder linked to the project.
func containerEnv(t *testing.T) (*env, *containertest.Stub, string) {
	t.Helper()
	e := newEnv(t)
	if err := os.MkdirAll(filepath.Join(e.dir, ".devcontainer"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.dir, ".devcontainer", "devcontainer.json"), []byte(`{"name":"t"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	remote := filepath.Join(t.TempDir(), "workspaces")
	if err := os.Symlink(e.dir, remote); err != nil {
		t.Fatal(err)
	}
	return e, containertest.Install(t, remote), remote
}

func (e *env) containerOpts(name, harness string) SpawnOptions {
	o := e.opts(name)
	o.Harness, o.Container, o.Root = harness, true, e.dir
	return o
}

// TestSpawn_ContainerBootLine: spawn --container types the same short boot
// line and returns starting at once, with no devcontainer call (up runs in
// the pane); the boot then runs up and plans devcontainer exec
// --workspace-folder <root> wipnote claude, with the brief's result path
// mapped into the container's workspace folder.
func TestSpawn_ContainerBootLine(t *testing.T) {
	e, stub, remote := containerEnv(t)
	s, err := Spawn(context.Background(), e.deps, e.containerOpts("w1", HarnessClaude))
	if err != nil {
		t.Fatal(err)
	}
	if typed := e.herdr.pane(PaneOf(s)).typed; len(typed) != 1 || !strings.Contains(typed[0], "'delegate' 'boot'") {
		t.Fatalf("typed %q: want the boot line", typed)
	}
	if calls := stub.Calls(t); len(calls) != 0 {
		t.Fatalf("spawn called devcontainer %q; up must run in the pane", calls)
	}
	if r := e.row(s.Run); r.Host != PlaceContainer || r.Harness != HarnessClaude {
		t.Errorf("row host %q harness %q", r.Host, r.Harness)
	}
	argv, err := ContainerBoot(context.Background(), e.deps.Store, s.Run, "caller-1", nil)
	if err != nil {
		t.Fatal(err)
	}
	head := []string{container.CLI, "exec", "--workspace-folder", e.dir}
	if !slices.Equal(argv[:4], head) {
		t.Fatalf("boot argv %q: want it to start %q", argv, head)
	}
	i := slices.Index(argv, "wipnote")
	if i < 0 || !slices.Equal(argv[i:i+6], []string{"wipnote", "claude", "--in-place", "--name", "w1", "--"}) {
		t.Fatalf("boot argv %q: want wipnote claude --in-place --name w1 -- <brief>", argv)
	}
	brief := argv[len(argv)-1]
	if !strings.Contains(brief, filepath.Join(remote, ".wipnote", "delegate", "mail", "w1", "outbox")) ||
		strings.Contains(brief, filepath.Join(e.dir, ".wipnote")) || !strings.Contains(brief, "Do the thing.") {
		t.Errorf("brief %q: want the result path in the container and the task", brief)
	}
	for _, kv := range []string{"WIPNOTE_DELEGATE_PLACE=container", "WIPNOTE_DELEGATE_RUN=" + s.Run, "WIPNOTE_DELEGATE_CALLER=caller-1"} {
		if !slices.Contains(argv, kv) {
			t.Errorf("boot argv %q lacks --remote-env %s", argv, kv)
		}
	}
	if calls := stub.Calls(t); len(calls) != 1 || calls[0][0] != "up" {
		t.Errorf("boot calls %q: want one up", calls)
	}
	info, _ := ReadInfo(e.runFolder(s.Run))
	if info.Place != PlaceContainer || info.Workspace != remote {
		t.Errorf("info %+v: want place container and the workspace folder", info)
	}
}

// TestSpawn_ContainerRefusesBeforeWriting: no config, no CLI, or a Codex
// worker on a host that did not opt in: refused, no row, no pane. A
// host-opted-in Codex worker gets --sandbox danger-full-access.
func TestSpawn_ContainerRefusesBeforeWriting(t *testing.T) {
	e, _, _ := containerEnv(t)
	if _, err := Spawn(context.Background(), e.deps, e.containerOpts("w1", HarnessCodex)); !errors.Is(err, codex.ErrRefused) {
		t.Errorf("codex without opt-in: %v, want the ladder's refusal", err)
	}
	t.Setenv("PATH", t.TempDir())
	if _, err := Spawn(context.Background(), e.deps, e.containerOpts("w2", HarnessClaude)); !errors.Is(err, container.ErrNoCLI) {
		t.Errorf("no CLI: %v", err)
	}
	if recs, _ := e.deps.Ledger.ReadAll(); len(recs) != 0 || hasCall(e.herdr.Calls(), "tab") || hasCall(e.herdr.Calls(), "split") {
		t.Fatalf("refusals wrote %d rows, herdr calls %v", len(recs), e.herdr.Calls())
	}

	e2, _, _ := containerEnv(t)
	o := e2.containerOpts("w3", HarnessCodex)
	o.CodexOptIn = true
	s, err := Spawn(context.Background(), e2.deps, o)
	if err != nil {
		t.Fatal(err)
	}
	argv, err := ContainerBoot(context.Background(), e2.deps.Store, s.Run, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.Index(argv, "wipnote")
	if i < 0 || !slices.Equal(argv[i:i+6], []string{"wipnote", "codex", "--in-place", "--", "--sandbox", "danger-full-access"}) {
		t.Errorf("codex boot argv %q: want wipnote codex --in-place -- --sandbox danger-full-access", argv)
	}

	e3, _, _ := containerEnv(t)
	if err := os.Remove(filepath.Join(e3.dir, ".devcontainer", "devcontainer.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := Spawn(context.Background(), e3.deps, e3.containerOpts("w4", HarnessClaude)); !errors.Is(err, container.ErrNoConfig) {
		t.Errorf("no config: %v", err)
	}
}

// TestStop_ChecksContainerPids: stop records the worker's container pids
// (through devcontainer exec) as well as the host's, and after closing the
// pane reports a container pid still running with the same start. It never
// signals. Other side: a pid gone, or reused with another start, is not a
// survivor; a failed probe is named, not taken as none.
func TestStop_ChecksContainerPids(t *testing.T) {
	e, stub, _ := containerEnv(t)
	e.herdr.exitOnEnter = true
	s, err := Spawn(context.Background(), e.deps, e.containerOpts("w1", HarnessClaude))
	if err != nil {
		t.Fatal(err)
	}
	e.herdr.setRunning(PaneOf(s), herdr.Proc{PID: 4242, Name: "devcontainer"})
	e.herdr.pane(PaneOf(s)).styled = "> \n"
	e.setAlive(4242, "Mon Oct 26 10:00:00 2026")
	stub.SetPids(t, "77 5001 claude")
	res := stop(t, e, "w1")
	if !res.PaneClosed {
		t.Fatalf("%+v", res)
	}
	var where []string
	for _, sv := range res.Survivors {
		where = append(where, sv.Where)
	}
	if len(res.Survivors) != 2 || !slices.Contains(where, PlaceContainer) || !slices.Contains(where, PlaceHost) {
		t.Fatalf("survivors %+v: want the host pid and the container pid", res.Survivors)
	}
	probes := 0
	for _, c := range stub.Calls(t) {
		if c[0] == "exec" && strings.Contains(strings.Join(c, " "), "wipnote-pids") {
			probes++
		}
	}
	if probes != 2 {
		t.Errorf("%d pid probes, want one before and one after", probes)
	}

	e2, stub2, _ := containerEnv(t)
	e2.herdr.exitOnEnter = true
	s2, _ := Spawn(context.Background(), e2.deps, e2.containerOpts("w1", HarnessClaude))
	e2.herdr.pane(PaneOf(s2)).styled = "> \n"
	stub2.SetPids(t, "77 5001 claude", "78 5002 node")
	e2.deps.ContainerPids = reusedAfterFirst(t)
	if res := stop(t, e2, "w1"); len(res.Survivors) != 0 || res.Unchecked != "" {
		t.Errorf("gone or reused container pids: %+v", res)
	}

	e3, _, _ := containerEnv(t)
	e3.herdr.exitOnEnter = true
	s3, _ := Spawn(context.Background(), e3.deps, e3.containerOpts("w1", HarnessClaude))
	e3.herdr.pane(PaneOf(s3)).styled = "> \n"
	t.Setenv("STUB_PIDS_EXIT", "1")
	if res := stop(t, e3, "w1"); !res.PaneClosed || !strings.Contains(res.Unchecked, "container") {
		t.Errorf("failed probe: %+v, want the pane closed and the unchecked container pids named", res)
	}
}

// reusedAfterFirst answers the first probe with two pids and the second
// with one gone and one reused (another start).
func reusedAfterFirst(t *testing.T) func(context.Context, string, string) ([]container.Proc, error) {
	n := 0
	return func(context.Context, string, string) ([]container.Proc, error) {
		n++
		if n == 1 {
			return []container.Proc{{PID: 77, Start: "5001"}, {PID: 78, Start: "5002"}}, nil
		}
		return []container.Proc{{PID: 78, Start: "9999"}}, nil
	}
}

// TestList_FoldsContainerWorkerSession: list folds a container worker's own
// session file into the host's sessions ledger with host=container and the
// real harness, and its bind into worker_sessions.
func TestList_FoldsContainerWorkerSession(t *testing.T) {
	e, _, _ := containerEnv(t)
	s, err := Spawn(context.Background(), e.deps, e.containerOpts("w1", HarnessClaude))
	if err != nil {
		t.Fatal(err)
	}
	sid := "12121212-1111-2222-3333-444444444444"
	wipnoteDir := filepath.Join(e.dir, ".wipnote")
	t.Setenv(containerwrite.RunEnv, s.Run)
	t.Setenv(containerwrite.PlaceEnv, containerwrite.PlaceContainer)
	if _, err := sessionledger.NewStore(wipnoteDir).Open(sessionledger.Record{SessionID: sid, Harness: "claude", StartedAt: e.deps.now()}); err != nil {
		t.Fatal(err)
	}
	t.Setenv(containerwrite.PlaceEnv, "")
	e.event(s.Run, sid, runstore.KindSessionStart)
	if _, err := List(context.Background(), e.deps, false); err != nil {
		t.Fatal(err)
	}
	row, ok, _ := sessionledger.NewStore(wipnoteDir).Get(sid)
	if !ok || row.Host != sessionledger.HostContainer || row.Harness != "claude" {
		t.Errorf("host ledger row %+v (found %v): want host=container, claude", row, ok)
	}
	if r := e.row(s.Run); !slices.Contains(r.WorkerSessions, sid) {
		t.Errorf("worker_sessions %v lacks %s", r.WorkerSessions, sid)
	}
}

// bootedContainerWorker spawns a container worker, runs its boot (which
// records the workspace folder) and binds sid-1 with an empty input line,
// so send may type.
func bootedContainerWorker(t *testing.T, e *env, name string) Spawned {
	t.Helper()
	o := e.containerOpts(name, HarnessClaude)
	o.Brief = ""
	s, err := Spawn(context.Background(), e.deps, o)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ContainerBoot(context.Background(), e.deps.Store, s.Run, "", nil); err != nil {
		t.Fatal(err)
	}
	e.herdr.prompts = true
	e.herdr.pane(PaneOf(s)).styled = "> \n"
	e.event(s.Run, "sid-1", runstore.KindSessionStart)
	return s
}

// TestSend_ContainerWorkerGetsWorkspacePaths: every task sent to a container
// worker names its result and question files in the container's workspace
// folder, as the brief does; a host worker's tasks name host paths. A send
// before the boot recorded the workspace folder is refused, writing nothing.
func TestSend_ContainerWorkerGetsWorkspacePaths(t *testing.T) {
	e, _, remote := containerEnv(t)
	bootedContainerWorker(t, e, "w1")
	sent := e.send("w1", "Count the files.")
	msgs, _ := e.mailbox("w1").Read(runstore.Inbox)
	body := msgs[len(msgs)-1].Body
	inside := filepath.Join(remote, ".wipnote", "delegate", "mail", "w1", "outbox")
	if !strings.Contains(body, filepath.Join(inside, sent.Task+"-result.json")) || !strings.Contains(body, filepath.Join(inside, sent.Task+"-q1.json")) ||
		strings.Contains(body, e.mailbox("w1").Dir()) {
		t.Errorf("task body %q: want the result and question paths in the workspace folder only", body)
	}

	o2 := e.containerOpts("w2", HarnessClaude)
	o2.Brief = ""
	s2, err := Spawn(context.Background(), e.deps, o2)
	if err != nil {
		t.Fatal(err)
	}
	e.herdr.pane(PaneOf(s2)).styled = "> \n"
	e.event(s2.Run, "sid-2", runstore.KindSessionStart)
	before, _ := e.mailbox("w2").Read(runstore.Inbox)
	if _, err := Send(context.Background(), e.deps, "w2", "x"); err == nil || !strings.Contains(err.Error(), "workspace folder") {
		t.Errorf("send before the boot: %v, want a refusal naming the workspace folder", err)
	}
	if after, _ := e.mailbox("w2").Read(runstore.Inbox); len(after) != len(before) {
		t.Errorf("a refused send posted a message")
	}

	h := newEnv(t)
	h.worker("w3", "")
	hs := h.send("w3", "x")
	hm, _ := h.mailbox("w3").Read(runstore.Inbox)
	if !strings.Contains(hm[len(hm)-1].Body, filepath.Join(h.mailbox("w3").Dir(), "outbox", hs.Task+"-result.json")) {
		t.Errorf("host task body %q: want the host path", hm[len(hm)-1].Body)
	}
}

// ownSessionFor writes, as the container worker's hooks do, its own session
// row (started and ended) in the run folder.
func ownSessionFor(t *testing.T, e *env, run, sid string) {
	t.Helper()
	wipnoteDir := filepath.Join(e.dir, ".wipnote")
	t.Setenv(containerwrite.RunEnv, run)
	t.Setenv(containerwrite.PlaceEnv, containerwrite.PlaceContainer)
	s := sessionledger.NewStore(wipnoteDir)
	_, _ = s.Open(sessionledger.Record{SessionID: sid, Harness: "claude", StartedAt: e.deps.now().Add(-time.Hour)})
	_ = s.Close(sid, e.deps.now())
	t.Setenv(containerwrite.PlaceEnv, "")
}

// TestClose_FoldsContainerWorkerFirst: every path that closes a container
// worker's row (wait's loss close, spawn's cleanup, a lost pane in list or
// stop, a gone pane at spawn) folds its own files into the host's ledgers first. A host
// worker's close folds nothing.
func TestClose_FoldsContainerWorkerFirst(t *testing.T) {
	paths := map[string]func(e *env, s Spawned){
		"closeLostRun": func(e *env, s Spawned) { closeLostRun(e.deps, s.Run, []string{"sid-1"}) },
		"abandon": func(e *env, s Spawned) {
			rf, _, _ := loadInfo(e.deps.Store, s.Run)
			abandon(e.deps, s.Run, rf, "")
		},
		"loseWorker": func(e *env, s Spawned) {
			rf, info, _ := loadInfo(e.deps.Store, s.Run)
			loseWorker(e.deps, e.row(s.Run), rf, info)
		},
		"checkLive": func(e *env, s Spawned) {
			e.herdr.pane(PaneOf(s)).gone = true
			_, _ = checkLive(context.Background(), e.deps, e.containerOpts(s.Name, HarnessClaude))
		},
	}
	for name, closeIt := range paths {
		e, _, _ := containerEnv(t)
		s, err := Spawn(context.Background(), e.deps, e.containerOpts("w1", HarnessClaude))
		if err != nil {
			t.Fatal(err)
		}
		sid := "13131313-1111-2222-3333-44444444444" + string(rune('0'+len(name)%10))
		ownSessionFor(t, e, s.Run, sid)
		closeIt(e, s)
		if r := e.row(s.Run); r.IsOpen() {
			t.Errorf("%s: the row stayed open", name)
		}
		row, ok, _ := sessionledger.NewStore(filepath.Join(e.dir, ".wipnote")).Get(sid)
		if !ok || row.Host != sessionledger.HostContainer || row.IsOpen() {
			t.Errorf("%s: host ledger row %+v (found %v): want the folded, ended container session", name, row, ok)
		}
	}
	h := newEnv(t)
	hs := h.worker("w1", "")
	closeLostRun(h.deps, hs.Run, []string{"sid-1"})
	if _, err := os.Stat(filepath.Join(h.dir, ".wipnote", sessionledger.FileName)); !os.IsNotExist(err) {
		t.Errorf("a host worker's close wrote the sessions ledger (err %v)", err)
	}
}

// TestInfo_BootAndAdoptRaceKeepsWorkspace: list (or send) loads session.json
// before the boot records the workspace folder, then adopts the agent and
// saves. The interleaving is forced through the infoLoaded seam: the boot
// runs between the load and the save. The save must keep the boot's
// workspace folder (and the boot must keep the adoption), so later sends
// still find it. The other side: without the race both fields are there too.
func TestInfo_BootAndAdoptRaceKeepsWorkspace(t *testing.T) {
	for _, verb := range []string{"list", "send"} {
		e, _, remote := containerEnv(t)
		o := e.containerOpts("w1", HarnessClaude)
		o.Brief = ""
		s, err := Spawn(context.Background(), e.deps, o)
		if err != nil {
			t.Fatal(err)
		}
		e.herdr.prompts = true
		e.herdr.pane(PaneOf(s)).styled = "> \n"
		e.event(s.Run, "sid-1", runstore.KindSessionStart)
		e.herdr.agents = []herdr.Agent{{Pane: PaneOf(s), TerminalID: "term-1"}}
		booted := false
		infoLoaded = func(run string) {
			if run == s.Run && !booted {
				booted = true
				if _, err := ContainerBoot(context.Background(), e.deps.Store, s.Run, "", nil); err != nil {
					t.Error(err)
				}
			}
		}
		t.Cleanup(func() { infoLoaded = nil })
		if verb == "list" {
			_, _ = List(context.Background(), e.deps, false)
		} else {
			_, _ = Send(context.Background(), e.deps, "w1", "x")
		}
		info, _ := ReadInfo(e.runFolder(s.Run))
		if !booted || info.Workspace != remote || !info.Renamed || info.TerminalID != "term-1" {
			t.Errorf("%s racing the boot: info %+v, want the workspace folder %s and the adoption both kept", verb, info, remote)
		}
		infoLoaded = nil
	}
}
