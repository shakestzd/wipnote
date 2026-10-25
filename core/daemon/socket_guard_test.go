package daemon

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// leftoverSocket leaves a socket file with no listener, as a crash would.
func leftoverSocket(t *testing.T, root string) string {
	t.Helper()
	sock := SocketPath(root)
	_ = os.MkdirAll(filepath.Dir(sock), 0o755)
	if err := os.WriteFile(sock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return sock
}

func newGuardListener(t *testing.T, root string, lease *Lease) (*Listener, error) {
	t.Helper()
	q, stop := startQueue(t)
	t.Cleanup(stop)
	ln, err := NewListener(ListenerConfig{SocketPath: SocketPath(root), Queue: q, Applier: RejectingApplier, Lease: lease})
	if err == nil {
		t.Cleanup(func() { _ = ln.Close() })
	}
	return ln, err
}

// The lease owner reclaims a leftover socket and binds.
func TestSocketGuard_OwnerReclaimsLeftoverSocket(t *testing.T) {
	root := t.TempDir()
	leftoverSocket(t, root)
	l, err := AcquireLease(root)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()
	if _, err := newGuardListener(t, root, l); err != nil {
		t.Fatalf("owner bind over leftover socket: %v", err)
	}
}

// Nobody but the lease owner may bind: not a fixture while a live owner
// holds the lease, and not a process whose lease was taken over.
func TestSocketGuard_NonOwnerRefused(t *testing.T) {
	root := t.TempDir()
	sock := leftoverSocket(t, root)
	writeRecordForTest(t, root, nil) // a live owner in another namespace
	if _, err := newGuardListener(t, root, nil); err == nil {
		t.Fatal("a fixture bound while another owner holds the lease")
	}
	if _, err := os.Stat(sock); err != nil {
		t.Fatalf("the live owner's socket was unlinked: %v", err)
	}

	writeRecordForTest(t, root, func(r *LeaseRecord) { r.HeartbeatAt = time.Now().Add(-time.Hour) })
	displaced, err := AcquireLease(root)
	if err != nil {
		t.Fatal(err)
	}
	writeRecordForTest(t, root, nil) // a reclaimer takes over
	if _, err := newGuardListener(t, root, displaced); err == nil {
		t.Fatal("a displaced owner bound the socket")
	}
}

// A fixture binds when the lease is missing or stale.
func TestSocketGuard_FixtureBindsWithoutLiveOwner(t *testing.T) {
	root := t.TempDir()
	leftoverSocket(t, root)
	writeRecordForTest(t, root, func(r *LeaseRecord) { r.HeartbeatAt = time.Now().Add(-time.Hour) })
	if _, err := newGuardListener(t, root, nil); err != nil {
		t.Fatalf("fixture bind over a stale lease: %v", err)
	}
}

// Close removes the socket path only while it is the inode this listener
// bound, so a displaced server never deletes its successor's socket.
func TestSocketGuard_CloseLeavesSuccessorsSocket(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, ".wipnote"), 0o755)
	ln, err := newGuardListener(t, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	sock := SocketPath(root)
	_ = os.Remove(sock)
	successor, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer successor.Close()
	_ = ln.Close()
	if _, err := os.Stat(sock); err != nil {
		t.Fatalf("closing the displaced listener removed the successor's socket: %v", err)
	}
}

// serveInBackground runs ServeWithIdleTimeout and returns its done channel.
func serveInBackground(ln *Listener, idle time.Duration) <-chan error {
	done := make(chan error, 1)
	go func() { done <- ln.ServeWithIdleTimeout(context.Background(), idle) }()
	return done
}

func shortWatch(t *testing.T) {
	old := idleCheckInterval
	idleCheckInterval = 20 * time.Millisecond
	t.Cleanup(func() { idleCheckInterval = old })
}

func expectExit(t *testing.T, done <-chan error, within time.Duration, what string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(within):
		t.Fatalf("listener did not exit within %v after %s", within, what)
	}
}

