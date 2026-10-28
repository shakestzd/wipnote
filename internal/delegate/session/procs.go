package session

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// psTimeout bounds one ps call.
const psTimeout = 2 * time.Second

// psStart reports a process's start time as ps prints it (lstart works on
// macOS and Linux procps). ok is false when no such process runs or ps
// fails: stop then reports no survivor for it rather than guessing.
func psStart(pid int) (string, bool) {
	if pid <= 0 {
		return "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), psTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ps", "-o", "lstart=", "-p", strconv.Itoa(pid)).Output()
	start := strings.TrimSpace(string(out))
	if err != nil || start == "" {
		return "", false
	}
	return start, true
}
