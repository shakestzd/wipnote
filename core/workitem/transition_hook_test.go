package workitem_test

import (
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/models"
)

// A LockedHook runs inside the item lock (issue #199): a concurrent
// transition on the same item cannot proceed until the hook returns, and the
// hook sees the pre-transition status and the state just written.
func TestTransitionHooksRunInsideItemLock(t *testing.T) {
	p := newTestProject(t)
	feat, err := p.Features.Create("Locked hook")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	completed := make(chan *models.Node, 1)
	var startPrev, completePrev models.NodeStatus
	var startedAt time.Time
	_, err = p.Features.Start(feat.ID, func(prev models.NodeStatus, n *models.Node) {
		startPrev, startedAt = prev, n.UpdatedAt
		if n.Status != models.StatusInProgress {
			t.Errorf("start hook saw status %q, want in-progress", n.Status)
		}
		go func() {
			n, err := p.Features.Complete(feat.ID, func(prev models.NodeStatus, _ *models.Node) {
				completePrev = prev
			})
			if err != nil {
				t.Errorf("Complete: %v", err)
			}
			completed <- n
		}()
		select {
		case <-completed:
			t.Error("Complete ran while Start's hook still held the item lock")
		case <-time.After(100 * time.Millisecond):
		}
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	done := <-completed
	if startPrev != models.StatusTodo {
		t.Errorf("start hook prev = %q, want todo", startPrev)
	}
	if completePrev != models.StatusInProgress {
		t.Errorf("complete hook prev = %q, want in-progress (ordered after the start)", completePrev)
	}
	if done == nil || !done.UpdatedAt.After(startedAt) {
		t.Errorf("completion UpdatedAt must order strictly after the start's (%v)", startedAt)
	}
}
