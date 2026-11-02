package herdr

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const runReply = `case "$1" in
get) echo '{"address":"t1","state":"live","labels":{"herdr.space":"s3"}}';;
run) echo '{"address":"t8","state":"live"}';;
esac`

// installApp puts a stub herdr-app next to the stub hc; its calls are logged.
func installApp(t *testing.T, log, script string) (appLog string) {
	t.Helper()
	dir := filepath.Dir(log)
	appLog = filepath.Join(dir, "applog")
	body := "#!/bin/sh\necho \"$*\" >> " + appLog + "\n" + script + "\n"
	if err := os.WriteFile(filepath.Join(dir, "herdr-app"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return appLog
}

func fastRetry(t *testing.T) {
	t.Helper()
	every, tries := tabRenameEvery, tabRenameTries
	tabRenameEvery, tabRenameTries = time.Millisecond, 5
	t.Cleanup(func() { tabRenameEvery, tabRenameTries = every, tries })
}

func TestCore_RunPassesTheTerminalName(t *testing.T) {
	c, log := installHC(t, runReply)
	if _, _, err := c.TabCreate(WithTerminalName(bg, "fix-bug-1"), "s3", "workers", "/repo"); err != nil {
		t.Fatal(err)
	}
	got := calls(t, log)
	if last := got[len(got)-1]; !strings.Contains(last, "--name fix-bug-1") {
		t.Fatalf("run = %q, want --name fix-bug-1", last)
	}
}

func TestCore_NoNameWhenNoneOrInvalid(t *testing.T) {
	c, log := installHC(t, runReply)
	for _, ctx := range []struct{ name string }{{""}, {"Bad Name"}} {
		if _, err := c.Split(WithTerminalName(bg, ctx.name), "", Right, "/repo"); err != nil {
			t.Fatal(err)
		}
	}
	for _, l := range calls(t, log) {
		if strings.Contains(l, "--name") {
			t.Fatalf("unexpected --name: %q", l)
		}
	}
}

func TestCore_NameCollisionStartsUnnamed(t *testing.T) {
	c, log := installHC(t, `case "$1" in
get) echo '{"address":"t1","state":"live","labels":{}}';;
run) case "$*" in *--name*) echo '{"error":{"kind":"conflict","message":"name taken"}}' >&2; exit 4;; esac
  echo '{"address":"t8","state":"live"}';;
esac`)
	pane, err := c.Split(WithTerminalName(bg, "w1"), "", Right, "/repo")
	if err != nil || pane != "t8" {
		t.Fatalf("Split = %q, %v: a taken name must not fail", pane, err)
	}
	got := calls(t, log)
	if len(got) < 3 || strings.Contains(got[len(got)-1], "--name") || !strings.Contains(got[len(got)-2], "--name w1") {
		t.Fatalf("calls = %q: want a named try, then an unnamed one", got)
	}
}

func TestCore_TabRenamedWithIDAndName(t *testing.T) {
	c, log := installHC(t, runReply)
	app := installApp(t, log, "echo '{\"ok\": true}'")
	if _, _, err := c.TabCreate(bg, "s3", "workers", "/repo"); err != nil {
		t.Fatal(err)
	}
	got := calls(t, app)
	if len(got) != 1 || got[0] != "do tab.rename --id wipnote-workers-t1 --name workers" {
		t.Fatalf("herdr-app calls = %q", got)
	}
}

func TestCore_TabRenameRetriesThenSucceeds(t *testing.T) {
	fastRetry(t)
	c, log := installHC(t, runReply)
	// fail twice, succeed on the third call
	app := installApp(t, log, `n=0; while read -r _; do n=$((n+1)); done < "${0%/*}/applog"
if [ "$n" -lt 3 ]; then echo '{"error": "no tab"}'; exit 1; fi
echo '{"ok": true}'`)
	if _, _, err := c.TabCreate(bg, "s3", "workers", "/repo"); err != nil {
		t.Fatal(err)
	}
	if got := calls(t, app); len(got) != 3 {
		t.Fatalf("herdr-app calls = %q, want 3 renames", got)
	}
}

func TestCore_TabRenameGivesUpQuietly(t *testing.T) {
	fastRetry(t)
	c, log := installHC(t, runReply)
	app := installApp(t, log, "exit 1")
	if _, _, err := c.TabCreate(bg, "s3", "workers", "/repo"); err != nil {
		t.Fatalf("a failed rename must not fail spawn: %v", err)
	}
	if got := calls(t, app); len(got) != tabRenameTries {
		t.Fatalf("herdr-app calls = %d, want the bound %d", len(got), tabRenameTries)
	}
}

func TestCore_NoSpaceNoRename(t *testing.T) {
	c, log := installHC(t, runReply)
	app := installApp(t, log, "exit 0")
	if _, _, err := c.TabCreate(bg, "", "workers", "/repo"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(app); !os.IsNotExist(err) {
		t.Fatal("bare hc must not rename a tab")
	}
}

func TestCore_MissingAppDoesNotFailOrWait(t *testing.T) {
	c, _ := installHC(t, runReply)
	start := time.Now()
	if _, _, err := c.TabCreate(bg, "s3", "workers", "/repo"); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("a missing herdr-app must not delay spawn")
	}
}

// The workers tab stays in the tab bar: spawn names it and never settles it.
func TestCore_WorkersTabIsNeverSettled(t *testing.T) {
	c, log := installHC(t, runReply)
	app := installApp(t, log, "echo '{\"ok\": true}'")
	if _, _, err := c.TabCreate(bg, "s3", "workers", "/repo"); err != nil {
		t.Fatal(err)
	}
	for _, call := range calls(t, app) {
		if strings.Contains(call, "tab.settle") {
			t.Fatalf("herdr-app call %q settles the tab", call)
		}
	}
}

// One caller spawning in two projects gets two tabs; a key replaces its pane.
func TestTabCreate_WorkersKeyNamesTheTab(t *testing.T) {
	c, _ := installHC(t, runReply)
	installApp(t, t.TempDir()+"/app.log", "echo '{\"ok\": true}'")
	one, _, err := c.TabCreate(WithWorkersKey(bg, "proj-a"), "s3", "workers", "/repo")
	if err != nil {
		t.Fatal(err)
	}
	two, _, err := c.TabCreate(WithWorkersKey(bg, "proj-b"), "s3", "workers", "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if one == two || one != "wipnote-workers-proj-a" {
		t.Fatalf("tabs %q and %q", one, two)
	}
}

// A worker's terminal is labelled when it starts.
func TestCore_WorkerLabelOnStart(t *testing.T) {
	c, log := installHC(t, runReply)
	if _, _, err := c.TabCreate(WithWorkerLabel(bg, "w1"), "s3", "workers", "/repo"); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(calls(t, log), "\n")
	if !strings.Contains(got, "run --label wipnote.worker=w1") {
		t.Fatalf("no worker label in:\n%s", got)
	}
}
