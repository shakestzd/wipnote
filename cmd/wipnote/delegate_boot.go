package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/shakestzd/wipnote/core/delegationledger"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
	"github.com/shakestzd/wipnote/internal/delegate/session"
)

// defaultBootExec replaces this process with the launcher, so the pane's
// foreground is the harness and the pane returns to its shell when it exits.
func defaultBootExec(argv0 string, argv, env []string) error { return syscall.Exec(argv0, argv, env) }

// delegateBootExec is the exec boot uses; tests replace it.
var delegateBootExec = defaultBootExec

// delegateBootCmd runs in a worker's pane: spawn types it. It reads the run
// folder (session.json, brief.md), scrubs the inherited session env and
// execs wipnote claude or wipnote codex with the brief as argv.
func delegateBootCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "boot <run-id>",
		Short:  "Start a delegate worker's launcher inside its herdr pane (internal)",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			argv, env, err := delegateBootPlan(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			argv0, err := exec.LookPath(argv[0]) // an absolute path is returned as it is
			if err != nil {
				return fmt.Errorf("delegate boot: %w", err)
			}
			if err := delegateBootExec(argv0, argv, env); err != nil {
				return fmt.Errorf("delegate boot: exec %s: %w", argv[0], err)
			}
			return nil
		},
	}
}

func delegateBootPlan(ctx context.Context, runID string) ([]string, []string, error) {
	wipnoteDir, err := findWipnoteDir()
	if err != nil {
		return nil, nil, err
	}
	row, ok, err := delegationledger.NewStore(wipnoteDir).Get(runID)
	if err != nil || !ok || !row.IsOpen() || row.Mode != delegationledger.ModeInteractive {
		return nil, nil, fmt.Errorf("delegate boot: run %s is not an open interactive run (err %v)", runID, err)
	}
	store, err := runstore.New(filepath.Dir(wipnoteDir))
	if err != nil {
		return nil, nil, err
	}
	rf, err := store.Run(runID)
	if err != nil {
		return nil, nil, err
	}
	info, err := session.ReadInfo(rf)
	if err != nil {
		return nil, nil, err
	}
	if info.Place == session.PlaceContainer {
		return containerBootPlan(ctx, store, info)
	}
	brief, _, _ := rf.ReadFrom(runstore.FileBrief, 0) // no brief: a bare launch
	if info.Cwd != "" {
		_ = os.Chdir(info.Cwd)
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, nil, err
	}
	argv := session.BootArgv(exe, info, string(brief))
	return argv, session.BootEnv(os.Environ(), runID, os.Getenv(session.CallerEnv)), nil
}

// containerBootPlan is a container worker's boot (plan-28b4a7f6 slice 21):
// devcontainer up runs here, in the pane, with its progress shown, then
// boot execs devcontainer exec --workspace-folder <root> wipnote claude|codex.
func containerBootPlan(ctx context.Context, store *runstore.Store, info session.Info) ([]string, []string, error) {
	fmt.Fprintln(os.Stderr, "wipnote: starting the project's devcontainer (devcontainer up)...")
	caller := os.Getenv(session.CallerEnv)
	argv, err := session.ContainerBoot(ctx, store, info.Run, caller, os.Stderr)
	if err != nil {
		return nil, nil, fmt.Errorf("delegate boot: %w", err)
	}
	_ = os.Chdir(info.Root)
	return argv, session.BootEnv(os.Environ(), info.Run, caller), nil
}
