package hooks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Orchestrator mode enforcement (feat-567c0211, GH-#19 / GH-#20).
//
// `wipnote orchestrator enable --strict` has always written
// .wipnote/orchestrator.json, but nothing read it back: the PreToolUse hook
// never inspected the Skill tool and only debug-logged direct orchestrator
// writes, so strict mode was a no-op. Skills and Bash were the two documented
// escape hatches the issues were filed about.
//
// The config type and its IO live here, next to the hook that enforces them;
// the CLI in cmd/wipnote/orchestrator.go is now a thin wrapper over these.

// OrchestratorConfig mirrors the JSON stored in .wipnote/orchestrator.json.
type OrchestratorConfig struct {
	Enabled       bool   `json:"enabled"`
	Mode          string `json:"mode,omitempty"`
	Violations    int    `json:"violations,omitempty"`
	MaxViolations int    `json:"max_violations,omitempty"`
}

// OrchestratorModeStrict blocks once MaxViolations is reached;
// OrchestratorModeGuidance only ever advises.
const (
	OrchestratorModeStrict   = "strict"
	OrchestratorModeGuidance = "guidance"
	// OrchestratorDefaultMaxViolations applies when the config names no cap,
	// so a hand-written `{"enabled":true,"mode":"strict"}` still escalates.
	OrchestratorDefaultMaxViolations = 3
	// OrchestratorRescueEnv is the escape hatch required by bug-c8ac6a11: when
	// subagents fail, the orchestrator must be able to take over and write
	// directly. Setting it to 1 disables this guard for the process.
	OrchestratorRescueEnv = "WIPNOTE_ORCHESTRATOR_RESCUE"
)

// OrchestratorConfigPath is the config file inside a .wipnote directory.
func OrchestratorConfigPath(wipnoteDir string) string {
	return filepath.Join(wipnoteDir, "orchestrator.json")
}

// LoadOrchestratorConfig reads the config. ok is false when there is no
// readable, parsable config — orchestrator mode is then simply off.
func LoadOrchestratorConfig(wipnoteDir string) (cfg OrchestratorConfig, ok bool) {
	if wipnoteDir == "" {
		return OrchestratorConfig{}, false
	}
	data, err := os.ReadFile(OrchestratorConfigPath(wipnoteDir))
	if err != nil {
		return OrchestratorConfig{}, false
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return OrchestratorConfig{}, false
	}
	return cfg, true
}

// SaveOrchestratorConfig writes the config back atomically enough for a
// single-writer counter: the file is small and rewritten whole.
func SaveOrchestratorConfig(wipnoteDir string, cfg OrchestratorConfig) error {
	if wipnoteDir == "" {
		return fmt.Errorf("orchestrator config: empty .wipnote dir")
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(OrchestratorConfigPath(wipnoteDir), append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

// orchestratorWhitelist is the set of tools an orchestrator may call directly:
// delegation itself, user interaction, task bookkeeping, and resuming a
// budget-paused subagent (SendMessage — the orchestrator-directives skill's
// documented Pattern A recovery, which must never itself count as a direct-
// execution violation). Skill is deliberately absent — invoking a skill runs
// its work in the orchestrator's own context, which is exactly the bypass
// GH-#19 reported.
var orchestratorWhitelist = map[string]bool{
	"Task": true, "Agent": true,
	"AskUserQuestion": true,
	"TodoWrite":       true,
	"TaskCreate":      true, "TaskUpdate": true, "TaskList": true, "TaskGet": true,
	"SendMessage": true,
}

// isOrchestratorWhitelistedTool reports whether an orchestrator may run this
// tool call directly. Shell tools qualify only when EVERY segment of the
// command is a `wipnote` invocation (isWipnoteCLICommand) — a raw string
// prefix check would let a compound command like `wipnote status && rm -rf x`
// slip through on its first word alone.
func isOrchestratorWhitelistedTool(toolName string, toolInput map[string]any) bool {
	if orchestratorWhitelist[toolName] {
		return true
	}
	if isShellTool(toolName) {
		return isWipnoteCLICommand(shellCommand(toolInput))
	}
	return false
}

// checkOrchestratorStrictGuard enforces orchestrator mode for the root session.
//
// It returns (advisory, block). A non-empty block must stop the tool call; a
// non-empty advisory is surfaced through additionalContext and the call
// proceeds. Both are empty when the guard does not apply: a subagent (which is
// the delegate, and must be free to work), orchestrator mode off, a
// whitelisted tool, or the bug-c8ac6a11 rescue escape hatch.
//
// guidance mode advises and never counts; strict mode counts each violation
// into orchestrator.json and blocks once max_violations is reached, so the
// escalation survives across tool calls and sessions until the next
// `wipnote orchestrator enable` resets it.
func checkOrchestratorStrictGuard(event *CloudEvent, ctx *toolUseContext) (advisory, block string) {
	if event == nil || ctx == nil || ctx.IsSubagent {
		return "", ""
	}
	if os.Getenv(OrchestratorRescueEnv) == "1" {
		return "", ""
	}
	cfg, ok := LoadOrchestratorConfig(ctx.HgDir)
	if !ok || !cfg.Enabled || event.ToolName == "" {
		return "", ""
	}
	if isOrchestratorWhitelistedTool(event.ToolName, event.ToolInput) {
		return "", ""
	}
	if cfg.Mode != OrchestratorModeStrict {
		return orchestratorAdvisory(event.ToolName, 0, 0), ""
	}

	maxViolations := cfg.MaxViolations
	if maxViolations <= 0 {
		maxViolations = OrchestratorDefaultMaxViolations
	}
	cfg.Violations++
	cfg.MaxViolations = maxViolations
	if err := SaveOrchestratorConfig(ctx.HgDir, cfg); err != nil {
		debugLog(ctx.ProjectDir, "[wipnote] orchestrator: could not persist violation: %v", err)
	}
	if cfg.Violations >= maxViolations {
		return "", orchestratorBlockReason(event.ToolName, cfg.Violations, maxViolations)
	}
	return orchestratorAdvisory(event.ToolName, cfg.Violations, maxViolations), ""
}

// orchestratorAdvisory is the non-blocking nudge. maxViolations == 0 means
// guidance mode, where nothing is counted.
func orchestratorAdvisory(toolName string, violations, maxViolations int) string {
	msg := fmt.Sprintf(
		"wipnote orchestrator advisory: %s was called directly instead of being delegated. "+
			"Orchestrators coordinate; subagents implement — wrap this in Task(...) to keep the "+
			"orchestrator's context for coordination.", toolName)
	if maxViolations > 0 {
		msg += fmt.Sprintf(" Violation %d of %d; at %d, strict mode will block.",
			violations, maxViolations, maxViolations)
	}
	return msg
}

// orchestratorBlockReason is the terminal message. It names the escape hatch so
// a genuine subagent rescue (bug-c8ac6a11) is never a dead end.
func orchestratorBlockReason(toolName string, violations, maxViolations int) string {
	return fmt.Sprintf(
		"Orchestrator strict mode: %s blocked after %d/%d direct-execution violations.\n"+
			"Delegate via Task(subagent_type=..., prompt=...) instead of running this tool yourself.\n"+
			"Allowed directly: Task, Agent, AskUserQuestion, TodoWrite, Task* bookkeeping, and `wipnote ...` commands.\n"+
			"To reset the counter: wipnote orchestrator enable [--strict]\n"+
			"To take over from a failed subagent: %s=1 in the environment.",
		toolName, violations, maxViolations, OrchestratorRescueEnv)
}
