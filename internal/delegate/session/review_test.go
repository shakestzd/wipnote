package session

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/shakestzd/wipnote/core/delegationledger"
	"github.com/shakestzd/wipnote/internal/delegate/herdr"
)

// A pinned worker's pane id now holds another terminal (herdr reused the
// id): stop sends no key, closes no pane or tab, and closes the row as gone.
func TestStop_ReusedPaneIDTouchesNothing(t *testing.T) {
	e := newEnv(t)
	e.herdr.exitOnEnter = true
	s := runningWorker(t, e, "w1")
	rf := e.runFolder(s.Run)
	info, err := ReadInfo(rf)
	if err != nil {
		t.Fatal(err)
	}
	info.TerminalID, info.Renamed = e.herdr.pane(PaneOf(s)).terminal, true
	if err := WriteInfo(rf, info); err != nil {
		t.Fatal(err)
	}
	e.herdr.pane(PaneOf(s)).terminal = "term-someone-else"
	mark := len(e.herdr.Calls())
	res := stop(t, e, "w1")
	for _, c := range e.herdr.Calls()[mark:] {
		if strings.HasPrefix(c, "send-") || strings.HasPrefix(c, "close ") || strings.HasPrefix(c, "tab close") {
			t.Fatalf("stop touched a reused pane: %s", c)
		}
	}
	if res.PaneClosed || res.TabClosed || !strings.Contains(res.Why, "term-someone-else") {
		t.Fatalf("%+v", res)
	}
	if r := e.row(s.Run); r.IsOpen() || r.EndReason != delegationledger.EndGone {
		t.Fatalf("row %+v", r)
	}
	// Other side: the same worker with its own terminal is stopped normally.
	e2 := newEnv(t)
	e2.herdr.exitOnEnter = true
	s2 := runningWorker(t, e2, "w1")
	rf2 := e2.runFolder(s2.Run)
	info2, _ := ReadInfo(rf2)
	info2.TerminalID = e2.herdr.pane(PaneOf(s2)).terminal
	_ = WriteInfo(rf2, info2)
	if res := stop(t, e2, "w1"); !res.PaneClosed {
		t.Fatalf("%+v", res)
	}
}

// Concurrent spawns of one name, from two callers, yield one worker; every
// other spawn gets the live-name refusal.
func TestSpawn_ConcurrentSameNameOneWins(t *testing.T) {
	e := newEnv(t)
	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			o := e.opts("w1")
			o.CallerPane = herdr.PaneID([]string{"w1:p1", "w1:p2"}[i%2])
			_, errs[i] = Spawn(context.Background(), e.deps, o)
		}(i)
	}
	wg.Wait()
	ok := 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case !errors.Is(err, ErrLiveName):
			t.Fatalf("unexpected refusal: %v", err)
		}
	}
	rows, _ := e.deps.Ledger.ReadAll()
	if ok != 1 || len(rows) != 1 {
		t.Fatalf("%d spawns succeeded, %d rows: %v", ok, len(rows), errs)
	}
}
