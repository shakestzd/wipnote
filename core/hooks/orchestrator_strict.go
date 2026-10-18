package hooks

import (
	"bytes"
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
//
// Read qualifies only when the read is short (feat-0e560d63): the orchestrator
// may glance at a short file itself, but long files must still be delegated
// to preserve its context. Grep and Glob stay non-whitelisted — they are
// exploration, which belongs to a subagent. Codex reads through its shell
// tool, so a single short cat/head/tail/sed read qualifies too.
//
// baseDir resolves relative paths and must be the directory the tool runs in
// (the event's cwd): in a managed worktree the canonical project dir points at
// the main checkout, which may hold a different copy of the file.
func isOrchestratorWhitelistedTool(toolName string, toolInput map[string]any, baseDir string) bool {
	if orchestratorWhitelist[toolName] {
		return true
	}
	if isShellTool(toolName) {
		cmd := shellCommand(toolInput)
		return isWipnoteCLICommand(cmd) || isShortShellRead(cmd, baseDir)
	}
	// read_file is Antigravity's spelling of Read (pluginbuild gemini_compat.go).
	if toolName == "Read" || toolName == "read_file" {
		return isShortRead(toolInput, baseDir)
	}
	return false
}

const (
	// orchestratorShortReadLines is the largest read an orchestrator may make
	// directly, either as an explicit limit or as the file's total length.
	orchestratorShortReadLines = 200
	// orchestratorShortReadMaxBytes caps the newline count: a file at least
	// this large is treated as long without reading it.
	orchestratorShortReadMaxBytes = 64 * 1024
)

// orchestratorReadGuidance explains the short-read allowance; it is appended
// to Read advisories and to the block message.
const orchestratorReadGuidance = "Read short files (≤200 lines, or one offset/limit range ≤200) directly; " +
	"delegate long files, multi-file reads and exploration to a subagent " +
	"(the native code explorer: Explore on Claude Code, explorer on Codex, researcher on Antigravity)."

// isShortRead reports whether a Read call reads at most
// orchestratorShortReadLines lines: an explicit limit in range, or a target
// file that is itself that short. A file that cannot be inspected counts as
// long, so the guard fails closed.
func isShortRead(toolInput map[string]any, baseDir string) bool {
	if limit := toInt(toolInput["limit"]); limit > 0 && limit <= orchestratorShortReadLines {
		return true
	}
	path, _ := toolInput["file_path"].(string)
	if path == "" {
		path, _ = toolInput["absolute_path"].(string) // Antigravity read_file
	}
	if path == "" {
		return false
	}
	return fileHasAtMostLines(resolveReadPath(path, baseDir), orchestratorShortReadLines)
}

// fileHasAtMostLines stats first so a large file is never read; a small one
// is read whole and its lines counted (a final unterminated line counts).
func fileHasAtMostLines(path string, maxLines int) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() >= orchestratorShortReadMaxBytes {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	lines := bytes.Count(data, []byte{'\n'})
	if len(data) > 0 && data[len(data)-1] != '\n' {
		lines++
	}
	return lines <= maxLines
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
	readDir := event.CWD
	if readDir == "" {
		readDir = ctx.ProjectDir
	}
	if isOrchestratorWhitelistedTool(event.ToolName, event.ToolInput, readDir) {
		return "", ""
	}
	if cfg.Mode != OrchestratorModeStrict {
		return onceAdvisory(ctx, event.ToolName, 0, 0), ""
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
	// The warning immediately before the block always shows; earlier ones
	// show once per session.
	if cfg.Violations == maxViolations-1 {
		return orchestratorAdvisory(event.ToolName, cfg.Violations, maxViolations), ""
	}
	return onceAdvisory(ctx, event.ToolName, cfg.Violations, maxViolations), ""
}

// onceAdvisory returns the advisory only the first time in a session. Since
// Claude Code now delivers hook context to the model, repeating it on every
// direct tool call would burn context; counting is unaffected.
func onceAdvisory(ctx *toolUseContext, toolName string, violations, maxViolations int) string {
	if !claimSessionOnce(ctx.HgDir, ctx.SessionID, "orchestrator-advisory") {
		return ""
	}
	return orchestratorAdvisory(toolName, violations, maxViolations)
}

// orchestratorAdvisory is the non-blocking nudge. maxViolations == 0 means
// guidance mode, where nothing is counted.
func orchestratorAdvisory(toolName string, violations, maxViolations int) string {
	msg := fmt.Sprintf("wipnote orchestrator advisory: delegate %s via Task(...).", toolName)
	if maxViolations > 0 {
		msg += fmt.Sprintf(" Violation %d/%d; strict mode blocks at %d.",
			violations, maxViolations, maxViolations)
	}
	if toolName == "Read" {
		msg += " " + orchestratorReadGuidance
	}
	return msg
}

// orchestratorBlockReason is the terminal message. It names the escape hatch so
// a genuine subagent rescue (bug-c8ac6a11) is never a dead end.
func orchestratorBlockReason(toolName string, violations, maxViolations int) string {
	return fmt.Sprintf(
		"Orchestrator strict mode: %s blocked after %d/%d direct-execution violations.\n"+
			"Delegate via Task(subagent_type=..., prompt=...) instead of running this tool yourself.\n"+
			"Allowed directly: Task, Agent, AskUserQuestion, TodoWrite, Task* bookkeeping, short Reads, and `wipnote ...` commands.\n"+
			"%s\n"+
			"To reset the counter: wipnote orchestrator enable [--strict]\n"+
			"To take over from a failed subagent: %s=1 in the environment.",
		toolName, violations, maxViolations, orchestratorReadGuidance, OrchestratorRescueEnv)
}
