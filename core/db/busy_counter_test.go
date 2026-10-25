package db

import (
	"errors"
	"fmt"
	"sync"
	"testing"
)

// TestBusyCounterClassifiesErrors is the slice-10 TDD anchor: a wrapped
// SQLite lock error must increment the right subsystem counter and leave
// other counters alone. Covers each first-party label plus the external
// label.
func TestBusyCounterClassifiesErrors(t *testing.T) {
	cases := []struct {
		name       string
		subsystem  BusySubsystem
		err        error
		wantHit    bool
		wantCount  int64
		otherCount int64 // every other subsystem must remain zero
	}{
		{
			name:      "hook_writer with SQLITE_BUSY",
			subsystem: SubsystemHookWriter,
			err:       errors.New("SQLITE_BUSY: database is locked"),
			wantHit:   true,
			wantCount: 1,
		},
		{
			name:      "indexer with database-is-locked fragment",
			subsystem: SubsystemIndexer,
			err:       errors.New("query failed: database is locked"),
			wantHit:   true,
			wantCount: 1,
		},
		{
			name:      "cli_mutation with synthetic helper",
			subsystem: SubsystemCLIMutation,
			err:       newSyntheticBusyError("SQLITE_BUSY (5)"),
			wantHit:   true,
			wantCount: 1,
		},
		{
			name:      "writer_service with locked substring",
			subsystem: SubsystemWriterService,
			err:       errors.New("transaction is locked"),
			wantHit:   true,
			wantCount: 1,
		},
		{
			name:      "external label still counted",
			subsystem: SubsystemExternal,
			err:       errors.New("SQLITE_BUSY"),
			wantHit:   true,
			wantCount: 1,
		},
		{
			name:      "nil error is not a hit",
			subsystem: SubsystemIndexer,
			err:       nil,
			wantHit:   false,
			wantCount: 0,
		},
		{
			name:      "unrelated error is not a hit",
			subsystem: SubsystemIndexer,
			err:       errors.New("syntax error near"),
			wantHit:   false,
			wantCount: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ResetBusyCounters()

			hit := Record(tc.subsystem, tc.err)
			if hit != tc.wantHit {
				t.Errorf("Record hit = %v, want %v", hit, tc.wantHit)
			}
			if got := busyCount(tc.subsystem); got != tc.wantCount {
				t.Errorf("busyCount(%s) = %d, want %d", tc.subsystem, got, tc.wantCount)
			}

			// Verify no other subsystem was incremented.
			for _, other := range []BusySubsystem{
				SubsystemHookWriter, SubsystemIndexer, SubsystemCLIMutation,
				SubsystemWriterService, SubsystemExternal,
			} {
				if other == tc.subsystem {
					continue
				}
				if got := busyCount(other); got != 0 {
					t.Errorf("busyCount(%s) = %d, want 0 (spilled from %s)", other, got, tc.subsystem)
				}
			}
		})
	}
}

// TestIsBusyError covers the detection heuristic directly so a future
// driver upgrade that changes the error text surfaces here rather than
// silently degrading the counter.
func TestIsBusyError(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{errors.New("SQLITE_BUSY"), true},
		{errors.New("SQLITE_BUSY: database is locked"), true},
		{errors.New("database is locked"), true},
		{errors.New("table foo is locked"), true}, // permissive: matches "locked"
		{errors.New("connection refused"), false},
		{errors.New("constraint violation"), false},
	}
	for _, tc := range tests {
		got := IsBusyError(tc.err)
		if got != tc.want {
			t.Errorf("IsBusyError(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}

// TestRecord_Concurrent verifies the counter is safe under concurrent
// load — the stress fixture in cmd/wipnote exercises this for real, but
// having a focused unit test here catches a race regression even when
// the heavy stress test is skipped (short mode).
func TestRecord_Concurrent(t *testing.T) {
	ResetBusyCounters()

	const (
		workers   = 32
		perWorker = 1000
		wantTotal = int64(workers * perWorker)
		subsystem = SubsystemHookWriter
	)
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			err := newSyntheticBusyError("SQLITE_BUSY")
			for j := 0; j < perWorker; j++ {
				Record(subsystem, err)
			}
		}()
	}
	wg.Wait()

	got := busyCount(subsystem)
	if got != wantTotal {
		t.Errorf("after %d concurrent records, busyCount(%s) = %d, want %d",
			wantTotal, subsystem, got, wantTotal)
	}
}

// TestBusyCounts_Snapshot verifies BusyCounts returns only non-zero
// counters and does not expose the underlying map. Returned map must be
// independent — caller mutations must not affect future reads.
func TestBusyCounts_Snapshot(t *testing.T) {
	ResetBusyCounters()
	busyErr := newSyntheticBusyError("")
	Record(SubsystemHookWriter, busyErr)
	Record(SubsystemIndexer, busyErr)
	Record(SubsystemIndexer, busyErr)

	snap := BusyCounts()
	if snap[SubsystemHookWriter] != 1 {
		t.Errorf("snapshot hook_writer = %d, want 1", snap[SubsystemHookWriter])
	}
	if snap[SubsystemIndexer] != 2 {
		t.Errorf("snapshot indexer = %d, want 2", snap[SubsystemIndexer])
	}
	if _, ok := snap[SubsystemCLIMutation]; ok {
		t.Errorf("snapshot includes zero-valued cli_mutation: %v", snap)
	}

	// Mutate the snapshot — must not affect the live counter.
	snap[SubsystemHookWriter] = 999
	if busyCount(SubsystemHookWriter) != 1 {
		t.Errorf("live counter was modified by snapshot mutation")
	}
}

// TestFirstPartyBusyTotal_ExcludesExternal is the launch-criterion
// invariant: external BUSY events do NOT contribute to the gate signal.
// A regression that lumps external counts into the first-party total
// would silently let the stress fixture pass while real producers
// degrade.
func TestFirstPartyBusyTotal_ExcludesExternal(t *testing.T) {
	ResetBusyCounters()
	busyErr := newSyntheticBusyError("")

	// Bump every first-party label once.
	for _, s := range FirstPartySubsystems {
		Record(s, busyErr)
	}
	// And bump external several times.
	for i := 0; i < 100; i++ {
		Record(SubsystemExternal, busyErr)
	}

	total := FirstPartyBusyTotal()
	wantTotal := int64(len(FirstPartySubsystems))
	if total != wantTotal {
		t.Errorf("FirstPartyBusyTotal = %d, want %d (external must be excluded)",
			total, wantTotal)
	}
	if got := busyCount(SubsystemExternal); got != 100 {
		t.Errorf("busyCount(external) = %d, want 100", got)
	}
}

// guardExample shows the canonical call-site pattern callers use to
// classify a query error. Documented here so reviewers see the intended
// API without spelunking through producer packages.
func ExampleRecord() {
	subsystem := SubsystemIndexer
	err := errors.New("SQLITE_BUSY: database is locked")
	if Record(subsystem, err) {
		// BUSY hit — caller may emit a structured log line.
		fmt.Println("busy classified to", subsystem)
	}
	// Output: busy classified to indexer
}

// busyCount returns the current count for one subsystem.
func busyCount(subsystem BusySubsystem) int64 { return BusyCounts()[subsystem] }

// syntheticBusyError is a BUSY error that needs no SQLite driver; its message
// carries the canonical fragment so IsBusyError matches it.
type syntheticBusyError struct{ msg string }

func (e *syntheticBusyError) Error() string { return e.msg }

func newSyntheticBusyError(detail string) error {
	if detail == "" {
		detail = "SQLITE_BUSY: database is locked"
	}
	return &syntheticBusyError{msg: detail}
}
