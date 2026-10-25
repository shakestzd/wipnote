package main

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/shakestzd/wipnote/core/hooks"
	"github.com/spf13/cobra"
)

// openSessionHookDB selects the writable hook handle for the three session
// handlers dispatched by hookSubcmdWithProject (roborev-476 finding 3).
//
// session-start runs on the launcher's post-selection critical path and its
// remaining direct writes (the end-note catch-up) must fail-fast under a held
// external write lock — so it takes a SHORT bounded busy_timeout
// (SessionStartBusyTimeout ~750ms) rather than stalling the interactive launcher
// on the default 5s. Every other session-phase handler (session-end,
// session-resume, exit-plan-mode) keeps the default 5s OpenHookDB; they are not
// on the post-selection critical path. A nil handle is treated as
// canonical-success by the caller exactly as for OpenHookDB.
func openSessionHookDB(use, sessionID, dbPath string) (*sql.DB, hooks.FallbackReason) {
	if use == "session-start" {
		return hooks.OpenHookDBWithBusyTimeout(use, sessionID, dbPath, hooks.SessionStartBusyTimeout)
	}
	return hooks.OpenHookDB(use, sessionID, dbPath)
}

// hookTrackEventCmd returns the track-event subcommand, which accepts an
// optional tool-name CLI argument.
func hookTrackEventCmd(fallback *hooks.HookResult) *cobra.Command {
	return &cobra.Command{
		Use:   "track-event [tool-name]",
		Short: "Record a generic hook event",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			toolName := "GenericEvent"
			if len(args) == 1 {
				toolName = args[0]
			}
			return runHookNamed("track-event", func(event *hooks.CloudEvent) (*hooks.HookResult, error) {
				projectDir := hooks.ResolveProjectDir(event.CWD, event.SessionID)
				if !hooks.IswipnoteProject(projectDir) {
					return fallback, nil
				}
				dbPath, err := hooks.DBPath(projectDir)
				if err != nil {
					hooks.LogError("track-event", event.SessionID,
						fmt.Sprintf("DBPath failed: %v", err))
					return fallback, nil
				}
				// Canonical-first contract — see hookSubcmd above.
				database, reason := hooks.OpenHookDB("track-event", event.SessionID, dbPath)
				if database == nil {
					_ = reason
					return fallback, nil
				}
				defer database.Close()
				return hooks.TrackEvent(toolName, event, database)
			})
		},
	}
}

// runHookNamed is like runHook but also records a trace entry for diagnostics.
// It performs harness detection from the raw stdin payload so that Codex and
// Gemini payloads are parsed with their own dialect adapters and responses are
// emitted in the harness-appropriate wire format. Claude is the default path and
// its behaviour is unchanged.
// headlessHookEnv marks a CLI process wipnote started headlessly. Every
// wipnote hook in that process allows and returns at once.
const headlessHookEnv = "WIPNOTE_HEADLESS"

