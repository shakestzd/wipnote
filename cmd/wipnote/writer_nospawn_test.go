package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Only a launcher may start the writer daemon (bug-5ca53f70). A hook or a CLI
// command that finds no socket falls back to the derived index at once and
// must never fork a `_serve-child`. Each fork used to outlive its parent as
// an orphan that hydrated the whole project.
func TestHooksAndCLINeverSpawnWriter(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("scans /proc for spawned processes")
	}
	bin := buildOtelCollectTestBinary(t)
	root := newNoSpawnProject(t)
	t.Cleanup(func() { killServeChildrenFor(root) })

	sid := "aaaaaaaa-0000-4000-8000-000000000001"
	payload := `{"session_id":"` + sid + `","hook_event_name":"PostToolUse","tool_name":"Read",` +
		`"tool_use_id":"tu1","tool_input":{"file_path":"` + root + `/README.md"},"tool_response":{},` +
		`"cwd":"` + root + `","source":"startup","transcript_path":""}`
	for _, args := range [][]string{
		{"hook", "session-start"}, {"hook", "posttooluse"}, {"hook", "user-prompt-submit"}, {"status"},
	} {
		runNoSpawnCommand(t, bin, root, sid, payload, args...)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if pids := serveChildrenFor(root); len(pids) > 0 {
			t.Fatalf("a hook or CLI command spawned the writer daemon: pids %v", pids)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// newNoSpawnProject makes a short temp wipnote project in a git repo.
func newNoSpawnProject(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp("", "nsp")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	if err := os.MkdirAll(filepath.Join(root, ".wipnote"), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", root, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	return root
}

// runNoSpawnCommand runs one wipnote command against root with daemon
// auto-start allowed, as in a real session with no launcher.
func runNoSpawnCommand(t *testing.T, bin, root, sid, stdin string, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = root
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Env = noSpawnEnv(root, sid)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); ctx.Err() != nil {
		t.Fatalf("wipnote %v did not return: %v\n%s", args, err, out.String())
	}
}

// noSpawnEnv is the parent environment without any project or daemon
// override, pointed at root.
func noSpawnEnv(root, sid string) []string {
	var env []string
	for _, kv := range os.Environ() {
		switch strings.SplitN(kv, "=", 2)[0] {
		case "WIPNOTE_PROJECT_DIR", "CLAUDE_PROJECT_DIR", "WIPNOTE_NO_AUTO_WRITER",
			"WIPNOTE_SESSION_ID", "CLAUDE_SESSION_ID", "WIPNOTE_DAEMON_SOCKET":
			continue
		}
		env = append(env, kv)
	}
	return append(env, "CLAUDE_PROJECT_DIR="+root, "CLAUDE_SESSION_ID="+sid)
}

// serveChildrenFor lists live `_serve-child` processes started for root.
func serveChildrenFor(root string) []int {
	entries, _ := os.ReadDir("/proc")
	var pids []int
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		raw, err := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline"))
		if err != nil {
			continue
		}
		args := strings.Split(string(raw), "\x00")
		if containsArg(args, "_serve-child") && containsArg(args, root) {
			pids = append(pids, pid)
		}
	}
	return pids
}

func containsArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

// killServeChildrenFor stops writers this test caused. root is unique to the
// test, so no other process matches.
func killServeChildrenFor(root string) {
	for _, pid := range serveChildrenFor(root) {
		_ = syscall.Kill(pid, syscall.SIGTERM)
	}
}
