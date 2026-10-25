package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// Only a launcher starts the writer (bug-5ca53f70).
//
// spawnHeadlessWriter is unexported and its one caller is EnsureDaemon, which
// only the harness launchers call. Hooks and CLI commands never start one:
// with no socket, a hook reads canonical files directly. They used to fork a
// writer of their own on a miss
// (SubmitOrSpawn), and on a slow bind mount, where the socket took longer
// than a hook to appear, every hook forked another. Those servers outlived
// their hooks as orphans, each hydrating the project (#260).

// spawnHeadlessWriter forks `<selfExe> --project-dir <projectRoot>
// _serve-child --headless` as a DETACHED process in its own process group so
// it outlives the spawning hook subprocess. Unlike the supervised
// _serve-child children, the writer must SURVIVE its spawner's exit (the
// hook exits in milliseconds), so it deliberately does NOT set Pdeathsig.
//
// stdout/stderr are routed to .wipnote/logs/writer.log; stdin is closed. The
// returned error is non-nil only when the fork itself fails (e.g. sandboxed
// exec). Readiness is not checked here: EnsureDaemon dial-waits for the
// socket itself.
func spawnHeadlessWriter(selfExe, projectRoot string) error {
	if selfExe == "" {
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("resolve self exe: %w", err)
		}
		selfExe = exe
	}
	logDir := filepath.Join(projectRoot, ".wipnote", "logs")
	_ = os.MkdirAll(logDir, 0o755)
	var out *os.File
	if f, err := os.OpenFile(filepath.Join(logDir, "writer.log"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		out = f
	}

	cmd := exec.Command(selfExe, "--project-dir", projectRoot, "_serve-child", "--headless")
	cmd.Dir = projectRoot
	cmd.Stdin = nil
	if out != nil {
		cmd.Stdout = out
		cmd.Stderr = out
	}
	cmd.Env = os.Environ()
	// Detach into a new process group so a SIGKILL to the hook's group does
	// not propagate. Portable across Linux + darwin (no Pdeathsig: the
	// writer must outlive the short-lived spawning hook).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		if out != nil {
			_ = out.Close()
		}
		return fmt.Errorf("spawn headless writer: %w", err)
	}
	// Reap the immediate child handle in the background so we don't leave a
	// zombie if it exits early (e.g. lost the lease race and exited 0). The
	// long-lived writer is in its own process group; this Wait only reaps
	// our direct fork handle.
	go func() { _ = cmd.Wait() }()
	if out != nil {
		_ = out.Close() // child holds its own fd after Start
	}
	return nil
}
