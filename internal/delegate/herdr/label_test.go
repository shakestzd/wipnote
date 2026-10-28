package herdr

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

const renameScript = `{"rules": [
  {"match": ["pane", "rename", "*", "**"], "replies": [{"shape": {"as": "ok"}}]}
]}`

const slowRenameScript = `{"rules": [
  {"match": ["pane", "rename", "*", "**"], "replies": [{"delay_ms": 5000, "shape": {"as": "ok"}}]}
]}`

func TestWorkLabel(t *testing.T) {
	if got := WorkLabel("feat-1", "Short title"); got != "feat-1: Short title" {
		t.Fatalf("WorkLabel = %q", got)
	}
	if got := WorkLabel("feat-1", "  spread\n over\tlines "); got != "feat-1: spread over lines" {
		t.Fatalf("WorkLabel whitespace = %q", got)
	}
	if got := WorkLabel("feat-1", ""); got != "feat-1" {
		t.Fatalf("WorkLabel no title = %q", got)
	}
	long := WorkLabel("feat-8d3d6bdd", strings.Repeat("é very long title ", 10))
	if n := utf8.RuneCountInString(long); n != LabelWidth {
		t.Fatalf("long label has %d runes, want %d: %q", n, LabelWidth, long)
	}
	if !strings.HasPrefix(long, "feat-8d3d6bdd: é very") || !strings.HasSuffix(long, "…") {
		t.Fatalf("long label = %q, want id prefix and an ellipsis", long)
	}
	exact := WorkLabel("f", strings.Repeat("x", LabelWidth-3))
	if utf8.RuneCountInString(exact) != LabelWidth || strings.HasSuffix(exact, "…") {
		t.Fatalf("label at the width must not be cut: %q", exact)
	}
}

func TestLabelCallerPane_InsideHerdr(t *testing.T) {
	f := installFakeScript(t, renameScript)
	t.Setenv("HERDR_PANE_ID", "w1:p4")
	env := EnvFrom(os.Getenv)
	if err := LabelCallerPane(bg, New(env), env, "feat-1: Title"); err != nil {
		t.Fatal(err)
	}
	if got := f.argvs(); !reflect.DeepEqual(got, []string{"pane rename w1:p4 feat-1: Title"}) {
		t.Fatalf("calls = %q", got)
	}
}

func TestLabelCallerPane_NoCallOutsideHerdr(t *testing.T) {
	f := installFakeScript(t, renameScript)
	t.Setenv("HERDR_PANE_ID", "w1:p4")
	for _, v := range []string{"", "0"} {
		t.Setenv("HERDR_ENV", v)
		env := EnvFrom(os.Getenv)
		if err := LabelCallerPane(bg, New(env), env, "feat-1: Title"); err != nil {
			t.Fatalf("HERDR_ENV=%q: err = %v, want nil", v, err)
		}
	}
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_PANE_ID", "")
	env := EnvFrom(os.Getenv)
	if err := LabelCallerPane(bg, New(env), env, "feat-1: Title"); err != nil {
		t.Fatalf("no caller pane: err = %v, want nil", err)
	}
	if err := LabelCallerPane(bg, New(env), Env{InHerdr: true, CallerPane: "w1:p4"}, "  "); err != nil {
		t.Fatalf("blank label: err = %v, want nil", err)
	}
	if n := len(f.calls()); n != 0 {
		t.Fatalf("herdr ran %d times, want none: %q", n, f.argvs())
	}
}

func TestLabelCallerPane_TimeoutAndAbsent(t *testing.T) {
	t.Run("timeout", func(t *testing.T) {
		installFakeScript(t, slowRenameScript)
		t.Setenv("HERDR_PANE_ID", "w1:p4")
		env := EnvFrom(os.Getenv)
		c := New(env)
		c.Timeout = 150 * time.Millisecond
		start := time.Now()
		err := LabelCallerPane(bg, c, env, "feat-1: Title")
		if !errors.Is(err, ErrTimeout) {
			t.Fatalf("err = %v, want ErrTimeout", err)
		}
		if d := time.Since(start); d > 3*time.Second {
			t.Fatalf("label call took %s, want it cut at the timeout", d)
		}
	})
	t.Run("absent", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		env := Env{InHerdr: true, CallerPane: "w1:p4"}
		if err := LabelCallerPane(bg, New(env), env, "feat-1: Title"); !errors.Is(err, ErrAbsent) {
			t.Fatalf("err = %v, want ErrAbsent", err)
		}
	})
}
