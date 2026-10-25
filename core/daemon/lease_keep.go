package daemon

import (
	"context"
	"time"
)

// Keep starts the owner's heartbeat. Every leaseHeartbeat it confirms the
// lease still names this owner and refreshes heartbeat_at. When the lease
// is gone or names another owner it marks the lease lost and calls onLost
// once; the owner must then stop serving. The heartbeat stops with ctx or
// Release.
func (l *Lease) Keep(ctx context.Context, onLost func()) {
	ctx, cancel := context.WithCancel(ctx)
	l.mu.Lock()
	l.stop = cancel
	l.mu.Unlock()
	go func() {
		ticker := time.NewTicker(leaseHeartbeat)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if l.beat() {
					continue
				}
				l.lost.Store(true)
				if onLost != nil {
					onLost()
				}
				return
			}
		}
	}()
}

// beat refreshes the heartbeat under the mutation lock and reports whether
// this process still owns the lease. A lock held by a racer for the whole
// budget skips one beat: the window is several beats wide.
func (l *Lease) beat() bool {
	unlock, err := lockLease(l.dir, leaseHeartbeat/2)
	if err != nil {
		return l.Verify()
	}
	defer unlock()
	if !l.Verify() {
		return false
	}
	l.mu.Lock()
	l.record.HeartbeatAt = time.Now().UTC()
	l.mu.Unlock()
	if err := l.writeRecord(); err != nil {
		return l.Verify()
	}
	return true
}

func (l *Lease) stopKeeping() {
	l.mu.Lock()
	stop := l.stop
	l.mu.Unlock()
	if stop != nil {
		stop()
	}
}
