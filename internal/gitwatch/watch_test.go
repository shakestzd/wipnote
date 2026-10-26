package gitwatch

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

const prViewArgv = "gh pr view 42 --json " + prFields

func checksJSON(state, bucket string) string {
	return `[{"name":"build","state":"` + state + `","bucket":"` + bucket + `","workflow":"CI","event":"pull_request",` +
		`"startedAt":"2026-10-26T11:00:00Z","link":"https://example.test/run/1"}]`
}

var (
	checksPending = checksJSON("IN_PROGRESS", BucketPending)
	checksPass    = checksJSON("SUCCESS", BucketPass)
	checksFail    = checksJSON("FAILURE", BucketFail)
)

// watchFake answers every read of a full snapshot of PR 42; checks answers
// the checks reads in order (the last repeats).
func watchFake(t *testing.T, checks ...string) *FakeRunner {
	noReleaseSetting(t)
	f := snapshotReviewFake(t)
	for k, v := range releaseFake(t).Results {
		f.Results[k] = v
	}
	f.Results[baseMainArgv] = FakeResult{Stdout: fixture(t, "base_runs_green.json")}
	f.Seq = map[string][]FakeResult{}
	for _, c := range checks {
		f.Seq[checksArgv] = append(f.Seq[checksArgv], FakeResult{Stdout: c, Exit: 8})
	}
	return f
}

type watchRun struct {
	code    int
	err     error
	emitted []*Snapshot
	clock   *FakeClock
}

func runWatch(ctx context.Context, f *FakeRunner, arg string, timeout time.Duration) watchRun {
	return runWatchWith(ctx, f, WatchOptions{Arg: arg, Timeout: timeout})
}

// runWatchWith runs Watch with o on a fresh fake clock, recording emits.
func runWatchWith(ctx context.Context, f *FakeRunner, o WatchOptions) watchRun {
	w := watchRun{clock: &FakeClock{T: fixedNow()}}
	o.Clock = w.clock
	o.Emit = func(s *Snapshot) error { w.emitted = append(w.emitted, s); return nil }
	w.code, w.err = Watch(ctx, f, o)
	return w
}

func calls(f *FakeRunner, argv string) int {
	n := 0
	for _, c := range f.Calls {
		if c == argv {
			n++
		}
	}
	return n
}

func waits(c *FakeClock) string {
	parts := make([]string, 0, len(c.Waits))
	for _, d := range c.Waits {
		parts = append(parts, d.String())
	}
	return strings.Join(parts, " ")
}

// Pending checks: checks and PR state every 15s, the slow sections every
// 60s. Two minutes is the first read, seven fast ticks (15s..105s) and the
// final wait into the deadline; the slow sections read at 0s and 60s.
func TestWatch_CadencePerSection(t *testing.T) {
	f := watchFake(t, checksPending)
	w := runWatch(context.Background(), f, "42", 2*time.Minute)
	if w.err != nil || w.code != WatchTimeout {
		t.Fatalf("want timeout exit 2, got %d, %v", w.code, w.err)
	}
	fast := map[string]int{checksArgv: 8, prViewArgv: 8}
	slow := map[string]int{reviewViewArgv: 2, graphqlArgv("github.com"): 2, baseMainArgv: 2, releaseViewArgv: 2}
	for _, want := range []map[string]int{fast, slow} {
		for argv, n := range want {
			if got := calls(f, argv); got != n {
				t.Errorf("%s: %d calls, want %d", argv[:min(30, len(argv))], got, n)
			}
		}
	}
	if got := waits(w.clock); got != "15s 15s 15s 15s 15s 15s 15s 15s" {
		t.Errorf("waits = %s", got)
	}
}

// With no check pending (none registered yet) everything polls every 60s.
func TestWatch_BacksOffWhenNothingPending(t *testing.T) {
	f := watchFake(t, `[]`)
	w := runWatchWith(context.Background(), f, WatchOptions{Arg: "42", Timeout: 3 * time.Minute,
		NoChecksGrace: 10 * time.Minute})
	if w.err != nil || w.code != WatchTimeout {
		t.Fatalf("want timeout exit 2, got %d, %v", w.code, w.err)
	}
	if got := waits(w.clock); got != "1m0s 1m0s 1m0s" {
		t.Errorf("waits = %s, want three 60s waits", got)
	}
	if calls(f, checksArgv) != 3 || calls(f, reviewViewArgv) != 3 {
		t.Errorf("checks %d, review %d reads; want 3 each", calls(f, checksArgv), calls(f, reviewViewArgv))
	}
}

func TestWatch_SettlesOnMerged(t *testing.T) {
	f := watchFake(t, checksPending)
	merged := strings.Replace(fixture(t, "pr_view_github.json"), `"OPEN"`, `"MERGED"`, 1)
	f.Seq[prViewArgv] = []FakeResult{{Stdout: fixture(t, "pr_view_github.json")}, {Stdout: merged}}
	w := runWatch(context.Background(), f, "42", time.Hour)
	if w.err != nil || w.code != WatchPassed || waits(w.clock) != "15s" {
		t.Fatalf("want exit 0 after one tick, got %d, %v, waits %s", w.code, w.err, waits(w.clock))
	}
	if last := w.emitted[len(w.emitted)-1]; last.PR.State != "MERGED" {
		t.Errorf("last emitted state = %s", last.PR.State)
	}
}

