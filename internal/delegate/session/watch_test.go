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
