package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestIdleExitAfterTimeout verifies ServeWithIdleTimeout closes the listener
// (returning from Serve) after the configured idle window elapses with zero
// requests. A short injected timeout keeps the test fast.
func TestIdleExitAfterTimeout(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".wipnote"), 0o755); err != nil {
		t.Fatalf("mkdir .wipnote: %v", err)
	}
	sock := SocketPath(dir)

	ln, err := NewListener(ListenerConfig{SocketPath: sock})
	if err != nil {
		t.Fatalf("NewListener: %v", err)
	}

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- ln.ServeWithIdleTimeout(context.Background(), 60*time.Millisecond)
	}()

	select {
	case err := <-serveErr:
		if err != nil {
			t.Fatalf("idle ServeWithIdleTimeout returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		_ = ln.Close()
		t.Fatal("writer did not idle-exit within 5s")
	}

	// After idle-exit the socket must be unlinked (Close removes it).
	if _, statErr := os.Stat(sock); !os.IsNotExist(statErr) {
		t.Fatalf("socket still present after idle-exit: stat err = %v", statErr)
	}
}

// TestIdleExitNotTriggeredWhileActive verifies a steady stream of reads keeps the
// writer alive past the idle window — idle-exit must not race in-flight work.
func TestIdleExitNotTriggeredWhileActive(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".wipnote"), 0o755); err != nil {
		t.Fatalf("mkdir .wipnote: %v", err)
	}
	sock := SocketPath(dir)

	ln, err := NewListener(ListenerConfig{SocketPath: sock})
	if err != nil {
		t.Fatalf("NewListener: %v", err)
	}

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- ln.ServeWithIdleTimeout(context.Background(), 50*time.Millisecond)
	}()

	client := NewReadClientForSocket(sock)
	// Drive reads for ~250ms (5x the idle window). The daemon must stay up.
	deadline := time.Now().Add(250 * time.Millisecond)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_, err := client.Ping(ctx)
		cancel()
		if err != nil {
			t.Fatalf("ping while active failed (daemon exited early?): %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	select {
	case err := <-serveErr:
		t.Fatalf("writer idle-exited while actively serving ops: %v", err)
	default:
		// Good — still serving.
	}

	// Now stop driving and confirm it eventually idle-exits.
	select {
	case <-serveErr:
	case <-time.After(5 * time.Second):
		_ = ln.Close()
		t.Fatal("writer did not idle-exit after activity stopped")
	}
}
