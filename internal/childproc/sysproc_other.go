//go:build !linux

package childproc

import "syscall"

// childSysProcAttr returns the platform-specific SysProcAttr for child
// processes. On non-Linux platforms Pdeathsig is unavailable; we still
// set Setpgid so the child is in its own process group and a SIGKILL to
// the parent's pgroup does not propagate automatically. On truly
// unsupported platforms (e.g. Windows) where SysProcAttr does not exist
// or Setpgid is not a field, this function returns nil and the caller
// leaves cmd.SysProcAttr unset — belt-and-suspenders stale-PID reaping
// remains the primary defence on those platforms.
func childSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

// WriterSysProcAttr returns the SysProcAttr for a serve-managed headless writer
// daemon (feat-075c110d increment 2). On non-Linux platforms Pdeathsig is
// unavailable; Setpgid still isolates the writer's process group. serve_child's
// explicit SIGTERM-on-shutdown reap is the primary lifecycle guard here.
func WriterSysProcAttr() *syscall.SysProcAttr {
	return childSysProcAttr()
}

// killChildProcessGroup force-kills pid's entire process group, not just pid
// itself. See the Linux variant's doc comment for why: a plain
// cmd.Process.Kill() leaves any subprocess the child forked (e.g. hydration's
// per-file `git log --follow` calls) running as an orphan.
func killChildProcessGroup(pid int) error {
	return syscall.Kill(-pid, syscall.SIGKILL)
}
