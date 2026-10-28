//go:build unix

package procsweep

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeProcRoot builds a minimal /proc lookalike: a btime line, a readable
// self/environ, and one directory per pid.
func fakeProcRoot(t *testing.T, selfEnv string) string {
	t.Helper()
	root := t.TempDir()
	write(t, filepath.Join(root, "stat"), "cpu  1 2 3\nbtime 1793091600\n")
	if selfEnv != "" {
		write(t, filepath.Join(root, "self", "environ"), selfEnv)
	}
	return root
}

func addFakeProc(t *testing.T, root, pid, comm, ppid string, startTicks string, cmdline, environ []string) {
	t.Helper()
	// Fields after "(comm)": state ppid pgrp session tty tpgid flags minflt
	// cminflt majflt cmajflt utime stime cutime cstime priority nice threads
	// itrealvalue starttime.
	stat := pid + " (" + comm + ") S " + ppid + " 1 1 0 -1 0 0 0 0 0 0 0 0 0 20 0 1 0 " + startTicks + " 0 0\n"
	write(t, filepath.Join(root, pid, "stat"), stat)
	write(t, filepath.Join(root, pid, "cmdline"), strings.Join(cmdline, "\x00")+"\x00")
	write(t, filepath.Join(root, pid, "environ"), strings.Join(environ, "\x00")+"\x00")
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestProcfsLister_ReadsSessionEnvCommandAndStart(t *testing.T) {
	root := fakeProcRoot(t, "PATH=/bin\x00")
	addFakeProc(t, root, "4242", "py (srv) x", "1", "500",
		[]string{"python", "-m", "http.server"},
		[]string{"SECRET=x", "CLAUDE_CODE_SESSION_ID=" + liveSession})
	procs, err := procfsLister{root: root, uid: os.Getuid()}.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(procs) != 1 {
		t.Fatalf("want 1 proc, got %+v", procs)
	}
	p := procs[0]
	if p.PID != 4242 || p.PPID != 1 || p.Command != "python -m http.server" {
		t.Fatalf("bad proc: %+v", p)
	}
	if p.Env["CLAUDE_CODE_SESSION_ID"] != liveSession || p.Env["SECRET"] != "" {
		t.Fatalf("env must keep only session keys: %v", p.Env)
	}
	if want := time.Unix(1793091600+5, 0); !p.StartedAt.Equal(want) {
		t.Fatalf("start = %v, want %v", p.StartedAt, want)
	}
}

func TestProcfsLister_SkipsOtherUsers(t *testing.T) {
	root := fakeProcRoot(t, "PATH=/bin\x00")
	addFakeProc(t, root, "7", "sleep", "1", "1", []string{"sleep"}, []string{"CLAUDE_CODE_SESSION_ID=" + liveSession})
	procs, err := procfsLister{root: root, uid: os.Getuid() + 1}.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(procs) != 0 {
		t.Fatalf("another user's process must be skipped, got %+v", procs)
	}
}

func TestProcfsLister_UnreadableSelfEnvironIsPlatformError(t *testing.T) {
	root := fakeProcRoot(t, "")
	_, err := procfsLister{root: root, uid: os.Getuid()}.List(context.Background())
	if !errors.Is(err, ErrEnvUnreadable) {
		t.Fatalf("want ErrEnvUnreadable, got %v", err)
	}
}
