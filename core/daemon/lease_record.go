package daemon

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/shakestzd/wipnote/core/provenance"
)

// leaseFormat is the lease record format this binary writes. A legacy lease
// (format 1) is the bare pid that writer.pid held before bug-5ca53f70.
const leaseFormat = 2

// Lease timing. The owner refreshes heartbeat_at every leaseHeartbeat, and a
// record whose heartbeat is older than leaseStaleAfter is free to reclaim.
// The window is six beats wide so a slow bind mount, a loaded host or a short
// sleep cannot make a live owner look dead. They are variables so tests can
// shorten them; production never changes them.
var (
	leaseHeartbeat  = 5 * time.Second
	leaseStaleAfter = 30 * time.Second
	// leaseFutureTolerance bounds clock skew between the hosts that share a
	// project. A heartbeat further in the future than this is corrupt, not
	// fresh, so it cannot hold the lease forever.
	leaseFutureTolerance = 5 * time.Minute
)

// LeaseRecord is the content of the writer lease file. It names its owner in
// a way that survives a PID-namespace boundary: liveness is the heartbeat,
// never kill(pid, 0), because a host and a container that share a
// bind-mounted .wipnote each see the other's pid as dead.
type LeaseRecord struct {
	Format  int    `json:"format"`
	Version string `json:"version"`
	// Token is unique per acquisition. Ownership checks compare tokens, not
	// pids, so a recycled pid or a second namespace can never pass as owner.
	Token        string    `json:"token"`
	PID          int       `json:"pid"`
	BootID       string    `json:"boot_id,omitempty"`
	PIDNamespace string    `json:"pid_namespace,omitempty"`
	Hostname     string    `json:"hostname,omitempty"`
	StartedAt    time.Time `json:"started_at"`
	HeartbeatAt  time.Time `json:"heartbeat_at"`
}

// newLeaseRecord describes this process as a new owner.
func newLeaseRecord() LeaseRecord {
	host, _ := os.Hostname()
	boot, pidNS := localNamespace()
	now := time.Now().UTC()
	return LeaseRecord{
		Format: leaseFormat, Version: provenance.CLIVersion(), Token: newToken(),
		PID: os.Getpid(), BootID: boot, PIDNamespace: pidNS, Hostname: host,
		StartedAt: now, HeartbeatAt: now,
	}
}

// parseLeaseRecord decodes a lease. ok is false for an empty, partial or
// foreign file; the caller then judges it by its modification time.
func parseLeaseRecord(data []byte) (LeaseRecord, bool) {
	var rec LeaseRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return LeaseRecord{}, false
	}
	return rec, rec.Token != "" && !rec.HeartbeatAt.IsZero()
}

// fresh reports whether rec still holds the lease at now. Staleness is the
// heartbeat age alone, with one shortcut: when rec was written in THIS boot
// and pid namespace, a pid the kernel reports as gone is a crashed owner and
// need not wait out the window. Across namespaces the shortcut never applies.
func (rec LeaseRecord) fresh(now time.Time) bool {
	age := now.Sub(rec.HeartbeatAt)
	if age >= leaseStaleAfter || age < -leaseFutureTolerance {
		return false
	}
	return !(rec.sameNamespace() && pidGone(rec.PID))
}

// sameNamespace reports whether rec was written in this boot and pid
// namespace. An unknown namespace on either side is never the same.
func (rec LeaseRecord) sameNamespace() bool {
	boot, pidNS := localNamespace()
	return boot != "" && pidNS != "" && rec.BootID == boot && rec.PIDNamespace == pidNS
}

// fileFresh judges a lease file that does not parse: a half-written or
// foreign file younger than the stale window counts as held.
func fileFresh(info fs.FileInfo, now time.Time) bool {
	age := now.Sub(info.ModTime())
	return age < leaseStaleAfter && age > -leaseFutureTolerance
}

// pidGone reports whether the kernel has no process with pid. EPERM means a
// live process this user cannot signal, so it is not gone.
func pidGone(pid int) bool {
	if pid <= 0 {
		return true
	}
	return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
}

// legacyOwnerPID returns the pid of a live pre-heartbeat writer that holds
// .wipnote/writer.pid, or 0. A legacy owner has no heartbeat, so its pid is
// the only signal there is; it is trusted only when it names a live writer
// process (see legacyWriterAlive).
func legacyOwnerPID(wipnoteDir string) int {
	data, err := leaseFS.ReadFile(filepath.Join(wipnoteDir, legacyLeaseName))
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 || pid == os.Getpid() {
		return 0
	}
	if pidGone(pid) || !legacyWriterAlive(pid) {
		return 0
	}
	return pid
}

func newToken() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36) + "-" + strconv.Itoa(os.Getpid())
	}
	return hex.EncodeToString(b[:])
}
