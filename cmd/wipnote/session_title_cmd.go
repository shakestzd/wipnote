package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/shakestzd/wipnote/core/hooks"
	"github.com/shakestzd/wipnote/internal/launcher"
)

// Generated session titles (feat-00e8112d). A hook starts this command
// detached; it asks a small model for a title and leaves it for the next
// prompt. It prints nothing and always exits 0: a failure leaves the name.

const (
	sessionTitleModel   = "haiku"
	sessionTitleTimeout = 30 * time.Second
)

// sessionTitleModelCall is the model call; a package var so tests never run
// a real model.
var sessionTitleModelCall hooks.TitleModel = haikuTitle

func sessionTitleCmd() *cobra.Command {
	var job hooks.TitleJob
	cmd := &cobra.Command{
		Use:    "session-title",
		Hidden: true,
		Short:  "Internal: write a generated title for a session (started by hooks)",
		RunE: func(_ *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), sessionTitleTimeout)
			defer cancel()
			_ = hooks.GenerateSessionTitle(ctx, job, sessionTitleModelCall)
			return nil
		},
	}
	cmd.Flags().StringVar(&job.ProjectDir, "project", "", "project whose .wipnote holds the session state")
	cmd.Flags().StringVar(&job.RepoDir, "repo", "", "git directory the session works in")
	cmd.Flags().StringVar(&job.SessionID, "session", "", "session id")
	cmd.Flags().StringVar(&job.Source, "from", hooks.TitleFromPrompt, "prompt or work")
	return cmd
}

// sessionTitleArgs is the claude argv for one title: print mode, the small
// model, one turn, no tools, plus the lean flags this install supports
// (no plugins, hooks, skills or saved session).
func sessionTitleArgs(ctx context.Context, system string) []string {
	args := []string{
		"-p", "--output-format", "json",
		"--model", sessionTitleModel,
		"--max-turns", "1",
		"--permission-mode", "dontAsk",
		"--tools", "",
		"--system-prompt", system,
	}
	return append(args, supportedClaudeLeanFlags(ctx)...)
}

// haikuTitle runs `claude -p` with the input on stdin. WIPNOTE_HEADLESS
// makes any wipnote hook in the child return at once, so the call never
// recurses into title generation.
func haikuTitle(ctx context.Context, system, input string) (string, error) {
	cmd := exec.CommandContext(ctx, "claude", sessionTitleArgs(ctx, system)...)
	cmd.Stdin = strings.NewReader(input)
	cmd.Env = launcher.AppendOrReplaceEnv(os.Environ(), headlessHookEnv+"=1", hooks.LaunchNameEnv+"=")
	cmd.WaitDelay = time.Second
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	res, err := parseClaudeOutput(out.Bytes())
	if err != nil {
		return "", err
	}
	return res.text, nil
}

// defaultLaunchName matches defaultSessionName's output:
// "[<project-slug>-]YYYYMMDD-HH.MM.SS".
var defaultLaunchName = regexp.MustCompile(`^([a-z0-9][a-z0-9-]*-)?\d{8}-\d{2}\.\d{2}\.\d{2}$`)

// launchNameEnv tells the session's hooks its launch name when it is the
// item-less default, so a generated title can follow it. Any other name (an
// explicit --name, an item name, none on resume) sets it empty, which also
// clears one inherited from an outer launch.
func launchNameEnv(name string) string {
	if !defaultLaunchName.MatchString(name) {
		name = ""
	}
	return hooks.LaunchNameEnv + "=" + name
}
