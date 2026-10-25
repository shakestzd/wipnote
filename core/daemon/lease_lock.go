package daemon

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"time"
)

// The lease mutation lock (bug-5ca53f70).
//
// Every change to an EXISTING lease file happens while holding this lock:
// a reclaim of a stale lease, the owner's heartbeat rewrite, and Release. The
// old reclaim was check, then remove, then create, and two racers that both
// judged the lease stale could each remove the other's fresh lease and both
// win. Under the lock the check and the replace are one step.
//
// WHY AN O_EXCL LOCK FILE AND NOT flock. flock is advisory and kernel-local.
// On a project bind-mounted into a container from another kernel (Docker
// Desktop's VM, for example), a flock taken on one side is invisible on the
// other, and on some network and FUSE file systems flock is emulated locally
// or not supported at all. That is exactly the setup this lease must survive.
// Exclusive create (O_CREAT|O_EXCL) is passed through to the backing file
// system by virtiofs, NFSv3+ and SMB, so it stays atomic across the boundary.
// The cost is that a lock file can outlive a crashed holder, so a lock older
// than lockStaleAfter is broken (breakStaleLock). The critical section is a
// read and a rename, milliseconds even on a slow mount, so ten seconds is a
// wide margin.
//
// The new lease content is always written to a temp file and renamed into
// place, so no reader ever sees a half-written lease.

const (
	leaseLockName   = "writer.lease.lock"
	leaseFileName   = "writer.lease.pid"
	legacyLeaseName = "writer.pid"
)

var (
	lockStaleAfter = 10 * time.Second
	// lockWaitBudget bounds how long an acquirer queues behind other racers.
	lockWaitBudget = 3 * time.Second
)

// lockRecord is the lock file content. Token tells a lock breaker whether
// the file it moved aside is the stale lock it judged or a fresh one.
type lockRecord struct {
	Token        string    `json:"token"`
	PID          int       `json:"pid"`
	BootID       string    `json:"boot_id,omitempty"`
	PIDNamespace string    `json:"pid_namespace,omitempty"`
	At           time.Time `json:"at"`
}

// lockLease takes the mutation lock for wipnoteDir, waiting at most budget.
// It returns ErrLeaseHeld when other racers kept the lock for the whole
// budget: one of them is taking or refreshing the lease.
func lockLease(wipnoteDir string, budget time.Duration) (unlock func(), err error) {
	path := filepath.Join(wipnoteDir, leaseLockName)
	boot, pidNS := localNamespace()
	rec := lockRecord{Token: newToken(), PID: os.Getpid(), BootID: boot, PIDNamespace: pidNS}
	deadline := time.Now().Add(budget)
	for {
		rec.At = time.Now().UTC()
		data, _ := json.Marshal(rec)
		err := leaseFS.CreateExcl(path, data)
		if err == nil {
			return func() { unlockLease(path, data) }, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
		breakStaleLock(wipnoteDir, path)
		if time.Now().After(deadline) {
			return nil, ErrLeaseHeld
		}
		time.Sleep(time.Duration(2+rand.IntN(8)) * time.Millisecond)
	}
}

// unlockLease removes the lock only if it is still the one this holder
// wrote, so a holder whose lock was broken never removes a successor's.
func unlockLease(path string, mine []byte) {
	if cur, err := leaseFS.ReadFile(path); err == nil && bytes.Equal(cur, mine) {
		_ = leaseFS.Remove(path)
	}
}

// breakStaleLock removes a lock whose holder is gone. It moves the lock to a
// private name first and checks that what it moved is the stale lock it
// read; if a fresh lock replaced it in between, it links that one back.
func breakStaleLock(wipnoteDir, path string) {
	data, err := leaseFS.ReadFile(path)
	if err != nil || !lockStale(path, data) {
		return
	}
	tomb := filepath.Join(wipnoteDir, ".writer.lease."+newToken()+".broken.lock")
	if leaseFS.Rename(path, tomb) != nil {
		return
	}
	if moved, err := leaseFS.ReadFile(tomb); err == nil && !bytes.Equal(moved, data) {
		_ = leaseFS.Link(tomb, path)
	}
	_ = leaseFS.Remove(tomb)
}

// lockStale judges a lock file by its recorded time, or by its mtime when
// it does not parse, plus the same-namespace crashed-holder shortcut.
func lockStale(path string, data []byte) bool {
	now := time.Now()
	var rec lockRecord
	if json.Unmarshal(data, &rec) != nil || rec.At.IsZero() {
		info, err := leaseFS.Stat(path)
		return err == nil && now.Sub(info.ModTime()) >= lockStaleAfter
	}
	if now.Sub(rec.At) >= lockStaleAfter {
		return true
	}
	boot, pidNS := localNamespace()
	same := boot != "" && pidNS != "" && rec.BootID == boot && rec.PIDNamespace == pidNS
	return same && pidGone(rec.PID)
}
