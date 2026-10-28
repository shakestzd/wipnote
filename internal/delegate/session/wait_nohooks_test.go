package session

import (
	"testing"
	"time"
)

// slice 23: in a Claude project thread no hook runs, in the caller or the
// worker. The worker's result file alone ends wait (done), result returns
// its body, and the ledger row stays the open record of the run: no hook
// event is needed anywhere.
func TestWait_NoHooksResultFileEnds(t *testing.T) {
	e := newEnv(t)
	s := e.unbound("w1", "brief") // spawned, then silent: no hook writes an event
	task := e.task("w1")
	if evs, err := e.runFolder(s.Run).ReadEvents(); err != nil || len(evs) != 0 {
		t.Fatalf("events %v err %v: want none", evs, err)
	}
	// Without the result file, nothing settles the wait.
	if res := e.wait("w1", 60*time.Millisecond); res.Outcome != OutcomeTimeout {
		t.Fatalf("settled with no signal: %+v", res)
	}
	if out, err := Result(e.deps, "w1"); err != nil || out.Status != "none" {
		t.Fatalf("result before the file: %+v err %v", out, err)
	}
	e.writeResult("w1", task, "the answer")
	res := e.wait("w1", 5*time.Second)
	if res.Outcome != OutcomeDone || res.Task != task || res.Run != s.Run {
		t.Fatalf("wait %+v", res)
	}
	out, err := Result(e.deps, "w1")
	if err != nil || out.Task != task || out.Body != "the answer" || out.Run != s.Run {
		t.Fatalf("result %+v err %v", out, err)
	}
	if r := e.row(s.Run); r.ID != s.Run || !r.Ended.IsZero() || r.Name != "w1" {
		t.Fatalf("ledger row %+v: want the run's open row", r)
	}
}
