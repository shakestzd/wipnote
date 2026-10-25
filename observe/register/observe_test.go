package observe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/hooks"
)

// TestInitRegistersLifecycleFns verifies that importing this package wires all
// three core-hook lifecycle injection points (feat-331927fb).
func TestInitRegistersLifecycleFns(t *testing.T) {
	if hooks.RetentionSweepFn == nil {
		t.Error("RetentionSweepFn not registered by observe.init()")
	}
	if hooks.SessionTelemetryFn == nil {
		t.Error("SessionTelemetryFn not registered by observe.init()")
	}
	if hooks.PortDriftPathsFn == nil {
		t.Error("PortDriftPathsFn not registered by observe.init()")
	}
}

// TestPortDriftPaths_NotPluginCoreRepo_ReturnsNil verifies the manifest-presence
// gate: a directory without packages/plugin-core/manifest.json (and the empty
// root) report no drift rather than erroring.
func TestPortDriftPaths_NotPluginCoreRepo_ReturnsNil(t *testing.T) {
	if got := portDriftPaths(t.TempDir()); got != nil {
		t.Errorf("non-plugin-core dir: want nil, got %v", got)
	}
	if got := portDriftPaths(""); got != nil {
		t.Errorf("empty repoRoot: want nil, got %v", got)
	}
}

// The SessionEnd hook passes the Claude id; telemetry lives in the launcher's
// shard (WIPNOTE_OTEL_SESSION_ID). The hook must finalize that shard.
func TestFinalizeSessionTelemetry_LauncherShard(t *testing.T) {
	projectDir := t.TempDir()
	const shard, claudeID = "01alauncher", "claude-one"
	sessions := filepath.Join(projectDir, ".wipnote", "sessions")
	if err := os.MkdirAll(filepath.Join(sessions, shard), 0o755); err != nil {
		t.Fatal(err)
	}
	html := `<html><body><article id="` + claudeID + `"><h1>s</h1></article></body></html>`
	if err := os.WriteFile(filepath.Join(sessions, claudeID+".html"), []byte(html), 0o644); err != nil {
		t.Fatal(err)
	}
	line := `{"kind":"log","harness":"claude_code","ts":"2026-04-24T19:00:00Z","signal_id":"s1","session_id":"` +
		claudeID + `","canonical":"api_request","native":"claude_code.api_request","cost_usd":0.5}` + "\n"
	if err := os.WriteFile(filepath.Join(sessions, shard, "events.ndjson"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WIPNOTE_OTEL_SESSION_ID", shard)

	if err := finalizeSessionTelemetry(projectDir, claudeID, 10*time.Second); err != nil {
		t.Fatalf("finalizeSessionTelemetry: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sessions, shard, ".index-offset")); err != nil {
		t.Fatalf("launcher shard not marked: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(sessions, claudeID+".html"))
	if !strings.Contains(string(got), `data-total-cost-usd="0.500000"`) {
		t.Fatalf("rollup missing cost:\n%s", got)
	}
}
