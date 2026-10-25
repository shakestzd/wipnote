package daemon

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/provenance"
)

// findDeadPID returns a PID that is not currently alive.
func findDeadPID() int {
	for pid := 999999; pid > 90000; pid -= 7919 {
		if pidGone(pid) {
			return pid
		}
	}
	return 999983
}

// writeRecordForTest writes a lease record owned by a dead pid in a foreign
// namespace with a fresh heartbeat, after edit adjusts it.
func writeRecordForTest(t *testing.T, root string, edit func(*LeaseRecord)) []byte {
	t.Helper()
	rec := newLeaseRecord()
	rec.PID, rec.BootID, rec.PIDNamespace = findDeadPID(), "foreign-boot", "pid:[1]"
	if edit != nil {
		edit(&rec)
	}
	data, _ := json.Marshal(rec)
	if err := os.MkdirAll(filepath.Dir(LeasePath(root)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(LeasePath(root), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return data
}

func assertLeaseBytes(t *testing.T, root string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(LeasePath(root))
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("lease changed: err %v\n got %s\nwant %s", err, got, want)
	}
}

func TestLease_AcquireHoldRelease(t *testing.T) {
	root := t.TempDir()
	first, err := AcquireLease(root)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if _, err := AcquireLease(root); err != ErrLeaseHeld {
		t.Fatalf("second acquire while held: err = %v, want ErrLeaseHeld", err)
	}
	if err := first.Release(); err != nil {
		t.Fatalf("release: %v", err)
	}
	second, err := AcquireLease(root)
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	_ = second.Release()
}

// The record names its owner in a way that survives a namespace boundary
// and carries the binary version, so a later launcher can tell an older
// owner apart.
func TestLease_RecordNamesOwnerAndVersion(t *testing.T) {
	root := t.TempDir()
	l, err := AcquireLease(root)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()
	data, _ := os.ReadFile(LeasePath(root))
	rec, ok := parseLeaseRecord(data)
	if !ok || rec.Format != leaseFormat || rec.Version != provenance.CLIVersion() ||
		rec.PID != os.Getpid() || rec.Token != l.Token() || rec.HeartbeatAt.IsZero() {
		t.Fatalf("lease record = %s", data)
	}
	if runtime.GOOS == "linux" && (rec.BootID == "" || rec.PIDNamespace == "") {
		t.Fatalf("linux lease record lacks a namespace token: %s", data)
	}
}

// Staleness is the heartbeat age: a live pid in this namespace does not keep
// a lease whose heartbeat stopped.
func TestLease_StaleHeartbeatIsReclaimed(t *testing.T) {
	root := t.TempDir()
	writeRecordForTest(t, root, func(r *LeaseRecord) {
		*r = newLeaseRecord()
		r.HeartbeatAt = time.Now().Add(-time.Hour)
	})
	l, err := AcquireLease(root)
	if err != nil {
		t.Fatalf("acquire over stale heartbeat: %v", err)
	}
	defer l.Release()
}

// A fresh heartbeat from another namespace is held, even though its pid
// means nothing here and kill(pid, 0) would call it dead.
func TestLease_ForeignNamespaceFreshHeartbeatIsHeld(t *testing.T) {
	root := t.TempDir()
	want := writeRecordForTest(t, root, nil)
	if _, err := AcquireLease(root); err != ErrLeaseHeld {
		t.Fatalf("acquire over foreign fresh lease: err = %v, want ErrLeaseHeld", err)
	}
	assertLeaseBytes(t, root, want)
}

// In this namespace a pid the kernel reports gone is a crashed owner, so the
// lease need not wait out the stale window.
func TestLease_SameNamespaceCrashedOwnerIsReclaimed(t *testing.T) {
	if boot, ns := localNamespace(); boot == "" || ns == "" {
		t.Skip("no namespace token on this platform")
	}
	root := t.TempDir()
	writeRecordForTest(t, root, func(r *LeaseRecord) {
		r.BootID, r.PIDNamespace = localNamespace()
	})
	l, err := AcquireLease(root)
	if err != nil {
		t.Fatalf("acquire over crashed same-namespace owner: %v", err)
	}
	defer l.Release()
}

// An empty or partial lease never reads as free while it is younger than the
// stale window; once older, it is reclaimed.
func TestLease_HalfWrittenLeaseIsHeldUntilStale(t *testing.T) {
	for name, content := range map[string]string{"empty": "", "blank": "  \n", "partial": `{"format":2,"tok`} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			_ = os.MkdirAll(filepath.Join(root, ".wipnote"), 0o755)
			if err := os.WriteFile(LeasePath(root), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := AcquireLease(root); err != ErrLeaseHeld {
				t.Fatalf("fresh %s lease: err = %v, want ErrLeaseHeld", name, err)
			}
			old := time.Now().Add(-2 * leaseStaleAfter)
			_ = os.Chtimes(LeasePath(root), old, old)
			l, err := AcquireLease(root)
			if err != nil {
				t.Fatalf("stale %s lease: %v", name, err)
			}
			_ = l.Release()
		})
	}
}

