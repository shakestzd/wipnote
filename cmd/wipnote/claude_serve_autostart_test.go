package main

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// TestCheckServeLock_NoLockfile verifies that checkServeLock returns
// (false, false) when no lock file exists.
func TestCheckServeLock_NoLockfile(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, ".wipnote"), 0o755)

	skip, stale := checkServeLock(dir)
	if skip {
		t.Error("checkServeLock: skipSpawn = true, want false (no lockfile)")
	}
	if stale {
		t.Error("checkServeLock: stale = true, want false (no lockfile)")
	}
}

// TestEnsureServeForOtel_SkipsWhenLockfileAlive verifies that
// ensureServeForOtel does not spawn a serve process when the lock file
// contains the PID of a live process (os.Getpid()).
//
// We test the checkServeLock helper directly: a lock pointing at
// os.Getpid() must return (skipSpawn=true, stale=false) because this
// process is alive.
func TestEnsureServeForOtel_SkipsWhenLockfileAlive(t *testing.T) {
	dir := t.TempDir()
	hgDir := filepath.Join(dir, ".wipnote")
	_ = os.MkdirAll(hgDir, 0o755)

	// Write a lock file pointing at the current (live) process.
	lockPath := serveLockPath(dir)
	if err := os.WriteFile(lockPath, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644); err != nil {
		t.Fatalf("write lockfile: %v", err)
	}

	skip, stale := checkServeLock(dir)
	if !skip {
		t.Error("checkServeLock: skipSpawn = false, want true (current PID is alive)")
	}
	if stale {
		t.Error("checkServeLock: stale = true, want false (current PID is alive)")
	}
}

// TestEnsureServeForOtel_CleansStaleLockfile verifies that checkServeLock
// returns (false, stale=true) when the lock file contains a non-existent PID,
// allowing the caller to clean up and proceed with a fresh spawn.
func TestEnsureServeForOtel_CleansStaleLockfile(t *testing.T) {
	dir := t.TempDir()
	hgDir := filepath.Join(dir, ".wipnote")
	_ = os.MkdirAll(hgDir, 0o755)

	// Use a PID that cannot exist: 99999999 (far beyond OS limit on any platform).
	lockPath := serveLockPath(dir)
	if err := os.WriteFile(lockPath, []byte("99999999\n"), 0o644); err != nil {
		t.Fatalf("write lockfile: %v", err)
	}

	skip, stale := checkServeLock(dir)
	if skip {
		t.Error("checkServeLock: skipSpawn = true, want false (PID 99999999 is not alive)")
	}
	if !stale {
		t.Error("checkServeLock: stale = false, want true (PID 99999999 is not alive)")
	}

	// Caller should remove the stale lockfile; verify our test assumption that
	// the file still exists (the helper does not remove it — that's the caller's job).
	if _, err := os.Stat(lockPath); err != nil {
		t.Error("lockfile unexpectedly removed by checkServeLock; removal is caller's responsibility")
	}
}

// TestWriteRemoveServeLock verifies the write/remove lifecycle.
func TestWriteRemoveServeLock(t *testing.T) {
	dir := t.TempDir()
	hgDir := filepath.Join(dir, ".wipnote")
	_ = os.MkdirAll(hgDir, 0o755)

	writeServeLock(dir)

	data, err := os.ReadFile(serveLockPath(dir))
	if err != nil {
		t.Fatalf("lockfile not written: %v", err)
	}
	pidStr := string(data)
	pid, err := strconv.Atoi(pidStr[:len(pidStr)-1]) // strip trailing newline
	if err != nil || pid != os.Getpid() {
		t.Errorf("lockfile PID = %q, want %d", pidStr, os.Getpid())
	}

	removeServeLock(dir)
	if _, err := os.Stat(serveLockPath(dir)); !os.IsNotExist(err) {
		t.Error("lockfile still exists after removeServeLock")
	}
}
