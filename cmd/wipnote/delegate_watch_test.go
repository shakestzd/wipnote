package main

import (
	"bufio"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/internal/delegate/session"
)

// Each line reaches the reader before the command exits.
func TestDelegateWatchCmd_FlushesEachLineBeforeExit(t *testing.T) {
	mailWorker(t)
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	cmd := delegateWatchCmd()
	cmd.SetOut(w)
	exited := make(chan int, 1)
	go func() {
		c, err := runDelegateWatch(cmd, "w1", 1500*time.Millisecond)
		if err != nil {
			t.Errorf("watch: %v", err)
		}
		w.Close()
		exited <- c
	}()
	lines := make(chan string, 8)
	go func() {
		sc := bufio.NewScanner(r)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	select {
	case first := <-lines:
		select {
		case <-exited:
			t.Fatal("command exited before the first line was read")
		default:
		}
		if !strings.Contains(first, "name=w1 state=") {
			t.Fatalf("first line %q", first)
		}
	case <-time.After(1200 * time.Millisecond):
		t.Fatal("no line before the command ended")
	}
	if c := <-exited; c != session.TimeoutExit {
		t.Fatalf("exit %d", c)
	}
	var last string
	for l := range lines {
		last = l
	}
	if last != session.WatchRerun {
		t.Fatalf("last line %q", last)
	}
}

func TestDelegateWatchCmd_TimeoutExits124(t *testing.T) {
	mailWorker(t)
	code := captureWaitExit(t)
	out, err := execDelegate(t, "watch", "w1", "--timeout", "50ms")
	if err != nil || *code != session.TimeoutExit || !strings.Contains(out, session.WatchRerun) {
		t.Fatalf("exit %d err %v out %s", *code, err, out)
	}
}
