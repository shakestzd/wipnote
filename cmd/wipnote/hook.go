package main

import (
	"database/sql"
	"fmt"
	"os"

	"github.com/shakestzd/wipnote/core/hooks"
	"github.com/shakestzd/wipnote/core/provenance"
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
			func(event *hooks.CloudEvent, database *sql.DB, projectDir string) (*hooks.HookResult, error) {
				hooks.ApplyTraceparent()
				return hooks.SessionStart(event, database, projectDir)
			}),
		hookSubcmdWithProject("session-end", "Handle SessionEnd event", continueResult, hooks.SessionEnd),
		hookSubcmdWithProject("session-resume", "Handle SessionResume event", continueResult, hooks.SessionResume),

		// Standard two-arg handlers (event + db only).
		// roborev-473 finding 1 (VERIFIED per-handler): user-prompt, pretooluse and
		// posttooluse route EVERY derived-index WRITE through the daemon and use
		// their DB handle ONLY for READS, so they dispatch with a READ-ONLY handle
		// (hookSubcmdReadOnly) — avoiding the writable open + cold-DB migration that
		// blocked the hot hooks under contention. posttooluse joined them once
		// bug-07fd4a51 moved its event write to the daemon-routed completion upsert
		// and dropped its dead on-disk writes; on a daemon miss routeHookWriteVia
		// opens its own bounded writable handle.
		//
		// The following hot hooks KEEP a writable handle because grep confirmed they
		// still issue DIRECT writes on the passed handle (correctness-first — forcing
		// read-only would cause "readonly database" errors):
		//   - subagent-start: finding-3 applied-ack pending fallback
		//     (db.UpsertPendingSubagentStart) + synthetic-sessions via-fallback.
		//   - subagent-stop: db.UpdateEventFields direct write.
		//   - stop: insertAssistantTextSignal, backfillMissedUserPrompts,
		//     runSessionExitReconcile, db.UpdateSessionHandoff — all direct writes on
		//     the passed handle that are not (yet) daemon-routable.
		hookSubcmdReadOnly("user-prompt", "Handle UserPromptSubmit event", emptyResult, hooks.UserPrompt),
		hookSubcmd("after-agent", "Handle Antigravity AfterAgent event", continueResult, hooks.AfterAgent),
		hookSubcmd("after-model", "Handle Gemini AfterModel event", continueResult, hooks.AfterModel),
		hookSubcmdReadOnly("pretooluse", "Handle PreToolUse event", allowResult, hooks.PreToolUse),
		// posttooluse: on Claude it only responds (quality warning, auto-complete
		// notices, loud block) behind a Write|Edit|MultiEdit|NotebookEdit|Bash
		// matcher; recording moved to the async posttooluse-record hook. Codex
		// and Antigravity keep one combined entry (PostToolUseForHarness).
		hookSubcmdReadOnly("posttooluse", "Handle PostToolUse event", continueResult, hooks.PostToolUseForHarness),
		hookSubcmdReadOnly("posttooluse-record", "Record a PostToolUse event (Claude async hook)", continueResult, hooks.PostToolUseRecord),
		hookSubcmd("subagent-start", "Handle SubagentStart event", continueResult, hooks.SubagentStart),
		hookSubcmd("subagent-stop", "Handle SubagentStop event", continueResult, hooks.SubagentStop),
		hookSubcmd("stop", "Handle Stop event", continueResult, hooks.Stop),
		hookSubcmd("posttooluse-failure", "Handle PostToolUseFailure event", continueResult, hooks.PostToolUseFailure),
		hookSubcmd("pre-compact", "Handle PreCompact event", continueResult, hooks.PreCompact),
		hookSubcmd("post-compact", "Handle PostCompact event", continueResult, hooks.PostCompact),
		hookSubcmd("stop-failure", "Handle StopFailure event", continueResult, hooks.StopFailure),
		hookWorktreeCreateCmd(),
		hookSubcmd("worktree-remove", "Handle WorktreeRemove event", continueResult, hooks.WorktreeRemove),
		hookSubcmd("teammate-idle", "Handle TeammateIdle event", continueResult, hooks.TeammateIdle),
		hookSubcmd("task-completed", "Handle TaskCompleted event", continueResult, hooks.TaskCompleted),
		hookSubcmd("task-created", "Handle TaskCreated event", continueResult, hooks.TaskCreated),
		// task-started is NOT registered for any harness in
		// packages/plugin-core/manifest.json (task-aborted now is: Codex
		// Interrupt, see below). Both were originally unreachable: they were
		// bound to the Codex names TaskStarted and TurnAborted, neither of which
		// Codex dispatches (bug-e39d408f — TaskStarted appears zero times in the
		// 0.147.0 binary; TurnAborted is an internal ThreadEvent telemetry
		// variant, not a hook). Both bodies are generic TrackEvent checkpoints
		// with no side effect the registered handlers lack, so nothing was lost
		// when the registrations were dropped. Kept as manually-invocable
		// subcommands (a user's own hooks.json can call them, and a future
		// harness may supply a real turn-lifecycle event to repoint them at);
		// the build-time gate in port/pluginbuild/hook_event_names.go is what
		// prevents them being re-registered against a name nothing fires.
		hookSubcmd("task-started", "Record a turn-start checkpoint (not wired to any harness event)", continueResult,
			func(event *hooks.CloudEvent, database *sql.DB) (*hooks.HookResult, error) {
				return hooks.TrackEvent("TaskStarted", event, database)
			}),
		// task-aborted is now wired to Codex's Interrupt event (0.150.0+), the
		// real turn-cancel signal it was kept for; a cancelled turn skips Stop.
		hookSubcmd("task-aborted", "Record a turn-abort checkpoint (Codex Interrupt)", continueResult,
			func(event *hooks.CloudEvent, database *sql.DB) (*hooks.HookResult, error) {
				return hooks.TrackEvent("TurnAborted", event, database)
			}),
		hookSubcmd("instructions-loaded", "Handle InstructionsLoaded event", continueResult, hooks.InstructionsLoaded),
		hookSubcmd("permission-request", "Handle PermissionRequest event", continueResult, hooks.PermissionRequest),
		hookSubcmd("config-change", "Handle ConfigChange event — persist permission_mode to session metadata", continueResult, hooks.ConfigChange),
		hookSubcmdWithProject("exit-plan-mode", "Handle ExitPlanMode event — convert markdown plan to CRISPI YAML", continueResult, handleExitPlanMode),

		// track-event accepts an optional tool-name argument.
		hookTrackEventCmd(continueResult),
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
			// trace and no checkpoint (WorktreeCreate accepts a nil handle).
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
			dbPath, err := hooks.DBPath(projectDir)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return err
			}
			// Canonical-first fallback (feat-075c110d): OpenHookDB direct-opens
			// the writable handle. When it is unavailable (writer_unavailable —
			// e.g. the daemon holds the single writable handle, or a transient
			// lock), we do NOT abort: worktree creation does not require the DB,
			// only the best-effort checkpoint does. We proceed with a nil handle
			// so the worktree is still created and its bare path is still echoed
			// (#119 contract). The derived-index checkpoint is recovered by
			// reindex on the next serve cycle. A genuine failure (missing
			// worktree_name / worktree_base_path, or a worktree creation error)
			// still returns a non-zero error with no JSON on stdout.
			var database *sql.DB
			if !headless {
				var reason hooks.FallbackReason
				database, reason = hooks.OpenHookDB(use, event.SessionID, dbPath)
				if database != nil {
					defer database.Close()
				} else {
					_ = reason // already logged + counted inside OpenHookDB
				}
			}

			path, err := hooks.WorktreeCreate(event, database)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return err
			}
			fmt.Fprintln(os.Stdout, path)
			return nil
		},
	}
}

