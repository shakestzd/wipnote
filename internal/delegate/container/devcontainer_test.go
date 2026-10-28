package container

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/internal/delegate/container/containertest"
)

func TestContainer_RefusesWithoutConfig(t *testing.T) {
	root, remote := containertest.Project(t)
	containertest.Install(t, remote)
	if err := Check(root); err != nil {
		t.Fatalf("with a config: %v", err)
	}
	bare := t.TempDir()
	err := Check(bare)
	if !errors.Is(err, ErrNoConfig) {
		t.Fatalf("without a config: %v, want ErrNoConfig", err)
	}
	for _, want := range []string{".devcontainer/devcontainer.json", "add"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q does not name %q", err, want)
		}
	}
	// A root-level .devcontainer.json is not the project's devcontainer
	// folder: refused, never guessed at.
	if err := os.WriteFile(filepath.Join(bare, ".devcontainer.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Check(bare); !errors.Is(err, ErrNoConfig) {
		t.Errorf("root .devcontainer.json: %v, want ErrNoConfig", err)
	}
}

func TestContainer_RefusesWithoutCLI(t *testing.T) {
	root, remote := containertest.Project(t)
	t.Setenv("PATH", t.TempDir())
	err := Check(root)
	if !errors.Is(err, ErrNoCLI) {
		t.Fatalf("without the CLI: %v, want ErrNoCLI", err)
	}
	if !strings.Contains(err.Error(), "npm install -g @devcontainers/cli") {
		t.Errorf("refusal %q does not name the install", err)
	}
	containertest.Install(t, remote)
	if err := Check(root); err != nil {
		t.Errorf("with the CLI: %v", err)
	}
}

