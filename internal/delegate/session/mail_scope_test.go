package session

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/delegationledger"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// A competing send lands, and the worker starts it, while this send is
// looking at the worker: the readiness this send acts on must be read under
// the send lock, so exactly one of the two sends is accepted.
func TestSend_ReadinessDecidedUnderLock(t *testing.T) {
	e := newEnv(t)
	s := e.worker("w1", "")
	var once atomic.Bool
	competitor := make(chan error, 1)
	e.herdr.onGet = func() {
		if !once.CompareAndSwap(false, true) {
			return
		}
		go func() {
			_, err := Send(context.Background(), e.deps, "w1", "competitor")
			competitor <- err
		}()
		select {
		case err := <-competitor:
			competitor <- err
			if err == nil { // it landed: the worker starts it
				e.startTurn(s, "sid-1")
			}
		case <-time.After(300 * time.Millisecond): // it is held by the lock
		}
	}
	_, outer := Send(context.Background(), e.deps, "w1", "outer")
	other := <-competitor
	if (outer == nil) == (other == nil) {
		t.Fatalf("outer err %v, competitor err %v: exactly one must land", outer, other)
	}
	for _, err := range []error{outer, other} {
		if err != nil && !errors.Is(err, ErrTaskUnacked) && !errors.Is(err, ErrNotAccepting) {
			t.Fatalf("refusal %v", err)
		}
	}
	if msgs, _ := e.mailbox("w1").Read(runstore.Inbox); len(msgs) != 1 {
		t.Fatalf("inbox %d tasks", len(msgs))
	}
}

// A new worker that reuses a stopped worker's name starts with nothing owed:
// the earlier run's unacked task, result and question are never its own.
func TestMailbox_NewRunIgnoresPreviousRun(t *testing.T) {
	e := newEnv(t)
	old := e.worker("w1", "old brief") // never acked
	oldTask := e.task("w1")
	e.writeResult("w1", oldTask, "old answer")
	e.writeQFile("w1", oldTask, 1, "old question?")
	if _, err := e.deps.Ledger.CloseIfOpen(old.Run, delegationledger.CloseInfo{Reason: delegationledger.EndStop}); err != nil {
		t.Fatal(err)
	}
	s := e.worker("w1", "")
	if r, _ := Result(e.deps, "w1"); r.Status != ResultNone || r.Run != s.Run {
		t.Fatalf("result reads the earlier run: %+v", r)
	}
	if qs := e.questions("w1"); len(qs) != 0 {
		t.Fatalf("questions read the earlier run: %+v", qs)
	}
	if rows, _ := Inbox(e.deps, s.Run); len(rows) != 0 {
		t.Fatalf("inbox reads the earlier run: %+v", rows)
	}
	if res := e.wait("w1", time.Second); res.Outcome != OutcomeReady || res.Task != "" || res.Turn != 0 {
		t.Fatalf("wait reads the earlier run: %+v", res)
	}
	sent, err := Send(context.Background(), e.deps, "w1", "first of the new run")
	if err != nil || sent.Turn != 1 {
		t.Fatalf("first send of the new run: %+v %v", sent, err)
	}
	// Other side: the new run's own unacked task still refuses a send.
	if _, err := Send(context.Background(), e.deps, "w1", "second"); !errors.Is(err, ErrTaskUnacked) {
		t.Fatalf("err %v", err)
	}
	if rows, _ := Inbox(e.deps, s.Run); len(rows) != 1 || rows[0].ID != sent.Task {
		t.Fatalf("%+v", rows)
	}
	if rows, _ := Inbox(e.deps, old.Run); len(rows) != 1 || rows[0].ID != oldTask {
		t.Fatalf("the earlier run's inbox: %+v", rows)
	}
}
