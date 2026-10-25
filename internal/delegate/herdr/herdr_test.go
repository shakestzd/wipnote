package herdr

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

var bg = context.Background()

func TestHerdr_OnlyWithHerdrEnv(t *testing.T) {
	f := installFake(t, "pane_get")
	for _, v := range []string{"", "0", "true", "yes"} {
		t.Setenv("HERDR_ENV", v)
		t.Setenv("HERDR_PANE_ID", "w1:p1")
		env := EnvFrom(os.Getenv)
		if env.InHerdr || env.CallerPane != "" {
			t.Fatalf("HERDR_ENV=%q: env = %+v, want not in herdr and no caller pane", v, env)
		}
		if _, err := New(env).Get(bg, "w1:p2"); !errors.Is(err, ErrNotInHerdr) {
			t.Fatalf("HERDR_ENV=%q: Get err = %v, want ErrNotInHerdr", v, err)
		}
	}
	if n := len(f.calls()); n != 0 {
		t.Fatalf("herdr ran %d times without HERDR_ENV=1", n)
	}
	t.Setenv("HERDR_ENV", "1")
	env := EnvFrom(os.Getenv)
	if !env.InHerdr || env.CallerPane != "w1:p1" {
		t.Fatalf("HERDR_ENV=1: env = %+v, want in herdr with caller pane w1:p1", env)
	}
	p, err := New(env).Get(bg, "w1:p2")
	if err != nil || p.ID != "w1:p2" || p.WorkspaceID != "w1" {
		t.Fatalf("Get = %+v, %v", p, err)
	}
	if got := f.argvs(); !reflect.DeepEqual(got, []string{"pane get w1:p2"}) {
		t.Fatalf("calls = %q", got)
	}
}

