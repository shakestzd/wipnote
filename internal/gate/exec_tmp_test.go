package gate

import (
	"context"
	"os"
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
	// Replace existing.
	out := UpsertEnv(env, "BAR", "9")
	if LookupEnv(out, "BAR") != "9" {
		t.Errorf("UpsertEnv replace failed: %v", out)
	}
	if len(out) != 2 {
		t.Errorf("UpsertEnv replace should not grow: %v", out)
	}
	// Insert new.
	out = UpsertEnv(env, "BAZ", "3")
	if LookupEnv(out, "BAZ") != "3" || len(out) != 3 {
		t.Errorf("UpsertEnv insert failed: %v", out)
	}
}

func TestEffectiveTmpDir(t *testing.T) {
	// GOTMPDIR wins over TMPDIR.
	env := []string{"TMPDIR=/a", "GOTMPDIR=/b"}
	if got := EffectiveTmpDir(env); got != "/b" {
		t.Errorf("GOTMPDIR precedence: got %q want /b", got)
	}
	// TMPDIR when no GOTMPDIR.
	if got := EffectiveTmpDir([]string{"TMPDIR=/a"}); got != "/a" {
		t.Errorf("TMPDIR: got %q want /a", got)
	}
	// Default /tmp when neither set.
	if got := EffectiveTmpDir(nil); got != "/tmp" {
		t.Errorf("default: got %q want /tmp", got)
	}
}

func TestDirIsExecCapable(t *testing.T) {
	// A fresh temp dir under the test's exec-capable GOTMPDIR/TMPDIR should be
	// exec-capable. t.TempDir() honours the test runner's temp dir, which the
	// project convention keeps exec-capable (GOTMPDIR=~/.gotest-tmp/...).
	dir := t.TempDir()
	if !DirIsExecCapable(dir) {
		t.Skipf("test temp dir %q is not exec-capable in this environment; skipping positive case", dir)
	}
	// A non-existent dir is never exec-capable.
	if DirIsExecCapable(filepath.Join(dir, "does-not-exist")) {
		t.Error("missing dir reported exec-capable")
	}
	// Empty path is never exec-capable.
	if DirIsExecCapable("") {
		t.Error("empty path reported exec-capable")
	}
}

func TestGateExecEnvRedirectsWhenTmpUnusable(t *testing.T) {
	codeRoot := t.TempDir()
	// Point the inherited temp dir at a non-existent path so GateExecEnv must
	// redirect to the project-tree scratch dir.
	t.Setenv("TMPDIR", filepath.Join(codeRoot, "nope-noexec"))
	t.Setenv("GOTMPDIR", "")

	env, redirected, dir := GateExecEnv(codeRoot)
	if !redirected {
		t.Skipf("project-tree scratch dir %q not exec-capable in this environment", filepath.Join(codeRoot, gateTmpDirName))
	}
	wantDir := filepath.Join(codeRoot, gateTmpDirName)
	if dir != wantDir {
		t.Errorf("redirect dir: got %q want %q", dir, wantDir)
	}
	if got := LookupEnv(env, "GOTMPDIR"); got != wantDir {
		t.Errorf("GOTMPDIR not injected: got %q want %q", got, wantDir)
	}
	if _, err := os.Stat(wantDir); err != nil {
		t.Errorf("scratch dir not created: %v", err)
	}
}

func TestGateExecEnvLeavesUsableTmpUntouched(t *testing.T) {
	codeRoot := t.TempDir()
	good := t.TempDir()
	if !DirIsExecCapable(good) {
		t.Skip("test temp dir not exec-capable; cannot assert no-redirect path")
	}
	t.Setenv("GOTMPDIR", good)

	env, redirected, dir := GateExecEnv(codeRoot)
	if redirected {
		t.Errorf("should not redirect when GOTMPDIR is usable; got dir %q", dir)
	}
	if LookupEnv(env, "GOTMPDIR") != good {
		t.Errorf("GOTMPDIR mutated unexpectedly: %q", LookupEnv(env, "GOTMPDIR"))
	}
}

func TestIsLikelyNoexecFailure(t *testing.T) {
	cases := []struct {
		out  string
		want bool
	}{
		{"fork/exec /tmp/go-build123/b001/test: permission denied", true},
		{"/tmp/go-build/exe: permission denied", true},
		{"text file busy", false}, // no "permission denied"
		{"permission denied: text file busy", true},
		{"undefined: foo", false},
		{"FAIL\tsome/pkg\t0.1s", false},
		{"permission denied accessing config.json", false}, // denied but not exec-related
	}
	for _, c := range cases {
		if got := IsLikelyNoexecFailure(c.out); got != c.want {
			t.Errorf("IsLikelyNoexecFailure(%q) = %v, want %v", c.out, got, c.want)
		}
	}
}

func TestGateTmpRemediationMentionsDir(t *testing.T) {
	root := "/work/project"
	rem := GateTmpRemediation(root)
	if !strings.Contains(rem, gateTmpDirName) {
		t.Errorf("remediation missing scratch dir name: %q", rem)
	}
	if !strings.Contains(rem, "GOTMPDIR=") || !strings.Contains(rem, "TMPDIR=") {
		t.Errorf("remediation missing env vars: %q", rem)
	}
}

func TestRunManagedGateSuccessCapturesOutput(t *testing.T) {
	var sout, serr strings.Builder
	out, err := RunManagedGate(context.Background(), "echo", "", nil, &sout, &serr, "sh", "-c", "echo hello")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "hello") {
		t.Errorf("captured output missing: %q", out)
	}
	if !strings.Contains(serr.String(), "running: echo (sh -c echo hello)") {
		t.Errorf("missing running announcement: %q", serr.String())
	}
}

func TestRunManagedGateCancelKillsProcessGroup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var sout, serr strings.Builder

	done := make(chan error, 1)
	go func() {
		// Long sleep that would hang the test if not killed on cancel.
		_, err := RunManagedGate(ctx, "sleep", "", nil, &sout, &serr, "sh", "-c", "sleep 60")
		done <- err
	}()

	// Give the child a moment to start, then cancel.
	time.Sleep(150 * time.Millisecond)
	cancel()

	select {
	case <-done:
		// Process group was killed and Wait returned — success.
	case <-time.After(5 * time.Second):
		t.Fatal("RunManagedGate did not return after context cancellation; process group not killed")
	}
}