// TestContainer_UpThenExec: up runs first with --workspace-folder and its JSON
// is read; exec then runs the command inside with the worker env. A failed up
// is an error naming its message, and nothing is executed after it.
func TestContainer_UpThenExec(t *testing.T) {
	root, remote := containertest.Project(t)
	stub := containertest.Install(t, remote)
	up, err := RunUp(context.Background(), root, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if up.RemoteWorkspaceFolder != remote || up.ContainerID != "c0ffee" || up.RemoteUser != "vscode" {
		t.Errorf("up = %+v", up)
	}
	out := filepath.Join(t.TempDir(), "env.txt")
	argv := ExecArgv(root, WorkerEnv("dlg-0123456789ab", "caller-1"), []string{"sh", "-c", `echo "$WIPNOTE_DELEGATE_PLACE $WIPNOTE_DELEGATE_RUN" > "$1"`, "sh", out})
	if b, err := exec.Command(argv[0], argv[1:]...).CombinedOutput(); err != nil {
		t.Fatalf("exec: %v %s", err, b)
	}
	if got, _ := os.ReadFile(out); strings.TrimSpace(string(got)) != "container dlg-0123456789ab" {
		t.Errorf("worker env inside = %q", got)
	}
	calls := stub.Calls(t)
	if len(calls) != 2 || !slices.Equal(calls[0], []string{"up", "--workspace-folder", root}) || calls[1][0] != "exec" {
		t.Fatalf("calls %q: want up then exec", calls)
	}

	t.Setenv("STUB_UP_FAIL", "1")
	if _, err := RunUp(context.Background(), root, io.Discard); err == nil || !strings.Contains(err.Error(), "stub up failed") {
		t.Errorf("failed up: %v, want its message", err)
	}
}

func TestContainer_MapsPathsToRemoteWorkspace(t *testing.T) {
	m := Mapper{Root: "/host/proj", Remote: "/workspaces/proj"}
	cases := map[string]string{
		"/host/proj":                            "/workspaces/proj",
		"/host/proj/.wipnote/delegate/runs/r/x": "/workspaces/proj/.wipnote/delegate/runs/r/x",
		"/host/proj/sub":                        "/workspaces/proj/sub",
	}
	for in, want := range cases {
		if got, err := m.Path(in); err != nil || got != want {
			t.Errorf("Path(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, out := range []string{"/host/project-other", "/etc/passwd", "relative/x", "/host"} {
		if got, err := m.Path(out); err == nil {
			t.Errorf("Path(%q) = %q, want a refusal (outside the workspace)", out, got)
		}
	}
	argv := []string{"codex", "exec", "--output-last-message", "/host/proj/.wipnote/l.md", "--cd=/host/proj/sub", "fix /host/proj/a.go now", "/etc/hosts"}
	want := []string{"codex", "exec", "--output-last-message", "/workspaces/proj/.wipnote/l.md", "--cd=/workspaces/proj/sub", "fix /host/proj/a.go now", "/etc/hosts"}
	if got := m.Argv(argv); !slices.Equal(got, want) {
		t.Errorf("Argv = %q\nwant %q", got, want)
	}
}

// TestContainer_NeverUsesAdHocImage: every call wipnote makes names the
// project's workspace folder and no image, config override or docker run.
func TestContainer_NeverUsesAdHocImage(t *testing.T) {
	root := "/host/proj"
	all := [][]string{UpArgv(root), ExecArgv(root, WorkerEnv("dlg-0123456789ab", ""), []string{"wipnote", "claude"}), PidsArgv(root, "dlg-0123456789ab")}
	for _, argv := range all {
		if len(argv) < 4 || argv[0] != CLI || (argv[1] != "up" && argv[1] != "exec") || argv[2] != "--workspace-folder" || argv[3] != root {
			t.Errorf("argv %q does not start with devcontainer up|exec --workspace-folder <root>", argv)
			continue
		}
		for _, w := range argv[:4] {
			for _, bad := range []string{"--image", "--override-config", "--config", "docker", "--id-label", "--container-id"} {
				if strings.Contains(w, bad) {
					t.Errorf("argv %q uses %s", argv, bad)
				}
			}
		}
		for _, w := range argv {
			if strings.HasPrefix(w, "--image") || strings.HasPrefix(w, "--override-config") || w == "--config" {
				t.Errorf("argv %q names an ad-hoc image or config", argv)
			}
		}
	}
}

// TestContainer_Pids: the pid probe's lines become processes; a failed
// probe is an error, never an empty list.
func TestContainer_Pids(t *testing.T) {
	root, remote := containertest.Project(t)
	stub := containertest.Install(t, remote)
	stub.SetPids(t, "41 9001 claude", "42 9002 node", "junk")
	got, err := Pids(context.Background(), root, "dlg-0123456789ab")
	if err != nil {
		t.Fatal(err)
	}
	want := []Proc{{PID: 41, Start: "9001", Name: "claude"}, {PID: 42, Start: "9002", Name: "node"}}
	if !slices.Equal(got, want) {
		t.Errorf("Pids = %+v, want %+v", got, want)
	}
	t.Setenv("STUB_PIDS_EXIT", "1")
	if _, err := Pids(context.Background(), root, "dlg-0123456789ab"); err == nil {
		t.Error("a failed probe gave no error")
	}
	if _, err := Pids(context.Background(), root, "not a run; rm -rf /"); err == nil {
		t.Error("a bad run id was put in the probe")
	}
}

// TestContainer_MapsPathsWithSpaces: a checkout whose path has a space still
// gets its path arguments mapped (a lone path, a path flag's value, and
// --flag=PATH), while prompt text that merely starts with a path is left as
// given.
func TestContainer_MapsPathsWithSpaces(t *testing.T) {
	m := Mapper{Root: "/host/My Project", Remote: "/workspaces/My Project"}
	argv := []string{"codex", "exec", "--output-last-message", "/host/My Project/.wipnote/l.md", "--cd=/host/My Project/sub",
		"-C", "/host/My Project/sub dir", "/host/My Project/last.md", "fix /host/My Project/a.go now", "/host/My Project/README.md explain it"}
	want := []string{"codex", "exec", "--output-last-message", "/workspaces/My Project/.wipnote/l.md", "--cd=/workspaces/My Project/sub",
		"-C", "/workspaces/My Project/sub dir", "/workspaces/My Project/last.md", "fix /host/My Project/a.go now", "/host/My Project/README.md explain it"}
	if got := m.Argv(argv, "/host/My Project/last.md"); !slices.Equal(got, want) {
		t.Errorf("Argv = %q\nwant %q", got, want)
	}
}