func runHookNamed(subcommand string, handler func(*hooks.CloudEvent) (*hooks.HookResult, error)) error {
	start := time.Now()

	// Read raw stdin bytes first so we can detect the harness before parsing.
	rawPayload, err := hooks.ReadRawStdin()
	if err != nil {
		hooks.LogError("runHook", "", fmt.Sprintf("read stdin: %v", err))
		// Fall back to the --harness override, else Claude, when stdin is unreadable.
		h, _ := hooks.ParseHarnessName(hookHarnessOverride)
		return hooks.WriteResultForHarness(h, hooks.AllowForHarness(h))
	}

	// An explicit --harness from the generated hooks.json wins; otherwise
	// detect the harness from the environment and raw payload shape.
	harness := hooks.DetectHarnessWithOverride(rawPayload, hookHarnessOverride)

	// Parse the event using the harness-specific input adapter.
	event, err := hooks.ParseEventForHarness(harness, rawPayload)
	if err != nil {
		hooks.LogError("runHook", "", fmt.Sprintf("parse event (%s): %v", harness, err))
		return hooks.WriteResultForHarness(harness, hooks.AllowForHarness(harness))
	}

	// A headless run that wipnote itself started (compliance grading, plan
	// chat, the headless-runs mod) must not record or gate anything. The
	// allow carries the event name: Antigravity reads a bare {} on PreToolUse
	// as a deny (emitAntigravityResponseForEvent).
	// PermissionRequest keeps its auto-approval: the headless child has no one
	// to answer a prompt, so dropping the decision would deny or stall it.
	if os.Getenv(headlessHookEnv) == "1" {
		result := hooks.AllowForHarness(harness)
		if subcommand == "permission-request" {
			result = hooks.PermissionDecisionOnly(event)
		}
		return hooks.WriteResultForHarnessEvent(harness, hookEventNameForResponse(subcommand, event), result)
	}

	hooks.TraceInvocation(subcommand, rawPayload, event)

	result, err := handler(event)
	if err != nil {
		var blockErr *hooks.BlockExit2Error
		if errors.As(err, &blockErr) {
			fmt.Fprintln(os.Stderr, blockErr.Message)
			os.Exit(2)
		}
		hooks.LogError("runHook", event.SessionID, fmt.Sprintf("handler error: %v", err))
		return hooks.WriteResultForHarnessEvent(harness, hookEventNameForResponse(subcommand, event), hooks.AllowForHarness(harness))
	}
	if result == nil {
		hooks.LogError("runHook", event.SessionID, "handler returned nil result")
		return hooks.WriteResultForHarnessEvent(harness, hookEventNameForResponse(subcommand, event), hooks.AllowForHarness(harness))
	}

	// Daemon-guarantee breach (feat-f6759e37). This session was started by a
	// wipnote launcher, which GUARANTEES the work-item daemon, and a read
	// against it failed after its bounded retries. The policy here is
	// deliberate and is the point of the whole feature: report loudly and pause
	// execution rather than quietly answering from the derived index. A silent
	// fallback is how two data paths diverge without anyone noticing.
	//
	// The check sits here, at the single point every hook emits its response,
	// rather than at each read site — a per-site check is one a future caller
	// can forget, and forgetting it would restore exactly the quiet failure
	// this replaces. Sessions with no launcher guarantee never latch a breach,
	// so this is inert for them.
	if blocked := hooks.DaemonGuaranteeBlockResult(); blocked != nil {
		hooks.LogError("runHook", event.SessionID, "daemon guarantee breached: pausing")
		return hooks.WriteResultForHarnessEvent(harness, hookEventNameForResponse(subcommand, event), blocked)
	}

	projectDir := hooks.ResolveProjectDir(event.CWD, event.SessionID)
	hookName := subcommand
	hooks.LogTimed(projectDir, "runHook", map[string]string{
		"hook":    hookName,
		"session": event.SessionID[:hooks.MinSessionLen(event.SessionID)],
	}, start, "completed")

	// Claude UserPromptSubmit also carries the current-time line that used to
	// come from a separate `date` shell hook (one fewer process per prompt).
	if subcommand == "user-prompt" && harness == hooks.HarnessClaude {
		return hooks.EmitClaudeUserPrompt(os.Stdout, result, time.Now())
	}

	// Emit the result in the harness-appropriate wire format.
	return hooks.WriteResultForHarnessEvent(harness, hookEventNameForResponse(subcommand, event), result)
}

// hookEventNameForResponse returns the hook event name that should be echoed in
// the response's hookSpecificOutput. It first checks the incoming CloudEvent's
// HookEventName field (populated by parseGeminiEvent, parseAntigravityEvent, and
// parseCodexEvent from the payload's hook_event_name). When that is present, it
// is echoed directly — ensuring Gemini/Antigravity receive their own native event
// names (BeforeAgent, BeforeTool, AfterTool) rather than Claude canonical names.
// When HookEventName is absent (Claude payloads lack hook_event_name), the
// subcommand-to-event-name mapping provides the Claude/Codex canonical name.
func hookEventNameForResponse(subcommand string, event *hooks.CloudEvent) string {
	// Prefer the harness-native event name echoed from the incoming payload.
	if event != nil && event.HookEventName != "" {
		return event.HookEventName
	}
	// Fall back to Claude/Codex canonical event names by subcommand.
	switch subcommand {
	case "session-start":
		return "SessionStart"
	case "session-end":
		return "SessionEnd"
	case "user-prompt":
		return "UserPromptSubmit"
	case "pretooluse":
		return "PreToolUse"
	case "posttooluse", "posttooluse-record":
		return "PostToolUse"
	case "after-agent":
		return "AfterAgent"
	case "after-model":
		return "AfterModel"
	case "task-started":
		return "TaskStarted"
	case "task-aborted":
		return "TurnAborted"
	default:
		return ""
	}
}
