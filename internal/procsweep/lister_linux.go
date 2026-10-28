//go:build linux

package procsweep

import "os"

// DefaultLister returns the platform's process lister: on Linux, /proc
// filtered to the current user.
func DefaultLister() Lister { return procfsLister{root: "/proc", uid: os.Getuid()} }