func TestHerdr_TimeoutAndAbsent(t *testing.T) {
	t.Run("absent", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		t.Setenv("HERDR_ENV", "1")
		_, err := newClient(t).Get(bg, "w1:p2")
		if !errors.Is(err, ErrAbsent) {
			t.Fatalf("err = %v, want ErrAbsent", err)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		installFake(t, "slow")
		c := newClient(t)
		c.Timeout = 150 * time.Millisecond
		start := time.Now()
		_, err := c.Get(bg, "w1:p2")
		if !errors.Is(err, ErrTimeout) {
			t.Fatalf("err = %v, want ErrTimeout", err)
		}
		if d := time.Since(start); d > 2*time.Second {
			t.Fatalf("timeout took %s", d)
		}
	})
	t.Run("in time", func(t *testing.T) {
		installFake(t, "pane_get")
		c := newClient(t)
		c.Timeout = 5 * time.Second
		if _, err := c.Get(bg, "w1:p2"); err != nil {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestHerdr_Exit1ParsesStderrJSON(t *testing.T) {
	installFake(t, "errors")
	c := newClient(t)
	_, err := c.Get(bg, "w1:p9")
	var he *Error
	if !errors.As(err, &he) || he.Exit != 1 || he.Code != "pane_not_found" || he.Message != "no pane w1:p9" {
		t.Fatalf("err = %#v", err)
	}
	if !IsAbsent(err) || errors.Is(err, ErrUsage) || errors.Is(err, ErrTimeout) {
		t.Fatalf("pane_not_found classified wrongly: absent=%v", IsAbsent(err))
	}
	// herdr's own wait timeout is a server error, not the client's deadline
	err = c.Prompt(bg, Pin{Pane: "w1:p2", TerminalID: "t-w1:p2"}, "hi")
	if !errors.As(err, &he) || he.Code != "timeout" || errors.Is(err, ErrTimeout) || IsAbsent(err) {
		t.Fatalf("prompt err = %#v", err)
	}
}

func TestHerdr_Exit2IsUsageNotTimeout(t *testing.T) {
	installFake(t, "errors")
	err := newClient(t).Close(bg, "w1:p2")
	if !errors.Is(err, ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage", err)
	}
	var he *Error
	if errors.Is(err, ErrTimeout) || !errors.As(err, &he) || he.Code != CodeUsage {
		t.Fatalf("exit 2 read as %#v", err)
	}
	if !strings.Contains(err.Error(), "unknown option") {
		t.Fatalf("usage error lost herdr's text: %v", err)
	}
}

func TestHerdr_CommandForms(t *testing.T) {
	f := installFake(t, "forms")
	c := newClient(t)
	ctx := bg
	pin := Pin{Pane: "w1:p2", TerminalID: "t-w1:p2"}
	tab, root, err := c.TabCreate(ctx, "w1", "workers:abcd1234", "/proj")
	must(t, err)
	if tab != "w1:t3" || root != "w1:p7" {
		t.Fatalf("TabCreate = %s %s", tab, root)
	}
	p, err := c.Split(ctx, "w1:p7", Right, "/proj")
	must(t, err)
	if p != "w1:p8" {
		t.Fatalf("Split = %s", p)
	}
	must(t, c.Run(ctx, "w1:p8", "WIPNOTE_DELEGATE_RUN=r1 wipnote claude"))
	must(t, c.Rename(ctx, "w1:p8", "worker one"))
	must(t, c.SendText(ctx, "w1:p8", "hello"))
	must(t, c.SendKeys(ctx, "w1:p8", "enter"))
	pi, err := c.Processes(ctx, "w1:p8")
	must(t, err)
	if !pi.Known || pi.ShellPID != 900 || len(pi.Foreground) != 1 || pi.Foreground[0].Name != "claude" {
		t.Fatalf("Processes = %+v", pi)
	}
	rects, err := c.Layout(ctx, "w1:p7")
	must(t, err)
	if len(rects) != 2 || rects[1] != (PaneRect{Pane: "w1:p8", Width: 80, Height: 40}) {
		t.Fatalf("Layout = %+v", rects)
	}
	must(t, c.Prompt(ctx, pin, "do the thing"))
	text, err := c.Read(ctx, pin, 50)
	must(t, err)
	if text != "line one\nline two" {
		t.Fatalf("Read = %q", text)
	}
	must(t, c.AgentRename(ctx, pin, "worker-one"))
	agents, err := c.AgentList(ctx)
	must(t, err)
	if len(agents) != 1 || agents[0].Name != "worker-one" || agents[0].TerminalID != "t-w1:p2" || agents[0].State != StateIdle {
		t.Fatalf("AgentList = %+v", agents)
	}
	must(t, c.Close(ctx, "w1:p8"))
	must(t, c.TabClose(ctx, "w1:t3"))
	want := readLines(t, filepath.Join("testdata", "scripts", "forms.calls"))
	if got := f.argvs(); !reflect.DeepEqual(got, want) {
		t.Fatalf("calls:\n got %q\nwant %q", got, want)
	}
}

func TestPin_MismatchTypesNothing(t *testing.T) {
	cases := map[string]Pin{
		"other terminal": {Pane: "w1:p2", TerminalID: "t-mine"},
		"other pane":     {Pane: "w1:p5", TerminalID: "t-w1:p2"},
		"lookup fails":   {Pane: "w1:p9", TerminalID: "t-w1:p9"},
	}
	for name, pin := range cases {
		t.Run(name, func(t *testing.T) {
			f := installFake(t, "pin")
			err := newClient(t).Prompt(bg, pin, "rm -rf")
			if !errors.Is(err, ErrPin) {
				t.Fatalf("err = %v, want ErrPin", err)
			}
			for _, call := range f.argvs() {
				if !strings.HasPrefix(call, "agent get ") {
					t.Fatalf("typed into a mismatched pane: %q", call)
				}
			}
		})
	}
	t.Run("match", func(t *testing.T) {
		f := installFake(t, "pin")
		must(t, newClient(t).Prompt(bg, Pin{Pane: "w1:p2", TerminalID: "t-w1:p2"}, "go"))
		want := []string{"agent get w1:p2", "agent prompt w1:p2 go"}
		if got := f.argvs(); !reflect.DeepEqual(got, want) {
			t.Fatalf("calls = %q", got)
		}
	})
}

func TestHerdr_CallerWorkspace(t *testing.T) {
	installFake(t, "pane_get")
	if _, err := newClient(t).CallerWorkspace(bg); !errors.Is(err, ErrNoCallerPane) {
		t.Fatalf("no HERDR_PANE_ID: err = %v", err)
	}
	t.Setenv("HERDR_PANE_ID", "w1:p2")
	ws, err := newClient(t).CallerWorkspace(bg)
	if err != nil || ws != "w1" {
		t.Fatalf("CallerWorkspace = %q, %v", ws, err)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	must(t, err)
	return strings.Split(strings.TrimRight(string(data), "\n"), "\n")
}
