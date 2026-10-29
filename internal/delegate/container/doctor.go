package container

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/internal/delegate/codex"
)

// DoctorOptions are what the container checks need: the project root, and a
// probe run (an id and its run folder on the host, made and removed by the
// caller) for the hook check.
type DoctorOptions struct {
	Root        string
	ProbeRun    string
	ProbeDir    string
	HostVersion string    // the host wipnote version, for the inside check
	Progress    io.Writer // devcontainer up's progress; discarded when nil
}

// insideTimeout bounds one check run inside the container.
const insideTimeout = time.Minute

// Doctor checks what a --container worker needs, inside the project's
// devcontainer: the config and the CLI, devcontainer up, wipnote inside, a
// wipnote hook firing inside and its file reaching the host, the container
// user's Claude config folder being writable, and Claude Code being logged
// in. Each failure names its fix; a check that cannot run after an earlier
// failure is skipped.
func Doctor(ctx context.Context, o DoctorOptions) []codex.Check {
	if err := Check(o.Root); err != nil {
		return []codex.Check{{Name: "devcontainer", Status: codex.CheckFail, Detail: err.Error()}}
	}
	checks := []codex.Check{{Name: "devcontainer", Status: codex.CheckPass, Detail: ConfigPath + " and the " + CLI + " CLI are there"}}
	up, err := RunUp(ctx, o.Root, o.Progress)
	if err != nil {
		return append(checks, codex.Check{Name: "devcontainer up", Status: codex.CheckFail, Detail: err.Error()})
	}
	checks = append(checks, codex.Check{Name: "devcontainer up", Status: codex.CheckPass,
		Detail: fmt.Sprintf("container %s, workspace folder %s, user %s", up.ContainerID, up.RemoteWorkspaceFolder, up.RemoteUser)})
	w := wipnoteInside(ctx, o.Root, o.HostVersion)
	checks = append(checks, w)
	if w.Status == codex.CheckPass {
		checks = append(checks, hookInside(ctx, o, up.RemoteWorkspaceFolder))
	} else {
		checks = append(checks, codex.Check{Name: "hook inside", Status: codex.CheckSkip, Detail: "needs wipnote inside"})
	}
	writable := claudeWritable(ctx, o.Root)
	return append(checks, writable, claudeLogin(ctx, o.Root, writable.Status == codex.CheckPass))
}

// inside runs argv in the container with env, bounded, and returns its
// combined output.
func inside(ctx context.Context, root string, env, argv []string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, insideTimeout)
	defer cancel()
	full := ExecArgv(root, env, argv)
	cmd := exec.CommandContext(ctx, full[0], full[1:]...)
	cmd.Dir = root
	cmd.WaitDelay = time.Second
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	return strings.TrimSpace(out.String()), err
}

func wipnoteInside(ctx context.Context, root, host string) codex.Check {
	c := codex.Check{Name: "wipnote inside"}
	out, err := inside(ctx, root, nil, []string{"wipnote", "version"})
	if err != nil {
		c.Status, c.Detail = codex.CheckFail, fmt.Sprintf("wipnote version failed inside the container (%v): %s; install wipnote in the container (its ~/.local volume keeps it)", err, out)
		return c
	}
	return versionCheck(root, out, host)
}

// probeSession is the session id the hook check fires its hook as.
const probeSession = "wipnote-doctor-probe"

// hookInside fires one wipnote hook inside as a container worker of the
// probe run, and checks that its own events file reached the host's run
// folder through the workspace mount.
func hookInside(ctx context.Context, o DoctorOptions, remote string) codex.Check {
	c := codex.Check{Name: "hook inside"}
	payload, _ := json.Marshal(map[string]string{"session_id": probeSession, "hook_event_name": "UserPromptSubmit", "prompt": "doctor", "cwd": remote})
	script := `printf '%s' "$1" | wipnote hook user-prompt --project-dir "$2" --harness claude`
	out, err := inside(ctx, o.Root, WorkerEnv(o.ProbeRun, ""), []string{"sh", "-c", script, "sh", string(payload), remote})
	if err != nil {
		c.Status, c.Detail = codex.CheckFail, fmt.Sprintf("wipnote hook user-prompt failed inside (%v): %s", err, out)
		return c
	}
	events := filepath.Join(o.ProbeDir, "events-"+probeSession+".ndjson")
	if fi, err := os.Stat(events); err != nil || fi.Size() == 0 {
		c.Status, c.Detail = codex.CheckFail, "the hook ran inside but its events file did not reach the host's .wipnote/delegate/ through the workspace mount"
		return c
	}
	c.Status, c.Detail = codex.CheckPass, "a hook fired inside and its events file reached the host's .wipnote/delegate/"
	return c
}

// writableScript proves the container user can create folders and files
// in its Claude config folder (Phase B B6: EACCES on mkdir plugins).
const writableScript = `d="$HOME/.claude" && mkdir -p "$d/plugins" && t="$d/.wipnote-doctor-$$" && : > "$t" && rm -f "$t" && echo "$d"`

func claudeWritable(ctx context.Context, root string) codex.Check {
	c := codex.Check{Name: "claude config writable"}
	out, err := inside(ctx, root, nil, []string{"sh", "-c", writableScript})
	if err != nil {
		c.Status, c.Detail = codex.CheckFail, fmt.Sprintf("the container user cannot write ~/.claude (%v): %s; fix the named volume's owner inside the container: sudo chown -R \"$(id -u):$(id -g)\" ~/.claude", err, out)
		return c
	}
	c.Status, c.Detail = codex.CheckPass, firstLine(out)+" is writable"
	return c
}

func claudeLogin(ctx context.Context, root string, writable bool) codex.Check {
	c := codex.Check{Name: "claude login"}
	if !writable {
		c.Status, c.Detail = codex.CheckSkip, "needs a writable ~/.claude first"
		return c
	}
	out, err := inside(ctx, root, nil, []string{"claude", "auth", "status"})
	if err != nil {
		c.Status, c.Detail = codex.CheckFail, fmt.Sprintf("Claude Code is not logged in inside the container (claude auth status: %v): %s; log in once inside: open a shell in the devcontainer, start claude and type /login (the named ~/.claude volume keeps the login)", err, out)
		return c
	}
	return loginResult(c, out)
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}
