package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// writeTestCollectorPID creates a fake .collector-pid in sessDir.
func writeTestCollectorPID(t *testing.T, sessDir string, pid int) {
	t.Helper()
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatal(err)
	}
	pidPath := filepath.Join(sessDir, ".collector-pid")
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(pid)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeTestCollectorStartEvent creates a minimal events.ndjson with a
// collector_start event so ReadCollectorStatus can extract port + timestamp.
func writeTestCollectorStartEvent(t *testing.T, sessDir string, port int, ts time.Time) {
	t.Helper()
	line := map[string]any{
		"kind":      "collector_start",
		"canonical": "collector_start",
		"ts":        ts.UTC().Format(time.RFC3339Nano),
		"attrs": map[string]any{
			"port": port,
		},
	}
	b, err := json.Marshal(line)
	if err != nil {
		t.Fatal(err)
	}
	evPath := filepath.Join(sessDir, "events.ndjson")
	if err := os.WriteFile(evPath, append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestReadCollectorStatus_Live creates a temp session dir with the current
// process PID (guaranteed alive) and a valid collector_start event, then
// asserts Alive=true and Port is correctly read.
func TestReadCollectorStatus_Live(t *testing.T) {
	sessDir := t.TempDir()
	pid := os.Getpid()
	port := 14317
	startTime := time.Now().Add(-10 * time.Second)

	writeTestCollectorPID(t, sessDir, pid)
	writeTestCollectorStartEvent(t, sessDir, port, startTime)

	status, err := ReadCollectorStatus(sessDir)
	if err != nil {
		t.Fatalf("ReadCollectorStatus: %v", err)
	}
	if !status.Alive {
		t.Errorf("Alive = false, want true (using current PID %d)", pid)
	}
	if status.PID != pid {
		t.Errorf("PID = %d, want %d", status.PID, pid)
	}
	if status.Port != port {
		t.Errorf("Port = %d, want %d", status.Port, port)
	}
	if status.UptimeSec < 0 {
		t.Errorf("UptimeSec = %d, should be >= 0", status.UptimeSec)
	}
}

// TestReadCollectorStatus_Dead spawns and reaps a short-lived child, then
// uses the reaped PID — guaranteed dead at probe time — and asserts
// Alive=false. Avoids the PID-reuse flakiness of arbitrary high PIDs.
func TestReadCollectorStatus_Dead(t *testing.T) {
	sessDir := t.TempDir()
	deadPID := spawnAndReapChild(t)
	writeTestCollectorPID(t, sessDir, deadPID)

	status, err := ReadCollectorStatus(sessDir)
	if err != nil {
		t.Fatalf("ReadCollectorStatus: %v", err)
	}
	if status.Alive {
		t.Errorf("Alive = true, want false for reaped PID %d", deadPID)
	}
	if status.PID != deadPID {
		t.Errorf("PID = %d, want %d", status.PID, deadPID)
	}
}

// spawnAndReapChild starts /bin/true (or the platform equivalent), waits
// for it to exit, and returns its PID. The PID is dead at return time.
func spawnAndReapChild(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", "exit 0")
	if err := cmd.Run(); err != nil {
		t.Fatalf("spawn/reap child: %v", err)
	}
	return cmd.ProcessState.Pid()
}

// TestReadCollectorStatus_MissingPIDFile verifies that missing .collector-pid
// returns an error (no PID file means status is not available).
func TestReadCollectorStatus_MissingPIDFile(t *testing.T) {
	sessDir := t.TempDir()
	_, err := ReadCollectorStatus(sessDir)
	if err == nil {
		t.Error("expected error for missing .collector-pid, got nil")
	}
}

// TestCollectorStatusEndpoint verifies the /api/otel/status HTTP handler
// returns valid JSON CollectorStatus given a properly formed session dir.
func TestCollectorStatusEndpoint(t *testing.T) {
	// Build a temp project dir tree.
	projectDir := t.TempDir()
	sessionID := "test-sess-collector"
	sessDir := filepath.Join(projectDir, ".htmlgraph", "sessions", sessionID)

	pid := os.Getpid()
	port := 14318
	startTime := time.Now().Add(-5 * time.Second)

	writeTestCollectorPID(t, sessDir, pid)
	writeTestCollectorStartEvent(t, sessDir, port, startTime)

	handler := collectorStatusHandler(projectDir)

	req := httptest.NewRequest(http.MethodGet, "/api/otel/status?session="+sessionID, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var got CollectorStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.PID != pid {
		t.Errorf("PID = %d, want %d", got.PID, pid)
	}
	if got.Port != port {
		t.Errorf("Port = %d, want %d", got.Port, port)
	}
	if !got.Alive {
		t.Errorf("Alive = false, want true")
	}
}

// TestCollectorStatusEndpoint_400ForMissingParam verifies that omitting the
// session query parameter returns 400.
func TestCollectorStatusEndpoint_400ForMissingParam(t *testing.T) {
	handler := collectorStatusHandler(t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/api/otel/status", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// TestCollectorStatusEndpoint_RejectNonGet verifies that non-GET requests return 405.
func TestCollectorStatusEndpoint_RejectNonGet(t *testing.T) {
	handler := collectorStatusHandler(t.TempDir())
	req := httptest.NewRequest(http.MethodPost, "/api/otel/status?session=x", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}
