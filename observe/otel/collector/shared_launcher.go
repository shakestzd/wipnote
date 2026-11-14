package collector

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// SharedCollectorOpts configures a SharedCollector.
type SharedCollectorOpts struct {
	// Log receives the watchdog's warnings, which happen while the harness
	// owns the terminal: a line written there lands inside its prompt
	// (bug-fa4bb927). Nil appends to the project's otel-collector.log.
	Log io.Writer
	// SpawnFn starts a shared collector. Nil means DefaultSharedSpawnFn.
	SpawnFn SharedSpawnFn
	// WatchdogIntervalEnv names the env var overriding the watchdog interval.
	WatchdogIntervalEnv string
	// Harness is "claude" or "codex" when the launcher knows it; it picks the
	// resume command in the moved-port warning. Empty names both.
	Harness string
}

// SharedCollector implements Lifecycle on top of the project's shared
// collector: Spawn attaches this launcher to the live collector (starting
// one only when none answers), and cleanup detaches without killing it.
type SharedCollector struct {
	opts SharedCollectorOpts
}

// NewSharedCollector returns a SharedCollector configured by opts.
func NewSharedCollector(opts SharedCollectorOpts) *SharedCollector {
	if opts.SpawnFn == nil {
		opts.SpawnFn = DefaultSharedSpawnFn
	}
	if opts.WatchdogIntervalEnv == "" {
		opts.WatchdogIntervalEnv = "WIPNOTE_OTEL_WATCHDOG_INTERVAL"
	}
	return &SharedCollector{opts: opts}
}

// Spawn attaches this launcher to the project's shared collector and returns
// its port. The session exports to SessionEndpointPath(sessionID) on it.
func (c *SharedCollector) Spawn(binPath, sessionID, projectDir string) (int, func(), error) {
	rec, err := EnsureShared(binPath, projectDir, c.opts.SpawnFn)
	if err != nil {
		return 0, nil, err
	}
	launcher := os.Getpid()
	if err := postLauncher(rec.Port, AttachPath, launcher); err != nil {
		return 0, nil, fmt.Errorf("attach to shared collector: %w", err)
	}
	w := &sharedWatch{c: c, binPath: binPath, sessionID: sessionID, projectDir: projectDir, rec: rec, exportPort: rec.Port}
	w.writePID(rec)

	stop := w.start()
	var once sync.Once
	cleanup := func() {
		once.Do(func() {
			stop()
			cur := w.current()
			_ = postLauncher(cur.Port, DetachPath+"?session="+sessionID+"&", launcher)
			removeCollectorPIDIfMatches(projectDir, sessionID, cur.PID)
		})
	}
	return rec.Port, cleanup, nil
}

// sharedWatch re-ensures the shared collector when the one this launcher is
// attached to dies or is replaced, and re-attaches to the replacement.
type sharedWatch struct {
	c                              *SharedCollector
	binPath, sessionID, projectDir string
	mu                             sync.Mutex
	rec                            SharedRecord
	// exportPort is the port the harness was launched against; it never
	// changes. warnedPort is the last moved-to port already reported.
	exportPort, warnedPort int
}

// writePID records rec as the collector and keeps the launch-time export port.
func (w *sharedWatch) writePID(rec SharedRecord) {
	WriteSharedSessionPID(w.projectDir, w.sessionID, rec.PID, rec.Port,
		SessionExport{Port: w.exportPort, Launcher: os.Getpid(), Harness: w.c.opts.Harness})
}

func (w *sharedWatch) current() SharedRecord {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.rec
}

func (w *sharedWatch) start() func() {
	done, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(WatchdogInterval(w.c.opts.WatchdogIntervalEnv))
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				w.check()
			}
		}
	}()
	return func() { close(done); <-stopped }
}

