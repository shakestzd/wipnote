package gitwatch

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Watch exit codes, as wipnote ci watch exits.
const (
	WatchPassed       = 0 // settled with no failed check, or no checks after the grace
	WatchFailed       = 1 // settled with a failed or cancelled check
	WatchTimeout      = 2 // --timeout reached first
	WatchNotAvailable = 3 // the first read, or maxFailedReads in a row, failed
)

// Watch cadence defaults and the floor for both intervals.
const (
	DefaultChecksInterval = 15 * time.Second
	DefaultSlowInterval   = 60 * time.Second
	DefaultWatchTimeout   = 60 * time.Minute
	DefaultNoChecksGrace  = 2 * time.Minute
	MinWatchInterval      = 5 * time.Second
)

// maxFailedReads is how many failed fast reads in a row end a watch.
const maxFailedReads = 3

// noChecksNote marks the final snapshot of a PR still without checks when
// the grace period ends.
const noChecksNote = "no checks reported"

// WatchOptions configures Watch. Zero durations take the defaults and a nil
// Clock is RealClock. NoChecksGrace is how long from the start a PR with no
// checks is still watched for them to register. Emit receives the first snapshot and every later one
// that differs from the last emitted (FetchedAt ignored).
type WatchOptions struct {
	Arg            string
	ChecksInterval time.Duration
	SlowInterval   time.Duration
	Timeout        time.Duration
	NoChecksGrace  time.Duration
	Clock          Clock
	Emit           func(*Snapshot) error
}

// PR states that settle a watch whatever its checks say.
var closedStates = map[string]bool{"MERGED": true, "CLOSED": true}

// Watch follows arg's PR in the foreground until it settles: no check
// pending (and at least one reported), the PR merged or closed, or still no
// check when NoChecksGrace ends (exit 0, noted). A failed read after a good
// one keeps the last good snapshot with a note and is retried; only
// maxFailedReads in a row end the watch. Checks
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
	start := o.Clock.Now()
	w := &watcher{r: r, o: o, deadline: start.Add(o.Timeout), graceEnd: start.Add(o.NoChecksGrace), lastSlow: start}
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
	o.NoChecksGrace = orDefault(o.NoChecksGrace, DefaultNoChecksGrace)
	if o.ChecksInterval < MinWatchInterval || o.SlowInterval < MinWatchInterval {
		return o, fmt.Errorf("watch intervals must be at least %s", MinWatchInterval)
	}
	if o.Timeout < 0 || o.NoChecksGrace < 0 {
		return o, fmt.Errorf("watch timeout and no-checks grace must not be negative")
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
	graceEnd time.Time // when zero checks stops being worth waiting on
	lastSlow time.Time
	last     string // change key of the last emitted snapshot
	failed   int    // fast reads failed in a row
}

// loop waits, re-reads and emits until s settles, the deadline passes,
// maxFailedReads reads fail in a row or ctx ends. s is always the last
// good snapshot.
func (w *watcher) loop(ctx context.Context, s *Snapshot) (int, error) {
	for {
		if code, done := settled(s); done {
			return code, nil
		}
		if w.graceOver(s) {
			return WatchPassed, w.emit(withNote(s, noChecksNote))
		}
		wait, remaining := w.wait(s), w.deadline.Sub(w.o.Clock.Now())
		if err := w.sleep(ctx, min(wait, remaining)); err != nil {
			return 0, err
		}
		if wait >= remaining {
			return WatchTimeout, nil
		}
		next, readErr, err := w.tick(ctx, s)
		switch {
		case err != nil:
			return 0, err
		case readErr == nil:
			w.failed, s = 0, next
		default:
			if done, err := w.readFailed(s, readErr); done || err != nil {
				return WatchNotAvailable, err
			}
		}
	}
}

// tick re-reads the fast section, and the slow ones when they are due, into
// a copy of prev, and emits it when it changed. A failed fast read is
// readErr, with nothing emitted; err is only a cancelled ctx or Emit.
func (w *watcher) tick(ctx context.Context, prev *Snapshot) (next *Snapshot, readErr, err error) {
	now := w.o.Clock.Now
	s := *prev
	s.FetchedAt = now().UTC()
	if err := refreshPR(ctx, w.r, prev.PR.Number, &s); err != nil {
		if ctx.Err() != nil {
			return nil, nil, err
		}
		return nil, err, nil
	}
	if now().Sub(w.lastSlow) >= w.o.SlowInterval {
		w.lastSlow = now()
		if err := refreshSlow(ctx, w.r, &s, now); err != nil {
			return nil, nil, err
		}
	}
	return &s, nil, w.emit(&s)
}

// readFailed emits good with a "read failed (n/max)" note and says whether
// that was the last allowed failure, when the snapshot is also marked not
// available.
func (w *watcher) readFailed(good *Snapshot, readErr error) (bool, error) {
	w.failed++
	s := withNote(good, fmt.Sprintf("read failed (%d/%d): %s", w.failed, maxFailedReads, readErr))
	s.FetchedAt = w.o.Clock.Now().UTC()
	done := w.failed >= maxFailedReads
	if done {
		s.Available, s.Reason = false, readErr.Error()
	}
	return done, w.emit(s)
}

// graceOver says s, freshly read, still has no checks at the grace end.
func (w *watcher) graceOver(s *Snapshot) bool {
	return s.Counts.Total == 0 && w.failed == 0 && !w.o.Clock.Now().Before(w.graceEnd)
}

// wait is the poll interval for s, cut short at the grace end while no
// check is reported, so the grace ends on a fresh read.
func (w *watcher) wait(s *Snapshot) time.Duration {
	d := w.o.SlowInterval
	if s.Counts.Pending > 0 {
		d = w.o.ChecksInterval
	}
	if g := w.graceEnd.Sub(w.o.Clock.Now()); s.Counts.Total == 0 && g > 0 {
		d = min(d, g)
	}
	return d
}

// withNote is a copy of s with note added to its own Notes slice.
func withNote(s *Snapshot, note string) *Snapshot {
	c := *s
	c.Notes = append(append([]string(nil), s.Notes...), note)
	return &c
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
// settled (they may still register; see graceOver).
func settled(s *Snapshot) (int, bool) {
	if !closedStates[s.PR.State] && (s.Counts.Total == 0 || s.Counts.Pending > 0) {
		return 0, false
	}
	if anyFailed(s.Checks) {
		return WatchFailed, true
	}
	return WatchPassed, true
}

// anyFailed says whether a check failed or was cancelled; skipping is not.
func anyFailed(checks []Check) bool {
	for _, c := range checks {
		if c.Bucket == BucketFail || c.Bucket == BucketCancel {
			return true
		}
	}
	return false
}
