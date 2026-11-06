package session

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// lineLog collects watch lines from the watch goroutine.
type lineLog struct {
	mu    sync.Mutex
	lines []string
}

func (l *lineLog) add(s string) { l.mu.Lock(); l.lines = append(l.lines, s); l.mu.Unlock() }

func (l *lineLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.lines...)
}

// waitLine blocks until a line contains want.
func (l *lineLog) waitLine(t *testing.T, want string) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
		for _, s := range l.snapshot() {
			if strings.Contains(s, want) {
				return
			}
		}
	}
	t.Fatalf("no line with %q in %v", want, l.snapshot())
}

func (e *env) startWatch(name string, timeout time.Duration) (*lineLog, chan WaitResult) {
	log, done := &lineLog{}, make(chan WaitResult, 1)
	go func() {
		res, err := Watch(context.Background(), e.deps, name, WaitOptions{Timeout: timeout, Poll: 5 * time.Millisecond}, log.add)
		if err != nil {
			e.t.Errorf("watch: %v", err)
		}
		done <- res
	}()
	return log, done
}

func TestWatch_SequenceDedupeAsksKeepsWatchingThenDone(t *testing.T) {
	e := newEnv(t)
	s := e.worker("w1", "brief")
	e.startTurn(s, "sid-1")
	log, done := e.startWatch("w1", 10*time.Second)
	log.waitLine(t, "state=working")
	time.Sleep(60 * time.Millisecond) // many polls: no repeat line
	task := e.task("w1")
	e.writeQFile("w1", task, 1, "Proceed\nwith the\tplan?")
	log.waitLine(t, `state=asks asks=1 q="Proceed with the plan?"`)
	select {
	case res := <-done:
		t.Fatalf("asks ended the watch: %+v", res)
	case <-time.After(50 * time.Millisecond):
	}
	e.writeResult("w1", task, "answer")
	res := <-done
	if res.Outcome != OutcomeDone || res.ExitCode() != 0 {
		t.Fatalf("%+v", res)
	}
	lines := log.snapshot()
	if len(lines) != 3 || !strings.HasSuffix(lines[0], "state=working") || !strings.HasSuffix(lines[2], "state=done") {
		t.Fatalf("lines %q", lines)
	}
}

func TestWatch_TimeoutPrintsRerunLineExit124(t *testing.T) {
	e := newEnv(t)
	s := e.worker("w1", "brief")
	e.startTurn(s, "sid-1")
	log, done := e.startWatch("w1", 100*time.Millisecond)
	res := <-done
	lines := log.snapshot()
	if res.ExitCode() != TimeoutExit || len(lines) == 0 || lines[len(lines)-1] != WatchRerun {
		t.Fatalf("%+v lines %q", res, lines)
	}
}

func TestWatch_GoneExits3(t *testing.T) {
	e := newEnv(t)
	s := e.unbound("w1", "brief")
	e.herdr.pane(PaneOf(s)).gone = true
	log, done := e.startWatch("w1", 5*time.Second)
	res := <-done
	if res.ExitCode() != GoneExit {
		t.Fatalf("%+v", res)
	}
	if ls := log.snapshot(); len(ls) != 1 || !strings.HasSuffix(ls[0], "state=gone") {
		t.Fatalf("lines %q", ls)
	}
}

func (e *env) startWatchAll(timeout time.Duration) (*lineLog, chan WaitResult) {
	log, done := &lineLog{}, make(chan WaitResult, 1)
	go func() {
		res, err := WatchAll(context.Background(), e.deps, WaitOptions{Timeout: timeout, Poll: 5 * time.Millisecond}, log.add)
		if err != nil {
			e.t.Errorf("watch all: %v", err)
		}
		done <- res
	}()
	return log, done
}

func TestWatchAll_OneWatchCoversEveryWorker(t *testing.T) {
	e := newEnv(t)
	s1 := e.worker("w1", "brief")
	e.startTurn(s1, "sid-1")
	s2 := e.worker("w2", "brief")
	e.startTurn(s2, "sid-1")
	log, done := e.startWatchAll(10 * time.Second)
	log.waitLine(t, "name=w1 state=working")
	log.waitLine(t, "name=w2 state=working")
	e.writeResult("w1", e.task("w1"), "r1")
	log.waitLine(t, "name=w1 state=done")
	select {
	case res := <-done:
		t.Fatalf("ended while w2 is live: %+v", res)
	case <-time.After(50 * time.Millisecond):
	}
	e.writeQFile("w2", e.task("w2"), 1, "Proceed?")
	log.waitLine(t, "name=w2 state=asks")
	e.writeResult("w2", e.task("w2"), "r2")
	log.waitLine(t, "name=w2 state=done")
	if res := <-done; res.ExitCode() != 0 {
		t.Fatalf("%+v", res)
	}
	n := 0
	for _, l := range log.snapshot() {
		if strings.Contains(l, "name=w1 state=done") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("w1 done printed %d times: %q", n, log.snapshot())
	}
}

func TestWatchAll_NoWorkersAndTimeout(t *testing.T) {
	e := newEnv(t)
	log, done := e.startWatchAll(time.Second)
	if res := <-done; res.ExitCode() != 0 || len(log.snapshot()) != 1 || log.snapshot()[0] != WatchNone {
		t.Fatalf("%+v %q", res, log.snapshot())
	}
	s := e.worker("w1", "brief")
	e.startTurn(s, "sid-1")
	log, done = e.startWatchAll(80 * time.Millisecond)
	if res := <-done; res.ExitCode() != TimeoutExit {
		t.Fatalf("%+v", res)
	}
	if ls := log.snapshot(); ls[len(ls)-1] != WatchRerun {
		t.Fatalf("%q", ls)
	}
}