// Release by an owner that lost the lease leaves the new owner's lease intact.
func TestLease_ReleaseByNonOwnerKeepsOwnersLease(t *testing.T) {
	root := t.TempDir()
	stalled, err := AcquireLease(root)
	if err != nil {
		t.Fatal(err)
	}
	writeRecordForTest(t, root, func(r *LeaseRecord) { // stalled owner's heartbeat stops
		r.Token, r.HeartbeatAt = stalled.Token(), time.Now().Add(-time.Hour)
	})
	owner, err := AcquireLease(root)
	if err != nil {
		t.Fatalf("reclaim: %v", err)
	}
	defer owner.Release()
	want, _ := os.ReadFile(LeasePath(root))
	if err := stalled.Release(); err != nil {
		t.Fatalf("non-owner release: %v", err)
	}
	assertLeaseBytes(t, root, want)
	if !owner.Verify() || stalled.Verify() {
		t.Fatalf("owner.Verify = %v, stalled.Verify = %v; want true, false", owner.Verify(), stalled.Verify())
	}
}

// The heartbeat refreshes heartbeat_at, and reports the lease lost once a
// reclaimer's record replaces it.
func TestLease_HeartbeatRefreshesAndDetectsLoss(t *testing.T) {
	old := leaseHeartbeat
	leaseHeartbeat = 20 * time.Millisecond
	t.Cleanup(func() { leaseHeartbeat = old })
	root := t.TempDir()
	l, err := AcquireLease(root)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()
	first := readRecordForTest(t, root).HeartbeatAt
	lost := make(chan struct{})
	l.Keep(t.Context(), func() { close(lost) })
	time.Sleep(150 * time.Millisecond)
	if !readRecordForTest(t, root).HeartbeatAt.After(first) {
		t.Fatal("heartbeat_at did not advance")
	}
	writeRecordForTest(t, root, nil)
	select {
	case <-lost:
	case <-time.After(2 * time.Second):
		t.Fatal("heartbeat did not report the lost lease")
	}
	if l.Held() || l.Verify() {
		t.Fatal("a lost lease still reports held")
	}
}

func readRecordForTest(t *testing.T, root string) LeaseRecord {
	t.Helper()
	data, _ := os.ReadFile(LeasePath(root))
	rec, ok := parseLeaseRecord(data)
	if !ok {
		t.Fatalf("unparsable lease: %s", data)
	}
	return rec
}

// A lock left by a crashed holder is broken once older than lockStaleAfter;
// a fresh lock held for the whole budget makes the acquirer back off.
func TestLease_MutationLockStaleAndBusy(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".wipnote")
	_ = os.MkdirAll(dir, 0o755)
	lockPath := filepath.Join(dir, leaseLockName)
	writeLock := func(at time.Time) {
		data, _ := json.Marshal(lockRecord{Token: "other", PID: findDeadPID(), BootID: "foreign", At: at})
		_ = os.WriteFile(lockPath, data, 0o644)
	}
	oldBudget := lockWaitBudget
	lockWaitBudget = 200 * time.Millisecond
	t.Cleanup(func() { lockWaitBudget = oldBudget })

	writeLock(time.Now())
	if _, err := AcquireLease(root); !errors.Is(err, ErrLeaseHeld) {
		t.Fatalf("acquire behind a fresh lock: err = %v, want ErrLeaseHeld", err)
	}
	writeLock(time.Now().Add(-time.Hour))
	l, err := AcquireLease(root)
	if err != nil {
		t.Fatalf("acquire behind a stale lock: %v", err)
	}
	defer l.Release()
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("lock left behind after acquire: %v", err)
	}
}

func TestVersionOlder(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"0.80.3", "0.81.0", true}, {"v0.81.0", "0.81.0", false}, {"0.82.0", "0.81.9", false},
		{"0.9", "0.10.0", true}, {"dev", "0.81.0", false}, {"0.81.0", "dev", false},
		{"0.81.0-rc1", "0.81.1", true}, {"", "0.1.0", false},
	}
	for _, c := range cases {
		if got := versionOlder(c.a, c.b); got != c.want {
			t.Errorf("versionOlder(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestOlderOwnerNotice_Version(t *testing.T) {
	root := t.TempDir()
	writeRecordForTest(t, root, func(r *LeaseRecord) { r.Version = "0.80.3" })
	if n := OlderOwnerNotice(root, "0.81.0"); n == "" {
		t.Fatal("no notice for an older-version owner")
	}
	if n := OlderOwnerNotice(root, "0.80.3"); n != "" {
		t.Fatalf("notice for a same-version owner: %q", n)
	}
}
