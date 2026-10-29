package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/daemon"
	"github.com/shakestzd/wipnote/core/hooks"
)

// stubEnsure replaces the daemon ensure for one test.
func stubEnsure(t *testing.T, fn func(ctx context.Context) (string, error)) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".wipnote"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := ensureDaemonFn
	ensureDaemonFn = func(ctx context.Context, _, _ string, _ int) (string, error) { return fn(ctx) }
	t.Cleanup(func() { ensureDaemonFn = old })
	return root
}

// The launcher never waits longer than the join budget for a daemon that is
// still starting, and says nothing about it (bug-06d2c91d).
func TestDaemonEnsure_StartingDaemonNeverBlocksLaunch(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	root := stubEnsure(t, func(ctx context.Context) (string, error) {
		<-release
		return "", daemon.ErrDaemonStarting
	})

	var out bytes.Buffer
	start := time.Now()
	env := startDaemonEnsure(root).sessionEnv(&out, 50*time.Millisecond)
	if elapsed := time.Since(start); elapsed > 300*time.Millisecond {
		t.Fatalf("launcher waited %v for the daemon, want at most the join budget", elapsed)
	}
	if env != nil || out.Len() != 0 {
		t.Fatalf("env=%v out=%q, want no env and no output while the daemon starts", env, out.String())
	}
}

func TestDaemonEnsure_StillStartingIsSilent(t *testing.T) {
	root := stubEnsure(t, func(context.Context) (string, error) { return "", daemon.ErrDaemonStarting })
	var out bytes.Buffer
	if env := startDaemonEnsure(root).sessionEnv(&out, time.Second); env != nil || out.Len() != 0 {
		t.Fatalf("env=%v out=%q, want silence for a daemon that is starting", env, out.String())
	}
}

// A real failure gets one quiet line, not the old two-line warning.
func TestDaemonEnsure_RealFailureIsOneQuietLine(t *testing.T) {
	root := stubEnsure(t, func(context.Context) (string, error) {
		return "", errors.New("daemon: spawn: permission denied")
	})
	var out bytes.Buffer
	if env := startDaemonEnsure(root).sessionEnv(&out, time.Second); env != nil {
		t.Fatalf("env = %v, want none after a failure", env)
	}
	got := out.String()
	if strings.Count(got, "\n") != 1 || !strings.Contains(got, "permission denied") {
		t.Fatalf("output = %q, want one line naming the cause", got)
	}
	if strings.Contains(got, "WITHOUT") {
		t.Fatalf("output = %q still uses the alarming warning", got)
	}
}

func TestDaemonEnsure_ReadyDaemonIsPublished(t *testing.T) {
	root := stubEnsure(t, func(context.Context) (string, error) { return "/tmp/x.sock", nil })
	var out bytes.Buffer
	env := startDaemonEnsure(root).sessionEnv(&out, time.Second)
	if len(env) != 1 || env[0] != hooks.DaemonSocketEnv+"=/tmp/x.sock" {
		t.Fatalf("env = %v, want the socket published", env)
	}
}

func TestDaemonEnsure_NotAProjectIsSilent(t *testing.T) {
	var out bytes.Buffer
	if env := startDaemonEnsure(t.TempDir()).sessionEnv(&out, time.Second); env != nil || out.Len() != 0 {
		t.Fatalf("env=%v out=%q, want nothing outside a wipnote project", env, out.String())
	}
}

// A lease held by an older wipnote gets exactly one line, whatever the
// ensure returned (bug-5ca53f70).
func TestDaemonEnsure_OlderOwnerIsOneLine(t *testing.T) {
	oldVersion := version
	version = "0.81.0"
	t.Cleanup(func() { version = oldVersion })
	for name, ensureErr := range map[string]error{
		"attached":     nil,
		"legacy owner": fmt.Errorf("%w (pid 42)", daemon.ErrDaemonLegacyOwner),
	} {
		root := stubEnsure(t, func(context.Context) (string, error) {
			if ensureErr != nil {
				return "", ensureErr
			}
			return "/tmp/x.sock", nil
		})
		if ensureErr == nil {
			writeOlderLeaseForTest(t, root)
		}
		var out bytes.Buffer
		startDaemonEnsure(root).sessionEnv(&out, time.Second)
		if got := out.String(); strings.Count(got, "older wipnote") != 1 {
			t.Fatalf("%s: output = %q, want one older-owner line", name, got)
		}
	}
}

// writeOlderLeaseForTest leaves a live-looking lease from version 0.0.1.
func writeOlderLeaseForTest(t *testing.T, root string) {
	t.Helper()
	now := time.Now().UTC()
	rec := daemon.LeaseRecord{Format: 2, Version: "0.0.1", Token: "t", PID: 1, BootID: "b", PIDNamespace: "n", StartedAt: now, HeartbeatAt: now}
	data, _ := json.Marshal(rec)
	if err := os.WriteFile(daemon.LeasePath(root), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// A container worker never starts or dials a work-item daemon: the project
// mount cannot carry writer.sock (bug-6715affb).
func TestDaemonEnsure_ContainerWorkerStartsNone(t *testing.T) {
	called := false
	root := stubEnsure(t, func(context.Context) (string, error) { called = true; return "/x/writer.sock", nil })
	t.Setenv("WIPNOTE_DELEGATE_PLACE", "container")
	t.Setenv("WIPNOTE_DELEGATE_RUN", "dlg-0123456789ab")
	var out bytes.Buffer
	if env := startDaemonEnsure(root).sessionEnv(&out, time.Second); env != nil || out.Len() != 0 {
		t.Fatalf("env=%v out=%q, want no env and no output", env, out.String())
	}
	if called {
		t.Fatal("the daemon ensure ran inside a container worker")
	}
}
