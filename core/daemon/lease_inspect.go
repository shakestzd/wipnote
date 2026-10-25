package daemon

import (
	"errors"
	"io/fs"
	"path/filepath"
	"time"
)

// LeaseStatus is what a reader can tell about a project's writer lease.
type LeaseStatus int

const (
	// LeaseFree means nobody holds the lease: absent, stale or unreadable
	// garbage older than the stale window.
	LeaseFree LeaseStatus = iota
	// LeaseHeld means a current-format owner holds it: a fresh heartbeat, or
	// a file that does not parse yet but is younger than the window.
	LeaseHeld
	// LeaseLegacyHeld means a live pre-heartbeat writer holds writer.pid.
	LeaseLegacyHeld
)

// LeaseState is a point-in-time reading of the lease.
type LeaseState struct {
	Status LeaseStatus
	// Record is the owner's record when Status is LeaseHeld and it parsed.
	Record *LeaseRecord
	// LegacyPID is the legacy owner's pid when Status is LeaseLegacyHeld.
	LegacyPID int
}

// Held reports whether anybody holds the lease.
func (s LeaseState) Held() bool { return s.Status != LeaseFree }

// InspectLease reads projectRoot's writer lease without changing it.
func InspectLease(projectRoot string) LeaseState {
	return inspectLeaseDir(filepath.Join(projectRoot, ".wipnote"), time.Now())
}

// inspectLeaseDir judges the lease in a .wipnote dir at now. A read error
// other than "missing" counts as held: never start a second writer on a
// guess.
func inspectLeaseDir(wipnoteDir string, now time.Time) LeaseState {
	path := filepath.Join(wipnoteDir, leaseFileName)
	data, err := leaseFS.ReadFile(path)
	switch {
	case err == nil:
		if rec, ok := parseLeaseRecord(data); ok {
			if rec.fresh(now) {
				return LeaseState{Status: LeaseHeld, Record: &rec}
			}
		} else if info, serr := leaseFS.Stat(path); serr != nil || fileFresh(info, now) {
			return LeaseState{Status: LeaseHeld}
		}
	case !errors.Is(err, fs.ErrNotExist):
		return LeaseState{Status: LeaseHeld}
	}
	if pid := legacyOwnerPID(wipnoteDir); pid > 0 {
		return LeaseState{Status: LeaseLegacyHeld, LegacyPID: pid}
	}
	return LeaseState{Status: LeaseFree}
}
