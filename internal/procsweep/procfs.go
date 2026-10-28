//go:build unix

package procsweep

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// userHZ is the kernel's USER_HZ, the unit of /proc/<pid>/stat starttime. It
// is 100 on every Linux architecture Go supports; reading sysconf(_SC_CLK_TCK)
// would need cgo.
const userHZ = 100

// procfsLister reads a Linux-style /proc. /proc/<pid>/environ holds the
// environment the program was started with (execve), and reading it is gated
// by a ptrace read check, so only processes we could inspect anyway are seen
// (https://man7.org/linux/man-pages/man5/proc_pid_environ.5.html).
type procfsLister struct {
	root string
	uid  int
}

// List returns the uid's processes. It fails with ErrEnvUnreadable when even
// our own environ cannot be read, since then no process could be matched.
func (l procfsLister) List(ctx context.Context) ([]RawProc, error) {
	if _, err := os.ReadFile(filepath.Join(l.root, "self", "environ")); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEnvUnreadable, err)
	}
	boot, err := l.bootTime()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(l.root)
	if err != nil {
		return nil, err
	}
	var out []RawProc
	for _, e := range entries {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil || !e.IsDir() || !l.ownedByUser(e.Name()) {
			continue
		}
		if rp, ok := l.read(pid, boot); ok {
			out = append(out, rp)
		}
	}
	return out, nil
}

// ownedByUser reports whether /proc/<pid> belongs to the lister's uid.
func (l procfsLister) ownedByUser(name string) bool {
	fi, err := os.Stat(filepath.Join(l.root, name))
	if err != nil {
		return false
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == l.uid
}

// read loads one process. A process that exits mid-read is skipped.
func (l procfsLister) read(pid int, boot time.Time) (RawProc, bool) {
	dir := filepath.Join(l.root, strconv.Itoa(pid))
	env, err := os.ReadFile(filepath.Join(dir, "environ"))
	if err != nil {
		return RawProc{}, false
	}
	stat, err := os.ReadFile(filepath.Join(dir, "stat"))
	if err != nil {
		return RawProc{}, false
	}
	ppid, ticks, ok := parseStat(string(stat))
	if !ok {
		return RawProc{}, false
	}
	cmdline, _ := os.ReadFile(filepath.Join(dir, "cmdline"))
	return RawProc{
		PID:       pid,
		PPID:      ppid,
		Command:   strings.TrimSpace(string(bytes.ReplaceAll(cmdline, []byte{0}, []byte{' '}))),
		StartedAt: boot.Add(time.Duration(ticks) * time.Second / userHZ),
		Env:       keepSessionEnv(strings.Split(string(env), "\x00")),
	}, true
}

// parseStat extracts ppid (field 4) and starttime (field 22) from
// /proc/<pid>/stat. The comm field may hold spaces and parentheses, so fields
// are counted from the last ')'.
func parseStat(s string) (ppid int, ticks int64, ok bool) {
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return 0, 0, false
	}
	f := strings.Fields(s[i+1:])
	if len(f) < 20 {
		return 0, 0, false
	}
	ppid, err1 := strconv.Atoi(f[1])
	ticks, err2 := strconv.ParseInt(f[19], 10, 64)
	return ppid, ticks, err1 == nil && err2 == nil
}

// bootTime reads the btime line of /proc/stat.
func (l procfsLister) bootTime() (time.Time, error) {
	b, err := os.ReadFile(filepath.Join(l.root, "stat"))
	if err != nil {
		return time.Time{}, err
	}
	for _, line := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(line, "btime "); ok {
			sec, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			if err != nil {
				return time.Time{}, err
			}
			return time.Unix(sec, 0), nil
		}
	}
	return time.Time{}, fmt.Errorf("procsweep: no btime in %s", filepath.Join(l.root, "stat"))
}
