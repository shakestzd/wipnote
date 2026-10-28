package container

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Mapper maps host paths under the project root to the container's
// workspace folder (up's remoteWorkspaceFolder).
type Mapper struct{ Root, Remote string }

// Path maps an absolute host path under Root. Anything outside the
// workspace has no place in the container and is refused.
func (m Mapper) Path(host string) (string, error) {
	if !filepath.IsAbs(host) {
		return "", fmt.Errorf("delegate: %q is not an absolute path", host)
	}
	rel, err := filepath.Rel(filepath.Clean(m.Root), filepath.Clean(host))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("delegate: %s is outside the workspace %s, so it is not in the container", host, m.Root)
	}
	if rel == "." {
		return m.Remote, nil
	}
	return path.Join(m.Remote, filepath.ToSlash(rel)), nil
}

// pathFlags take a path value in the next word or after "=". Their value is
// mapped even when it holds a space (a checkout under "My Project").
var pathFlags = map[string]bool{
	"-o": true, "--output-last-message": true, "-C": true, "--cd": true, "--add-dir": true,
	"-i": true, "--image": true, "--mcp-config": true, "--settings": true, "--plugin-dir": true,
}

// Argv maps the path arguments in argv: a word equal to one of known (paths
// the caller put there, such as the run folder's answer file), the value of
// a path flag, --flag=PATH, and any other word that is a host path under
// Root and holds no whitespace. Prompt text is left as given, even when it
// starts with a path.
func (m Mapper) Argv(argv []string, known ...string) []string {
	out := make([]string, len(argv))
	for i, w := range argv {
		pathValue := i > 0 && pathFlags[argv[i-1]]
		out[i] = m.word(w, pathValue || slices.Contains(known, w))
	}
	return out
}

// word maps w; isPath says it is known to be a path, whitespace or not.
func (m Mapper) word(w string, isPath bool) string {
	if isPath || !strings.ContainsAny(w, " \t\n") {
		if mapped, err := m.Path(w); err == nil {
			return mapped
		}
	}
	if flag, val, ok := strings.Cut(w, "="); ok && strings.HasPrefix(flag, "-") &&
		(pathFlags[flag] || !strings.ContainsAny(val, " \t\n")) {
		if mapped, err := m.Path(val); err == nil {
			return flag + "=" + mapped
		}
	}
	return w
}

// Proc is one worker process inside the container: its pid and its start
// (clock ticks since boot, from /proc/<pid>/stat), so a reused pid is not
// taken for the worker.
type Proc struct {
	PID   int    `json:"pid"`
	Start string `json:"start"`
	Name  string `json:"name"`
}

// pidsScript lists the container processes whose environment holds the
// worker's run id. The marker comment names it in logs and tests.
const pidsScript = `# wipnote-pids
for d in /proc/[0-9]*; do
  if { tr '\0' '\n' < "$d/environ"; } 2>/dev/null | grep -qx "WIPNOTE_DELEGATE_RUN=$1"; then
    st=$(sed 's/.*) //' "$d/stat" 2>/dev/null | cut -d' ' -f20)
    echo "${d#/proc/} $st $(cat "$d/comm" 2>/dev/null)"
  fi
done`

var runIDRe = regexp.MustCompile(`^dlg-[0-9a-f]{12,64}$`)

// PidsArgv is the probe: a devcontainer exec that reads /proc inside. It
// reads only; it never signals.
func PidsArgv(root, run string) []string {
	return ExecArgv(root, nil, []string{"sh", "-c", pidsScript, "sh", run})
}

// pidsTimeout bounds one probe.
const pidsTimeout = 15 * time.Second

// Pids lists the worker's processes inside the container. A failed probe
// is an error, never an empty list.
func Pids(ctx context.Context, root, run string) ([]Proc, error) {
	if !runIDRe.MatchString(run) {
		return nil, fmt.Errorf("delegate: %q is not a run id", run)
	}
	ctx, cancel := context.WithTimeout(ctx, pidsTimeout)
	defer cancel()
	argv := PidsArgv(root, run)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("delegate: the container pid probe failed: %w", err)
	}
	return parsePids(string(out)), nil
}

func parsePids(out string) []Proc {
	var procs []Proc
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 2 {
			continue
		}
		pid, err := strconv.Atoi(f[0])
		if err != nil || pid <= 0 {
			continue
		}
		p := Proc{PID: pid, Start: f[1]}
		if len(f) > 2 {
			p.Name = strings.Join(f[2:], " ")
		}
		procs = append(procs, p)
	}
	return procs
}
