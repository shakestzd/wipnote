package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// autoMaintenanceStarted commits one file in root with git's trace2 event log
// on, and reports whether the commit started git's auto maintenance child
// (`git maintenance run --auto`, or `git gc --auto` on older git). Asking
// whether the child STARTED, not whether it packed anything, keeps the check
// independent of each git version's repack heuristics (bug-6a5b608b: git 2.56
// stopped packing under the gc.auto=1 trigger this test used to rely on).
func autoMaintenanceStarted(t *testing.T, root string) bool {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "probe"), []byte(root+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	trace := filepath.Join(t.TempDir(), "trace2.json")
	t.Setenv("GIT_TRACE2_EVENT", trace)
	provGit(t, root, "add", "--all")
	provGit(t, root, "commit", "-q", "-m", "probe")
	data, err := os.ReadFile(trace)
	if err != nil {
		t.Fatalf("read trace2 log: %v", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		var ev struct {
			Event string   `json:"event"`
			Argv  []string `json:"argv"`
		}
		if json.Unmarshal([]byte(line), &ev) != nil || ev.Event != "child_start" {
			continue
		}
		if isAutoMaintenanceArgv(ev.Argv) {
			return true
		}
	}
	return false
}

// isAutoMaintenanceArgv matches `git maintenance run --auto` and `git gc --auto`.
func isAutoMaintenanceArgv(argv []string) bool {
	joined := " " + strings.Join(argv, " ") + " "
	return strings.Contains(joined, " --auto ") &&
		(strings.Contains(joined, " maintenance run ") || strings.Contains(joined, " gc "))
}

// bug-a2ea88db: after each commit git runs `maintenance run --auto`, which
// detaches and can repack into .git/objects/pack after provGit has returned —
// racing t.TempDir's RemoveAll ("objects/pack: directory not empty"). The
// fixture repo must never start auto maintenance.
func TestProvInitRepo_DisablesAutoMaintenance(t *testing.T) {
	// An empty global config, so the host's git settings decide neither side.
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))

	// Control: an ordinary repo does start auto maintenance on commit, so the
	// fixture's "no" below is not vacuous.
	plain := t.TempDir()
	provGit(t, plain, "init", "-q")
	if !autoMaintenanceStarted(t, plain) {
		t.Fatal("control repo started no auto maintenance on commit; the probe is not observing it")
	}

	fixture := t.TempDir()
	provInitRepo(t, fixture)
	if autoMaintenanceStarted(t, fixture) {
		t.Error("provInitRepo repo started git auto maintenance on commit")
	}
}
