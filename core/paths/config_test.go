package paths

import (
	"os"
	"path/filepath"
	"testing"
)

// TestReadProjectConfig pins the shared config read: a valid file decodes
// the requested keys (unknown keys ignored); a missing or malformed file
// reports false.
func TestReadProjectConfig(t *testing.T) {
	type cfg struct {
		TTL  int   `json:"ttl"`
		Warn *bool `json:"warn"`
	}
	write := func(body string) string {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, ".wipnote"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(ProjectConfigPath(dir), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return dir
	}

	var got cfg
	if !ReadProjectConfig(write(`{"ttl": 30, "warn": false, "other": [1]}`), &got) {
		t.Fatal("valid config reported false")
	}
	if got.TTL != 30 || got.Warn == nil || *got.Warn {
		t.Errorf("decoded %+v, want ttl 30 and warn false", got)
	}

	for name, dir := range map[string]string{
		"missing":   t.TempDir(),
		"malformed": write(`{"ttl": `),
		"wrongtype": write(`{"ttl": "x"}`),
	} {
		if ReadProjectConfig(dir, &cfg{}) {
			t.Errorf("%s config reported true", name)
		}
	}
	if want := filepath.Join("p", ".wipnote", "config.json"); ProjectConfigPath("p") != want {
		t.Errorf("ProjectConfigPath = %q, want %q", ProjectConfigPath("p"), want)
	}
}
