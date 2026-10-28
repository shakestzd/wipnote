package oneshot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func readConfig(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	must(t, err)
	cfg := map[string]any{}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.UseNumber()
	must(t, dec.Decode(&cfg))
	return cfg
}

func trusted(cfg map[string]any, dir string) bool {
	p, _ := cfg["projects"].(map[string]any)
	d, _ := p[dir].(map[string]any)
	return d["hasTrustDialogAccepted"] == true
}

// A write another process makes between the read and the rename is kept:
// the seed is re-applied on top of it.
func TestPreseed_KeepsConcurrentWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude.json")
	must(t, os.WriteFile(path, []byte(`{"numStartups":1}`), 0o600))
	once := false
	preseedBeforeRename = func() {
		if !once {
			once = true
			time.Sleep(10 * time.Millisecond) // a new mtime even on coarse clocks
			must(t, os.WriteFile(path, []byte(`{"numStartups":2,"concurrent":"kept"}`), 0o600))
		}
	}
	t.Cleanup(func() { preseedBeforeRename = nil })
	must(t, preseedClaude(path, "/w"))
	cfg := readConfig(t, path)
	if cfg["concurrent"] != "kept" || cfg["numStartups"] != json.Number("2") || !trusted(cfg, "/w") || cfg["hasCompletedOnboarding"] != true {
		t.Fatalf("cfg %v", cfg)
	}
}

// Numbers round-trip exactly and the file keeps its mode; an unchanged
// config is not rewritten.
func TestPreseed_LargeIntsAndMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude.json")
	must(t, os.WriteFile(path, []byte(`{"big":12345678901234567890,"f":0.1}`), 0o644))
	must(t, preseedClaude(path, "/w"))
	data, err := os.ReadFile(path)
	must(t, err)
	if !strings.Contains(string(data), "12345678901234567890") || !strings.Contains(string(data), "0.1") {
		t.Fatalf("numbers changed: %s", data)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o644 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
	before, _ := os.Stat(path)
	must(t, preseedClaude(path, "/w"))
	if after, _ := os.Stat(path); !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("an already seeded config was rewritten")
	}
}

// Claude Code writes the config under <config>.lock (a directory); the seed
// takes the same lock, waits while another holder has it, and writes
// nothing if it never frees.
func TestPreseed_TakesClaudeLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude.json")
	must(t, os.WriteFile(path, []byte(`{}`), 0o600))
	lock := path + ".lock"
	must(t, os.Mkdir(lock, 0o777))
	old := preseedLockWait
	preseedLockWait = 100 * time.Millisecond
	t.Cleanup(func() { preseedLockWait = old })
	if err := preseedClaude(path, "/w"); err == nil {
		t.Fatal("a held lock must stop the seed")
	}
	if trusted(readConfig(t, path), "/w") {
		t.Fatal("written under another's lock")
	}
	// Freed while we wait: the seed goes ahead and releases the lock.
	go func() { time.Sleep(30 * time.Millisecond); _ = os.Remove(lock) }()
	preseedLockWait = 2 * time.Second
	must(t, preseedClaude(path, "/w"))
	if !trusted(readConfig(t, path), "/w") {
		t.Fatal("not seeded")
	}
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Fatalf("lock left behind: %v", err)
	}
}
