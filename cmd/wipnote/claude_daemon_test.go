package main

import (
	"bytes"
	"context"
	"errors"
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
