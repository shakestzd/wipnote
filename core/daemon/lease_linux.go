//go:build linux

package daemon

import (
	"os"
	"strconv"
	"strings"
	"sync"
)

var (
	nsOnce        sync.Once
	nsBoot, nsPID string
)

// localNamespace returns this process's boot id and pid-namespace id. The
// pair names one pid space: containers on one kernel share the boot id but
// not the namespace, and every kernel's initial namespace has the same inode,
// so neither half is unique alone.
func localNamespace() (bootID, pidNS string) {
	nsOnce.Do(func() {
		if b, err := os.ReadFile("/proc/sys/kernel/random/boot_id"); err == nil {
			nsBoot = strings.TrimSpace(string(b))
		}
		if l, err := os.Readlink("/proc/self/ns/pid"); err == nil {
			nsPID = l
		}
	})
	return nsBoot, nsPID
}

// legacyWriterAlive reports whether the live pid runs a wipnote writer
// (`_serve-child` in its argv). It guards a legacy writer.pid against pid
// reuse: an unrelated process that inherited the pid is not an owner.
func legacyWriterAlive(pid int) bool {
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
	if err != nil {
		return false
	}
	for _, arg := range strings.Split(string(raw), "\x00") {
		if arg == "_serve-child" {
			return true
		}
	}
	return false
}
