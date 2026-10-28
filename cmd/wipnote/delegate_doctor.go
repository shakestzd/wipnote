package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/shakestzd/wipnote/core/paths"
	"github.com/shakestzd/wipnote/internal/delegate/codex"
	"github.com/shakestzd/wipnote/internal/delegate/oneshot"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// delegateDoctorCmd is `wipnote delegate doctor` (plan-28b4a7f6 slice 12):
// pass/warn/fail per check, with the bwrap probe's exact output, for what a
// Codex worker on this host needs.
func delegateDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check what delegated Codex workers need on this host (codex, bwrap sandbox, opt-in)",
		Long: `Checks the codex binary, probes Codex's bwrap sandbox (Linux), shows whether
this host is in delegate.codex.dangerFullAccessHosts (~/.wipnote/config.json
only, never a committed file) and the --sandbox a worker gets by default and
with --write. The probe result refreshes the cache delegate run reads.
Exits non-zero when a check fails.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if n := runDelegateDoctor(cmd.Context(), cmd.OutOrStdout()); n > 0 {
				return fmt.Errorf("delegate doctor: %d check(s) failed", n)
			}
			return nil
		},
	}
}

func runDelegateDoctor(ctx context.Context, w io.Writer) int {
	projectDir := ""
	if wipnoteDir, err := findWipnoteDir(); err == nil {
		projectDir = filepath.Dir(wipnoteDir)
	}
	s := codexSandboxOptions(projectDir, false)
	pf := codexPreflight(projectDir, s.Host)
	checks := codex.Doctor(ctx, codex.DoctorOptions{Host: s.Host, OptIn: s.OptIn, OptInNotes: s.notes, Probe: pf.Fresh(ctx)})
	return codex.WriteChecks(w, append(checks, herdrCheck()))
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
