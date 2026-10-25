package daemon

import (
	"testing"
	"time"
)

// herdSize and herdRounds size the concurrent-starter tests. Every round
// starts herdSize real processes on one project and releases them together.
const (
	herdSize   = 32
	herdRounds = 4
)

// A stale lease must still admit exactly one server when many starters race
// to reclaim it (bug-5ca53f70). Before the fix, the check-remove-create
// reclaim let two or three racers each believe they had won.
func TestLeaseHerd_StaleLeaseOneServer(t *testing.T) {
	for r := 0; r < herdRounds; r++ {
		root := newHerdRoot(t)
		if err := writeStaleLeaseForTest(root); err != nil {
			t.Fatal(err)
		}
		h := startHerd(t, root, herdSize, time.Minute)
		assertOneServer(t, h, r)
		h.stop()
	}
}
