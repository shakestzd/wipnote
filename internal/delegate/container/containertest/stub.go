// Package containertest is a stub devcontainer CLI for tests (plan-28b4a7f6
// slice 21): a shell script put first on PATH that logs every call, answers
// `up` with the JSON the real CLI prints, and runs `exec`'s command on the
// host with the --remote-env values exported. No Docker is involved.
package containertest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const script = `#!/bin/sh
{ for a in "$@"; do printf '%s\n' "$a"; done; echo "--END--"; } >> "$STUB_LOG"
case "$1" in
up)
  echo "stub: building the dev container" >&2
  if [ -n "$STUB_UP_FAIL" ]; then echo '{"outcome":"error","message":"stub up failed"}'; exit 1; fi
  echo '{"outcome":"success","containerId":"c0ffee","remoteUser":"vscode","remoteWorkspaceFolder":"'"$STUB_REMOTE"'"}'
  ;;
exec)
  shift
  while [ $# -gt 0 ]; do
    case "$1" in
      --workspace-folder) shift 2 ;;
      --remote-env) export "$2"; shift 2 ;;
      *) break ;;
    esac
  done
  case "$3" in *wipnote-pids*) cat "$STUB_PIDS" 2>/dev/null; exit "${STUB_PIDS_EXIT:-0}" ;; esac
  exec "$@"
  ;;
*) echo "stub: unknown $1" >&2; exit 2 ;;
esac
`

// Stub is one installed stub CLI.
type Stub struct {
	Dir    string // the folder on PATH holding devcontainer
	Log    string // every call's argv
	Remote string // remoteWorkspaceFolder up reports
	Pids   string // the file the pid probe prints
}

// Install puts the stub first on PATH. remote is the workspace folder `up`
// reports; a symlink to the project root lets mapped paths work on the host.
func Install(t testing.TB, remote string) *Stub {
	t.Helper()
	dir := t.TempDir()
	s := &Stub{Dir: dir, Log: filepath.Join(dir, "calls.log"), Remote: remote, Pids: filepath.Join(dir, "pids.txt")}
	if err := os.WriteFile(filepath.Join(dir, "devcontainer"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("STUB_LOG", s.Log)
	t.Setenv("STUB_REMOTE", remote)
	t.Setenv("STUB_PIDS", s.Pids)
	t.Setenv("STUB_UP_FAIL", "")
	return s
}

// Calls returns every logged call's argv, oldest first.
func (s *Stub) Calls(t testing.TB) [][]string {
	t.Helper()
	data, err := os.ReadFile(s.Log)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out [][]string
	var cur []string
	for _, line := range strings.Split(string(data), "\n") {
		if line == "--END--" {
			out = append(out, cur)
			cur = nil
			continue
		}
		cur = append(cur, line)
	}
	return out
}

// SetPids sets what the pid probe prints, one "pid start name" per line.
func (s *Stub) SetPids(t testing.TB, lines ...string) {
	t.Helper()
	if err := os.WriteFile(s.Pids, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Project is a temp project root with .devcontainer/devcontainer.json and a
// remote workspace folder that is a symlink to it.
func Project(t testing.TB) (root, remote string) {
	t.Helper()
	base := t.TempDir()
	root = filepath.Join(base, "proj")
	if err := os.MkdirAll(filepath.Join(root, ".devcontainer"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".devcontainer", "devcontainer.json"), []byte(`{"name":"t","image":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	remote = filepath.Join(base, "workspaces-proj")
	if err := os.Symlink(root, remote); err != nil {
		t.Fatal(err)
	}
	return root, remote
}
