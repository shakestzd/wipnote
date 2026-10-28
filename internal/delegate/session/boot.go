package session

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ScrubbedEnv is the inherited session env boot removes before it starts the
// launcher. Left in place, the parent variables make session_start mark the
// worker as a subagent of its caller (bug-5333ea49) and the session ids leak
// the caller's identity. The worker is its own root; the caller link travels
// only in CallerEnv and the ledger's caller_session.
var ScrubbedEnv = []string{
	"WIPNOTE_PARENT_SESSION", "WIPNOTE_PARENT_EVENT", "WIPNOTE_PARENT_PROMPT_EVENT", "WIPNOTE_PARENT_AGENT",
	"WIPNOTE_NESTING_DEPTH", "WIPNOTE_SESSION_ID", "WIPNOTE_OTEL_SESSION_ID", "WIPNOTE_SESSION_FAMILY_ID",
	"WIPNOTE_AGENT_ID", "WIPNOTE_AGENT_TYPE", "WIPNOTE_CONTINUED_FROM", "WIPNOTE_HARNESS",
	"WIPNOTE_PROJECT_DIR", RunEnv, CallerEnv,
}

// BootEnv returns environ without ScrubbedEnv, plus the worker's run and,
// when there is one, its caller.
func BootEnv(environ []string, runID, caller string) []string {
	drop := map[string]bool{}
	for _, k := range ScrubbedEnv {
		drop[k] = true
	}
	out := make([]string, 0, len(environ)+2)
	for _, kv := range environ {
		k, _, _ := strings.Cut(kv, "=")
		if !drop[k] {
			out = append(out, kv)
		}
	}
	out = append(out, RunEnv+"="+runID)
	if caller != "" {
		out = append(out, CallerEnv+"="+caller)
	}
	return out
}

// BootArgv is the launcher argv: wipnote claude or wipnote codex, in place
// (the caller's cwd, no worktree chooser), with the brief as the first
// prompt. No --settings: the launcher's plugin hooks are the worker's hooks
// (q-settings). A Claude flag would go after "--" (Phase B). A resumed worker
// gets --resume <id> (both launchers take it) and no --name: the resumed
// session keeps its own name (slice 20).
func BootArgv(exe string, info Info, brief string) []string {
	argv := []string{exe, info.Harness, "--in-place"}
	switch {
	case info.ResumeSession != "":
		argv = append(argv, "--resume", info.ResumeSession)
	case info.Harness == HarnessClaude:
		argv = append(argv, "--name", info.Name)
	}
	if brief != "" {
		argv = append(argv, "--", brief)
	}
	return argv
}

// Preamble is put before the brief (decision R): the worker writes its
// answer to the task's result file, which is what wait and result read.
func Preamble(info Info, mailDir string) string {
	result := filepath.Join(mailDir, "outbox", info.Task+"-result.json")
	return fmt.Sprintf(`You are the wipnote delegate worker %q (run %s, work item %s).
When you finish the task below, write your final answer as JSON {"body": "<your answer>"}
to %s : write a temporary file in the same folder first, then rename it to that name.
Then end your turn and wait for the next task.

---

`, info.Name, info.Run, info.WorkItem, result)
}
