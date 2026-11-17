//go:build !windows

package hooks

import "syscall"

// detachAttr puts the generator in its own process group, so the end of the
// hook process (or a signal to its group) does not stop it.
func detachAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setpgid: true} }
