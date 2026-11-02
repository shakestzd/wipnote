package detect

import (
	"strings"
	"testing"
)

func TestRedirect_AgentLaunchThroughHerdrNamesSpawn(t *testing.T) {
	cases := map[string]string{
		`hc run --bg --name fix --cwd /repo claude "Fix x"`:          "claude",
		`hc run --bg codex "review src/x.rs"`:                        "codex",
		`hc run --bg --label a=b -- codex --no-daemon`:               "codex",
		`herdr-core run --bg claude`:                                 "claude",
		`/usr/local/bin/hc run --bg claude`:                          "claude",
		`hc run --bg "claude --model opus"`:                          "claude",
		`FOO=1 hc run --bg claude`:                                   "claude",
		`hc run --bg /opt/bin/codex`:                                 "codex",
		`herdr agent start reviewer --kind codex --pane w1:p2`:       "codex",
		`herdr agent start reviewer --kind=claude --pane w1:p2 -- x`: "claude",
	}
	for cmd, harness := range cases {
		got := Redirect(cmd)
		if !strings.Contains(got, "wipnote delegate spawn --harness "+harness) {
			t.Errorf("Redirect(%q) = %q, want the spawn form for %s", cmd, got, harness)
		}
	}
}

func TestRedirect_OtherHerdrCommandsRun(t *testing.T) {
	cases := []string{
		`hc run --bg --name web bun dev`,
		`hc run cargo test`,
		`hc run reviewer "ask claude nothing"`,
		`hc run --name claude bun dev`,
		`hc read claude`,
		`hc send codex "hello"`,
		`herdr agent start helper --kind pi --pane w1:p2`,
		`herdr agent start helper --kind pi --pane w1:p2 -- --kind codex`,
		`herdr agent get codex`,
		`echo hc run claude`,
		`hc run --bg claude; echo started`,
		`wipnote delegate spawn --harness claude --brief "hc run claude"`,
	}
	for _, cmd := range cases {
		if got := Redirect(cmd); got != "" {
			t.Errorf("Redirect(%q) = %q, want it to run as typed", cmd, got)
		}
	}
}
