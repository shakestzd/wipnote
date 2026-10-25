package hooks

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// PostToolUse handles the PostToolUse hook event with BOTH jobs: it records
// the call (postToolUseRecord) and then computes what goes back to the model
// (postToolUseRespond). This is the single handler Codex and Antigravity run,
// and the one tests drive directly.
//
// On Claude the two jobs are split (bug-07fd4a51 step 6): the async
// `posttooluse-record` hook records every call (PostToolUseRecord) and the
// sync, matcher-narrowed `posttooluse` hook only responds
// (PostToolUseForHarness routes it to PostToolUseRespond).
func PostToolUse(event *CloudEvent, database *sql.DB) (*HookResult, error) {
	ctx := resolvePostToolUseContext(event, database)
	if ctx == nil {
		return &HookResult{Continue: true}, nil
	}
	postToolUseRecord(event, ctx, database)
	return postToolUseRespond(event, ctx, database), nil
}

// PostToolUseRecord is the record-only half: event row and session HTML. It
// never blocks and never returns context, which is what makes it safe to run
// as a Claude `async` hook (port/pluginbuild asyncSafeHandlers).
func PostToolUseRecord(event *CloudEvent, database *sql.DB) (*HookResult, error) {
	if ctx := resolvePostToolUseContext(event, database); ctx != nil {
		postToolUseRecord(event, ctx, database)
	}
	return &HookResult{Continue: true}, nil
}

// PostToolUseRespond is the respond-only half: the CheckFileQuality warning
// and commit/merge auto-complete notices. It records nothing.
func PostToolUseRespond(event *CloudEvent, database *sql.DB) (*HookResult, error) {
	ctx := resolvePostToolUseContext(event, database)
	if ctx == nil {
		return &HookResult{Continue: true}, nil
	}
	return postToolUseRespond(event, ctx, database), nil
}

// PostToolUseForHarness is what the `posttooluse` subcommand dispatches. On
// Claude, recording belongs to the separate async `posttooluse-record` hook,
// so this only responds; running both here would append every call to the
// session HTML twice. Every other harness gets the combined handler.
func PostToolUseForHarness(event *CloudEvent, database *sql.DB) (*HookResult, error) {
	if event != nil && event.Harness == HarnessClaude {
		return PostToolUseRespond(event, database)
	}
	return PostToolUse(event, database)
}

// resolvePostToolUseContext trusts WIPNOTE_PARENT_PROMPT_EVENT set by the
// paired PreToolUse (trustParentEnvVar=true) to avoid the race where a new
// UserQuery arrives while the tool was executing.
func resolvePostToolUseContext(event *CloudEvent, database *sql.DB) *toolUseContext {
	return resolveToolUseContext(event, database, true)
}

// postToolUseRecord writes the complete tool_call row and the session HTML
// entry.
func postToolUseRecord(event *CloudEvent, ctx *toolUseContext, database *sql.DB) {
	success := isSuccess(event.ToolResult)
	status := "completed"
	if !success {
		status = "failed"
	}
	outputSummary := summariseToolOutput(event.ToolName, event.ToolInput, event.ToolResult, success)

	// bug-07fd4a51: write the COMPLETE event row ourselves, keyed on the
	// tool_use_id-derived id PreToolUse also uses, through the daemon-routed
	// path. The old FindStartedEvent lookup ran on this hook's own handle, which
	// never holds the started row PreToolUse routed to the daemon, and it paired
	// by "newest started row with this tool name", which mis-pairs parallel
	// calls to the same tool.
	eventID := recordToolCompletion(event, ctx, database, status, outputSummary)

	// Append event to session HTML activity log (non-critical, errors silently logged).
	AppendEventToSessionHTML(ctx.ProjectDir, ctx.SessionID, SessionEvent{
		Timestamp: time.Now().UTC(),
		ToolName:  event.ToolName,
		Success:   success,
		EventID:   eventID,
		FeatureID: ctx.FeatureID,
		Summary:   SummariseInput(event.ToolName, event.ToolInput),
	})

	// No per-call orphan sweep here any more. posttooluse dispatches with no
	// database handle (hookSubcmdReadOnly), where a per-session
	// sweep could see no rows and could not mark one aborted anyway. Started rows a daemon-miss pretooluse left on the disk
	// fallback are still swept by session-start's capped project-wide sweep;
	// daemon-routed rows are completed in place by recordToolCompletion.
	//
	// bug-07fd4a51 step 4: the git_commits insert, the claim agent-id tag and
	// the feature_files / session_files attribution upserts that used to live
	// here were removed. They wrote to this hook's on-disk handle, and every
	// reader of those tables (graph/lineage/provenance, who, api, session show,
	// the session-end handoff) reads an in-memory projection that never sees
	// that file: git_commits is rebuilt from commit trailers by
	// reindex_trailers, claims and file touches from canonical state.
}

// postToolUseRespond builds the HookResult that goes back to the model.
func postToolUseRespond(event *CloudEvent, ctx *toolUseContext, database *sql.DB) *HookResult {
	result := &HookResult{Continue: true}

	// Quality gate: warn when Write/Edit/MultiEdit produces an oversized file.
	switch event.ToolName {
	case "Write", "Edit", "MultiEdit":
		if filePath := extractFilePath(event.ToolInput); filePath != "" {
			if warnings := CheckFileQuality(filePath); warnings != "" {
				result.AdditionalContext = warnings
			}
		}
	}

	// Auto-complete work items referenced in commit messages.
	if event.ToolName == "Bash" && isSuccess(event.ToolResult) {
		if cmd := extractBashCommand(event.ToolInput); looksLikeGitCommit(cmd) {
			if _, msg := parseGitCommitOutput(summarizeToolOutput(event.ToolResult)); msg != "" {
				if completed := autoCompleteFromCommit(msg, ctx, database); len(completed) > 0 {
					notice := fmt.Sprintf("Auto-completed: %s", strings.Join(completed, ", "))
					if result.AdditionalContext != "" {
						result.AdditionalContext += "\n" + notice
					} else {
						result.AdditionalContext = notice
					}
				}
			}
		}

		// Auto-complete work items when a branch merge completes.
		// "git merge trk-xxxxx" or "git merge feat-xxxxx" should close
		// all in-progress items on that track/feature.
		if cmd := extractBashCommand(event.ToolInput); looksLikeGitMerge(cmd) {
			if branch := extractMergeBranch(cmd); branch != "" {
				if completed := autoCompleteByBranch(branch, database); len(completed) > 0 {
					notice := fmt.Sprintf("Auto-completed (merge): %s", strings.Join(completed, ", "))
					if result.AdditionalContext != "" {
						result.AdditionalContext += "\n" + notice
					} else {
						result.AdditionalContext = notice
					}
				}
			}
		}
	}

	return result
}

// extractBashCommand extracts the "command" field from a Bash tool_input map.
func extractBashCommand(input map[string]any) string {
	if input == nil {
		return ""
	}
	if v, ok := input["command"].(string); ok {
		return v
	}
	return ""
}
