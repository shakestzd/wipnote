package herdr

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestGridPlan_NoPaneUnderAQuarter(t *testing.T) {
	// Simulate the plan on a unit square: panes are rects in creation order.
	type rect struct{ w, h float64 }
	panes := []rect{{1, 1}}
	for n := 1; n < 6; n++ {
		rank, dir := gridPlan(n)
		if rank >= len(panes) {
			t.Fatalf("n=%d: rank %d out of range", n, rank)
		}
		r := panes[rank]
		if dir == Right {
			r.w /= 2
		} else {
			r.h /= 2
		}
		panes[rank] = r
		panes = append(panes, r)
	}
	for i, p := range panes {
		if p.w < 0.25 || p.h < 0.5 {
			t.Fatalf("pane %d is %vx%v of the tab", i, p.w, p.h)
		}
	}
}

// gridApp stubs herdr-app with a tab of two terminals (t1 started first) and
// a caller looking at tab tbCaller, pane pCaller. terminal.keep fails
// keepFails times (as the app does until it learns of the new terminal), then
// succeeds; keepFails of 99 never succeeds.
func gridApp(t *testing.T, log string, keepFails int) string {
	t.Helper()
	count := filepath.Join(filepath.Dir(log), "keepcount")
	kept := filepath.Join(filepath.Dir(log), "kept") // set once keep succeeded: only then does ls list the terminal
	return installApp(t, log, `case "$1" in
ls) new=; [ -f `+kept+` ] && new=',{"terminal":"t8","pane":"p8"}'
  echo '{"spaces":[{"tabs":[{"id":"tb9","terminals":[{"terminal":"t2","pane":"p2"},{"terminal":"t1","pane":"p1"}'$new']}]}]}';;
status) echo '{"tab":"tbCaller","focused_pane":"pCaller"}';;
do) case "$2" in terminal.keep)
  n=0; [ -f `+count+` ] && read n < `+count+`; echo $((n+1)) > `+count+`
  if [ "$n" -lt `+strconv.Itoa(keepFails)+` ]; then echo '{"error":"no running terminal"}' >&2; exit 1; fi
  : > `+kept+`;; esac; echo '{"ok":true}';;
esac`)
}

func fastGrid(t *testing.T) {
	t.Helper()
	every, tries := gridEvery, gridTries
	gridEvery, gridTries = time.Millisecond, 5
	t.Cleanup(func() { gridEvery, gridTries = every, tries })
}

const gridHC = `case "$1" in
get) case "$2" in
  t1) echo '{"address":"t1","state":"live","created_ms":1,"labels":{"herdr.space":"s3","herdr.tab":"tb9"}}';;
  t2) echo '{"address":"t2","state":"live","created_ms":2}';;
  *) echo '{"address":"t8","state":"live","created_ms":3}';; esac;;
run) echo '{"address":"t8","state":"live"}';;
esac`

func TestCore_SplitGridChoosesTheTargetPane(t *testing.T) {
	c, log := installHC(t, gridHC)
	app := gridApp(t, log, 0)
	fastGrid(t)
	pane, err := c.Split(bg, "t1", Right, "/repo")
	if err != nil || pane != "t8" {
		t.Fatalf("Split = %q, %v", pane, err)
	}
	// Two panes: the third goes down from the first-created pane t1 (p1).
	data, _ := os.ReadFile(app)
	got := string(data)
	if !strings.Contains(got, "do pane.focus --pane p1") || !strings.Contains(got, "do terminal.keep --terminal t8 --place down") {
		t.Fatalf("app calls:\n%s", got)
	}
	for _, l := range calls(t, log) {
		if strings.HasPrefix(l, "run") && strings.Contains(l, "--label") {
			t.Fatalf("the grid path must start the terminal unlabelled: %q", l)
		}
	}
}

// The app answers "no running terminal" to keep for a moment after hc run.
// Live, a single try always failed and every worker was adopted by labels.
func TestCore_SplitGridRetriesKeepUntilTheAppKnowsTheTerminal(t *testing.T) {
	c, log := installHC(t, gridHC)
	app := gridApp(t, log, 3)
	fastGrid(t)
	if pane, err := c.Split(bg, "t1", Right, "/repo"); err != nil || pane != "t8" {
		t.Fatalf("Split = %q, %v", pane, err)
	}
	data, _ := os.ReadFile(app)
	if n := strings.Count(string(data), "do terminal.keep"); n != 4 {
		t.Fatalf("keep tried %d times, want 4 (3 refusals then success):\n%s", n, data)
	}
	if n := strings.Count(string(data), "do pane.focus --pane p1"); n != 4 {
		t.Fatalf("the anchor must be focused before every keep try, got %d:\n%s", n, data)
	}
	if got := strings.Join(calls(t, log), "\n"); strings.Contains(got, "kill t8") {
		t.Fatalf("a retried keep must not fall back:\n%s", got)
	}
}

// After placing, the caller's own pane is focused again and the workers tab is
// put away, so a spawn leaves the person where they were.
func TestCore_SplitRestoresFocusAndSettlesTheTab(t *testing.T) {
	for _, keepFails := range []int{0, 99} { // grid, and the label fallback
		c, log := installHC(t, gridHC)
		app := gridApp(t, log, keepFails)
		fastGrid(t)
		if _, err := c.Split(bg, "t1", Right, "/repo"); err != nil {
			t.Fatal(err)
		}
		data, _ := os.ReadFile(app)
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		n := len(lines)
		if n < 2 || lines[n-2] != "do pane.focus --pane pCaller" || lines[n-1] != "do tab.settle --id tb9" {
			t.Fatalf("keepFails=%d: last app calls = %q, want focus pCaller then settle tb9", keepFails, lines[max(0, n-2):])
		}
	}
}

func TestCore_SplitGridFallsBackToLabels(t *testing.T) {
	c, log := installHC(t, gridHC)
	gridApp(t, log, 99)
	fastRetry(t)
	fastGrid(t)
	pane, err := c.Split(bg, "t1", Right, "/repo")
	if err != nil || pane != "t8" {
		t.Fatalf("Split = %q, %v: layout must not fail a spawn", pane, err)
	}
	got := strings.Join(calls(t, log), "\n")
	if !strings.Contains(got, "kill t8") || !strings.Contains(got, "rm t8") {
		t.Fatalf("the unadopted terminal must be removed:\n%s", got)
	}
	last := calls(t, log)
	if l := last[len(last)-1]; !strings.Contains(l, "--label herdr.tab=tb9") {
		t.Fatalf("last call = %q, want the labelled run", l)
	}
}

func TestCore_SplitWithoutAppAdoptsByLabels(t *testing.T) {
	c, log := installHC(t, gridHC)
	if _, err := c.Split(bg, "t1", Right, "/repo"); err != nil {
		t.Fatal(err)
	}
	last := calls(t, log)
	if l := last[len(last)-1]; !strings.Contains(l, "--label herdr.space=s3") {
		t.Fatalf("last call = %q", l)
	}
}