func TestWatch_SettlesOnAllPassed(t *testing.T) {
	f := watchFake(t, checksPending, checksPending, checksPass)
	w := runWatch(context.Background(), f, "42", time.Hour)
	if w.err != nil || w.code != WatchPassed || waits(w.clock) != "15s 15s" {
		t.Fatalf("want exit 0 at 30s, got %d, %v, waits %s", w.code, w.err, waits(w.clock))
	}
	if len(w.emitted) != 2 || w.emitted[1].Counts.Pass != 1 {
		t.Errorf("want 2 emits (pending, passed), got %d", len(w.emitted))
	}
}

func TestWatch_SettlesOnFailure(t *testing.T) {
	f := watchFake(t, checksPending, checksFail)
	w := runWatch(context.Background(), f, "42", time.Hour)
	if w.err != nil || w.code != WatchFailed {
		t.Fatalf("want exit 1, got %d, %v", w.code, w.err)
	}
	if w.emitted[len(w.emitted)-1].Checks[0].Required != true {
		t.Errorf("a fast refresh must keep the review read's required marks")
	}
}

// A first read that has already settled exits without waiting.
func TestWatch_AlreadySettled(t *testing.T) {
	w := runWatch(context.Background(), watchFake(t, checksFail), "42", time.Hour)
	if w.code != WatchFailed || len(w.clock.Waits) != 0 || len(w.emitted) != 1 {
		t.Errorf("got %d, waits %s, %d emits", w.code, waits(w.clock), len(w.emitted))
	}
}

func TestWatch_NotAvailable(t *testing.T) {
	f := &FakeRunner{Results: map[string]FakeResult{prViewArgv: {Err: ErrNotAvailable}}}
	w := runWatch(context.Background(), f, "42", time.Hour)
	if w.err != nil || w.code != WatchNotAvailable || len(w.emitted) != 1 || w.emitted[0].Available {
		t.Fatalf("want exit 3 with one unavailable emit, got %d, %v, %d", w.code, w.err, len(w.emitted))
	}
	// A gh read that fails mid-watch is not available too.
	f = watchFake(t, checksPending, checksPending)
	f.Seq[checksArgv][1] = FakeResult{Stderr: "HTTP 502: Bad Gateway", Exit: 1}
	w = runWatch(context.Background(), f, "42", time.Hour)
	if w.code != WatchNotAvailable || w.emitted[len(w.emitted)-1].Available || waits(w.clock) != "15s 15s 15s" {
		t.Errorf("mid-watch failure: got %d, waits %s; want exit 3 after three failed reads", w.code, waits(w.clock))
	}
}

// Emit only on change: identical reads (FetchedAt aside) are not emitted.
func TestWatch_EmitsOnlyOnChange(t *testing.T) {
	f := watchFake(t, checksPending, checksPending, checksPending, checksPass)
	w := runWatch(context.Background(), f, "42", time.Hour)
	if w.code != WatchPassed || len(w.emitted) != 2 {
		t.Fatalf("want 2 emits over 4 reads, got %d (exit %d)", len(w.emitted), w.code)
	}
	if !w.emitted[1].FetchedAt.Equal(fixedNow().Add(45 * time.Second)) {
		t.Errorf("second emit fetched_at = %v", w.emitted[1].FetchedAt)
	}
}

func TestWatch_NoPROneSnapshot(t *testing.T) {
	f := watchFake(t, checksPending)
	f.Results["gh pr list --head nobody --state all --limit 1 --json number"] = FakeResult{Stdout: `[]`}
	w := runWatch(context.Background(), f, "nobody", time.Hour)
	if w.err != nil || w.code != WatchPassed || len(w.emitted) != 1 || len(w.clock.Waits) != 0 {
		t.Fatalf("no PR: got %d, %v, %d emits, waits %s", w.code, w.err, len(w.emitted), waits(w.clock))
	}
	if w.emitted[0].PR != nil || w.emitted[0].Base == nil {
		t.Errorf("no-PR snapshot still carries base: %+v", w.emitted[0])
	}
}

func TestWatch_CancelStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	clock := &FakeClock{T: fixedNow()}
	_, err := Watch(ctx, watchFake(t, checksPending), WatchOptions{Arg: "42", Clock: clock,
		Emit: func(*Snapshot) error { cancel(); return nil }})
	if !errors.Is(err, context.Canceled) || len(clock.Waits) != 0 {
		t.Errorf("want context.Canceled before any wait, got %v, waits %s", err, waits(clock))
	}
}

func TestWatch_IntervalFloor(t *testing.T) {
	f := watchFake(t, checksPending)
	for _, o := range []WatchOptions{{ChecksInterval: time.Second}, {SlowInterval: 4 * time.Second}} {
		o.Arg, o.Clock = "42", &FakeClock{}
		if _, err := Watch(context.Background(), f, o); err == nil || len(f.Calls) != 0 {
			t.Errorf("%+v: want an error before any read, got %v, %d calls", o, err, len(f.Calls))
		}
	}
	o := WatchOptions{Arg: "42", Clock: &FakeClock{}, ChecksInterval: MinWatchInterval, SlowInterval: MinWatchInterval,
		Timeout: time.Minute}
	if _, err := Watch(context.Background(), f, o); err != nil {
		t.Errorf("the floor itself is allowed: %v", err)
	}
}
