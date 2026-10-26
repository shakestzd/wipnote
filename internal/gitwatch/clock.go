package gitwatch

import "time"

// Clock is the watch loop's time source: Now for timestamps and the
// deadline, After for every wait. Tests pass a FakeClock.
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

// RealClock is the wall clock.
type RealClock struct{}

// Now implements Clock.
func (RealClock) Now() time.Time { return time.Now() }

// After implements Clock.
func (RealClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// FakeClock never sleeps: After moves T forward by d and fires at once.
// Waits records every duration asked for, in order.
type FakeClock struct {
	T     time.Time
	Waits []time.Duration
}

// Now implements Clock.
func (c *FakeClock) Now() time.Time { return c.T }

// After implements Clock.
func (c *FakeClock) After(d time.Duration) <-chan time.Time {
	c.Waits = append(c.Waits, d)
	c.T = c.T.Add(d)
	ch := make(chan time.Time, 1)
	ch <- c.T
	return ch
}
