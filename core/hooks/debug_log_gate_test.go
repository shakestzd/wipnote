package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeEveryDebugLogKind drives each path that can append to debug.log.
func writeEveryDebugLogKind(projectDir string) {
	debugLog(projectDir, "plain %s", "line")
	debugLogFields(projectDir, "h", map[string]string{"k": "v"}, "fields")
	LogTimed(projectDir, "h", nil, time.Now(), "timed")
	LogError("h", "sess-1234567890", "error")
}

// debugLogProject makes a temp project, runs from inside it and returns the
// debug.log path the hook logger would write.
func debugLogProject(t *testing.T) (string, string) {
	t.Helper()
	t.Setenv("WIPNOTE_PROJECT_DIR", "")
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".wipnote"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	return dir, filepath.Join(dir, ".wipnote", "debug.log")
}

// TestDebugLog_OffByDefault pins that no hook path writes debug.log unless
// WIPNOTE_DEBUG=1 (feat-af8d1689).
func TestDebugLog_OffByDefault(t *testing.T) {
	dir, logPath := debugLogProject(t)
	t.Setenv("WIPNOTE_DEBUG", "")
	writeEveryDebugLogKind(dir)
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatalf("debug.log written without %s=1 (stat err=%v)", "WIPNOTE_DEBUG", err)
	}
}

// TestDebugLog_OnWithEnv is the other side: WIPNOTE_DEBUG=1 writes one line per call.
func TestDebugLog_OnWithEnv(t *testing.T) {
	dir, logPath := debugLogProject(t)
	t.Setenv("WIPNOTE_DEBUG", "1")
	writeEveryDebugLogKind(dir)
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("debug.log not written with %s=1: %v", "WIPNOTE_DEBUG", err)
	}
	if got := strings.Count(string(raw), "\n"); got != 4 {
		t.Fatalf("want 4 lines, got %d:\n%s", got, raw)
	}
}
