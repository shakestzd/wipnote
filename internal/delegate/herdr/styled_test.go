package herdr

import (
	"errors"
	"strings"
	"testing"
)

func TestPaneReadStyled_FormAndUsageError(t *testing.T) {
	installFakeScript(t, `{"rules": [
	  {"match": ["pane", "read", "w1:p8", "--source", "recent", "--lines", "12", "--format", "ansi"],
	   "replies": [{"stdout": "> \u001b[2mtry this\u001b[0m\n"}]},
	  {"match": ["pane", "read", "w1:p9", "**"], "replies": [{"stderr": "unknown option --format\n", "exit": 2}]}
	]}`)
	c := newClient(t)
	text, err := c.PaneReadStyled(bg, "w1:p8", 12)
	if err != nil || !strings.Contains(text, "\x1b[2mtry this") {
		t.Fatalf("text %q err %v", text, err)
	}
	if _, err := c.PaneReadStyled(bg, "w1:p9", 12); !errors.Is(err, ErrUsage) {
		t.Fatalf("err %v", err)
	}
}
