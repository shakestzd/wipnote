package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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
