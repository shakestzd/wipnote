//go:build windows

package hooks

import "os"

func statIno(os.FileInfo) uint64 { return 0 }
