package session

import (
	"context"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/core/hooks"
	"github.com/shakestzd/wipnote/internal/delegate/herdr"
)

func PaneOf(s Spawned) herdr.PaneID { return herdr.PaneID(s.Pane) }

func TestShellQuote_RoundTrip(t *testing.T) {
	words := []string{"", "plain", "with space", "it's", `"dq"`, "$HOME", "`id`", "a\\b", "semi;colon", "new\nline", "ünï", "!bang", "*glob?"}
	var line []string
	for _, w := range words {
		line = append(line, Quote(w))
	}
	script := "set -- " + strings.Join(line, " ") + `; for a in "$@"; do printf '%s\0' "$a"; done`
	out, err := exec.Command("sh", "-c", script).Output()
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	if !slices.Equal(got, words) {
		t.Fatalf("round trip\n got %q\nwant %q", got, words)
	}
	// Other side: an unquoted word is split and expanded by the shell.
	out, _ = exec.Command("sh", "-c", `printf '%s\0' with space $HOME`).Output()
	if strings.Count(string(out), "\x00") == 1 {
		t.Fatal("the shell did not split an unquoted word; the round trip proves nothing")
	}
}

func TestSpawn_ScrubsInheritedSessionEnv(t *testing.T) {
	inherited := []string{
		"PATH=/usr/bin", "HOME=/h", "WIPNOTE_PARENT_SESSION=sess-caller", "WIPNOTE_PARENT_EVENT=ev-1",
		"WIPNOTE_NESTING_DEPTH=1", "WIPNOTE_SESSION_ID=sess-caller", "WIPNOTE_AGENT_ID=agent-x",
		"WIPNOTE_SESSION_FAMILY_ID=fam", RunEnv + "=dlg-000000000000", CallerEnv + "=old", "WIPNOTE_OTEL_ENABLED=0",
	}
	env := BootEnv(inherited, "dlg-0123456789ab", "sess-caller")
	for _, kv := range inherited {
		k, _, _ := strings.Cut(kv, "=")
		if slices.Contains(ScrubbedEnv, k) && slices.Contains(env, kv) {
			t.Fatalf("inherited %s kept: %v", kv, env)
		}
	}
	for _, want := range []string{"PATH=/usr/bin", "HOME=/h", "WIPNOTE_OTEL_ENABLED=0", RunEnv + "=dlg-0123456789ab", CallerEnv + "=sess-caller"} {
		if !slices.Contains(env, want) {
			t.Fatalf("missing %s: %v", want, env)
		}
	}
	// Other side: with no caller the caller variable is absent, not empty.
	for _, kv := range BootEnv(inherited, "dlg-0123456789ab", "") {
		if strings.HasPrefix(kv, CallerEnv+"=") {
			t.Fatalf("empty caller exported: %v", kv)
		}
	}
}

func TestSpawn_WorkerShardIsOwnRoot(t *testing.T) {
	info := Info{Run: "dlg-0123456789ab", Name: "w1", Harness: HarnessClaude}
	argv := BootArgv("/bin/wipnote", info, "the brief")
	want := []string{"/bin/wipnote", "claude", "--in-place", "--name", "w1", "--", "the brief"}
	if !slices.Equal(argv, want) {
		t.Fatalf("argv %q", argv)
	}
	info.Harness = HarnessCodex
	if argv := BootArgv("/bin/wipnote", info, ""); !slices.Equal(argv, []string{"/bin/wipnote", "codex", "--in-place"}) {
		t.Fatalf("argv %q", argv)
	}
	// No --settings and no parent session: the launcher makes a root session.
	env := BootEnv([]string{"WIPNOTE_PARENT_SESSION=p", "WIPNOTE_NESTING_DEPTH=2"}, info.Run, "p")
	for _, kv := range env {
		if strings.HasPrefix(kv, "WIPNOTE_PARENT_SESSION=") || strings.HasPrefix(kv, "WIPNOTE_NESTING_DEPTH=") {
			t.Fatalf("worker inherits %s", kv)
		}
	}
	if slices.Contains(argv, "--settings") {
		t.Fatal("--settings passed")
	}
}

