package render

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// stepTitleBound caps a step line's title so one line stays one short event.
const stepTitleBound = 200

// Progress follows a run's clean.md and calls step once for each tool step
// (a fold) as it completes, until ctx ends. It then reads clean.md once more
// as final, so the last steps and a fold the run never closed are reported
// too. It is what delegate run --progress streams for Claude Code's Monitor.
func Progress(ctx context.Context, rf *runstore.RunFolder, poll time.Duration, step func(Part)) {
	if poll <= 0 {
		poll = time.Second
	}
	seen := 0
	pass := func(final bool) {
		data, _, err := rf.ReadFrom(runstore.FileCleanMD, 0)
		if err != nil {
			return
		}
		folds := 0
		for _, p := range ParseRun(string(data), final) {
			if p.Kind != PartFold {
				continue
			}
			if folds++; folds > seen {
				step(p)
			}
		}
		if folds > seen {
			seen = folds
		}
	}
	for {
		select {
		case <-ctx.Done():
			pass(true)
			return
		case <-time.After(poll):
			pass(false)
		}
	}
}

// StepLine is one step as a key=value line: its outcome and its title,
// quoted, on one line and bounded.
func StepLine(runID string, p Part) string {
	outcome := "ok"
	if p.Failed {
		outcome = "fail"
	}
	title := strings.Join(strings.Fields(p.Title), " ")
	if r := []rune(title); len(r) > stepTitleBound {
		title = string(r[:stepTitleBound]) + "..."
	}
	return fmt.Sprintf("run=%s state=step step=%s title=%q", runID, outcome, title)
}
