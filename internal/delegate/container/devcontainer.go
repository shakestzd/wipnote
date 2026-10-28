// Package container runs delegate workers inside the project's own
// devcontainer (plan-28b4a7f6 slice 21, decision C): the one described by
// .devcontainer/devcontainer.json, started with `devcontainer up` and entered
// with `devcontainer exec --workspace-folder <root>`. It never names an
// image, a config override or a docker command, so a worker always runs on
// the project's own toolchain. These are the calls scripts/devcontainer.sh
// makes, in Go.
//
// The workspace bind mount carries .wipnote/delegate/, so the worker's hooks
// inside write their own files there (core/containerwrite) and Fold, on the
// host, folds them into the shared ledgers.
package container

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/shakestzd/wipnote/core/containerwrite"
)

// CLI is the devcontainer CLI's name on PATH.
const CLI = "devcontainer"

// ConfigPath is the project's devcontainer config, relative to the root.
const ConfigPath = ".devcontainer/devcontainer.json"

// Refusals. Test them with errors.Is.
var (
	ErrNoConfig = errors.New("delegate: --container needs the project's own devcontainer")
	ErrNoCLI    = errors.New("delegate: --container needs the devcontainer CLI")
	ErrUp       = errors.New("delegate: devcontainer up failed")
)

// Check refuses, naming the fix, when the project has no
// .devcontainer/devcontainer.json or the devcontainer CLI is not on PATH.
// Nothing else counts as the project's devcontainer: a worker is never put
// in an image of wipnote's choosing.
func Check(root string) error {
	cfg := filepath.Join(root, filepath.FromSlash(ConfigPath))
	if fi, err := os.Stat(cfg); err != nil || !fi.Mode().IsRegular() {
		return fmt.Errorf("%w: %s is missing; add the project's devcontainer config there, or run without --container", ErrNoConfig, ConfigPath)
	}
	if _, err := exec.LookPath(CLI); err != nil {
		return fmt.Errorf("%w: %s is not on PATH; install it with: npm install -g @devcontainers/cli", ErrNoCLI, CLI)
	}
	return nil
}

// Up is what `devcontainer up` prints.
type Up struct {
	Outcome               string `json:"outcome"`
	Message               string `json:"message,omitempty"`
	ContainerID           string `json:"containerId,omitempty"`
	RemoteUser            string `json:"remoteUser,omitempty"`
	RemoteWorkspaceFolder string `json:"remoteWorkspaceFolder,omitempty"`
}

// UpArgv starts or reuses the project's devcontainer (idempotent).
func UpArgv(root string) []string { return []string{CLI, "up", "--workspace-folder", root} }

// RunUp runs `devcontainer up` with its progress on progress, and reads the
// JSON it prints last. Anything but outcome success, or no workspace folder,
// is ErrUp with the CLI's message.
func RunUp(ctx context.Context, root string, progress io.Writer) (Up, error) {
	argv := UpArgv(root)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = root
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, progress
	runErr := cmd.Run()
	up, ok := lastUp(out.Bytes())
	switch {
	case !ok && runErr != nil:
		return Up{}, fmt.Errorf("%w: %v: %s", ErrUp, runErr, strings.TrimSpace(out.String()))
	case !ok:
		return Up{}, fmt.Errorf("%w: it printed no result: %s", ErrUp, strings.TrimSpace(out.String()))
	case up.Outcome != "success" || runErr != nil:
		return up, fmt.Errorf("%w: outcome %q: %s", ErrUp, up.Outcome, up.Message)
	case up.RemoteWorkspaceFolder == "":
		return up, fmt.Errorf("%w: no remoteWorkspaceFolder in its result", ErrUp)
	}
	return up, nil
}

// lastUp is the last line of out that decodes as up's JSON.
func lastUp(out []byte) (Up, bool) {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		var up Up
		if json.Unmarshal([]byte(strings.TrimSpace(lines[i])), &up) == nil && up.Outcome != "" {
			return up, true
		}
	}
	return Up{}, false
}

// CallerEnv names the caller's session inside the worker (session.CallerEnv).
const CallerEnv = "WIPNOTE_DELEGATE_CALLER"

// WorkerEnv is the env a worker gets inside: its place, its run and, when
// there is one, its caller. The hooks inside read it (core/containerwrite).
func WorkerEnv(run, caller string) []string {
	env := []string{containerwrite.PlaceEnv + "=" + containerwrite.PlaceContainer, containerwrite.RunEnv + "=" + run}
	if caller != "" {
		env = append(env, CallerEnv+"="+caller)
	}
	return env
}

// ExecArgv runs argv inside the project's devcontainer with env set there.
func ExecArgv(root string, env, argv []string) []string {
	out := []string{CLI, "exec", "--workspace-folder", root}
	for _, kv := range env {
		out = append(out, "--remote-env", kv)
	}
	return append(out, argv...)
}

// InDir runs argv in dir inside the container. devcontainer exec starts in
// the remote workspace folder, so a deeper folder needs a cd first.
func InDir(dir, remote string, argv []string) []string {
	if dir == "" || filepath.Clean(dir) == filepath.Clean(remote) {
		return argv
	}
	return append([]string{"sh", "-c", `cd "$1" && shift && exec "$@"`, "sh", dir}, argv...)
}
