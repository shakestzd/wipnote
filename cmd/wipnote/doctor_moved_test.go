package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/observe/otel/collector"
)

func writeSessionRecord(t *testing.T, root, sid, body string) {
	t.Helper()
	dir := filepath.Join(root, ".wipnote", "sessions", sid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".collector-pid"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDoctorMovedSessions_ReportsOnlyLiveDivergentSessions(t *testing.T) {
	root := t.TempDir()
	lease, _ := json.Marshal(collector.SharedRecord{PID: os.Getpid(), Port: 5555})
	if err := os.MkdirAll(filepath.Join(root, ".wipnote"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(collector.SharedLeasePath(root), lease, 0o644); err != nil {
		t.Fatal(err)
	}
	me := os.Getpid()
	rec := func(export, launcher int, harness string) string {
		return strings.Join([]string{"1", "port=5555", "export_port=" + itoa(export), "launcher=" + itoa(launcher), "harness=" + harness, "shared", ""}, "\n")
	}
	writeSessionRecord(t, root, "moved", rec(5000, me, "codex"))
	writeSessionRecord(t, root, "same", rec(5555, me, "claude"))
	writeSessionRecord(t, root, "gone", rec(5000, 4000003, "claude")) // dead launcher pid; never signalled
	writeSessionRecord(t, root, "legacy", "1\nport=5555\nshared\n")

	var out bytes.Buffer
	n := writeMovedSessions(&out, root)
	got := out.String()
	if n != 1 {
		t.Fatalf("moved = %d, want 1:\n%s", n, got)
	}
	for _, want := range []string{"session moved", "exports to port 5000", "collector is on port 5555", "wipnote codex --continue"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	for _, not := range []string{"session same", "session gone", "session legacy"} {
		if strings.Contains(got, not) {
			t.Errorf("unexpected %q in:\n%s", not, got)
		}
	}
}

func TestDoctorMovedSessions_NoLeaseReportsNothing(t *testing.T) {
	root := t.TempDir()
	writeSessionRecord(t, root, "s", "1\nexport_port=5000\nlauncher=1\n")
	var out bytes.Buffer
	if n := writeMovedSessions(&out, root); n != 0 || out.Len() != 0 {
		t.Fatalf("n=%d out=%q", n, out.String())
	}
}
