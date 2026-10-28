package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/shakestzd/wipnote/internal/delegate/codex"
	"github.com/shakestzd/wipnote/internal/delegate/container"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// Container workers (plan-28b4a7f6 slice 21, decision C). spawn --container
// refuses before anything is written when the project has no devcontainer
// config or the CLI is missing, and a Codex worker unless this host opted in
// (bwrap fails in containers; slice 12's ladder). It then types the same
// short boot line and returns starting at once: the boot, in the pane, runs
// devcontainer up (which can take minutes the first time) and execs
// devcontainer exec --workspace-folder <root> wipnote claude|codex.

// checkContainerSpawn is spawn's container refusals; nil for a host spawn.
func checkContainerSpawn(o SpawnOptions) error {
	if !o.Container {
		return nil
	}
	if o.Root == "" {
		return errors.New("delegate: spawn --container needs the project root")
	}
	if err := container.Check(o.Root); err != nil {
		return err
	}
	if _, err := (container.Mapper{Root: o.Root, Remote: "/"}).Path(o.Cwd); err != nil {
		return fmt.Errorf("delegate: spawn --container: the working folder must be inside the project: %w", err)
	}
	if o.Harness == HarnessCodex && !o.CodexOptIn {
		return fmt.Errorf("delegate: %w: Codex's bwrap sandbox does not work inside the devcontainer, so a Codex worker there runs with danger-full-access, and this host is not in delegate.codex.%s in ~/.wipnote/config.json. Add it when the container is the isolation boundary, or spawn on the host. Run \"wipnote delegate doctor\" to check",
			codex.ErrRefused, codex.OptInKey)
	}
	return nil
}

func placeOf(o SpawnOptions) string {
	if o.Container {
		return PlaceContainer
	}
	return PlaceHost
}

// ContainerBoot runs in the worker's pane: devcontainer up (its progress on
// progress), then the devcontainer exec argv that starts the launcher
// inside, with the cwd and the brief's result path mapped into the
// container's workspace folder. It records the workspace folder in the run
// folder (session.json and container.json).
func ContainerBoot(ctx context.Context, store *runstore.Store, run, caller string, progress io.Writer) ([]string, error) {
	rf, info, err := loadInfo(store, run)
	if err != nil {
		return nil, err
	}
	if info.Place != PlaceContainer || info.Root == "" {
		return nil, fmt.Errorf("delegate boot: run %s is not a container worker", run)
	}
	up, err := container.RunUp(ctx, info.Root, progress)
	if err != nil {
		return nil, err
	}
	info.Workspace = up.RemoteWorkspaceFolder
	if err := recordContainer(rf, info, up); err != nil {
		return nil, err
	}
	m := container.Mapper{Root: info.Root, Remote: up.RemoteWorkspaceFolder}
	cwd, err := m.Path(info.Cwd)
	if err != nil {
		return nil, err
	}
	brief, err := containerBrief(store, rf, info, m)
	if err != nil {
		return nil, err
	}
	env := append(container.WorkerEnv(run, caller), termEnv()...)
	return container.ExecArgv(info.Root, env, container.InDir(cwd, up.RemoteWorkspaceFolder, containerArgv(info, brief))), nil
}

func recordContainer(rf *runstore.RunFolder, info Info, up container.Up) error {
	if err := WriteInfo(rf, info); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(map[string]string{"place": PlaceContainer, "workspace_folder": up.RemoteWorkspaceFolder,
		"container_id": up.ContainerID, "remote_user": up.RemoteUser}, "", "  ")
	return rf.WriteFile(runstore.FileContainer, append(data, '\n'))
}

// containerBrief is brief.md with its preamble's result path in the
// container. A brief that does not start with its own preamble is refused:
// nothing is guessed about where the worker writes its answer.
func containerBrief(store *runstore.Store, rf *runstore.RunFolder, info Info, m container.Mapper) (string, error) {
	brief, _, _ := rf.ReadFrom(runstore.FileBrief, 0)
	if len(brief) == 0 {
		return "", nil
	}
	mb, err := store.Mailbox(info.Name)
	if err != nil {
		return "", err
	}
	host := Preamble(info, mb.Dir())
	if !strings.HasPrefix(string(brief), host) {
		return "", fmt.Errorf("delegate boot: %s does not start with its result preamble; nothing was started", runstore.FileBrief)
	}
	remote, err := m.Path(mb.Dir())
	if err != nil {
		return "", err
	}
	return Preamble(info, remote) + strings.TrimPrefix(string(brief), host), nil
}

// containerArgv is the launcher inside: the container's own wipnote, in
// place. A Codex worker gets exactly one --sandbox, danger-full-access (its
// host opted in at spawn), so the launcher's own fallback never decides.
func containerArgv(info Info, brief string) []string {
	argv := BootArgv("wipnote", info, "")
	if info.Harness == HarnessCodex {
		argv = append(argv, "--", "--sandbox", string(codex.DangerFullAccess))
		if brief != "" {
			argv = append(argv, brief)
		}
		return argv
	}
	if brief != "" {
		argv = append(argv, "--", brief)
	}
	return argv
}

// termEnv is what scripts/devcontainer.sh passes for an interactive shell.
func termEnv() []string {
	color := os.Getenv("COLORTERM")
	if color == "" {
		color = "truecolor"
	}
	return []string{"TERM=xterm-256color", "COLORTERM=" + color}
}

// foldContainer folds a container worker's own files into the host's
// ledgers; a host worker has none. It never fails the caller.
func foldContainer(d Deps, rf *runstore.RunFolder, info Info) {
	if info.Place == PlaceContainer && rf != nil {
		_, _ = container.Fold(filepath.Dir(d.Store.Root()), rf.Dir())
	}
}
