package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// isMCPToolName reports whether name selects one of the read-only MCP tools
// (the wipnote_ prefix is optional). `wipnote report <tool>` uses it to tell a
// tool snapshot from the legacy `report <session-id>` timeline.
func isMCPToolName(name string) bool {
	full := name
	if !strings.HasPrefix(full, "wipnote_") {
		full = "wipnote_" + full
	}
	for _, t := range mcpToolSpecs() {
		if t.Name == full {
			return true
		}
	}
	return false
}

// runReportTool runs one MCP tool through the same code path as `wipnote mcp`
// and writes its structured result as a single JSON document to w. Nothing
// else is written to w, so stdout is machine-parseable; errors go to stderr via
// the returned error. Read-only: it never writes to .wipnote/ or a database.
func runReportTool(w io.Writer, tool, args string) error {
	wipnoteDir, err := findWipnoteDir()
	if err != nil {
		return err
	}
	if fi, statErr := os.Stat(wipnoteDir); statErr != nil || !fi.IsDir() {
		return fmt.Errorf("no .wipnote directory at %s", filepath.Dir(wipnoteDir))
	}
	res, err := runMCPTool(wipnoteDir, tool, json.RawMessage(args), mcpNow())
	if err != nil {
		return err
	}
	return json.NewEncoder(w).Encode(res)
}

// reportSnapshotArg is the pseudo tool name for `wipnote report snapshot`.
const reportSnapshotArg = "snapshot"

// snapshotView is one document the Overview artifact reads from its
// `snapshots` collection. key is the page's own view key, so the document id
// is "<key>-<hours>h".
type snapshotView struct {
	key  string
	tool string
	args func(hours int) map[string]any
}

// snapshotViews must stay in step with the keys docs/artifacts/wipnote-overview.html
// reads (overview, cost_day, cost_model, work, work_stale).
func snapshotViews() []snapshotView {
	return []snapshotView{
		{"overview", "overview", func(h int) map[string]any {
			return map[string]any{"since_hours": h, "max_attention": 50, "max_sessions": 10}
		}},
		{"cost_day", "cost", func(h int) map[string]any {
			return map[string]any{"group_by": "day", "since_hours": h, "limit": 31}
		}},
		{"cost_model", "cost", func(h int) map[string]any {
			return map[string]any{"group_by": "model", "since_hours": h, "limit": 10}
		}},
		{"work", "work_items", func(h int) map[string]any {
			return map[string]any{"limit": 25, "since_hours": h}
		}},
		{"work_stale", "work_items", func(h int) map[string]any {
			return map[string]any{"limit": 25, "since_hours": h, "stale_days": 30}
		}},
	}
}

const (
	maxSnapshotWindows = 6
	maxSnapshotDocSize = 250 << 10 // artifact db documents are capped at 256 KiB
)

// runReportSnapshot runs every artifact view for each window through the same
// tool code as `wipnote mcp` and writes one JSON document per view and window
// ({generated_at, payload}) plus batch.json into outDir. A Claude session
// uploads them with one ArtifactData batch call that reads batch.json, which
// keeps the artifact live in any browser without MCP. Read-only with respect to
// .wipnote/; files are written 0600 because they hold work-item titles.
func runReportSnapshot(w io.Writer, outDir string, windows []int) error {
	if len(windows) == 0 || len(windows) > maxSnapshotWindows {
		return fmt.Errorf("--windows needs 1 to %d values", maxSnapshotWindows)
	}
	seen := map[int]bool{}
	for _, h := range windows {
		if h < 1 || h > 720 {
			return fmt.Errorf("window %d out of range (1-720 hours)", h)
		}
		if seen[h] {
			return fmt.Errorf("window %d given twice", h)
		}
		seen[h] = true
	}
	wipnoteDir, err := findWipnoteDir()
	if err != nil {
		return err
	}
	if fi, statErr := os.Stat(wipnoteDir); statErr != nil || !fi.IsDir() {
		return fmt.Errorf("no .wipnote directory at %s", filepath.Dir(wipnoteDir))
	}
	absDir, err := filepath.Abs(outDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(absDir, 0o700); err != nil {
		return err
	}
	now := mcpNow()

	type writeOp struct {
		Op         string `json:"op"`
		Collection string `json:"collection"`
		DocID      string `json:"doc_id"`
		FilePath   string `json:"file_path"`
	}
	var writes []writeOp
	var ids []string
	for _, h := range windows {
		for _, v := range snapshotViews() {
			args, err := json.Marshal(v.args(h))
			if err != nil {
				return err
			}
			res, err := runMCPTool(wipnoteDir, v.tool, args, now)
			if err != nil {
				return fmt.Errorf("%s (%dh): %w", v.key, h, err)
			}
			payload, err := json.Marshal(res)
			if err != nil {
				return err
			}
			var head struct {
				GeneratedAt string `json:"generated_at"`
			}
			_ = json.Unmarshal(payload, &head)
			if head.GeneratedAt == "" {
				head.GeneratedAt = now.UTC().Format(time.RFC3339)
			}
			doc, err := json.Marshal(map[string]any{"generated_at": head.GeneratedAt, "payload": json.RawMessage(payload)})
			if err != nil {
				return err
			}
			if len(doc) > maxSnapshotDocSize {
				return fmt.Errorf("%s (%dh) is %d bytes, over the artifact document limit", v.key, h, len(doc))
			}
			id := fmt.Sprintf("%s-%dh", v.key, h)
			path := filepath.Join(absDir, id+".json")
			if err := os.WriteFile(path, doc, 0o600); err != nil {
				return err
			}
			writes = append(writes, writeOp{Op: "set", Collection: "snapshots", DocID: id, FilePath: path})
			ids = append(ids, id)
		}
	}
	batchPath := filepath.Join(absDir, "batch.json")
	batch, err := json.Marshal(map[string]any{"writes": writes})
	if err != nil {
		return err
	}
	if err := os.WriteFile(batchPath, batch, 0o600); err != nil {
		return err
	}
	return json.NewEncoder(w).Encode(map[string]any{
		"dir": absDir, "batch": batchPath, "docs": ids, "generated_at": now.UTC().Format(time.RFC3339),
	})
}
