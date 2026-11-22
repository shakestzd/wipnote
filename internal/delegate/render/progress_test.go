package render

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

const foldOK = "<!-- fold closed: Bash: ls · exit 0 · 1 lines -->\n**Bash** `ls`\n<!-- /fold -->\n"

// Progress reports each tool step once as it completes, and at the end the
// fold the run never closed.
func TestProgress_EachStepOnceThenTheUnclosedFold(t *testing.T) {
	rf := newRun(t)
	write(t, rf, runstore.FileCleanMD, "### Assistant\n\nlooking\n"+foldOK)
	var mu sync.Mutex
	var got []Part
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		Progress(ctx, rf, time.Millisecond, func(p Part) {
			mu.Lock()
			got = append(got, p)
			mu.Unlock()
		})
		close(done)
	}()
	count := func() int { mu.Lock(); defer mu.Unlock(); return len(got) }
	waitFor(t, "the first step", func() bool { return count() == 1 })
	write(t, rf, runstore.FileCleanMD, "### Assistant\n\nlooking\n"+foldOK+
		"<!-- fold open: Bash: go test · exit 1 · 3 lines -->\n**Bash** `go test`\nFAIL\n<!-- /fold -->\n"+
		"<!-- fold closed: Read: a.go -->\n**Read** `a.go`\n")
	waitFor(t, "the failed step", func() bool { return count() == 2 })
	time.Sleep(20 * time.Millisecond)
	if n := count(); n != 2 {
		t.Fatalf("an unclosed fold was reported before the end: %d steps", n)
	}
	cancel()
	<-done
	mu.Lock()
	defer mu.Unlock()
	titles := []string{}
	for _, p := range got {
		titles = append(titles, p.Title)
	}
	want := "Bash: ls · exit 0 · 1 lines|Bash: go test · exit 1 · 3 lines|Read: a.go"
	if strings.Join(titles, "|") != want {
		t.Fatalf("steps = %q, want %q", strings.Join(titles, "|"), want)
	}
	if got[0].Failed || !got[1].Failed {
		t.Fatalf("failed flags = %v %v, want false true", got[0].Failed, got[1].Failed)
	}
}

// A step line is one key=value line with the title quoted and bounded.
func TestStepLine(t *testing.T) {
	ok := StepLine("dlg-1", Part{Kind: PartFold, Title: "Bash: ls · exit 0"})
	if ok != `run=dlg-1 state=step step=ok title="Bash: ls · exit 0"` {
		t.Fatalf("ok line = %s", ok)
	}
	fail := StepLine("dlg-1", Part{Kind: PartFold, Title: "Bash: x\ny " + strings.Repeat("z", 400), Failed: true})
	if !strings.HasPrefix(fail, `run=dlg-1 state=step step=fail title="Bash: x y zz`) || strings.Contains(fail, "\n") || len(fail) > 260 {
		t.Fatalf("fail line = %s", fail)
	}
}
