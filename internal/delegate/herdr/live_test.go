package herdr

import (
	"os"
	"strings"
	"testing"
	"time"
)

// TestHerdr_LiveTabRoundTrip runs against a REAL herdr and is skipped unless
// the test process started inside one (HERDR_ENV=1). It creates a background
// tab in the caller's workspace, runs a command, reads it back and closes the
// tab it made.
func TestHerdr_LiveTabRoundTrip(t *testing.T) {
	if liveEnv["HERDR_ENV"] != "1" {
		t.Skip("live herdr test: run inside a herdr pane (HERDR_ENV=1)")
	}
	for name, value := range liveEnv {
		t.Setenv(name, value)
	}
	c := newClient(t)
	ws, err := c.CallerWorkspace(bg)
	must(t, err)
	cwd, err := os.Getwd()
	must(t, err)
	tab, pane, err := c.TabCreate(bg, ws, "wipnote-live-test", cwd)
	must(t, err)
	t.Cleanup(func() { _ = c.TabClose(bg, tab) })
	must(t, c.Run(bg, pane, "echo wipnote-live-marker"))
	deadline := time.Now().Add(10 * time.Second)
	for {
		text, err := c.PaneRead(bg, pane, 20)
		if err == nil && strings.Contains(text, "wipnote-live-marker") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("marker never appeared: %q, %v", text, err)
		}
		time.Sleep(200 * time.Millisecond)
	}
	must(t, c.TabClose(bg, tab))
	if _, err := c.Get(bg, pane); !IsAbsent(err) {
		t.Fatalf("pane survived its tab: %v", err)
	}
}
