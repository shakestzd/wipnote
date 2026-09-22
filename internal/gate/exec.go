package gate

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
)

func SignalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

const gateTmpDirName = ".test-tmp"

// GateExecEnv builds the environment a gate command runs under, redirecting
// any cache/temp directory that a managed sandbox has made unusable to a
// workspace-local one under codeRoot/.test-tmp. tmpDir reports the effective
// Go temp dir (redirected or not); redirected reports whether ANY redirection
// happened — Go temp/cache, uv cache, or both.
func GateExecEnv(codeRoot string) (env []string, redirected bool, tmpDir string) {
	env = os.Environ()
	tmpDir = EffectiveTmpDir(env)

	if tmpDir == "" || !DirIsExecCapable(tmpDir) {
		dir := filepath.Join(codeRoot, gateTmpDirName)
		if err := os.MkdirAll(dir, 0o755); err == nil && DirIsExecCapable(dir) {
			env = UpsertEnv(env, "GOTMPDIR", dir)
			if !goCacheUsable(env) {
				cache := filepath.Join(dir, "gocache")
				if err := os.MkdirAll(cache, 0o755); err == nil && DirIsExecCapable(cache) {
					env = UpsertEnv(env, "GOCACHE", cache)
				}
			}
			redirected = true
			tmpDir = dir
		}
	}

	if !uvCacheUsable(env) {
		uvDir := filepath.Join(codeRoot, gateTmpDirName, "uv-cache")
		if err := os.MkdirAll(uvDir, 0o755); err == nil {
			env = UpsertEnv(env, "UV_CACHE_DIR", uvDir)
			redirected = true
		}
	}

	return env, redirected, tmpDir
}

func EffectiveTmpDir(env []string) string {
	if v := LookupEnv(env, "GOTMPDIR"); strings.TrimSpace(v) != "" {
		return v
	}
	if v := LookupEnv(env, "TMPDIR"); strings.TrimSpace(v) != "" {
		return v
	}
	return "/tmp"
}

func goCacheUsable(env []string) bool {
	c := strings.TrimSpace(LookupEnv(env, "GOCACHE"))
	if c == "" || c == "off" {
		return true
	}
	return DirIsExecCapable(c)
}

// effectiveUVCacheDir returns the cache directory `uv` resolves to given env,
// following uv's own precedence: UV_CACHE_DIR, then XDG_CACHE_HOME/uv, then
// ~/.cache/uv. Returns "" when none can be determined (e.g. no home dir),
// in which case uvCacheUsable treats it as usable rather than interfering.
func effectiveUVCacheDir(env []string) string {
	if v := strings.TrimSpace(LookupEnv(env, "UV_CACHE_DIR")); v != "" {
		return v
	}
	if v := strings.TrimSpace(LookupEnv(env, "XDG_CACHE_HOME")); v != "" {
		return filepath.Join(v, "uv")
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, ".cache", "uv")
}

// uvCacheUsable reports whether the effective uv cache directory (per
// effectiveUVCacheDir) is writable. Unlike DirIsExecCapable, this only checks
// write access — the uv cache never needs to be exec-capable — so a
// writability-only probe avoids false redirects on mounts that are writable
// but noexec.
func uvCacheUsable(env []string) bool {
	dir := effectiveUVCacheDir(env)
	if dir == "" {
		return true
	}
	return DirIsWritable(dir)
}

// DirIsWritable reports whether dir (or its nearest existing ancestor, if dir
// itself does not yet exist) can be written to. It is a lighter probe than
// DirIsExecCapable: no exec bit is required, only file creation — the right
// bar for cache directories such as uv's, which managed sandboxes may block
// even for plain reads/writes outside the workspace (issue #137).
func DirIsWritable(dir string) bool {
	if strings.TrimSpace(dir) == "" {
		return false
	}
	probeDir := dir
	for {
		if info, err := os.Stat(probeDir); err == nil {
			if !info.IsDir() {
				return false
			}
			break
		}
		parent := filepath.Dir(probeDir)
		if parent == probeDir {
			return false
		}
		probeDir = parent
	}
	f, err := os.CreateTemp(probeDir, ".wipnote-writeprobe-*")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return true
}

