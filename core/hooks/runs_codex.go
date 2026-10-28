package hooks

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/shakestzd/wipnote/core/filelock"
	"github.com/shakestzd/wipnote/core/paths"
	"github.com/shakestzd/wipnote/core/runledger"
)

// Codex has no background flag: every shell call is a candidate run, and its
// PostToolUse arrives when the command finishes. Only SLOW calls become runs
// (feat-36658c22, owner decision): PreToolUse writes the start to a scratch
// file in the gitignored session folder; PostToolUse promotes it to the
// tracked ledger (start and end rows) when the call ran at least the
// threshold, and drops it otherwise. A start whose PostToolUse never comes is
// bounded by codexPendingMax and removed at SessionEnd.

// codexMinRunSeconds is the default promotion threshold. A project may set
// "runs": {"codexMinSeconds": N} in .wipnote/config.json.
const codexMinRunSeconds = 10

// codexPendingMax caps the scratch file: the oldest starts are dropped first.
const codexPendingMax = 64

const codexPendingName = "codex-pending-runs.jsonl"

// codexPendingPath is the session's scratch file of unfinished starts.
func codexPendingPath(projectDir, sessionID string) string {
	return filepath.Join(projectDir, ".wipnote", "sessions", sessionID, codexPendingName)
}

// recordRunFromPreToolUse is the PreToolUse half: a Codex shell call's start
// goes to the scratch file, never to the tracked ledger.
func recordRunFromPreToolUse(event *CloudEvent, ctx *toolUseContext) {
	if event.Harness != HarnessCodex || event.ToolName != "Bash" || event.ToolUseID == "" {
		return
	}
	if ctx.ProjectDir == "" || ctx.SessionID == "" {
		return
	}
	start := newRunStart(event, ctx, runledger.KindShell, "", runClock().UTC())
	path := codexPendingPath(ctx.ProjectDir, ctx.SessionID)
	updatePendingCodexRuns(path, func(p []runledger.Event) []runledger.Event {
		p = append(p, start)
		if len(p) > codexPendingMax {
			p = p[len(p)-codexPendingMax:]
		}
		return p
	})
}

// finishCodexRun is the PostToolUse half: it takes the pending start (or,
// when the start was lost, back-dates one from the payload's duration) and
// promotes the call when it ran at least the threshold.
func finishCodexRun(event *CloudEvent, ctx *toolUseContext) {
	now := runClock().UTC()
	start, ok := takePendingCodexRun(codexPendingPath(ctx.ProjectDir, ctx.SessionID), event.ToolUseID)
	if !ok {
		d := event.DurationMS.Duration()
		if d <= 0 {
			return
		}
		start = newRunStart(event, ctx, runledger.KindShell, "", now.Add(-d))
	}
	if now.Sub(start.At) < codexMinRun(ctx.ProjectDir) {
		return
	}
	status := runledger.StatusCompleted
	if !isSuccess(event.ToolResult) {
		status = runledger.StatusFailed
	}
	end := runEndFor(start, now, status)
	last, first := runledger.SummarizeText(summarizeToolOutput(event.ToolResult))
	end.LastLine, end.FirstError = runledger.Clean(last, ctx.ProjectDir), runledger.Clean(first, ctx.ProjectDir)
	appendRunEvent(ctx.ProjectDir, start)
	appendRunEvent(ctx.ProjectDir, end)
}

// codexMinRun is the promotion threshold: the project setting, else the
// default. A negative or missing setting uses the default.
func codexMinRun(projectDir string) time.Duration {
	var cfg struct {
		Runs struct {
			CodexMinSeconds *float64 `json:"codexMinSeconds"`
		} `json:"runs"`
	}
	if projectDir != "" && paths.ReadProjectConfig(projectDir, &cfg) {
		if v := cfg.Runs.CodexMinSeconds; v != nil && *v >= 0 {
			return time.Duration(*v * float64(time.Second))
		}
	}
	return codexMinRunSeconds * time.Second
}

// takePendingCodexRun removes and returns the pending start for toolUseID.
func takePendingCodexRun(path, toolUseID string) (runledger.Event, bool) {
	var found runledger.Event
	ok := false
	if toolUseID == "" {
		return found, false
	}
	updatePendingCodexRuns(path, func(p []runledger.Event) []runledger.Event {
		for i, e := range p {
			if e.ToolUseID == toolUseID {
				found, ok = e, true
				return append(p[:i:i], p[i+1:]...)
			}
		}
		return p
	})
	return found, ok
}

// updatePendingCodexRuns is the scratch read-modify-write under the file
// guard; an emptied scratch file is removed.
func updatePendingCodexRuns(path string, mutate func([]runledger.Event) []runledger.Event) {
	release := filelock.Guard(path)
	defer release()
	pending := mutate(readPendingCodexRuns(path))
	if len(pending) == 0 {
		_ = os.Remove(path)
		return
	}
	var buf bytes.Buffer
	for _, e := range pending {
		if b, err := json.Marshal(e); err == nil {
			buf.Write(b)
			buf.WriteByte('\n')
		}
	}
	_ = filelock.WriteFileAtomic(path, buf.Bytes(), 0o644)
}

// readPendingCodexRuns reads the scratch file; bad lines are skipped.
func readPendingCodexRuns(path string) []runledger.Event {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []runledger.Event
	for _, line := range bytes.Split(data, []byte("\n")) {
		var e runledger.Event
		if len(line) > 0 && json.Unmarshal(line, &e) == nil {
			out = append(out, e)
		}
	}
	return out
}

// pruneCodexPendingRuns drops a session's unfinished starts at SessionEnd:
// a call that never reported back is not a run.
func pruneCodexPendingRuns(projectDir, sessionID string) {
	if projectDir == "" || sessionID == "" {
		return
	}
	path := codexPendingPath(projectDir, sessionID)
	_ = os.Remove(path)
	_ = os.Remove(path + filelock.LockSuffix)
}
