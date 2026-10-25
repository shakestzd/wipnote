package herdr

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The split sequence the mod's pane_sim.sh asserts for six tile panes, as
// "target:direction" with the caller's pane written "current".
const modTileSix = "current:right w1:p101:down w1:p101:right w1:p102:right w1:p104:down w1:p105:down"

// tileSequence asks TileTarget for n panes, minting ids as the mod's stub
// does (w1:p101, w1:p102, ...), and returns the splits and how many it refused.
func tileSequence(n, maxPanes int) (splits []string, refused int) {
	var live []PaneID
	for i := 0; i < n; i++ {
		target, dir, ok := TileTarget(live, maxPanes)
		if !ok {
			refused++
			continue
		}
		name := string(target)
		if target == "" {
			name = "current"
		}
		splits = append(splits, name+":"+string(dir))
		live = append(live, PaneID(fmt.Sprintf("w1:p%d", 101+len(live))))
	}
	return splits, refused
}

func TestLayout_MatchesModTile(t *testing.T) {
	want := strings.Fields(modTileSix)
	for n := 1; n <= 7; n++ {
		splits, refused := tileSequence(n, 6)
		wantN := min(n, 6)
		if !reflect.DeepEqual(splits, want[:wantN]) || refused != n-wantN {
			t.Fatalf("n=%d: splits %q refused %d, want %q refused %d", n, splits, refused, want[:wantN], n-wantN)
		}
	}
	// the mod's "tile cap 2, 3 at once" case
	if splits, refused := tileSequence(3, 2); strings.Join(splits, " ") != "current:right w1:p101:down" || refused != 1 {
		t.Fatalf("cap 2: %q refused %d", splits, refused)
	}
	if _, _, ok := TileTarget(nil, 0); !ok {
		t.Fatal("a cap below 1 must still allow one pane, as the mod does")
	}
}

// TestHerdr_StubScript drives the real client against the mod's own herdr
// stub (testdata/herdr) and checks the stub saw the mod's split sequence.
func TestHerdr_StubScript(t *testing.T) {
	stubDir, err := filepath.Abs("testdata")
	must(t, err)
	simlog := filepath.Join(t.TempDir(), "log")
	t.Setenv("SIMLOG", simlog)
	t.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HERDR_ENV", "1")
	c := newClient(t)
	var live []PaneID
	for i := 0; i < 7; i++ {
		target, dir, ok := TileTarget(live, 6)
		if !ok {
			continue
		}
		p, err := c.Split(bg, target, dir, "/proj")
		must(t, err)
		live = append(live, p)
	}
	data, err := os.ReadFile(simlog + ".splits")
	must(t, err)
	got := strings.ReplaceAll(strings.TrimSpace(string(data)), " ", ":")
	if got = strings.ReplaceAll(got, "\n", " "); got != modTileSix {
		t.Fatalf("stub splits = %q, want %q", got, modTileSix)
	}
	must(t, c.Close(bg, live[0]))
	if _, err := os.Stat(simlog + ".closed." + string(live[0])); err != nil {
		t.Fatalf("stub did not see the close: %v", err)
	}
}

func TestLayout_WorkersTabSplitOrder(t *testing.T) {
	panes := []PaneID{"w1:p11", "w1:p12", "w1:p13", "w1:p14", "w1:p15"}
	wide := []PaneRect{{"w1:p11", 40, 20}, {"w1:p12", 100, 20}, {"w1:p13", 40, 20}, {"w1:p14", 40, 20}}
	tall := []PaneRect{{"w1:p11", 40, 20}, {"w1:p12", 40, 20}, {"w1:p13", 30, 60}, {"w1:p14", 40, 20}}
	cases := []struct {
		live  []PaneID
		sizes []PaneRect
		want  Placement
	}{
		{nil, nil, Placement{NewTab: true}},
		{panes[:1], nil, Placement{Target: "w1:p11", Dir: Right}},
		{panes[:2], nil, Placement{Target: "w1:p11", Dir: Down}},
		{panes[:3], nil, Placement{Target: "w1:p12", Dir: Down}},
		{panes[:4], wide, Placement{Target: "w1:p12", Dir: Right}},
		{panes[:4], tall, Placement{Target: "w1:p13", Dir: Down}},
		{panes[:4], nil, Placement{Target: "w1:p14", Dir: Down}},
		{panes[:5], nil, Placement{Target: "w1:p15", Dir: Down}},
	}
	for i, c := range cases {
		got, err := WorkerPlacement(c.live, c.sizes)
		if err != nil || got != c.want {
			t.Fatalf("case %d (%d live): %+v, %v; want %+v", i, len(c.live), got, err, c.want)
		}
	}
}

func TestLayout_WorkersTabCap6(t *testing.T) {
	live := []PaneID{"a", "b", "c", "d", "e"}
	if _, err := WorkerPlacement(live, nil); err != nil {
		t.Fatalf("6th worker refused: %v", err)
	}
	live = append(live, "f")
	if _, err := WorkerPlacement(live, nil); !errors.Is(err, ErrWorkersCap) {
		t.Fatalf("7th worker: err = %v, want ErrWorkersCap", err)
	}
	if MaxWorkers != 6 {
		t.Fatalf("MaxWorkers = %d", MaxWorkers)
	}
}

// TestLayout_PlaceWorkers runs Place through the fake: the first worker makes
// ONE workers tab in the caller's workspace, later ones split inside it, the
// caller's own pane is never a split target, and the last stop closes the tab.
func TestLayout_PlaceWorkers(t *testing.T) {
	f := installFake(t, "place")
	t.Setenv("HERDR_PANE_ID", "w1:p1")
	c := newClient(t)
	var tab *WorkersTab
	var live []PaneID
	for i := 0; i < 4; i++ {
		var p PaneID
		var err error
		tab, p, err = Place(bg, c, tab, live, "workers:abcd1234", "/proj")
		must(t, err)
		live = append(live, p)
	}
	if tab == nil || tab.ID != "w1:t2" || tab.Workspace != "w1" {
		t.Fatalf("tab = %+v", tab)
	}
	if want := []PaneID{"w1:p20", "w1:p21", "w1:p22", "w1:p23"}; !reflect.DeepEqual(live, want) {
		t.Fatalf("panes = %q", live)
	}
	if closed, err := ReleaseTab(bg, c, tab, live[:1]); closed || err != nil {
		t.Fatalf("closed a tab with a live worker: %v %v", closed, err)
	}
	if closed, err := ReleaseTab(bg, c, tab, nil); !closed || err != nil {
		t.Fatalf("ReleaseTab = %v, %v", closed, err)
	}
	want := readLines(t, filepath.Join("testdata", "scripts", "place.calls"))
	got := f.argvs()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("calls:\n got %q\nwant %q", got, want)
	}
	for _, call := range got {
		if strings.HasPrefix(call, "pane split w1:p1 ") {
			t.Fatalf("split the caller's pane: %q", call)
		}
	}
}

func TestLayout_PlaceRefusesWithoutCallerPane(t *testing.T) {
	installFake(t, "place")
	_, _, err := Place(bg, newClient(t), nil, nil, "workers:x", "/proj")
	if !errors.Is(err, ErrNoCallerPane) {
		t.Fatalf("err = %v, want ErrNoCallerPane", err)
	}
}
