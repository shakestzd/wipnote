//go:build !windows

package hooks

import (
	"os"
	"syscall"
)

func statIno(fi os.FileInfo) uint64 {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Ino)
	}
	return 0
}