// bindHook runs the real SessionStart bind writer as the worker's hook would.
func bindHook(t *testing.T, e *env, run, sid string) {
	t.Helper()
	t.Setenv(RunEnv, run)
	hooks.RecordDelegateEvent(e.dir, hooks.DelegateEvent{Kind: "session_start", SID: sid})
	os.Unsetenv(RunEnv)
}

func TestSpawn_BindsSessionOnStart(t *testing.T) {
	e := newEnv(t)
	s := e.spawn("w1")
	rows, err := List(context.Background(), e.deps, false)
	if err != nil || len(rows) != 1 || rows[0].State != StateStarting {
		t.Fatalf("before bind: %+v err %v", rows, err)
	}
	bindHook(t, e, s.Run, "sid-one")
	rows, err = List(context.Background(), e.deps, false)
	if err != nil || len(rows) != 1 || rows[0].State != StateReady || !slices.Equal(rows[0].Sessions, []string{"sid-one"}) {
		t.Fatalf("after bind: %+v err %v", rows, err)
	}
	if r := e.row(s.Run); !slices.Equal(r.WorkerSessions, []string{"sid-one"}) {
		t.Fatalf("ledger not enriched: %+v", r)
	}
}

func TestSpawn_ClearAddsSecondSession(t *testing.T) {
	e := newEnv(t)
	s := e.spawn("w1")
	bindHook(t, e, s.Run, "sid-one")
	e.event(s.Run, "sid-one", "prompt")
	e.event(s.Run, "sid-one", "stop")
	bindHook(t, e, s.Run, "sid-two") // /clear: a new session in the same pane
	rows, err := List(context.Background(), e.deps, false)
	if err != nil || len(rows) != 1 || !slices.Equal(rows[0].Sessions, []string{"sid-one", "sid-two"}) {
		t.Fatalf("rows %+v err %v", rows, err)
	}
	if r := e.row(s.Run); !slices.Equal(r.WorkerSessions, []string{"sid-one", "sid-two"}) {
		t.Fatalf("ledger %+v", r)
	}
	// The new session has run no turn yet: it is ready, not done.
	if rows[0].State != StateReady {
		t.Fatalf("state %s", rows[0].State)
	}
}

func TestList_RenamesAndPinsLateAgent(t *testing.T) {
	e := newEnv(t)
	s := e.spawn("w1")
	if _, err := List(context.Background(), e.deps, false); err != nil {
		t.Fatal(err)
	}
	if hasCall(e.herdr.Calls(), "agent rename") {
		t.Fatal("renamed before the agent appeared")
	}
	e.herdr.agents = []herdr.Agent{{Pane: PaneOf(s), TerminalID: "term-x", Name: "claude", Kind: "claude"}}
	if _, err := List(context.Background(), e.deps, false); err != nil {
		t.Fatal(err)
	}
	if !hasCall(e.herdr.Calls(), "agent rename "+s.Pane+" term-x w1") {
		t.Fatalf("calls %v", e.herdr.Calls())
	}
	info, _ := ReadInfo(e.runFolder(s.Run))
	if info.TerminalID != "term-x" || !info.Renamed {
		t.Fatalf("not pinned: %+v", info)
	}
	before := strings.Count(strings.Join(e.herdr.Calls(), "\n"), "agent rename")
	if _, err := List(context.Background(), e.deps, false); err != nil {
		t.Fatal(err)
	}
	if after := strings.Count(strings.Join(e.herdr.Calls(), "\n"), "agent rename"); after != before {
		t.Fatal("renamed twice")
	}
}

func TestSpawn_TrustDialogReportedBlockedNoKeys(t *testing.T) {
	e := newEnv(t)
	s := e.spawn("w1")
	e.herdr.pane(PaneOf(s)).screen = "Do you trust the files in this folder?\n❯ No, exit\n  Yes, I trust this folder\n"
	rows, err := List(context.Background(), e.deps, false)
	if err != nil || rows[0].State != StateBlocked || !strings.Contains(rows[0].Detail, "Yes, I trust this folder") {
		t.Fatalf("rows %+v err %v", rows, err)
	}
	for _, c := range e.herdr.Calls() {
		if strings.HasPrefix(c, "send-") {
			t.Fatalf("a key was pressed on the trust dialog: %s", c)
		}
	}
}
