package main

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

// TestComplianceLockHelper is the subprocess half of TestAcquireComplianceLock_Exclusive:
// it takes the lock, reports "locked", and holds it until stdin closes.
func TestComplianceLockHelper(t *testing.T) {
	path := os.Getenv("COMPLIANCE_LOCK_HELPER_PATH")
	if path == "" {
		t.Skip("subprocess helper")
	}
	release, err := acquireComplianceLock(path)
	if err != nil {
		os.Stdout.WriteString("error\n")
		return
	}
	defer release()
	os.Stdout.WriteString("locked\n")
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
}

// TestAcquireComplianceLock_Exclusive: a second process cannot take the lock
// while the first holds it, and the lock is free again once the holder exits.
func TestAcquireComplianceLock_Exclusive(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "locks", "feat-x.lock")
	// A lockfile left by a crashed holder whose PID is now a live, unrelated
	// process (PID reuse) holds no flock, so it must not block acquisition.
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		t.Fatal(err)
	}
	stale, err := acquireComplianceLock(lockPath)
	if err != nil {
		t.Fatalf("stale lockfile with a live reused PID blocked acquisition: %v", err)
	}
	stale()
	cmd := exec.Command(os.Args[0], "-test.run=^TestComplianceLockHelper$")
	cmd.Env = append(os.Environ(), "COMPLIANCE_LOCK_HELPER_PATH="+lockPath)
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line, _ := bufio.NewReader(stdout).ReadString('\n')
	if line != "locked\n" {
		t.Fatalf("helper did not acquire the lock: %q", line)
	}

	if release, err := acquireComplianceLock(lockPath); err == nil {
		release()
		t.Fatal("second acquire succeeded while the lock was held")
	}

	stdin.Close()
	_ = cmd.Wait()
	release, err := acquireComplianceLock(lockPath)
	if err != nil {
		t.Fatalf("acquire after holder exit: %v", err)
	}
	release()
}
