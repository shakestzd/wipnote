package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// tmuxWrapAction describes what maybeTmuxWrap should do.
type tmuxWrapAction int

const (
	tmuxActionSkip  tmuxWrapAction = iota // already inside tmux — proceed normally
	tmuxActionError                       // tmux flag set but tmux binary missing
	tmuxActionExec                        // re-exec under tmux
)

// decideTmuxWrap returns the action to take given the current environment.
// Extracted as a pure function so it can be unit-tested without side effects.
//
// Parameters:
//   - tmuxFlag:    whether --tmux was passed by the user
//   - tmuxEnv:     value of the TMUX environment variable (empty = not in tmux)
//   - tmuxOnPath:  whether the tmux binary was found on PATH
func decideTmuxWrap(tmuxFlag bool, tmuxEnv string, tmuxOnPath bool) tmuxWrapAction {
	if !tmuxFlag {
		return tmuxActionSkip
	}
	if tmuxEnv != "" {
		// Already inside a tmux session — no need to wrap again.
		return tmuxActionSkip
	}
	if !tmuxOnPath {
		return tmuxActionError
	}
	return tmuxActionExec
}

// stripTmuxFlag returns a copy of args with any --tmux or --tmux=... element removed.
func stripTmuxFlag(args []string) []string {
	result := make([]string, 0, len(args))
	for _, a := range args {
		if a == "--tmux" || strings.HasPrefix(a, "--tmux=") {
			continue
		}
		result = append(result, a)
	}
	return result
}

// maybeTmuxWrap checks whether the current process should be re-executed inside
// a tmux session. It must be called early in a command's Run function —
// before any side-effecting work — because if it decides to exec, the current
// process is replaced entirely and nothing after the call runs.
//
// sessionName is the tmux session name to attach to or create (e.g. "wipnote-yolo"
// or "wipnote-dev"). Different commands pass different names so their sessions
// are independent and can survive in parallel.
//
// When --tmux is not set, or we are already inside tmux, the function is a no-op
// and returns nil so the caller can continue normally.
//
// When tmux is required but missing from PATH, an error is returned.
//
// When re-exec is needed, this function does not return (on Unix) — it replaces
// the process via syscall.Exec.
func maybeTmuxWrap(sessionName string) error {
	// Determine whether --tmux flag was present via os.Args inspection.
	tmuxFlag := false
	for _, a := range os.Args {
		if a == "--tmux" || strings.HasPrefix(a, "--tmux=") {
			tmuxFlag = true
			break
		}
	}

	tmuxEnv := os.Getenv("TMUX")
	_, lookErr := exec.LookPath("tmux")
	tmuxOnPath := lookErr == nil

	action := decideTmuxWrap(tmuxFlag, tmuxEnv, tmuxOnPath)

	switch action {
	case tmuxActionSkip:
		return nil

	case tmuxActionError:
		return fmt.Errorf(
			"tmux not found on PATH. Install with 'apt install tmux' (Debian/Ubuntu) or 'brew install tmux' (macOS), then retry.",
		)

	case tmuxActionExec:
		tmuxPath, _ := exec.LookPath("tmux")
		// Rebuild argv: replace the binary name with the tmux invocation, and
		// strip --tmux so the re-exec doesn't loop.
		childArgv := stripTmuxFlag(os.Args)

		// tmux new-session -A -s <name> -- <argv>
		tmuxArgv := []string{tmuxPath, "new-session", "-A", "-s", sessionName, "--"}
		tmuxArgv = append(tmuxArgv, childArgv...)

		// Replace the current process with tmux. On Unix this never returns.
		if err := syscall.Exec(tmuxPath, tmuxArgv, os.Environ()); err != nil {
			// Fallback for platforms where syscall.Exec is unavailable or fails.
			cmd := exec.Command(tmuxArgv[0], tmuxArgv[1:]...)
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if runErr := cmd.Run(); runErr != nil {
				os.Exit(1)
			}
			os.Exit(0)
		}
	}

	return nil
}
