package procsweep

import (
	"regexp"
	"strings"

	"github.com/shakestzd/wipnote/core/agent"
)

// harnessKey is the wipnote launcher's harness stamp. It decides between
// natives when a nested launch inherits more than one harness's variables.
const harnessKey = "WIPNOTE_HARNESS"

// nativeKeys are the harness-native session variables, by harness.
//
//   - Claude Code sets CLAUDE_CODE_SESSION_ID in Bash/PowerShell tool, hook and
//     stdio MCP subprocesses (https://code.claude.com/docs/en/env-vars).
//   - Codex injects CODEX_THREAD_ID into the terminal environment
//     (openai/codex PR #10096); CODEX_SESSION_ID is the root thread fallback.
var nativeKeys = map[string][]string{
	agent.HarnessClaude:      {"CLAUDE_CODE_SESSION_ID", "CLAUDE_SESSION_ID"},
	agent.HarnessCodex:       {"CODEX_THREAD_ID", "CODEX_SESSION_ID"},
	agent.HarnessGemini:      {"GEMINI_SESSION_ID"},
	agent.HarnessAntigravity: {"ANTIGRAVITY_SESSION_ID"},
}

// fallbackOrder is used when no harness stamp is present. WIPNOTE_SESSION_ID
// comes last: the SessionStart hook writes it into CLAUDE_ENV_FILE, and a
// child session can inherit its parent's stale value (core/agent/detect.go).
var fallbackOrder = []string{
	"CLAUDE_CODE_SESSION_ID", "CLAUDE_SESSION_ID",
	"CODEX_THREAD_ID", "CODEX_SESSION_ID",
	"ANTIGRAVITY_SESSION_ID", "GEMINI_SESSION_ID",
	"WIPNOTE_SESSION_ID",
}

// sessionEnvKeys is every variable a Lister keeps from a process environment.
var sessionEnvKeys = func() map[string]bool {
	keys := map[string]bool{harnessKey: true}
	for _, k := range fallbackOrder {
		keys[k] = true
	}
	return keys
}()

// idToken is the shape a session id must have to be trusted: an opaque token
// with no whitespace, path separators or shell metacharacters.
var idToken = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

// SessionIDFromEnv returns the session id a process inherited, or "" when it
// carries none. It mirrors hooks.ResolveSessionID: a recognised launcher
// harness stamp limits the lookup to that harness's native variables, then
// WIPNOTE_SESSION_ID, so another harness's native inherited from a parent
// shell never wins. With no or an unknown stamp, the full fallback order runs.
func SessionIDFromEnv(env map[string]string) string {
	order := fallbackOrder
	h := strings.ToLower(strings.TrimSpace(env[harnessKey]))
	if natives, ok := nativeKeys[h]; ok {
		order = append(append([]string{}, natives...), "WIPNOTE_SESSION_ID")
	}
	for _, k := range order {
		id := agent.NormaliseSessionID(strings.TrimSpace(env[k]))
		if idToken.MatchString(id) {
			return id
		}
	}
	return ""
}

// keepSessionEnv reduces KEY=VALUE pairs to the session-identifying ones.
func keepSessionEnv(pairs []string) map[string]string {
	out := map[string]string{}
	for _, kv := range pairs {
		k, v, ok := strings.Cut(kv, "=")
		if ok && sessionEnvKeys[k] {
			out[k] = v
		}
	}
	return out
}
