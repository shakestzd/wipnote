package hooks

import (
	"fmt"
	"strings"

	"github.com/shakestzd/wipnote/core/ingest"
)

// UserPrompt handles the UserPromptSubmit Claude Code hook event.
// It returns the active work item (the session's open claim) as a
// terse one-liner.
func UserPrompt(event *CloudEvent) (*HookResult, error) {
	sessionID := resolveSessionIDWithHarness(event)
	if sessionID == "" || event.Prompt == "" {
		return &HookResult{Continue: true}, nil
	}

	// Skip subagent-dispatched prompts. Subagent prompts are already captured via
	// the SubagentStart handler's task_delegation event, which links to the
	// orchestrator's UserQuery as parent. Recording a separate UserQuery row for
	// the subagent would create a duplicate top-level turn. The task_delegation
	// and subsequent subagent tool calls provide full lineage and prompt context.
	if isSubagentEvent(event) {
		return &HookResult{Continue: true}, nil
	}

	projectDir := ResolveProjectDir(event.CWD, event.SessionID)
	recordDelegateHook(event, projectDir, sessionID, "prompt")
	// Background tasks that ended while the agent waited (feat-36658c22).
	syncTranscriptRunsFor(event, projectDir, sessionID, runPromptPass)
	// The open claim in the canonical claim ledger is the active work item.
	featureID := canonicalActiveWorkItem(projectDir, sessionID)

	promptSummary := sanitizePrompt(event.Prompt)
	if promptSummary == "" || ingest.IsSystemMessage(promptSummary) {
		return &HookResult{Continue: true}, nil
	}

	// Per-turn context is only the terse active-item line. The keyword
	// classifier that added delegation directives on every prompt was retired:
	// the launcher system prompt already states the delegation rules once.
	result := &HookResult{}
	if hint := buildActiveItemOneLiner(projectDir, featureID); hint != "" {
		result.AdditionalContext = hint
	} else {
		result.Continue = true
	}
	if title := pendingSessionTitle(projectDir, sessionID, featureID); title != "" {
		result.HookSpecificOutput = &HookSpecificOutput{SessionTitle: title}
	}
	return result, nil
}

// buildActiveItemOneLiner returns a terse "ACTIVE: <id> — <title>" string when
// an active item is set, or empty string when none. Used per-turn in
// UserPromptSubmit. The title is read from the work item's canonical HTML.
func buildActiveItemOneLiner(projectDir, featureID string) string {
	if featureID == "" {
		return ""
	}
	if title := canonicalWorkItemTitle(projectDir, featureID); title != "" {
		return fmt.Sprintf("ACTIVE: %s — %s", featureID, title)
	}
	return fmt.Sprintf("ACTIVE: %s", featureID)
}

// sanitizePrompt strips XML notification/reminder blocks from prompt text.
func sanitizePrompt(s string) string {
	for _, tag := range []string{"task-notification", "system-reminder", "command-message", "local-command-caveat"} {
		open := "<" + tag + ">"
		close := "</" + tag + ">"
		for {
			i := strings.Index(s, open)
			if i == -1 {
				break
			}
			j := strings.Index(s[i:], close)
			if j == -1 {
				s = s[:i]
				break
			}
			s = s[:i] + s[i+j+len(close):]
		}
	}
	// Strip lines that are just notification artifacts
	var cleaned []string
	for line := range strings.SplitSeq(s, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "Full transcript available at:") {
			continue
		}
		if strings.HasPrefix(trimmed, "Read the output file to retrieve") {
			continue
		}
		cleaned = append(cleaned, trimmed)
	}
	return strings.TrimSpace(strings.Join(cleaned, "\n"))
}

func joinLines(lines []string) string {
	result := ""
	for i, l := range lines {
		result += l
		if i < len(lines)-1 {
			result += "\n"
		}
	}
	return result
}
