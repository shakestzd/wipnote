package container

import (
	"slices"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/internal/delegate/codex"
)

func TestVersionCheck(t *testing.T) {
	cases := []struct {
		name, out, host string
		fail            bool
		want            []string
	}{
		{"old inside (issue 306)", "wipnote 0.71.0 (go)", "0.85.0", true, []string{"wipnote 0.71.0 inside, 0.85.0 on the host: run devcontainer exec --workspace-folder /proj wipnote upgrade"}},
		{"same", "wipnote 0.85.0 (go)", "0.85.0", false, nil},
		{"newer inside", "wipnote 0.90.1 (go)\nUpdate available", "0.85.0", false, nil},
		{"older than host, above minimum", "wipnote 0.86.0 (go)", "0.88.2", true, []string{"0.86.0", "0.88.2", "wipnote upgrade"}},
		{"dev host, old inside", "wipnote 0.71.0 (go)", "dev", true, []string{"0.71.0", MinWorkerVersion}},
		{"dev host, recent inside", "wipnote 0.85.0 (go)", "dev", false, nil},
		{"dev build inside", "wipnote dev (go)", "0.85.0", false, nil},
	}
	for _, c := range cases {
		got := versionCheck("/proj", c.out, c.host)
		if (got.Status == codex.CheckFail) != c.fail {
			t.Errorf("%s: status %v (%s), fail=%v", c.name, got.Status, got.Detail, c.fail)
		}
		for _, w := range c.want {
			if !strings.Contains(got.Detail, w) {
				t.Errorf("%s: detail %q lacks %q", c.name, got.Detail, w)
			}
		}
	}
}

// The worker env reaches the container as --remote-env pairs (issue 306).
func TestExecArgv_CarriesRunAsRemoteEnv(t *testing.T) {
	argv := ExecArgv("/p", WorkerEnv("dlg-0123456789ab", ""), []string{"wipnote", "version"})
	for _, kv := range []string{"WIPNOTE_DELEGATE_RUN=dlg-0123456789ab", "WIPNOTE_DELEGATE_PLACE=container"} {
		i := slices.Index(argv, kv)
		if i < 1 || argv[i-1] != "--remote-env" {
			t.Errorf("argv %q: %s is not passed after --remote-env", argv, kv)
		}
	}
}