func DirIsExecCapable(dir string) bool {
	if strings.TrimSpace(dir) == "" {
		return false
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	execCapableCacheMu.Lock()
	if v, ok := execCapableCache[abs]; ok {
		execCapableCacheMu.Unlock()
		return v
	}
	execCapableCacheMu.Unlock()
	result := probeExecCapable(abs)
	execCapableCacheMu.Lock()
	execCapableCache[abs] = result
	execCapableCacheMu.Unlock()
	return result
}

var (
	execCapableCache   = map[string]bool{}
	execCapableCacheMu sync.Mutex
)

func probeExecCapable(dir string) bool {
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return false
	}
	if mountIsNoexec(dir) {
		return false
	}
	f, err := os.CreateTemp(dir, ".wipnote-execprobe-*.sh")
	if err != nil {
		return false
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err := f.WriteString("#!/bin/sh\nexit 0\n"); err != nil {
		_ = f.Close()
		return false
	}
	if err := f.Close(); err != nil {
		return false
	}
	if err := os.Chmod(name, 0o755); err != nil {
		return false
	}
	if err := exec.Command(name).Run(); err != nil {
		return false
	}
	return true
}

func GateTmpRemediation(codeRoot string) string {
	dir := filepath.Join(codeRoot, gateTmpDirName)
	return fmt.Sprintf("mkdir -p %q && TMPDIR=%q GOTMPDIR=%q wipnote check --gate", dir, dir, dir)
}

func IsLikelyNoexecFailure(output string) bool {
	o := strings.ToLower(output)
	if !strings.Contains(o, "permission denied") {
		return false
	}
	return strings.Contains(o, "/tmp/") || strings.Contains(o, "go-build") || strings.Contains(o, "fork/exec") || strings.Contains(o, "exec format") || strings.Contains(o, "text file busy")
}

// IsLikelyCacheSandboxFailure reports whether output looks like a managed
// sandbox (e.g. Codex) denying access to a user-level cache directory outside
// the workspace — e.g. `uv` failing to open a file under ~/.cache/uv with
// "Operation not permitted" (issue #137). Unlike IsLikelyNoexecFailure (an
// exec-bit failure on a temp dir), this is a plain read/write denial on a
// cache path, so it looks for the permission phrase alongside a cache-path
// marker rather than an exec-specific one.
func IsLikelyCacheSandboxFailure(output string) bool {
	o := strings.ToLower(output)
	if !strings.Contains(o, "operation not permitted") && !strings.Contains(o, "permission denied") {
		return false
	}
	return strings.Contains(o, "/cache/") || strings.Contains(o, ".cache") || strings.Contains(o, "uv_cache_dir") || strings.Contains(o, "xdg_cache_home")
}

// GateCacheRemediation returns the command to retry a gate run with `uv`'s
// cache redirected to a workspace-local, exec-sandbox-writable directory
// under codeRoot/.test-tmp, mirroring GateTmpRemediation for the Go temp/cache
// case.
func GateCacheRemediation(codeRoot string) string {
	dir := filepath.Join(codeRoot, gateTmpDirName, "uv-cache")
	return fmt.Sprintf("mkdir -p %q && UV_CACHE_DIR=%q wipnote check --gate", dir, dir)
}

func RunManagedGate(ctx context.Context, name, dir string, env []string, stdout, stderr io.Writer, argv ...string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	fmt.Fprintf(stderr, "running: %s (%s)\n", name, strings.Join(argv, " "))
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = env
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var combined strings.Builder
	cmd.Stdout = io.MultiWriter(stdout, &combined)
	cmd.Stderr = io.MultiWriter(stderr, &combined)
	if err := cmd.Start(); err != nil {
		return combined.String(), err
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			killProcessGroup(cmd)
		case <-done:
		}
	}()
	err := cmd.Wait()
	close(done)
	return combined.String(), err
}

func killProcessGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	if pgid, err := syscall.Getpgid(pid); err == nil {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		return
	}
	_ = cmd.Process.Kill()
}

func LookupEnv(env []string, key string) string {
	prefix := key + "="
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			return kv[len(prefix):]
		}
	}
	return ""
}

func UpsertEnv(env []string, key, val string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env)+1)
	replaced := false
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			out = append(out, prefix+val)
			replaced = true
			continue
		}
		out = append(out, kv)
	}
	if !replaced {
		out = append(out, prefix+val)
	}
	return out
}
