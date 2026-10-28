package codex

import (
	"bytes"
	"encoding/json"
	"regexp"
)

// Usage is one Codex turn's token split.
type Usage struct {
	Input, Cached, Output, Reasoning int
}

// ExecInfo is what the ledger takes from a `codex exec --json` log.
type ExecInfo struct {
	ThreadID string // thread.started's thread_id: the Codex session id
	Usage    Usage  // the last turn.completed's usage
	HasUsage bool
}

// threadIDPattern keeps an id that is one plain token, so it can never put
// a path or spaces into the ledger's worker-sessions column.
var threadIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

type execEvent struct {
	Type     string `json:"type"`
	ThreadID string `json:"thread_id"`
	Usage    *struct {
		Input     float64 `json:"input_tokens"`
		Cached    float64 `json:"cached_input_tokens"`
		Output    float64 `json:"output_tokens"`
		Reasoning float64 `json:"reasoning_output_tokens"`
	} `json:"usage"`
}

// ParseExecJSONL reads the thread id and the last turn's usage from a codex
// exec --json log (codex-cli 0.164.0 event shapes). Lines that are not JSON
// (stderr is merged into the log) are skipped. codex exec runs one turn, so
// the last turn.completed is the run's usage, as the report shows it.
func ParseExecJSONL(raw []byte) ExecInfo {
	var info ExecInfo
	for _, line := range bytes.Split(raw, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var e execEvent
		if json.Unmarshal(line, &e) != nil {
			continue
		}
		switch {
		case e.Type == "thread.started" && info.ThreadID == "" && threadIDPattern.MatchString(e.ThreadID):
			info.ThreadID = e.ThreadID
		case e.Type == "turn.completed" && e.Usage != nil:
			u := e.Usage
			info.Usage = Usage{Input: int(u.Input), Cached: int(u.Cached), Output: int(u.Output), Reasoning: int(u.Reasoning)}
			info.HasUsage = true
		}
	}
	return info
}
