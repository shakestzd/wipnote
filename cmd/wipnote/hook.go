package main

import (
	"fmt"
	"os"

	"github.com/shakestzd/wipnote/core/hooks"
	"github.com/shakestzd/wipnote/core/provenance"
	"github.com/shakestzd/wipnote/internal/delegate/detect"
	"github.com/spf13/cobra"
)

// hookHarnessOverride is bound to the persistent --harness flag. Generated
// non-Claude hooks.json files pass it (e.g. `wipnote hook session-start
// --harness codex`) so the response wire format never depends on environment
// variables inherited from a parent harness (GitHub issue #184).
var hookHarnessOverride string

// hookCmd returns the "wipnote hook" parent command with all subcommands.
func hookCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "hook",
		Short:         "Claude Code hook handlers (replaces Python hook scripts)",
		SilenceErrors: true,
		SilenceUsage:  true,
		Long: `Hook subcommands read a CloudEvent JSON payload from stdin and write a
JSON result to stdout. They replace the Python hook scripts, eliminating the
~500ms uv cold-start cost per hook invocation.

Usage in hooks.json:
  "command": "wipnote hook session-start"
  "command": "wipnote hook pretooluse"
  etc.`,
		// Propagate the compiled version to the hooks package so session-start
		// can detect CLI/plugin version mismatches and so provenance attribution
		// records the binary that wrote each session/work item.
		PersistentPreRun: func(_ *cobra.Command, _ []string) {
			hooks.CLIVersion = version
			provenance.SetCLIVersion(version)
			// The bare-delegation redirect needs delegate run's argv
			// classifier, which core cannot import (plan-28b4a7f6 slice 9).
			hooks.DelegateRedirect = detect.RedirectAt
		},
	}
	cmd.PersistentFlags().StringVar(&hookHarnessOverride, "harness", "",
		"harness that invoked the hook (claude, codex, antigravity); overrides payload/env detection")

	// Shared fallback results used across commands.
	continueResult := &hooks.HookResult{Continue: true}
	allowResult := &hooks.HookResult{} // Empty object = allow (avoids Claude Code "hook error" label)
	emptyResult := &hooks.HookResult{}

	cmd.AddCommand(
		// Session lifecycle — need projectDir passed to the handler.
		hookSubcmdWithProject("session-start", "Handle SessionStart event", emptyResult,
			func(event *hooks.CloudEvent, projectDir string) (*hooks.HookResult, error) {
				hooks.ApplyTraceparent()
				return hooks.SessionStart(event, projectDir)
			}),
		hookSubcmdWithProject("session-end", "Handle SessionEnd event", continueResult, hooks.SessionEnd),
		hookSubcmdWithProject("session-resume", "Handle SessionResume event", continueResult, hooks.SessionResume),

		// No hook gets a database handle (feat-174dc958): every handler works
		// from canonical files only.
		hookSubcmd("user-prompt", "Handle UserPromptSubmit event", emptyResult, hooks.UserPrompt),
		hookSubcmd("pretooluse", "Handle PreToolUse event", allowResult, hooks.PreToolUse),
		// posttooluse: on Claude it only responds (quality warning, auto-complete
		// notices, loud block) behind a Write|Edit|MultiEdit|NotebookEdit|Bash
		// matcher; recording moved to the async posttooluse-record hook. Codex
		// and Antigravity keep one combined entry (PostToolUseForHarness).
		hookSubcmd("posttooluse", "Handle PostToolUse event", continueResult, hooks.PostToolUseForHarness),
		hookSubcmd("posttooluse-record", "Record a PostToolUse event (Claude async hook)", continueResult, hooks.PostToolUseRecord),
		hookSubcmd("subagent-start", "Handle SubagentStart event", continueResult, hooks.SubagentStart),
		hookSubcmd("subagent-stop", "Handle SubagentStop event", continueResult, hooks.SubagentStop),
		hookSubcmd("stop", "Handle Stop event", continueResult, hooks.Stop),
		hookSubcmd("posttooluse-failure", "Handle PostToolUseFailure event", continueResult, hooks.PostToolUseFailure),
		hookSubcmd("stop-failure", "Handle StopFailure event", continueResult, hooks.StopFailure),
		hookWorktreeCreateCmd(),
		hookSubcmd("worktree-remove", "Handle WorktreeRemove event", continueResult, hooks.WorktreeRemove),
		hookSubcmd("task-completed", "Handle TaskCompleted event", continueResult, hooks.TaskCompleted),
		hookSubcmd("task-created", "Handle TaskCreated event", continueResult, hooks.TaskCreated),
		hookSubcmd("permission-request", "Handle PermissionRequest event", continueResult, hooks.PermissionRequest),
		hookSubcmd("permission-denied", "Handle PermissionDenied event — record the denied call as failed", continueResult, hooks.PermissionDenied),
		hookSubcmd("config-change", "Handle ConfigChange event — persist permission_mode to session metadata", continueResult, hooks.ConfigChange),
		hookSubcmdWithProject("exit-plan-mode", "Handle ExitPlanMode event — convert markdown plan to CRISPI YAML", continueResult, handleExitPlanMode),
	)
	return cmd
}

