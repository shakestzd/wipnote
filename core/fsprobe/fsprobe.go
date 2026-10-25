// Package fsprobe is a test seam on the canonical-file paths a SessionEnd hook
// touches. Production leaves Hook nil, so each call costs one nil check.
//
// Tests install a Hook to (a) add latency per filesystem operation, which
// models a slow bind mount, and (b) record the order of operations, which pins
// what may happen before the session-end note is durable.
package fsprobe

import "sync/atomic"

// Op names a filesystem operation class.
type Op string

const (
	OpRead  Op = "read"  // open or stat for reading (shard, ledger, directory scan)
	OpWrite Op = "write" // create, write, sync or rename
)

var hook atomic.Pointer[func(op Op, path string)]

// Set installs fn as the probe, or removes it when fn is nil.
func Set(fn func(op Op, path string)) {
	if fn == nil {
		hook.Store(nil)
		return
	}
	hook.Store(&fn)
}

// Touch reports one filesystem operation to the installed probe, if any.
func Touch(op Op, path string) {
	if fn := hook.Load(); fn != nil {
		(*fn)(op, path)
	}
}
