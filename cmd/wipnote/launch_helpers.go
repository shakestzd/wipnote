package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// devcontainerDetector is the function used to detect a devcontainer.
// Tests can replace this to control detection behavior deterministically.
var devcontainerDetector = defaultDevcontainerDetector

func defaultDevcontainerDetector() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	if os.Getenv("CODESPACES") == "true" {
		return true
	}
	if os.Getenv("REMOTE_CONTAINERS") == "true" {
		return true
	}
	return false
}

// isDevcontainer returns true when wipnote is running inside a Docker
// container or GitHub Codespace.
func isDevcontainer() bool {
	return devcontainerDetector()
}

// otelNoticeMarkerPath returns the path of the one-time notice marker file.
func otelNoticeMarkerPath(projectDir string) string {
	return filepath.Join(projectDir, ".wipnote", ".otel-notice-shown")
}

// MaybeShowOtelNotice prints a one-time notice to STDERR on first launch
// explaining that wipnote captures Claude Code telemetry via OTel.
// Subsequent launches are silent (a marker file records that the notice
// has been shown). Safe to call when .wipnote/ doesn't exist — it
// simply returns without creating the directory or printing anything.
func MaybeShowOtelNotice(projectDir string) {
	if projectDir == "" {
		return
	}
	// Respect explicit opt-out — no need to explain what we're not doing.
	if isExplicitlyDisabled(os.Getenv("WIPNOTE_OTEL_ENABLED")) {
		return
	}
	// Only print when .wipnote/ already exists — don't create it just
	// to write the marker.
	wipnoteDir := filepath.Join(projectDir, ".wipnote")
	if _, err := os.Stat(wipnoteDir); os.IsNotExist(err) {
		return
	}
	markerPath := otelNoticeMarkerPath(projectDir)
	if _, err := os.Stat(markerPath); err == nil {
		return // notice already shown on a previous launch
	}

	notice := strings.Join([]string{
		"",
		"  wipnote: OTel telemetry is on (first-launch notice)",
		"  -------------------------------------------------------",
		"  wipnote auto-captures Claude Code activity via OpenTelemetry:",
		"    tool calls, prompts, costs, token usage, and latencies.",
		"",
		"  A per-session OTLP collector is started automatically.",
		"  Data stays 100% local, stored in .wipnote/wipnote.db.",
		"",
		"  Powers: activity feed · per-turn cost badges · span timeline",
		"  Opt out: set WIPNOTE_OTEL_ENABLED=0 before launching.",
		"",
	}, "\n")
	fmt.Fprint(os.Stderr, notice)

	// Write marker so the notice doesn't repeat. Ignore errors — if the
	// write fails, re-showing the notice next launch is acceptable.
	_ = os.WriteFile(markerPath, []byte("shown\n"), 0o644)
}

// debugLog writes a message to stderr only when WIPNOTE_DEBUG is set.
// Used for low-level operational tracing that should not appear in normal output.
func debugLog(msg string) {
	if os.Getenv("WIPNOTE_DEBUG") != "" {
		fmt.Fprintf(os.Stderr, "wipnote [debug]: %s\n", msg)
	}
}
