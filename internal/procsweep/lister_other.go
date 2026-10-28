//go:build !linux && !darwin

package procsweep

import "context"

// DefaultLister returns a lister that reports ErrEnvUnreadable: this platform
// has no process-environment reader, and guessing would be silent.
func DefaultLister() Lister {
	return ListerFunc(func(context.Context) ([]RawProc, error) { return nil, ErrEnvUnreadable })
}