// hookSubcmd creates a hook subcommand that resolves the project dir and opens
// the DB before calling handler. fallback is returned when the project is not
// an wipnote project or when the DB cannot be opened.
func hookSubcmd(
	use, short string,
	fallback *hooks.HookResult,
	handler func(*hooks.CloudEvent, *sql.DB) (*hooks.HookResult, error),
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
				dbPath, err := hooks.DBPath(projectDir)
				if err != nil {
					hooks.LogError(use, event.SessionID,
						fmt.Sprintf("DBPath failed: %v", err))
					return fallback, nil
				}
				// Canonical-first contract (plan-ae0c37b2 slice 7): the
				// writable open is best-effort. A failure is logged and
				// counted as writer_unavailable; we then return the
				// fallback HookResult so Claude Code sees SUCCESS. The
				// canonical NDJSON written elsewhere in the handler tree
				// (collector, session-start Rosetta) is the authoritative
				// copy; the next projection rebuild derives the index
				// from it.
				database, reason := hooks.OpenHookDB(use, event.SessionID, dbPath)
				if database == nil {
					_ = reason
					return fallback, nil
				}
				defer database.Close()
				return handler(event, database)
			})
		},
	}
}

// hookSubcmdReadOnly is hookSubcmd for the hot hooks that route EVERY
// derived-index write through the daemon and never read anything a hook could
// have written (user-prompt, pretooluse, posttooluse, posttooluse-record). They
// get NO database handle (feat-af8d1689).
//
// They used to get a fresh in-memory tables-only projection, which no hook
// ever hydrates: every read through it returned zero rows, and building its
// ~100 tables was about half of each call's in-process time. These handlers
// treat a nil handle exactly as that empty projection (pinned by
// TestReadOnlyHooks_NilMatchesEmptyProjection in core/hooks), so dropping it
// changes no output. Writes still go daemon-first; on a daemon miss
// routeHookWriteVia opens its own bounded writable handle as before.
func hookSubcmdReadOnly(
	use, short string,
	fallback *hooks.HookResult,
	handler func(*hooks.CloudEvent, *sql.DB) (*hooks.HookResult, error),
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
				return handler(event, nil)
			})
		},
	}
}

