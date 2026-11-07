package collector

// Shared per-project collector (issue #183).
//
// Before this, every `wipnote claude` launch spawned its own otel-collect, so
// a project with 22 sessions ran 22 collectors. A shared collector serves every
// session of one project on one port: each session exports to
// http://127.0.0.1:<port>/s/<session-id>, and the collector routes the request
// to that session's events.ndjson.
//
// The lease is <project>/.wipnote/otel-collector.lease.pid, a JSON
// SharedRecord. Launchers take <project>/.wipnote/otel-collector.lock
// (flock) around "read the lease, probe it, spawn if it is dead, write the
// lease", so concurrent launches spawn at most one collector. A lease is
// trusted only when the collector on its port answers /healthz with the
// lease's pid, which also defeats a recycled pid or port.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// HTTP paths the collector serves besides the OTLP signal paths.
const (
	HealthPath = "/healthz"
	AttachPath = "/attach"
	DetachPath = "/detach"
)

// sessionPathPrefix starts a shared collector's per-session OTLP path.
const sessionPathPrefix = "/s/"

// Health is the /healthz body. Mode is "shared" or "session".
type Health struct {
	PID      int    `json:"pid"`
	Mode     string `json:"mode"`
	Sessions int    `json:"sessions,omitempty"`
	Attached int    `json:"attached,omitempty"`
}

// SharedRecord is the content of the shared collector lease.
type SharedRecord struct {
	PID       int       `json:"pid"`
	Port      int       `json:"port"`
	StartedAt time.Time `json:"started_at"`
}

// SharedSpawnFn starts a shared collector bound to requestedPort (0 = any)
// and returns the port it listens on.
type SharedSpawnFn func(binPath, projectDir string, requestedPort int) (int, *os.Process, error)

// sharedLockTimeout bounds how long a launch waits for another launch that is
// spawning the collector right now.
var sharedLockTimeout = 5 * time.Second

// SharedLeasePath is the shared collector lease file for projectDir.
func SharedLeasePath(projectDir string) string {
	return filepath.Join(projectDir, ".wipnote", "otel-collector.lease.pid")
}

func sharedLockPath(projectDir string) string {
	return filepath.Join(projectDir, ".wipnote", "otel-collector.lock")
}

// SessionEndpointPath is the OTLP base path a session exports to on the
// shared collector. The exporter appends /v1/<signal> to it.
func SessionEndpointPath(sessionID string) string {
	return sessionPathPrefix + sessionID
}

var sessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// ValidSessionID reports whether sid is safe to use as a directory name.
func ValidSessionID(sid string) bool { return sessionIDPattern.MatchString(sid) }

// SplitSessionPath splits "/s/<sid>/v1/<signal>" into sid and "/v1/<signal>".
// ok is false for any other shape or an unsafe sid.
func SplitSessionPath(path string) (sid, rest string, ok bool) {
	tail, found := strings.CutPrefix(path, sessionPathPrefix)
	if !found {
		return "", "", false
	}
	idx := strings.IndexByte(tail, '/')
	if idx <= 0 || !ValidSessionID(tail[:idx]) {
		return "", "", false
	}
	return tail[:idx], tail[idx:], true
}

// ReadSharedRecord reads the shared collector lease for projectDir.
func ReadSharedRecord(projectDir string) (SharedRecord, error) {
	data, err := os.ReadFile(SharedLeasePath(projectDir))
	if err != nil {
		return SharedRecord{}, err
	}
	var rec SharedRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return SharedRecord{}, fmt.Errorf("parse shared collector lease: %w", err)
	}
	if rec.PID <= 0 || rec.Port <= 0 {
		return SharedRecord{}, errors.New("shared collector lease has no pid or port")
	}
	return rec, nil
}

// writeSharedRecord replaces the lease atomically (temp file + rename).
func writeSharedRecord(projectDir string, rec SharedRecord) error {
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	path := SharedLeasePath(projectDir)
	tmp := path + ".tmp-" + strconv.Itoa(os.Getpid())
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ProbeHealth asks the collector on port for /healthz and returns the pid it
// reports. Any failure — refused, timeout, bad body — is an error.
func ProbeHealth(port int, timeout time.Duration) (Health, error) {
	client := http.Client{Timeout: timeout}
	resp, err := client.Get("http://127.0.0.1:" + strconv.Itoa(port) + HealthPath)
	if err != nil {
		return Health{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Health{}, fmt.Errorf("health: HTTP %d", resp.StatusCode)
	}
	var h Health
	if err := json.NewDecoder(resp.Body).Decode(&h); err != nil {
		return Health{}, fmt.Errorf("health: decode: %w", err)
	}
	return h, nil
}

// sharedHealthy reports whether rec names a live collector that answers its
// own health probe with rec's pid.
func sharedHealthy(rec SharedRecord) bool {
	h, err := ProbeHealth(rec.Port, time.Second)
	return err == nil && h.PID == rec.PID
}

// EnsureShared returns the project's live shared collector, spawning one when
// the lease is missing or names a collector that does not answer. A respawn
// asks for the previous port first, so sessions already exporting there keep
// a live endpoint.
func EnsureShared(binPath, projectDir string, spawn SharedSpawnFn) (SharedRecord, error) {
	unlock, err := lockShared(projectDir)
	if err != nil {
		return SharedRecord{}, err
	}
	defer unlock()

	prev, err := ReadSharedRecord(projectDir)
	if err == nil && sharedHealthy(prev) {
		return prev, nil
	}
	port, proc, err := spawn(binPath, projectDir, prev.Port)
	if err != nil && prev.Port != 0 {
		port, proc, err = spawn(binPath, projectDir, 0)
	}
	if err != nil {
		return SharedRecord{}, fmt.Errorf("spawn shared collector: %w", err)
	}
	go func() { _, _ = proc.Wait() }() // reap our child; no-op for a non-child
	rec := SharedRecord{PID: proc.Pid, Port: port, StartedAt: time.Now().UTC()}
	if err := writeSharedRecord(projectDir, rec); err != nil {
		return SharedRecord{}, fmt.Errorf("write shared collector lease: %w", err)
	}
	return rec, nil
}

// lockShared takes the project's shared-collector flock, waiting up to
// sharedLockTimeout for a launch that holds it.
func lockShared(projectDir string) (func(), error) {
	if err := os.MkdirAll(filepath.Join(projectDir, ".wipnote"), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(sharedLockPath(projectDir), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(sharedLockTimeout)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() {
				_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
				_ = f.Close()
			}, nil
		}
		if time.Now().After(deadline) {
			_ = f.Close()
			return nil, fmt.Errorf("shared collector lock busy: %w", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// PIDAlive reports whether pid names a live process (EPERM counts as alive:
// the process exists but belongs to someone else).
func PIDAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
