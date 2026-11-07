package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/daemon"
	"github.com/shakestzd/wipnote/internal/bgproc"
	"github.com/shakestzd/wipnote/internal/procsweep"
)

// Without --restart-all the doctor only reports: a hung writer is listed with
// the restart hint, and no restart is attempted.
func TestDoctorProcesses_ReportOnly(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".wipnote"), 0o755); err != nil {
		t.Fatal(err)
	}
	origDir := projectDirFlag
	projectDirFlag = root
	t.Cleanup(func() { projectDirFlag = origDir })
	const pid = 4000003 // only in the fake table below; never signalled
	now := time.Now()
	lease, _ := json.Marshal(daemon.LeaseRecord{Format: 2, Token: "t", PID: pid, StartedAt: now, HeartbeatAt: now})
	if err := os.WriteFile(daemon.LeasePath(root), lease, 0o644); err != nil {
		t.Fatal(err)
	}
	restarted := false
	orig := doctorInspectOpts
	t.Cleanup(func() { doctorInspectOpts = orig })
	doctorInspectOpts = func() bgproc.Options {
		return bgproc.Options{
			ExeNames: []string{"wipnote"}, ProbeTimeout: 300 * time.Millisecond,
			Lister: procsweep.ListerFunc(func(context.Context) ([]procsweep.RawProc, error) {
				return []procsweep.RawProc{{PID: pid, PPID: 1, Command: "wipnote --project-dir " + root + " _serve-child --headless"}}, nil
			}),
			RestartWriter: func(context.Context, string) error { restarted = true; return nil },
		}
	}

	var out bytes.Buffer
	if err := runDoctorProcesses(context.Background(), &out, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "hung") || !strings.Contains(out.String(), "--restart-all") {
		t.Fatalf("report:\n%s", out.String())
	}
	if restarted {
		t.Fatal("report-only mode restarted the writer")
	}
}
