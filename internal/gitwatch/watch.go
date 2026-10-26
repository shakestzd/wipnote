package gitwatch

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Watch exit codes, as wipnote ci watch exits.
const (
	WatchPassed       = 0 // settled with no failed check
	WatchFailed       = 1 // settled with a failed check
	WatchTimeout      = 2 // --timeout reached first
	WatchNotAvailable = 3 // gh missing or a gh read failed
)

// Watch cadence defaults and the floor for both intervals.
const (
	DefaultChecksInterval = 15 * time.Second
	DefaultSlowInterval   = 60 * time.Second
	DefaultWatchTimeout   = 60 * time.Minute
	MinWatchInterval      = 5 * time.Second
)

// WatchOptions configures Watch. Zero durations take the defaults and a nil
// Clock is RealClock. Emit receives the first snapshot and every later one
// that differs from the last emitted (FetchedAt ignored).
type WatchOptions struct {
	Arg            string
	ChecksInterval time.Duration
	SlowInterval   time.Duration
	Timeout        time.Duration
	Clock          Clock
	Emit           func(*Snapshot) error
}

// PR states that settle a watch whatever its checks say.
var closedStates = map[string]bool{"MERGED": true, "CLOSED": true}

// Watch follows arg's PR in the foreground until it settles: no check
// pending (and at least one reported), or the PR merged or closed. Checks
// and PR state are re-read every ChecksInterval while any check is pending;
// review, base, release and branch health every SlowInterval; everything
// every SlowInterval when nothing is pending. It returns a Watch* exit
// code; with no PR it emits one snapshot and returns WatchPassed. Only bad
// options, an Emit failure or a cancelled ctx are errors.
func Watch(ctx context.Context, r Runner, o WatchOptions) (int, error) {
	o, err := o.withDefaults()
	if err != nil {
		return 0, err
	}
	w := &watcher{r: r, o: o, deadline: o.Clock.Now().Add(o.Timeout), lastSlow: o.Clock.Now()}
	s, err := BuildSnapshot(ctx, r, o.Arg, o.Clock.Now)
	if err != nil {
		return 0, err
	}
	if err := w.emit(s); err != nil {
		return 0, err
	}
	switch {
	case !s.Available:
		return WatchNotAvailable, nil
	case s.PR == nil:
		return WatchPassed, nil
	}
	return w.loop(ctx, s)
}

func (o WatchOptions) withDefaults() (WatchOptions, error) {
	o.ChecksInterval = orDefault(o.ChecksInterval, DefaultChecksInterval)
	o.SlowInterval = orDefault(o.SlowInterval, DefaultSlowInterval)
	o.Timeout = orDefault(o.Timeout, DefaultWatchTimeout)
	if o.ChecksInterval < MinWatchInterval || o.SlowInterval < MinWatchInterval {
		return o, fmt.Errorf("watch intervals must be at least %s", MinWatchInterval)
	}
	if o.Timeout < 0 {
		return o, fmt.Errorf("watch timeout must not be negative")
	}
	if o.Clock == nil {
		o.Clock = RealClock{}
	}
	return o, nil
}

func orDefault(d, def time.Duration) time.Duration {
	if d == 0 {
		return def
	}
	return d
}

type watcher struct {
	r        Runner
	o        WatchOptions
	deadline time.Time
	lastSlow time.Time
	last     string // change key of the last emitted snapshot
}

// loop waits, re-reads and emits until s settles, the deadline passes, a
// read is not available or ctx ends.
func (w *watcher) loop(ctx context.Context, s *Snapshot) (int, error) {
	for {
		if code, done := settled(s); done {
			return code, nil
		}
		wait, remaining := w.interval(s), w.deadline.Sub(w.o.Clock.Now())
		if err := w.sleep(ctx, min(wait, remaining)); err != nil {
			return 0, err
		}
		if wait >= remaining {
			return WatchTimeout, nil
		}
		next, err := w.tick(ctx, s)
		if err != nil {
			return 0, err
		}
		if !next.Available {
			return WatchNotAvailable, nil
		}
		s = next
	}
}

// tick re-reads the fast section, and the slow ones when they are due, into
// a copy of prev, and emits it when it changed.
func (w *watcher) tick(ctx context.Context, prev *Snapshot) (*Snapshot, error) {
	now := w.o.Clock.Now
	s := *prev
	s.FetchedAt = now().UTC()
	if err := refreshPR(ctx, w.r, prev.PR.Number, &s); err != nil {
		if ctx.Err() != nil {
			return nil, err
		}
		s.Available, s.Reason = false, err.Error()
		return &s, w.emit(&s)
	}
	if now().Sub(w.lastSlow) >= w.o.SlowInterval {
		w.lastSlow = now()
		if err := refreshSlow(ctx, w.r, &s, now); err != nil {
			return nil, err
		}
	}
	return &s, w.emit(&s)
}

func (w *watcher) interval(s *Snapshot) time.Duration {
	if s.Counts.Pending > 0 {
		return w.o.ChecksInterval
	}
	return w.o.SlowInterval
}

func (w *watcher) sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-w.o.Clock.After(d):
		return nil
	}
}

// emit hands s to Emit when it differs from the last one emitted.
func (w *watcher) emit(s *Snapshot) error {
	key := changeKey(s)
	if key == w.last {
		return nil
	}
	w.last = key
	if w.o.Emit == nil {
		return nil
	}
	return w.o.Emit(s)
}

// changeKey is s as JSON with FetchedAt cleared.
func changeKey(s *Snapshot) string {
	c := *s
	c.FetchedAt = time.Time{}
	data, _ := json.Marshal(&c)
	return string(data)
}

// settled says whether s ends the watch and with which code: the PR merged
// or closed, or every reported check done. No checks reported yet is not
// settled (they may still register).
func settled(s *Snapshot) (int, bool) {
	if !closedStates[s.PR.State] && (s.Counts.Total == 0 || s.Counts.Pending > 0) {
		return 0, false
	}
	if s.Counts.Fail > 0 {
		return WatchFailed, true
	}
	return WatchPassed, true
}
