package oneshot

import (
	"bufio"
	"encoding/json"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// statusMax bounds the status line input read.
const statusMax = 1 << 20

// workerStatus is what the worker's status line command last saw.
type workerStatus struct {
	CostUSD      float64 `json:"cost_usd"`
	InputTokens  float64 `json:"input_tokens"`
	OutputTokens float64 `json:"output_tokens"`
	SessionID    string  `json:"session_id,omitempty"`
}

// RecordStatusLine is the worker's status line command (`wipnote delegate
// status-line`, slice 22): Claude Code pipes it cost.total_cost_usd and the
// context window's token totals on each refresh; it saves them in the run
// folder (status.json, and the usage line the report prints) and returns
// the line to show. It reads stdin once, bounded, and writes two small
// files: it never waits on anything.
func RecordStatusLine(rf *runstore.RunFolder, in io.Reader) (string, error) {
	var feed struct {
		SessionID string `json:"session_id"`
		Cost      struct {
			Total float64 `json:"total_cost_usd"`
		} `json:"cost"`
		Window struct {
			In  float64 `json:"total_input_tokens"`
			Out float64 `json:"total_output_tokens"`
		} `json:"context_window"`
	}
	if err := json.NewDecoder(io.LimitReader(in, statusMax)).Decode(&feed); err != nil {
		return "", err
	}
	st := workerStatus{CostUSD: feed.Cost.Total, InputTokens: feed.Window.In, OutputTokens: feed.Window.Out, SessionID: feed.SessionID}
	data, err := json.Marshal(st)
	if err != nil {
		return "", err
	}
	if err := rf.WriteFile(runstore.FileStatus, append(data, '\n')); err != nil {
		return "", err
	}
	line := st.usage()
	return line, rf.WriteUsage(line)
}

// usage is the claude -p report's usage line: "<in> in / <out> out tokens",
// then ", $<cost>" rounded as the stream-json totals are.
func (s workerStatus) usage() string {
	line := num(s.InputTokens) + " in / " + num(s.OutputTokens) + " out tokens"
	if s.CostUSD > 0 {
		line += ", $" + num(math.Round(s.CostUSD*10000)/10000)
	}
	return line
}

func num(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

// readStatus reads status.json; ok is false before the first refresh.
func readStatus(rf *runstore.RunFolder) (workerStatus, bool) {
	var st workerStatus
	data, _, err := rf.ReadFrom(runstore.FileStatus, 0)
	if err != nil || json.Unmarshal(data, &st) != nil {
		return workerStatus{}, false
	}
	return st, true
}

// turnCounter counts the assistant messages of a transcript, reading only
// what was appended since the last look. It is the turn count --max-turns
// caps in print mode (one per API response, sidechains not counted).
type turnCounter struct {
	path string
	off  int64
	ids  map[string]bool
}

func (c *turnCounter) count() int {
	if c.ids == nil {
		c.ids = map[string]bool{}
	}
	f, err := os.Open(filepath.Clean(c.path))
	if err != nil {
		return len(c.ids)
	}
	defer f.Close()
	if _, err := f.Seek(c.off, io.SeekStart); err != nil {
		return len(c.ids)
	}
	r := bufio.NewReader(f)
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			break // a partial last line is read again next time
		}
		c.off += int64(len(line))
		c.take(line)
	}
	return len(c.ids)
}

func (c *turnCounter) take(line string) {
	if !strings.Contains(line, `"assistant"`) {
		return
	}
	var rec struct {
		Type      string `json:"type"`
		Sidechain bool   `json:"isSidechain"`
		Message   struct {
			ID string `json:"id"`
		} `json:"message"`
	}
	if json.Unmarshal([]byte(line), &rec) == nil && rec.Type == "assistant" && !rec.Sidechain && rec.Message.ID != "" {
		c.ids[rec.Message.ID] = true
	}
}
