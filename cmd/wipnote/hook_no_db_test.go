package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	dbpkg "github.com/shakestzd/wipnote/core/db"
)

// TestEveryHookSubcommand_OpensNoDatabase pins feat-174dc958's done-when: no
// hook path opens the per-project SQLite file. Every `wipnote hook <name>`
// subcommand runs once in a temp project with WIPNOTE_DB_PATH pointed at a
// fresh path; a hook that opened the file db would create it there (db.Open
// creates and migrates). The positive control proves the probe can see an
// open at that path at all.
func TestEveryHookSubcommand_OpensNoDatabase(t *testing.T) {
	projectDir := isolateHookRun(t)
	dbPath := filepath.Join(t.TempDir(), "hook-file.db")
	t.Setenv("WIPNOTE_DB_PATH", dbPath)

	ran := 0
	for _, sub := range hookCmd().Commands() {
		withStdin(t, hookProbePayload(projectDir, sub.Name()))
		runQuiet(t, func() { _ = sub.RunE(sub, nil) })
		ran++
		if _, err := os.Stat(dbPath); err == nil {
			t.Errorf("hook %q opened the file db", sub.Name())
			for _, p := range []string{dbPath, dbPath + "-wal", dbPath + "-shm"} {
				_ = os.Remove(p)
			}
		}
	}
	if ran < 10 {
		t.Fatalf("only %d hook subcommands ran; the probe is not covering the hook tree", ran)
	}
	control, err := dbpkg.Open(dbPath)
	if err != nil {
		t.Fatalf("positive control: %v", err)
	}
	control.Close()
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("positive control: opening the path did not create %s, so the probe is blind", dbPath)
	}
}

// isolateHookRun points every per-user and per-session path at temp dirs and
// returns a temp wipnote project to run the hooks in.
func isolateHookRun(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	for _, k := range []string{"WIPNOTE_PROJECT_DIR", "CLAUDE_PROJECT_DIR", "WIPNOTE_SESSION_ID",
		"WIPNOTE_DAEMON_SOCKET", "WIPNOTE_PARENT_SESSION", "WIPNOTE_SESSION_FAMILY_ID", headlessHookEnv} {
		t.Setenv(k, "")
	}
	t.Setenv("CLAUDE_ENV_FILE", filepath.Join(home, "claude.env"))
	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, ".wipnote"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(projectDir)
	return projectDir
}

// hookProbePayload is one generic event that every handler can parse.
func hookProbePayload(projectDir, name string) string {
	return fmt.Sprintf(`{"session_id":"019ee380-abcd-7000-8000-0000000000db","cwd":%q,`+
		`"hook_event_name":%q,"tool_name":"Read","tool_use_id":"toolu_nodb",`+
		`"tool_input":{"file_path":"a.go"},"tool_response":{"output":"x"},`+
		`"agent_id":"agent-nodb","agent_type":"patch-coder","task_id":"t1","prompt":"hi"}`,
		projectDir, name)
}

// runQuiet runs fn with stdout sent to /dev/null.
func runQuiet(t *testing.T, fn func()) {
	t.Helper()
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer devNull.Close()
	orig := os.Stdout
	os.Stdout = devNull
	defer func() { os.Stdout = orig }()
	fn()
}
