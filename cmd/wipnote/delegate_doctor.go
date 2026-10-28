package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/shakestzd/wipnote/core/delegationledger"
	"github.com/shakestzd/wipnote/core/paths"
	"github.com/shakestzd/wipnote/internal/delegate/codex"
	"github.com/shakestzd/wipnote/internal/delegate/container"
	"github.com/shakestzd/wipnote/internal/delegate/oneshot"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// delegateDoctorCmd is `wipnote delegate doctor` (plan-28b4a7f6 slice 12):
// pass/warn/fail per check, with the bwrap probe's exact output, for what a
// Codex worker on this host needs.
func delegateDoctorCmd() *cobra.Command {
	var inContainer bool
	cmd := &cobra.Command{
		Use:   "doctor [--container]",
		Short: "Check what delegated workers need on this host (codex, bwrap sandbox, opt-in, devcontainer)",
		Long: `Checks the codex binary, probes Codex's bwrap sandbox (Linux), shows whether
this host is in delegate.codex.dangerFullAccessHosts (~/.wipnote/config.json
only, never a committed file) and the --sandbox a worker gets by default and
with --write. The probe result refreshes the cache delegate run reads.

--container also checks the project's devcontainer for --container workers:
the config and the devcontainer CLI, devcontainer up (it starts the container),
wipnote inside, a wipnote hook firing inside and reaching the host's
.wipnote/delegate/, the container user's ~/.claude being writable, and Claude
Code being logged in there. Each failure names its fix.
Exits non-zero when a check fails.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if n := runDelegateDoctor(cmd.Context(), cmd.OutOrStdout(), inContainer); n > 0 {
				return fmt.Errorf("delegate doctor: %d check(s) failed", n)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&inContainer, "container", false, "also check the project's devcontainer (runs devcontainer up)")
	return cmd
}

func runDelegateDoctor(ctx context.Context, w io.Writer, inContainer bool) int {
	projectDir := ""
	if wipnoteDir, err := findWipnoteDir(); err == nil {
		projectDir = filepath.Dir(wipnoteDir)
	}
	s := codexSandboxOptions(projectDir, false)
	pf := codexPreflight(projectDir, s.Host)
	checks := codex.Doctor(ctx, codex.DoctorOptions{Host: s.Host, OptIn: s.OptIn, OptInNotes: s.notes, Probe: pf.Fresh(ctx)})
	checks = append(checks, herdrCheck())
	return codex.WriteChecks(w, append(checks, containerChecks(ctx, projectDir, inContainer)...))
}

// containerChecks are the devcontainer checks (slice 21), only with
// --container: they start the container. The hook check runs as a worker
// of a probe run folder that is removed afterwards.
func containerChecks(ctx context.Context, projectDir string, inContainer bool) []codex.Check {
	if !inContainer {
		return []codex.Check{{Name: "devcontainer", Status: codex.CheckSkip, Detail: "pass --container to check the project's devcontainer (it runs devcontainer up)"}}
	}
	if projectDir == "" {
		return []codex.Check{{Name: "devcontainer", Status: codex.CheckFail, Detail: "no wipnote project here (.wipnote/ not found)"}}
	}
	store, err := runstore.New(projectDir)
	if err != nil {
		return []codex.Check{{Name: "devcontainer", Status: codex.CheckFail, Detail: err.Error()}}
	}
	probe := delegationledger.NewRunID()
	rf, err := store.CreateRun(probe)
	if err != nil {
		return []codex.Check{{Name: "devcontainer", Status: codex.CheckFail, Detail: err.Error()}}
	}
	defer os.RemoveAll(rf.Dir())
	return container.Doctor(ctx, container.DoctorOptions{Root: projectDir, ProbeRun: probe, ProbeDir: rf.Dir(), Progress: io.Discard})
}

// herdrCheck: panes need herdr on PATH and HERDR_ENV=1; without them runs
// are headless, which is not a failure.
func herdrCheck() codex.Check {
	path, err := exec.LookPath("herdr")
	switch {
	case err != nil:
		return codex.Check{Name: "herdr", Status: codex.CheckSkip, Detail: "herdr is not on PATH: runs are headless"}
	case os.Getenv("HERDR_ENV") != "1":
		return codex.Check{Name: "herdr", Status: codex.CheckSkip, Detail: path + ": HERDR_ENV is not 1, so runs are headless here"}
	}
	return codex.Check{Name: "herdr", Status: codex.CheckPass, Detail: path + ": runs open in a pane"}
}

// sandboxSetup is oneshot.SandboxOptions plus the notes about ignored
// opt-in keys.
type sandboxSetup struct {
	oneshot.SandboxOptions
	notes []string
}

// codexSandboxOptions reads this host's name and the user-config opt-in
// for the Codex sandbox ladder; the preflight is cached in the run store.
func codexSandboxOptions(projectDir string, write bool) sandboxSetup {
	host, _ := os.Hostname()
	var ignored []string
	if projectDir != "" {
		ignored = []string{paths.ProjectConfigPath(projectDir), paths.LocalConfigPath(projectDir)}
	}
	hosts, notes := codex.LoadOptIn(paths.UserConfigPath(), ignored...)
	pf := codexPreflight(projectDir, host)
	return sandboxSetup{SandboxOptions: oneshot.SandboxOptions{Write: write, Host: host, OptIn: hosts, Preflight: pf.Result}, notes: notes}
}

func codexPreflight(projectDir, host string) codex.Preflight {
	pf := codex.Preflight{Host: host}
	if projectDir != "" {
		pf.Dir = filepath.Join(projectDir, ".wipnote", runstore.DirName)
	}
	return pf
}
