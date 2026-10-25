package daemon

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func TestReadyTimeout_EnvOverride(t *testing.T) {
	for v, want := range map[string]time.Duration{
		"": ensureReadyBudget, "15s": 15 * time.Second, "junk": ensureReadyBudget, "-1s": ensureReadyBudget,
	} {
		t.Setenv(readyTimeoutEnv, v)
		if got := ReadyTimeout(); got != want {
			t.Errorf("%s=%q: ReadyTimeout() = %v, want %v", readyTimeoutEnv, v, got, want)
		}
	}
}

// A launcher that finds a fresh lease but no socket waits for the owner and
// spawns nothing: the spawn binary here would fail the test if it ran.
func TestEnsureDaemon_HeldLeaseDoesNotSpawn(t *testing.T) {
	root := t.TempDir()
	writeRecordForTest(t, root, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_, err := EnsureDaemon(ctx, root, "/nonexistent/wipnote", os.Getpid())
	if !errors.Is(err, ErrDaemonStarting) {
		t.Fatalf("err = %v, want ErrDaemonStarting (waiting on the owner, no spawn)", err)
	}
}
