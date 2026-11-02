package herdr

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// installHC puts a stub herdr-core on PATH. Replies are keyed by the first
// argument, and each call is logged one per line.
func installHC(t *testing.T, script string) (c *Core, log string) {
	t.Helper()
	dir := t.TempDir()
	log = filepath.Join(dir, "log")
	body := "#!/bin/sh\necho \"$*\" >> " + log + "\n" + script + "\n"
	if err := os.WriteFile(filepath.Join(dir, "herdr-core"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	return &Core{Env: Env{InHerdr: true, Core: true, CallerPane: "t1"}}, log
}

func calls(t *testing.T, log string) []string {
	t.Helper()
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

func TestEnvFrom_ChoosesTheBackend(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want Env
	}{
		{"outside", nil, Env{}},
		{"tui", map[string]string{"HERDR_ENV": "1", "HERDR_PANE_ID": "w1:p2"}, Env{InHerdr: true, CallerPane: "w1:p2"}},
		{"core", map[string]string{"HC_TERMINAL_ID": "t7"}, Env{InHerdr: true, Core: true, CallerPane: "t7"}},
		{"the tui keeps its behaviour when both are set", map[string]string{"HERDR_ENV": "1", "HERDR_PANE_ID": "w1:p2", "HC_TERMINAL_ID": "t7"}, Env{InHerdr: true, CallerPane: "w1:p2"}},
		{"a blank terminal id is not core", map[string]string{"HC_TERMINAL_ID": "  "}, Env{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := EnvFrom(func(k string) string { return tc.env[k] }); got != tc.want {
				t.Fatalf("EnvFrom = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestNewClient_MatchesEnv(t *testing.T) {
	if _, ok := NewClient(Env{InHerdr: true, Core: true}, 0).(*Core); !ok {
		t.Fatal("a core env must get the hc client")
	}
	if _, ok := NewClient(Env{InHerdr: true}, 0).(*CLI); !ok {
		t.Fatal("a tui env must get the herdr client")
	}
}

func TestCore_OutsideHerdrRunsNothing(t *testing.T) {
	c, log := installHC(t, "exit 0")
	c.Env = Env{}
	if _, err := c.Get(bg, "t1"); !errors.Is(err, ErrNotInHerdr) {
		t.Fatalf("err = %v, want ErrNotInHerdr", err)
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Fatal("hc must not run outside herdr")
	}
}

func TestCore_NoBinaryIsAbsent(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	c := &Core{Env: Env{InHerdr: true, Core: true}}
	if _, err := c.Get(bg, "t1"); !errors.Is(err, ErrAbsent) {
		t.Fatalf("err = %v, want ErrAbsent", err)
	}
}

func TestCore_GoneTerminalsAreAbsent(t *testing.T) {
	cases := map[string]string{
		"not found": `echo '{"error":{"kind":"not_found","message":"no terminal t9"}}' >&2; exit 3`,
		"exit 11":   `echo '{"error":{"kind":"finished","message":"finished"}}' >&2; exit 11`,
		"finished":  `echo '{"address":"t9","state":"finished"}'`,
	}
	for name, script := range cases {
		t.Run(name, func(t *testing.T) {
			c, _ := installHC(t, script)
			if _, err := c.Get(bg, "t9"); !IsAbsent(err) {
				t.Fatalf("err = %v, want absent", err)
			}
		})
	}
}

func TestCore_FailureIsNotAbsence(t *testing.T) {
	c, _ := installHC(t, `echo '{"error":{"kind":"blocked","message":"it is blocked"}}' >&2; exit 9`)
	_, err := c.Get(bg, "t1")
	if err == nil || IsAbsent(err) {
		t.Fatalf("err = %v: a blocked terminal must not read as gone", err)
	}
	var he *Error
	if !errors.As(err, &he) || he.Code != "blocked" {
		t.Fatalf("err = %#v, want code blocked", err)
	}
}

// The workers tab is one label pair: every worker carries the caller's space
// and a tab id that names the caller.
func TestCore_WorkersShareOneTabInTheCallersSpace(t *testing.T) {
	c, log := installHC(t, `case "$1" in
get) echo '{"address":"t1","state":"live","labels":{"herdr.space":"s3","herdr.tab":"tb9"}}';;
run) echo '{"address":"t8","state":"live"}';;
esac`)
	ws, err := c.CallerWorkspace(bg)
	if err != nil || ws != "s3" {
		t.Fatalf("CallerWorkspace = %q, %v", ws, err)
	}
	tab, pane, err := c.TabCreate(bg, ws, "workers", "/repo")
	if err != nil || pane != "t8" || tab != "wipnote-workers-t1" {
		t.Fatalf("TabCreate = %q %q %v", tab, pane, err)
	}
	got := calls(t, log)
	want := "run --bg --outlive-parent --no-notify --cwd /repo --label herdr.space=s3 --label herdr.tab=wipnote-workers-t1 --json"
	if got[len(got)-1] != want {
		t.Fatalf("run = %q\nwant  %q", got[len(got)-1], want)
	}
}

// Under bare hc the caller has no labels, so a worker gets none: upstream's
// manual says not to add window labels only to run a background worker.
func TestCore_BareCoreAddsNoWindowLabels(t *testing.T) {
	c, log := installHC(t, `case "$1" in
get) echo '{"address":"t1","state":"live","labels":{}}';;
run) echo '{"address":"t8","state":"live"}';;
esac`)
	if _, err := c.Split(bg, "", Right, "/repo"); err != nil {
		t.Fatal(err)
	}
	got := calls(t, log)
	if strings.Contains(got[len(got)-1], "--label") {
		t.Fatalf("bare core run has labels: %q", got[len(got)-1])
	}
}

func TestCore_ProcessesSeesABareShell(t *testing.T) {
	c, _ := installHC(t, `echo '{"address":"t2","state":"live","pid":40,"foreground":{"pid":40,"name":"zsh","cwd":"/r"}}'`)
	pi, err := c.Processes(bg, "t2")
	if err != nil {
		t.Fatal(err)
	}
	want := ProcessInfo{Known: true, ShellPID: 40, Foreground: []Proc{{PID: 40, Name: "zsh"}}}
	if !reflect.DeepEqual(pi, want) {
		t.Fatalf("Processes = %+v, want %+v", pi, want)
	}
}

// Unknown is never "just a shell": without a pid or foreground, stop must not
// close the terminal.
func TestCore_ProcessesUnknownIsNotKnown(t *testing.T) {
	c, _ := installHC(t, `echo '{"address":"t2","state":"live","pid":null,"foreground":null}'`)
	pi, err := c.Processes(bg, "t2")
	if err != nil || pi.Known {
		t.Fatalf("Processes = %+v, %v; want unknown", pi, err)
	}
}

// The ls reply here is the real one from herdr-core 0.1.16: status is a plain
// word, and app, labels and foreground exist only on hc get.
func TestCore_AgentListKeepsAgentsOnly(t *testing.T) {
	c, _ := installHC(t, `case "$1 $2" in
"ls --json") echo '{"terminals":[{"address":"t1","state":"live","status":"working"},{"address":"t2","state":"live"},{"address":"t3","state":"live"},{"address":"t4","state":"live"}]}';;
"get t1") echo '{"address":"t1","state":"live","name":"lead","reported":true,"turn":4,"cwd":"/a","status":{"state":"working","app":"claude-code"}}';;
"get t2") echo '{"address":"t2","state":"live","reported":false,"cwd":"/b","foreground":{"pid":9,"name":"codex","cwd":"/b/sub"}}';;
"get t3") echo '{"address":"t3","state":"live","reported":false,"foreground":{"pid":8,"name":"zsh"}}';;
"get t4") exit 3;;
esac`)
	got, err := c.AgentList(bg)
	if err != nil {
		t.Fatal(err)
	}
	want := []Agent{
		{Pane: "t1", TerminalID: "t1", Name: "lead", Kind: "claude-code", State: StateWorking, CompletionSeq: 4, Cwd: "/a"},
		{Pane: "t2", TerminalID: "t2", Kind: "codex", State: StateUnknown, Cwd: "/b", ForegroundCwd: "/b/sub"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("AgentList = %+v\nwant %+v", got, want)
	}
}

func TestCore_PromptChecksThePinFirst(t *testing.T) {
	c, log := installHC(t, `echo '{"address":"t2","state":"live"}'`)
	if err := c.Prompt(bg, Pin{Pane: "t2", TerminalID: "t9"}, "hi"); !errors.Is(err, ErrPin) {
		t.Fatalf("err = %v, want ErrPin", err)
	}
	for _, l := range calls(t, log) {
		if strings.HasPrefix(l, "send") {
			t.Fatalf("a send went out past a failed pin: %q", l)
		}
	}
}

func TestCore_CloseAcceptsARemovedTerminal(t *testing.T) {
	c, log := installHC(t, `case "$1" in
kill) exit 0;;
rm) echo '{"error":{"kind":"live","message":"still live"}}' >&2; exit 1;;
esac`)
	if err := c.Close(bg, "t2"); err != nil {
		t.Fatalf("a failed best-effort rm must not fail Close: %v", err)
	}
	got := calls(t, log)
	if len(got) != 2 || !strings.HasPrefix(got[0], "kill t2") || !strings.HasPrefix(got[1], "rm t2") {
		t.Fatalf("calls = %q", got)
	}
}

func TestCore_StyledReadUsesAnObserver(t *testing.T) {
	c, log := installHC(t, `printf 'paint\r\n'; while :; do :; done`)
	got, err := c.PaneReadStyled(bg, "t2", 12)
	if err != nil || !strings.Contains(got, "paint") {
		t.Fatalf("PaneReadStyled = %q, %v", got, err)
	}
	if line := calls(t, log)[0]; line != "attach --observe t2" {
		t.Fatalf("call = %q", line)
	}
}

func TestCore_StyledReadOfAGoneTerminalIsAbsent(t *testing.T) {
	c, _ := installHC(t, `echo '{"error":{"kind":"not_found","message":"gone"}}' >&2; exit 3`)
	if _, err := c.PaneReadStyled(bg, "t2", 12); !IsAbsent(err) {
		t.Fatalf("err = %v, want absent", err)
	}
}

func TestKeyNames(t *testing.T) {
	got, err := keyNames("/exit A")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/", "e", "x", "i", "t", "space", "shift+a"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("keyNames = %q, want %q", got, want)
	}
	if _, err := keyNames("é"); err == nil {
		t.Fatal("a character hc cannot press must be refused, not dropped")
	}
}
