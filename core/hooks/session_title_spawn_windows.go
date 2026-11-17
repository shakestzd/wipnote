//go:build windows

package hooks

import "syscall"

// detachAttr has no process-group setting on Windows; the generator still
// runs on after the hook exits.
func detachAttr() *syscall.SysProcAttr { return nil }
