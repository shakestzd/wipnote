package hooks

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestActiveLineIfChanged_ConcurrentAnnouncesOnce(t *testing.T) {
	dir := t.TempDir()
	var wg sync.WaitGroup
	var won int32
	start := make(chan struct{})
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if activeLineIfChanged(dir, "sess", "feat-aaaa1111") != "" {
				atomic.AddInt32(&won, 1)
			}
		}()
	}
	close(start)
	wg.Wait()
	if won != 1 {
		t.Fatalf("want exactly 1 announcement, got %d", won)
	}
}

func TestActiveLineIfChanged_ChangeAndClear(t *testing.T) {
	dir := t.TempDir()
	if activeLineIfChanged(dir, "s", "feat-aaaa1111") == "" {
		t.Fatal("first item must announce")
	}
	if activeLineIfChanged(dir, "s", "feat-aaaa1111") != "" {
		t.Fatal("same item must not repeat")
	}
	if activeLineIfChanged(dir, "s", "bug-bbbb2222") == "" {
		t.Fatal("a new item must announce")
	}
	if activeLineIfChanged(dir, "s", "feat-aaaa1111") == "" {
		t.Fatal("switching back must announce again")
	}
	clearAnnouncedItem(dir, "s")
	if activeLineIfChanged(dir, "s", "feat-aaaa1111") == "" {
		t.Fatal("after a reset the item must announce again")
	}
}
