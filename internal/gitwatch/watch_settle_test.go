package gitwatch

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// noChecks is how gh pr checks answers for a PR with no CI (FetchChecks
// reads it as zero checks).
var noChecks = FakeResult{Stderr: "no checks reported on the 'feat-x' branch\n", Exit: 1}

var checkBlip = FakeResult{Stderr: "HTTP 502: Bad Gateway\n", Exit: 1}

func checksSeq(f *FakeRunner, rs ...FakeResult) { f.Seq[checksArgv] = rs }

func checkResult(stdout string) FakeResult { return FakeResult{Stdout: stdout, Exit: 8} }

func hasNote(s *Snapshot, prefix string) bool {
	for _, n := range s.Notes {
		if strings.HasPrefix(n, prefix) {
			return true
		}
	}
	return false
}

func lastEmit(w watchRun) *Snapshot { return w.emitted[len(w.emitted)-1] }

// Zero checks for the whole grace period (default 2m): read at 0s, 60s and
// 120s, then exit 0 with a "no checks reported" note on the final snapshot.
func TestWatch_NoChecksAfterGraceExitsZero(t *testing.T) {
	f := watchFake(t)
	checksSeq(f, noChecks)
	w := runWatch(context.Background(), f, "42", time.Hour)
	if w.err != nil || w.code != WatchPassed || waits(w.clock) != "1m0s 1m0s" {
		t.Fatalf("want exit 0 at the 2m grace end, got %d, %v, waits %s", w.code, w.err, waits(w.clock))
	}
	if last := lastEmit(w); !hasNote(last, "no checks reported") || last.Counts.Total != 0 {
		t.Errorf("final snapshot must carry the note: %+v", last.Notes)
	}
	if hasNote(w.emitted[0], "no checks reported") {
		t.Errorf("the note belongs to the final snapshot only")
	}
}

// The grace end caps the wait, and a timeout inside the grace still wins.
func TestWatch_NoChecksGraceFlag(t *testing.T) {
	f := watchFake(t)
	checksSeq(f, noChecks)
	w := runWatchWith(context.Background(), f, WatchOptions{Arg: "42", Timeout: time.Hour, NoChecksGrace: 90 * time.Second})
	if w.code != WatchPassed || waits(w.clock) != "1m0s 30s" || !hasNote(lastEmit(w), "no checks reported") {
		t.Errorf("90s grace: got %d, waits %s", w.code, waits(w.clock))
	}
	f = watchFake(t)
	checksSeq(f, noChecks)
	w = runWatch(context.Background(), f, "42", 90*time.Second)
	if w.code != WatchTimeout || hasNote(lastEmit(w), "no checks reported") {
		t.Errorf("timeout before grace end: got %d, want 2", w.code)
	}
	if _, err := Watch(context.Background(), f, WatchOptions{Arg: "42", Clock: &FakeClock{}, NoChecksGrace: -time.Second}); err == nil {
		t.Errorf("a negative grace must be an error")
	}
}

// A check that appears inside the grace period is watched as normal.
func TestWatch_ChecksAppearInsideGrace(t *testing.T) {
	f := watchFake(t)
	checksSeq(f, noChecks, checkResult(checksPending), checkResult(checksPass))
	w := runWatch(context.Background(), f, "42", time.Hour)
	if w.code != WatchPassed || waits(w.clock) != "1m0s 15s" || hasNote(lastEmit(w), "no checks reported") {
		t.Errorf("got %d, waits %s, notes %v", w.code, waits(w.clock), lastEmit(w).Notes)
	}
	f = watchFake(t)
	checksSeq(f, noChecks, checkResult(checksPending), checkResult(checksFail))
	if w = runWatch(context.Background(), f, "42", time.Hour); w.code != WatchFailed {
		t.Errorf("a check failing after appearing: got %d, want 1", w.code)
	}
}

// A cancelled check fails the watch like a failed one; skipping does not.
func TestWatch_CancelledCountsAsFailed(t *testing.T) {
	cases := []struct {
		name, checks string
		want         int
	}{
		{"cancel", checksJSON("CANCELLED", BucketCancel), WatchFailed},
		{"skipping", checksJSON("SKIPPED", BucketSkipping), WatchPassed},
		{"pass", checksPass, WatchPassed},
	}
	for _, c := range cases {
		f := watchFake(t, checksPending, c.checks)
		if w := runWatch(context.Background(), f, "42", time.Hour); w.err != nil || w.code != c.want {
			t.Errorf("%s: exit %d, %v; want %d", c.name, w.code, w.err, c.want)
		}
	}
}

// A failed fast read mid-watch keeps the last good snapshot with a note and
// retries on the next tick; a good read clears the note.
func TestWatch_ReadBlipIsRetried(t *testing.T) {
	f := watchFake(t)
	checksSeq(f, checkResult(checksPending), checkBlip, checkResult(checksPending), checkResult(checksPass))
	w := runWatch(context.Background(), f, "42", time.Hour)
	if w.err != nil || w.code != WatchPassed || waits(w.clock) != "15s 15s 15s" {
		t.Fatalf("want exit 0 after the retry, got %d, %v, waits %s", w.code, w.err, waits(w.clock))
	}
	blip := w.emitted[1]
	if !blip.Available || !hasNote(blip, "read failed (1/3): ") || blip.Counts.Pending != 1 {
		t.Errorf("the blip keeps the last good snapshot with a note: %+v", blip)
	}
	if !blip.FetchedAt.Equal(fixedNow().Add(15 * time.Second)) {
		t.Errorf("blip fetched_at = %v", blip.FetchedAt)
	}
	if hasNote(lastEmit(w), "read failed") || len(w.emitted) != 4 {
		t.Errorf("a good read clears the note; got %d emits, last notes %v", len(w.emitted), lastEmit(w).Notes)
	}
}

// Three failed fast reads in a row exit 3; a good read in between resets.
func TestWatch_ThreeBlipsInARowNotAvailable(t *testing.T) {
	f := watchFake(t)
	checksSeq(f, checkResult(checksPending), checkBlip, checkBlip, checkBlip)
	w := runWatch(context.Background(), f, "42", time.Hour)
	if w.code != WatchNotAvailable || waits(w.clock) != "15s 15s 15s" {
		t.Fatalf("want exit 3 on the third failure, got %d, waits %s", w.code, waits(w.clock))
	}
	last := lastEmit(w)
	if last.Available || !hasNote(last, "read failed (3/3): ") || !hasNote(w.emitted[2], "read failed (2/3): ") {
		t.Errorf("final snapshot: available %v, notes %v", last.Available, last.Notes)
	}
	f = watchFake(t)
	pending := checkResult(checksPending)
	checksSeq(f, pending, checkBlip, checkBlip, pending, checkBlip, checkBlip, checkResult(checksPass))
	if w = runWatch(context.Background(), f, "42", time.Hour); w.code != WatchPassed {
		t.Errorf("a good read must reset the count: got %d, waits %s", w.code, waits(w.clock))
	}
}

// Ctrl-C during a retry is still a cancelled ctx, not exit 3.
func TestWatch_CancelDuringRetry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	f := watchFake(t)
	checksSeq(f, checkResult(checksPending), checkBlip)
	_, err := Watch(ctx, f, WatchOptions{Arg: "42", Clock: &FakeClock{T: fixedNow()}, Emit: func(s *Snapshot) error {
		if hasNote(s, "read failed (1/3)") {
			cancel()
		}
		return nil
	}})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("want context.Canceled, got %v", err)
	}
}
