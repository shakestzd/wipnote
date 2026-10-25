package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestReadTaskCompletionConfig_MissingFile(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".wipnote"), 0o755)
	if readTaskCompletionConfig(dir) {
		t.Error("expected false when config.json is missing")
	}
}

func TestReadTaskCompletionConfig_FlagOff(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, ".wipnote")
	os.MkdirAll(cfgDir, 0o755)
	data, _ := json.Marshal(map[string]any{"block_task_completion_on_quality_failure": false})
	os.WriteFile(filepath.Join(cfgDir, "config.json"), data, 0o644)

	if readTaskCompletionConfig(dir) {
		t.Error("expected false when flag is explicitly false")
	}
}

func TestReadTaskCompletionConfig_FlagOn(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, ".wipnote")
	os.MkdirAll(cfgDir, 0o755)
	data, _ := json.Marshal(map[string]any{"block_task_completion_on_quality_failure": true})
	os.WriteFile(filepath.Join(cfgDir, "config.json"), data, 0o644)

	if !readTaskCompletionConfig(dir) {
		t.Error("expected true when flag is true")
	}
}

func TestRunTaskCompletionGate_UnknownProject(t *testing.T) {
	// Empty dir with no manifest files → passes (unknown project type).
	dir := t.TempDir()
	result := runTaskCompletionGate(dir)
	if !result.Passed {
		t.Errorf("expected pass for unknown project, got failed: %s", result.Output)
	}
}

func TestRunTaskCompletionGate_DetectsGoProject(t *testing.T) {
	dir := t.TempDir()
	// Create a go.mod so DetectProjectType returns Go.
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n"), 0o644)

	result := runTaskCompletionGate(dir)
	// The gate will likely fail (no Go source), but it should detect the project.
	if result.GateName != "go test ./..." {
		t.Errorf("GateName = %q, want %q", result.GateName, "go test ./...")
	}
}