// check is one watchdog tick. A lease that still names our live collector
// costs one small file read; anything else re-runs EnsureShared.
func (w *sharedWatch) check() {
	cur := w.current()
	if rec, err := ReadSharedRecord(w.projectDir); err == nil && rec.PID == cur.PID && PIDAlive(cur.PID) {
		return
	}
	rec, err := EnsureShared(w.binPath, w.projectDir, w.c.opts.SpawnFn)
	if err != nil {
		w.logf("shared collector re-ensure failed: %v", err)
		return
	}
	if err := postLauncher(rec.Port, AttachPath, os.Getpid()); err != nil {
		w.logf("shared collector re-attach failed: %v", err)
		return
	}
	if rec.Port != w.exportPort && rec.Port != w.warnedPort {
		w.warnedPort = rec.Port
		w.logf("shared collector moved to port %d; session %s still exports to %d: %s",
			rec.Port, w.sessionID, w.exportPort, MovedPortAdvice(w.c.opts.Harness))
	}
	w.writePID(rec)
	w.mu.Lock()
	w.rec = rec
	w.mu.Unlock()
}

// logf writes one watchdog warning to opts.Log, or to the project's
// otel-collector.log, where `wipnote doctor` and a person debugging telemetry
// look.
func (w *sharedWatch) logf(format string, args ...any) {
	out := w.c.opts.Log
	if out == nil {
		out = CollectorLog(w.projectDir)
	}
	fmt.Fprintf(out, "wipnote: warning: "+format+"\n", args...)
}

// postLauncher registers (AttachPath) or releases (DetachPath) a launcher
// pid with the shared collector on port.
func postLauncher(port int, path string, pid int) error {
	client := http.Client{Timeout: time.Second}
	sep := "?"
	if strings.HasSuffix(path, "&") {
		sep = ""
	}
	url := "http://127.0.0.1:" + strconv.Itoa(port) + path + sep + "pid=" + strconv.Itoa(pid)
	resp, err := client.Post(url, "text/plain", nil)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("%s: HTTP %d", path, resp.StatusCode)
	}
	return nil
}

// DefaultSharedSpawnFn starts `otel-collect --shared` in its own process
// group and waits for its handshake line.
func DefaultSharedSpawnFn(binPath, projectDir string, requestedPort int) (int, *os.Process, error) {
	listen := "127.0.0.1:" + strconv.Itoa(requestedPort)
	cmd := exec.Command(binPath, "otel-collect", "--shared",
		"--project-dir", projectDir,
		"--listen", listen,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// The shared collector outlives the launcher that started it, so it must
	// not write to that launcher's stderr: a closed pipe there would kill it.
	logFile := openSharedLog(projectDir)
	if logFile != nil {
		cmd.Stderr = logFile
		defer logFile.Close()
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return 0, nil, fmt.Errorf("stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return 0, nil, fmt.Errorf("start otel-collect --shared: %w", err)
	}
	port, err := readHandshake(bufio.NewScanner(stdout))
	if err != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		return 0, nil, err
	}
	return port, cmd.Process, nil
}

// openSharedLog opens <project>/.wipnote/logs/otel-collector.log for append,
// or returns nil (stderr then goes to /dev/null).
func openSharedLog(projectDir string) *os.File {
	f, err := openCollectorLog(projectDir)
	if err != nil {
		return nil
	}
	return f
}

// openCollectorLog opens <project>/.wipnote/logs/otel-collector.log for
// append, creating the logs folder when it is missing.
func openCollectorLog(projectDir string) (*os.File, error) {
	dir := filepath.Join(projectDir, ".wipnote", "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return os.OpenFile(filepath.Join(dir, "otel-collector.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
}

// CheckCollectorLog reports whether the collector log can be opened. Launchers
// call it before the harness takes the terminal, the last moment a warning can
// be printed there: once the harness runs, collector warnings go only to this
// log, so a log that cannot be opened would make them vanish unseen.
func CheckCollectorLog(projectDir string) error {
	f, err := openCollectorLog(projectDir)
	if err != nil {
		return err
	}
	return f.Close()
}