// hookWorktreeCreateCmd handles Claude Code's WorktreeCreate replacement hook.
// It must print exactly the created worktree path on stdout, bypassing the JSON
// HookResult response layer used by ordinary hooks.
func hookWorktreeCreateCmd() *cobra.Command {
	const use = "worktree-create"
	return &cobra.Command{
		Use:   use,
		Short: "Handle WorktreeCreate event",
		RunE: func(_ *cobra.Command, _ []string) error {
			rawPayload, err := hooks.ReadRawStdin()
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return err
			}
			event, err := hooks.ParseClaudeWorktreeCreateEvent(rawPayload)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return err
			}
			// A headless run still needs its worktree, but records nothing: no
			// trace and no checkpoint.
			headless := os.Getenv(headlessHookEnv) == "1"
			if !headless {
				hooks.TraceInvocation(use, rawPayload, event)
			}

			projectDir := hooks.ResolveProjectDir(event.CWD, event.SessionID)
			if !hooks.IswipnoteProject(projectDir) {
				err := fmt.Errorf("not a wipnote project: %s", projectDir)
				fmt.Fprintln(os.Stderr, err)
				return err
			}
			path, err := hooks.WorktreeCreate(event)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return err
			}
			fmt.Fprintln(os.Stdout, path)
			return nil
		},
	}
}

// hookSubcmd creates a hook subcommand that resolves the project dir and calls
// handler. fallback is returned when the project is not a wipnote project.
func hookSubcmd(
	use, short string,
	fallback *hooks.HookResult,
	handler func(*hooks.CloudEvent) (*hooks.HookResult, error),
) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runHookNamed(use, func(event *hooks.CloudEvent) (*hooks.HookResult, error) {
				projectDir := hooks.ResolveProjectDir(event.CWD, event.SessionID)
				if !hooks.IswipnoteProject(projectDir) {
					return fallback, nil
				}
				return handler(event)
			})
		},
	}
}

// hookSubcmdWithProject is like hookSubcmd but also passes projectDir to the
// handler (needed by session-start, session-end, session-resume).
func hookSubcmdWithProject(
	use, short string,
	fallback *hooks.HookResult,
	handler func(*hooks.CloudEvent, string) (*hooks.HookResult, error),
) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(_ *cobra.Command, _ []string) error {
			// session-start gets a fresh trace file before anything else; a
			// headless child must not wipe the interactive session's trace.
			if use == "session-start" && os.Getenv(headlessHookEnv) != "1" {
				hooks.TruncateTraceFile()
			}
			return runHookNamed(use, func(event *hooks.CloudEvent) (*hooks.HookResult, error) {
				projectDir := hooks.ResolveProjectDir(event.CWD, event.SessionID)
				if !hooks.IswipnoteProject(projectDir) {
					return fallback, nil
				}
				if use == "session-end" {
					// Claude Code gives this hook 1.5s: make the end durable
					// first, and keep every commit intent free of git calls
					// (flush does the git work).
					commitIntentLight.Store(true)
					hooks.RecordSessionEndNote(event, projectDir)
				}
				return handler(event, projectDir)
			})
		},
	}
}
