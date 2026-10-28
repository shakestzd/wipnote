//go:build darwin

package procsweep

import (
	"context"
	"os"
	"os/exec"
	"strconv"
)

// DefaultLister returns the macOS lister: two ps runs over the current
// user's processes. `ps -E` displays the environment as well, as it was at
// launch (ps(1)). This path is not verified on a Mac in CI; when ps shows no
// environment for our own process it reports ErrEnvUnreadable instead of
// guessing.
func DefaultLister() Lister { return psLister{uid: os.Getuid()} }

type psLister struct{ uid int }

// List runs ps twice. It takes no context into exec on purpose: exec kills a
// child when its context ends, and this package never signals. The caller's
// time bound (listBounded) abandons a slow ps, which then exits on its own.
func (l psLister) List(context.Context) ([]RawProc, error) {
	user := strconv.Itoa(l.uid)
	info, err := runPS("-x", "-ww", "-U", user, "-o", "pid=,ppid=,uid=,lstart=,command=")
	if err != nil {
		return nil, err
	}
	env, err := runPS("-E", "-x", "-ww", "-U", user, "-o", "pid=,command=")
	if err != nil {
		return nil, err
	}
	return parsePS(info, env, l.uid, os.Getpid())
}

func runPS(args ...string) (string, error) {
	cmd := exec.Command("ps", args...)
	// lstart is locale-formatted; the C locale keeps it parseable.
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		return "", psError(args, err)
	}
	return string(out), nil
}