// A server that loses the lease or its socket exits within one watch
// interval, even with a live attached client and no idle timeout.
func TestSocketGuard_ExitsOnLostOwnership(t *testing.T) {
	shortWatch(t)
	for _, loss := range []string{"lease", "socket"} {
		t.Run(loss, func(t *testing.T) {
			root := t.TempDir()
			l, err := AcquireLease(root)
			if err != nil {
				t.Fatal(err)
			}
			defer l.Release()
			ln, err := newGuardListener(t, root, l)
			if err != nil {
				t.Fatal(err)
			}
			ln.attached = map[int]struct{}{os.Getpid(): {}}
			done := serveInBackground(ln, 0)
			if loss == "lease" {
				writeRecordForTest(t, root, nil)
			} else {
				_ = os.Remove(SocketPath(root))
			}
			expectExit(t, done, time.Second, "losing its "+loss)
		})
	}
}

// With no client, the idle window ends the server even while its startup
// projection is still building; a live attached launcher keeps it up.
func TestIdle_NoClientExitsDuringHydrateAttachedStays(t *testing.T) {
	var ready atomic.Bool // never ready: the hydrate is running throughout
	ln, _ := newGatedListener(t, RejectingApplier, ready.Load)
	expectExit(t, serveInBackground(ln, 100*time.Millisecond), 3*time.Second, "100ms with no client mid-hydrate")

	attached, _ := newGatedListener(t, RejectingApplier, ready.Load)
	attached.attached = map[int]struct{}{os.Getpid(): {}}
	done := serveInBackground(attached, 50*time.Millisecond)
	select {
	case <-done:
		t.Fatal("a server with a live attached launcher idle-exited")
	case <-time.After(500 * time.Millisecond):
	}
	_ = attached.Close()
	<-done
}

// A live pre-heartbeat writer that holds writer.pid is left alone: the new
// lease reads as held and the launcher gets a one-line notice.
func TestLease_LegacyLiveOwnerIsHeld(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil || !hasProcCmdline() {
		t.Skip("needs bash and /proc")
	}
	legacy := exec.Command(bash, "-c", "exec -a _serve-child sleep 30")
	if err := legacy.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = legacy.Process.Kill(); _ = legacy.Wait() }()
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, ".wipnote"), 0o755)
	pid := strconv.Itoa(legacy.Process.Pid)
	if err := os.WriteFile(LegacyLeasePath(root), []byte(pid+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitLegacyArgv(t, legacy.Process.Pid)
	if _, err := AcquireLease(root); err != ErrLeaseHeld {
		t.Fatalf("acquire beside a live legacy owner: err = %v, want ErrLeaseHeld", err)
	}
	if st := InspectLease(root); st.Status != LeaseLegacyHeld || OlderOwnerNotice(root, "0.81.0") == "" {
		t.Fatalf("legacy owner not reported: %+v", st)
	}
	_ = legacy.Process.Kill()
	_ = legacy.Wait()
	l, err := AcquireLease(root)
	if err != nil {
		t.Fatalf("acquire after the legacy owner died: %v", err)
	}
	_ = l.Release()
}

func hasProcCmdline() bool {
	_, err := os.Stat("/proc/self/cmdline")
	return err == nil
}

// waitLegacyArgv waits until the exec has replaced bash's argv.
func waitLegacyArgv(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if legacyWriterAlive(pid) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("legacy stand-in never showed _serve-child in its argv")
}

// A launcher beside a live legacy owner leaves it alone: no spawn, and an
// error the launcher turns into its one-line notice.
func TestEnsureDaemon_LegacyOwnerNotDisturbed(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil || !hasProcCmdline() {
		t.Skip("needs bash and /proc")
	}
	legacy := exec.Command(bash, "-c", "exec -a _serve-child sleep 30")
	if err := legacy.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = legacy.Process.Kill(); _ = legacy.Wait() }()
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, ".wipnote"), 0o755)
	_ = os.WriteFile(LegacyLeasePath(root), []byte(strconv.Itoa(legacy.Process.Pid)), 0o644)
	waitLegacyArgv(t, legacy.Process.Pid)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := EnsureDaemon(ctx, root, "/nonexistent/wipnote", os.Getpid()); !errors.Is(err, ErrDaemonLegacyOwner) {
		t.Fatalf("err = %v, want ErrDaemonLegacyOwner", err)
	}
}
