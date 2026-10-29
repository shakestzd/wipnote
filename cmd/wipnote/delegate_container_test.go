package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/containerwrite"
	"github.com/shakestzd/wipnote/core/delegationledger"
	"github.com/shakestzd/wipnote/core/sessionledger"
	"github.com/shakestzd/wipnote/internal/delegate/container/containertest"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
	"github.com/shakestzd/wipnote/internal/delegate/session"
)

// containerCmdProject is a delegate run project with the project's own
// devcontainer config, a stub devcontainer CLI, and the cwd in the project.
func containerCmdProject(t *testing.T) (dir, remote string, stub *containertest.Stub) {
	t.Helper()
	dir = delegateRunProject(t)
	if err := os.MkdirAll(filepath.Join(dir, ".devcontainer"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".devcontainer", "devcontainer.json"), []byte(`{"name":"t"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	remote = filepath.Join(t.TempDir(), "workspaces")
	if err := os.Symlink(dir, remote); err != nil {
		t.Fatal(err)
	}
	stub = containertest.Install(t, remote)
	t.Chdir(dir)
	return dir, remote, stub
}

func TestDelegateRun_ContainerFlag(t *testing.T) {
	dir, _, stub := containerCmdProject(t)
	if _, code := runDelegate(t, "--container", "--", "claude", "-p", "x"); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if r := onlyRow(t, dir); r.Host != "container" || r.Harness != "claude-print" {
		t.Errorf("row host %q harness %q, want container and claude-print", r.Host, r.Harness)
	}
	if calls := stub.Calls(t); len(calls) < 2 || calls[0][0] != "up" || calls[1][0] != "exec" {
		t.Errorf("calls %q: want up then exec", calls)
	}
	// Other side: without --container the same run stays on the host.
	runDelegate(t, "--", "claude", "-p", "x")
	if r := onlyRow(t, dir); r.Host != "host" {
		t.Errorf("host run recorded host %q", r.Host)
	}
	if err := os.Remove(filepath.Join(dir, ".devcontainer", "devcontainer.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := execDelegate(t, "run", "--container", "--", "claude", "-p", "x"); err == nil || !strings.Contains(err.Error(), ".devcontainer/devcontainer.json") {
		t.Errorf("no config: %v, want the refusal naming the file", err)
	}
}

func TestDelegateSpawn_ContainerFlag(t *testing.T) {
	dir, _, _ := containerCmdProject(t)
	writeFeature(t, filepath.Join(dir, ".wipnote"), "feat-0a1b2c3d") // spawn resolves the item first (slice 16)
	t.Setenv("WIPNOTE_SESSION_ID", "sess-delegate-spawn-test")
	_, err := execDelegate(t, "spawn", "w1", "--harness", "claude", "--work-item", "feat-0a1b2c3d", "--container")
	if err == nil || strings.Contains(err.Error(), "unknown flag") || !strings.Contains(err.Error(), "herdr") {
		t.Fatalf("spawn --container outside herdr: %v, want the herdr refusal", err)
	}
}

// boot of a container worker runs devcontainer up in the pane, then execs
// devcontainer exec --workspace-folder <root> wipnote claude.
func TestDelegateBoot_ContainerExecsDevcontainer(t *testing.T) {
	dir, remote, stub := containerCmdProject(t)
	store, _ := runstore.New(dir)
	row, err := delegationledger.NewStore(filepath.Join(dir, ".wipnote")).Open(delegationledger.Record{
		Mode: delegationledger.ModeInteractive, Harness: "claude", Name: "w1", WorkItem: "feat-0a1b2c3d", Host: "container",
	})
	if err != nil {
		t.Fatal(err)
	}
	rf, _ := store.CreateRun(row.ID)
	info := session.Info{Run: row.ID, Name: "w1", Harness: "claude", Cwd: dir, Place: session.PlaceContainer, Root: dir}
	if err := session.WriteInfo(rf, info); err != nil {
		t.Fatal(err)
	}
	var gotArgv []string
	var gotArgv0 string
	delegateBootExec = func(argv0 string, argv, env []string) error {
		gotArgv0, gotArgv = argv0, argv
		return nil
	}
	t.Cleanup(func() { delegateBootExec = defaultBootExec })
	if _, err := execDelegate(t, "boot", row.ID); err != nil {
		t.Fatal(err)
	}
	if gotArgv0 != filepath.Join(stub.Dir, "devcontainer") || !slices.Equal(gotArgv[:4], []string{"devcontainer", "exec", "--workspace-folder", dir}) {
		t.Fatalf("exec %s %q: want devcontainer exec --workspace-folder <root>", gotArgv0, gotArgv)
	}
	if i := slices.Index(gotArgv, "wipnote"); i < 0 || gotArgv[i+1] != "claude" {
		t.Errorf("argv %q: want wipnote claude inside", gotArgv)
	}
	if got, _ := session.ReadInfo(rf); got.Workspace != remote {
		t.Errorf("workspace folder %q, want %q", got.Workspace, remote)
	}
}

// fakeInside puts a wipnote and a claude for the container checks first on
// PATH (the stub CLI runs exec's command on the host).
func fakeInside(t *testing.T) {
	t.Helper()
	bin := t.TempDir()
	wipnote := `#!/bin/sh
case "$1" in
version) echo "wipnote ${STUB_INSIDE_VERSION:-99.0.0} (go)" ;;
hook)
  [ "$WIPNOTE_DELEGATE_PLACE" = container ] || exit 0
  cat > /dev/null
  printf '{"v":1,"kind":"prompt"}\n' > "$4/.wipnote/delegate/runs/$WIPNOTE_DELEGATE_RUN/events-wipnote-doctor-probe.ndjson"
  echo '{"continue":true}' ;;
esac
`
	claude := `#!/bin/sh
if [ "$1 $2" = "auth status" ]; then
  if [ "${STUB_AUTH_EXIT:-0}" = 0 ]; then echo "Logged in as test"; else echo "Not logged in"; fi
  exit "${STUB_AUTH_EXIT:-0}"
fi
exit 0
`
	for name, body := range map[string]string{"wipnote": wipnote, "claude": claude} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func runDoctorArgs(t *testing.T, args ...string) (string, error) {
	t.Helper()
	return execDelegate(t, append([]string{"doctor"}, args...)...)
}

// TestDoctor_ContainerChecks: doctor --container runs devcontainer up,
// finds wipnote inside, fires a hook inside whose file reaches the host,
// and checks the Claude config folder and the login, naming each fix.
func TestDoctor_ContainerChecks(t *testing.T) {
	dir, _, stub := containerCmdProject(t)
	fakeInside(t)
	if err := os.MkdirAll(filepath.Join(os.Getenv("HOME"), ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	out, _ := runDoctorArgs(t, "--container")
	for _, want := range []string{"pass  devcontainer:", "pass  devcontainer up:", "pass  wipnote inside:", "pass  hook inside:",
		"pass  claude config writable:", "pass  claude login:"} {
		if !strings.Contains(out, want) {
			t.Errorf("doctor --container lacks %q:\n%s", want, out)
		}
	}
	if entries, _ := os.ReadDir(filepath.Join(dir, ".wipnote", "delegate", "runs")); len(entries) != 0 {
		t.Errorf("doctor left probe run folders: %v", entries)
	}

	t.Setenv("STUB_INSIDE_VERSION", "0.71.0")
	out, err := runDoctorArgs(t, "--container")
	if err == nil || !strings.Contains(out, "fail  wipnote inside: wipnote 0.71.0 inside") || !strings.Contains(out, "wipnote upgrade") {
		t.Errorf("old wipnote inside: err %v\n%s", err, out)
	}
	t.Setenv("STUB_INSIDE_VERSION", "")

	t.Setenv("STUB_AUTH_EXIT", "1")
	out, err = runDoctorArgs(t, "--container")
	if err == nil || !strings.Contains(out, "fail  claude login:") || !strings.Contains(out, "/login") {
		t.Errorf("not logged in: err %v\n%s", err, out)
	}

	t.Setenv("STUB_AUTH_EXIT", "0")
	if err := os.RemoveAll(filepath.Join(os.Getenv("HOME"), ".claude")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(os.Getenv("HOME"), ".claude"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	out, err = runDoctorArgs(t, "--container")
	if err == nil || !strings.Contains(out, "fail  claude config writable:") || !strings.Contains(out, "chown") || !strings.Contains(out, "skip  claude login:") {
		t.Errorf("unwritable ~/.claude: err %v\n%s", err, out)
	}

	// Other side: without --container nothing runs in the container.
	before := len(stub.Calls(t))
	out, _ = runDoctorArgs(t)
	if !strings.Contains(out, "skip  devcontainer:") || len(stub.Calls(t)) != before {
		t.Errorf("doctor without --container:\n%s\ncalls %d -> %d", out, before, len(stub.Calls(t)))
	}
}

// TestDelegateList_FoldsPendingContainerFolders: delegate list folds a
// container run folder whose own files were written after its last fold
// (here: never folded, its row already closed), so a late session record
// still reaches the host's sessions ledger. The other side: with nothing
// pending, list writes no sessions ledger.
func TestDelegateList_FoldsPendingContainerFolders(t *testing.T) {
	dir := delegateSessionProject(t)
	wipnoteDir := filepath.Join(dir, ".wipnote")
	if _, err := execDelegate(t, "list"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(wipnoteDir, sessionledger.FileName)); !os.IsNotExist(err) {
		t.Fatalf("list with nothing pending wrote the sessions ledger (err %v)", err)
	}
	store, _ := runstore.New(dir)
	run := delegationledger.NewRunID()
	if _, err := store.CreateRun(run); err != nil {
		t.Fatal(err)
	}
	sid := "c0c0c0c0-1111-2222-3333-444444444444"
	t.Setenv(containerwrite.RunEnv, run)
	t.Setenv(containerwrite.PlaceEnv, containerwrite.PlaceContainer)
	s := sessionledger.NewStore(wipnoteDir)
	_, _ = s.Open(sessionledger.Record{SessionID: sid, Harness: "claude", StartedAt: time.Now().UTC().Add(-time.Hour)})
	_ = s.Close(sid, time.Now().UTC())
	t.Setenv(containerwrite.PlaceEnv, "")
	t.Setenv(containerwrite.RunEnv, "")
	if _, err := execDelegate(t, "list"); err != nil {
		t.Fatal(err)
	}
	row, ok, _ := sessionledger.NewStore(wipnoteDir).Get(sid)
	if !ok || row.IsOpen() || row.Host != sessionledger.HostContainer {
		t.Errorf("host row %+v (found %v): want the folded, ended container session", row, ok)
	}
}
