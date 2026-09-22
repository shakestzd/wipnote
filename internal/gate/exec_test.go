package gate

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLookupAndUpsertEnv(t *testing.T) {
	env := []string{"FOO=1", "BAR=2"}
	if got := LookupEnv(env, "BAR"); got != "2" {
		t.Errorf("LookupEnv BAR: got %q want 2", got)
	}
	if got := LookupEnv(env, "MISSING"); got != "" {
		t.Errorf("LookupEnv MISSING: got %q want empty", got)
	}
	out := UpsertEnv(env, "BAR", "9")
	if LookupEnv(out, "BAR") != "9" || len(out) != 2 {
		t.Errorf("UpsertEnv replace failed: %v", out)
	}
	out = UpsertEnv(env, "BAZ", "3")
	if LookupEnv(out, "BAZ") != "3" || len(out) != 3 {
		t.Errorf("UpsertEnv insert failed: %v", out)
	}
}

func TestResolveCodeRoot(t *testing.T) {
	const proj = "/repo/main"
	const wt = "/repo/.worktrees/feat"
	const common = "/repo/main/.git"
	cases := []struct {
		name                          string
		cwdTop, cwdCommon, projCommon string
		want                          string
	}{
		{"cwd-not-in-git", "", "", common, proj},
		{"project-not-a-repo", wt, "/repo/.worktrees/feat/.git", "", proj},
		{"cwd-is-project", proj, common, common, proj},
		{"linked-worktree-same-repo", wt, common, common, wt},
		{"unrelated-repo", "/other/repo", "/other/repo/.git", common, proj},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveCodeRoot(proj, tc.cwdTop, tc.cwdCommon, tc.projCommon); got != tc.want {
				t.Fatalf("ResolveCodeRoot() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCodeRoot_LinkedWorktreeOverride(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	mainRepo := filepath.Join(root, "main")
	git := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := exec.Command("git", "init", mainRepo).Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}
	git(mainRepo, "config", "user.email", "t@example.com")
	git(mainRepo, "config", "user.name", "t")
	git(mainRepo, "commit", "--allow-empty", "-m", "init")
	wt := filepath.Join(root, "wt-feat")
	git(mainRepo, "worktree", "add", "-b", "feat-x", wt)
	resolve := func(p string) string {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return r
		}
		return p
	}
	t.Chdir(wt)
	if got := resolve(CodeRoot(mainRepo)); got != resolve(wt) {
		t.Fatalf("CodeRoot from worktree = %q, want %q", got, resolve(wt))
	}
}

func TestGateExecEnvRedirectsWhenTmpUnusable(t *testing.T) {
	codeRoot := t.TempDir()
	t.Setenv("TMPDIR", filepath.Join(codeRoot, "nope-noexec"))
	t.Setenv("GOTMPDIR", "")
	env, redirected, dir := GateExecEnv(codeRoot)
	if !redirected {
		t.Skipf("scratch dir %q not exec-capable", filepath.Join(codeRoot, gateTmpDirName))
	}
	wantDir := filepath.Join(codeRoot, gateTmpDirName)
	if dir != wantDir || LookupEnv(env, "GOTMPDIR") != wantDir {
		t.Fatalf("unexpected redirect: dir=%q env=%q want=%q", dir, LookupEnv(env, "GOTMPDIR"), wantDir)
	}
	if _, err := os.Stat(wantDir); err != nil {
		t.Fatalf("scratch dir not created: %v", err)
	}
}

func TestEffectiveUVCacheDir(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir available")
	}
	cases := []struct {
		name         string
		uvCacheDir   string
		xdgCacheHome string
		want         string
	}{
		{"UV_CACHE_DIR wins", "/explicit/uv-cache", "/xdg/cache", "/explicit/uv-cache"},
		{"falls back to XDG_CACHE_HOME/uv", "", "/xdg/cache", filepath.Join("/xdg/cache", "uv")},
		{"falls back to ~/.cache/uv", "", "", filepath.Join(home, ".cache", "uv")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := []string{"UV_CACHE_DIR=" + tc.uvCacheDir, "XDG_CACHE_HOME=" + tc.xdgCacheHome}
			if got := effectiveUVCacheDir(env); got != tc.want {
				t.Fatalf("effectiveUVCacheDir() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDirIsWritable(t *testing.T) {
	dir := t.TempDir()
	if !DirIsWritable(dir) {
		t.Errorf("DirIsWritable(%q) = false, want true for a fresh temp dir", dir)
	}

	// A not-yet-created child of a writable dir is writable via the nearest
	// existing ancestor (uv creates its cache dir lazily on first use).
	child := filepath.Join(dir, "not-yet-created", "uv")
	if !DirIsWritable(child) {
		t.Errorf("DirIsWritable(%q) = false, want true (writable via existing ancestor)", child)
	}

	// A path that exists as a regular FILE, not a directory, is never
	// writable-as-a-directory — this is a permission-independent way to force
	// the "unusable" branch even when the test runs as root (chmod-based
	// denial has no effect on root).
	filePath := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if DirIsWritable(filePath) {
		t.Errorf("DirIsWritable(%q) = true, want false: path is a regular file", filePath)
	}

	if DirIsWritable("") {
		t.Error("DirIsWritable(\"\") = true, want false")
	}
}

// TestGateExecEnvUpsertsUVCacheDirWhenUnwritable is the issue #137 regression:
// when the resolved uv cache directory is unusable (simulated here with a
// regular file standing in the way, since chmod-based denial has no effect
// running as root — see TestDirIsWritable), GateExecEnv must redirect
// UV_CACHE_DIR to a workspace-local, writable directory under
// codeRoot/.test-tmp/uv-cache.
func TestGateExecEnvUpsertsUVCacheDirWhenUnwritable(t *testing.T) {
	codeRoot := t.TempDir()
	blocked := filepath.Join(codeRoot, "blocked-uv-cache")
	if err := os.WriteFile(blocked, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Setenv("UV_CACHE_DIR", blocked)

	env, redirected, _ := GateExecEnv(codeRoot)
	if !redirected {
		t.Fatal("expected redirected=true when UV_CACHE_DIR is unwritable")
	}
	wantUVDir := filepath.Join(codeRoot, gateTmpDirName, "uv-cache")
	if got := LookupEnv(env, "UV_CACHE_DIR"); got != wantUVDir {
		t.Fatalf("UV_CACHE_DIR = %q, want %q", got, wantUVDir)
	}
	if _, err := os.Stat(wantUVDir); err != nil {
		t.Fatalf("workspace-local uv cache dir not created: %v", err)
	}
}

// TestGateExecEnvLeavesUVCacheDirWhenWritable verifies the no-op path: an
// already-writable UV_CACHE_DIR is left untouched.
func TestGateExecEnvLeavesUVCacheDirWhenWritable(t *testing.T) {
	codeRoot := t.TempDir()
	writable := t.TempDir()
	t.Setenv("UV_CACHE_DIR", writable)
	t.Setenv("TMPDIR", t.TempDir())
	t.Setenv("GOTMPDIR", "")

	env, _, _ := GateExecEnv(codeRoot)
	if got := LookupEnv(env, "UV_CACHE_DIR"); got != writable {
		t.Fatalf("UV_CACHE_DIR = %q, want unchanged %q", got, writable)
	}
}

func TestIsLikelyCacheSandboxFailure(t *testing.T) {
	cases := []struct {
		name   string
		output string
		want   bool
	}{
		{
			name:   "issue #137 uv sandbox denial",
			output: "error: failed to open file `/Users/shakes/.cache/uv/sdists-v9/.git`: Operation not permitted (os error 1)",
			want:   true,
		},
		{
			name:   "permission denied on a cache path",
			output: "open /home/user/.cache/uv/foo: permission denied",
			want:   true,
		},
		{
			name:   "operation not permitted without a cache path",
			output: "chmod /workspace/main.go: operation not permitted",
			want:   false,
		},
		{
			name:   "cache path without a permission phrase",
			output: "warning: /home/user/.cache/uv is using a lot of disk space",
			want:   false,
		},
		{
			name:   "unrelated ruff lint failure",
			output: "main.py:3:1: F401 'os' imported but unused",
			want:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsLikelyCacheSandboxFailure(tc.output); got != tc.want {
				t.Errorf("IsLikelyCacheSandboxFailure(%q) = %v, want %v", tc.output, got, tc.want)
			}
		})
	}
}

func TestRunManagedGateCancelKillsProcessGroup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var sout, serr strings.Builder
	done := make(chan error, 1)
	go func() {
		_, err := RunManagedGate(ctx, "sleep", "", nil, &sout, &serr, "sh", "-c", "sleep 60")
		done <- err
	}()
	time.Sleep(150 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RunManagedGate did not return after context cancellation")
	}
}
