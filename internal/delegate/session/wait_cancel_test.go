package session

import (
	"context"
	"errors"
	"testing"
	"time"
)

// A caller's cancellation during a look (Ctrl-C, SIGTERM, a parent deadline)
// is returned as cancellation, never as the wait's own timeout (PR 296
// review).
func TestWait_CallerCancelDuringLookIsNotTimeout(t *testing.T) {
	e := newEnv(t)
	s := e.worker("w1", "brief")
	e.startTurn(s, "sid-1")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e.herdr.getDelay = 200 * time.Millisecond
	e.herdr.onGet = cancel
	res, err := Wait(ctx, e.deps, "w1", WaitOptions{Timeout: time.Hour, Poll: 5 * time.Millisecond})
	if !errors.Is(err, context.Canceled) || res.Outcome == OutcomeTimeout {
		t.Fatalf("cancel read as %+v, err %v", res, err)
	}
	// Other side: the wait's own deadline cutting a look short is still a
	// timeout with no error.
	e.herdr.onGet = nil
	res, err = Wait(context.Background(), e.deps, "w1", WaitOptions{Timeout: 20 * time.Millisecond, Poll: 5 * time.Millisecond})
	if err != nil || res.Outcome != OutcomeTimeout {
		t.Fatalf("own deadline: %+v, err %v", res, err)
	}
}