// hookSubcmdWithProject is like hookSubcmd but also passes projectDir to the
// handler (needed by session-start, session-end, session-resume).
func hookSubcmdWithProject(
	use, short string,
	fallback *hooks.HookResult,
	handler func(*hooks.CloudEvent, *sql.DB, string) (*hooks.HookResult, error),
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
					// before the projection opens, and keep every commit
					// intent free of git calls (flush does the git work).
					commitIntentLight.Store(true)
					hooks.RecordSessionEndNote(event, projectDir)
				}
				dbPath, err := hooks.DBPath(projectDir)
				if err != nil {
					hooks.LogError(use, event.SessionID,
						fmt.Sprintf("DBPath failed: %v", err))
					return fallback, nil
				}
				// Canonical-first contract — see hookSubcmd above.
				//
				// Handle selection is PER-HANDLER (roborev-476 finding 3):
				//   - session-start runs on the launcher's post-selection critical
				//     path. It routes every session/lineage/event/family write through
				//     the daemon (RouteHookWrite / routeSQLApplied) and uses this handle
				//     for READS plus ONE residual direct write — the bounded orphan sweep
				//     (SweepOrphanedEventsForProjectCapped → db.MarkEventAborted), whose
				//     atomic started→aborted transition needs RowsAffected and so cannot
				//     be routed enqueue-only. Because that write still touches this
				//     handle, session-start takes a BOUNDED handle
				//     (SessionStartBusyTimeout ~750ms): under a held external write lock
				//     the orphan-sweep UPDATE fail-fasts in well under a second (the
				//     residual orphan drains out-of-band via serve) instead of stalling
				//     the interactive launcher ~5s.
				//   - session-end / session-resume / exit-plan-mode keep the default 5s
				//     OpenHookDB: they are not on the post-selection critical path and
				//     their residual writes (FinalizeSessionHTML, runSessionExitReconcile)
				//     are tolerant of the default busy_timeout.
				database, reason := openSessionHookDB(use, event.SessionID, dbPath)
				if database == nil {
					_ = reason
					return fallback, nil
				}
				defer database.Close()
				return handler(event, database, projectDir)
			})
		},
	}
}
