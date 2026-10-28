package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// cloudSetupScript is scripts/cloud-env-setup.sh, relative to this package.
var cloudSetupScript = filepath.Join("..", "..", "scripts", "cloud-env-setup.sh")

// The script passes shellcheck when it is installed, else bash -n.
func TestCloudEnvSetup_ScriptChecks(t *testing.T) {
	if path, err := exec.LookPath("shellcheck"); err == nil {
		if out, err := exec.Command(path, cloudSetupScript).CombinedOutput(); err != nil {
			t.Fatalf("shellcheck: %v\n%s", err, out)
		}
		return
	}
	if out, err := exec.Command("bash", "-n", cloudSetupScript).CombinedOutput(); err != nil {
		t.Fatalf("bash -n: %v\n%s", err, out)
	}
	body, err := os.ReadFile(cloudSetupScript)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `WIPNOTE_PIN="`) {
		t.Fatal("the script pins no version (WIPNOTE_PIN)")
	}
}

// fakeRelease writes a release for version in the layout GitHub serves
// (<base>/v<version>/<asset>): a tarball with a stand-in binary and a
// plugin tree, and a checksums file. badSum writes a wrong checksum.
func fakeRelease(t *testing.T, base, version string, badSum bool) {
	t.Helper()
	dir := filepath.Join(base, "v"+version)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "wipnote_" + version + "_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	tarball := filepath.Join(dir, name)
	writeTarGz(t, tarball, map[string]string{
		"wipnote":                           "#!/bin/sh\necho \"wipnote " + version + " (go)\"\n",
		"plugin/.claude-plugin/plugin.json": `{"name": "wipnote"}`,
	})
	b, err := os.ReadFile(tarball)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	hexSum := hex.EncodeToString(sum[:])
	if badSum {
		hexSum = strings.Repeat("0", 64)
	}
	writeTestFile(t, filepath.Join(dir, "wipnote_"+version+"_checksums.txt"), hexSum+"  "+name+"\n")
}

func writeTarGz(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		hdr := &tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
}

// runCloudSetup runs the script in a temp HOME against the file:// release
// at base, with no network and only the system tools on PATH.
func runCloudSetup(t *testing.T, home, base string, env ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("bash", cloudSetupScript)
	cmd.Env = append([]string{"HOME=" + home, "PATH=/usr/bin:/bin", "WIPNOTE_RELEASE_BASE_URL=file://" + base}, env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func requireScriptTools(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("bash script")
	}
	for _, tool := range []string{"bash", "curl", "tar"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not installed", tool)
		}
	}
}

// The script installs the pinned version's binary and plugin tree into a
// temp HOME; a second run with the same pin downloads nothing.
func TestCloudEnvSetup_InstallsPinnedAndIsIdempotent(t *testing.T) {
	requireScriptTools(t)
	home, base := t.TempDir(), t.TempDir()
	fakeRelease(t, base, "9.9.9", false)
	out, err := runCloudSetup(t, home, base, "WIPNOTE_VERSION=9.9.9")
	if err != nil {
		t.Fatalf("first run: %v\n%s", err, out)
	}
	bin := filepath.Join(home, ".local", "bin", "wipnote")
	if got, err := exec.Command(bin).Output(); err != nil || strings.TrimSpace(string(got)) != "wipnote 9.9.9 (go)" {
		t.Fatalf("installed binary: %q err %v\n%s", got, err, out)
	}
	if _, err := os.Stat(filepath.Join(home, ".local", "share", "wipnote", "plugin", ".claude-plugin", "plugin.json")); err != nil {
		t.Fatalf("plugin tree: %v", err)
	}
	// Second run: the release is gone, so any download would fail.
	if err := os.RemoveAll(base); err != nil {
		t.Fatal(err)
	}
	out, err = runCloudSetup(t, home, base, "WIPNOTE_VERSION=9.9.9", "WIPNOTE_SETUP_STRICT=1")
	if err != nil || !strings.Contains(out, "already installed") {
		t.Fatalf("second run: %v\n%s", err, out)
	}
	// A different pin is a new install, which needs its release.
	out, err = runCloudSetup(t, home, base, "WIPNOTE_VERSION=9.9.10", "WIPNOTE_SETUP_STRICT=1")
	if err == nil || strings.Contains(out, "already installed") {
		t.Fatalf("new pin reused the old install: %v\n%s", err, out)
	}
}

// A checksum mismatch installs nothing. By default the script still exits
// 0 so a failed install never blocks the cloud environment from starting;
// WIPNOTE_SETUP_STRICT=1 makes it exit non-zero.
func TestCloudEnvSetup_ChecksumMismatchInstallsNothing(t *testing.T) {
	requireScriptTools(t)
	home, base := t.TempDir(), t.TempDir()
	fakeRelease(t, base, "9.9.9", true)
	out, err := runCloudSetup(t, home, base, "WIPNOTE_VERSION=9.9.9")
	if err != nil || !strings.Contains(out, "checksum mismatch") || !strings.Contains(out, "wipnote doctor --project-thread") {
		t.Fatalf("lenient run: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(home, ".local", "bin", "wipnote")); !os.IsNotExist(err) {
		t.Fatalf("a binary was installed despite the bad checksum: %v", err)
	}
	if out, err := runCloudSetup(t, home, base, "WIPNOTE_VERSION=9.9.9", "WIPNOTE_SETUP_STRICT=1"); err == nil {
		t.Fatalf("strict run exited 0:\n%s", out)
	}
}

// "latest" is refused: the cloud environment installs a pinned version.
func TestCloudEnvSetup_RefusesLatest(t *testing.T) {
	requireScriptTools(t)
	out, err := runCloudSetup(t, t.TempDir(), t.TempDir(), "WIPNOTE_VERSION=latest", "WIPNOTE_SETUP_STRICT=1")
	if err == nil || !strings.Contains(out, "pinned") {
		t.Fatalf("latest accepted: %v\n%s", err, out)
	}
}

// A release host that accepts the connection and never answers must not
// hang the environment's start: curl's timeouts end the download and the
// script exits 0 (non-strict). The timeouts are shortened for the test.
func TestCloudEnvSetup_StalledServerReturnsBounded(t *testing.T) {
	requireScriptTools(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	var held []net.Conn
	var mu sync.Mutex
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			held = append(held, c) // accepted, never answered
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		mu.Lock()
		defer mu.Unlock()
		for _, c := range held {
			c.Close()
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", cloudSetupScript)
	cmd.WaitDelay = 3 * time.Second // a killed bash leaves curl holding the pipe
	cmd.Env = []string{"HOME=" + t.TempDir(), "PATH=/usr/bin:/bin", "WIPNOTE_VERSION=9.9.9",
		"WIPNOTE_RELEASE_BASE_URL=http://" + ln.Addr().String(), "WIPNOTE_CURL_MAX_TIME=1"}
	start := time.Now()
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("the script hung on a stalled server (killed after %s):\n%s", time.Since(start), out)
	}
	if err != nil || !strings.Contains(string(out), "download failed") {
		t.Fatalf("err %v after %s:\n%s", err, time.Since(start), out)
	}
}

// The skip trusts the stamp only when the binary reports the pinned
// version and the plugin tree is there; otherwise it reinstalls.
func TestCloudEnvSetup_SkipNeedsMatchingBinaryAndPlugin(t *testing.T) {
	requireScriptTools(t)
	home, base := t.TempDir(), t.TempDir()
	fakeRelease(t, base, "9.9.9", false)
	if out, err := runCloudSetup(t, home, base, "WIPNOTE_VERSION=9.9.9"); err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	bin := filepath.Join(home, ".local", "bin", "wipnote")
	plugin := filepath.Join(home, ".local", "share", "wipnote", "plugin")
	reinstalled := func(why string) {
		t.Helper()
		out, err := runCloudSetup(t, home, base, "WIPNOTE_VERSION=9.9.9", "WIPNOTE_SETUP_STRICT=1")
		if err != nil || strings.Contains(out, "already installed") || !strings.Contains(out, "Installed wipnote v9.9.9") {
			t.Fatalf("%s: not reinstalled: %v\n%s", why, err, out)
		}
		if got, _ := exec.Command(bin).Output(); strings.TrimSpace(string(got)) != "wipnote 9.9.9 (go)" {
			t.Fatalf("%s: binary reports %q", why, got)
		}
		if _, err := os.Stat(filepath.Join(plugin, ".claude-plugin", "plugin.json")); err != nil {
			t.Fatalf("%s: plugin tree %v", why, err)
		}
	}
	writeTestFile(t, bin, "#!/bin/sh\necho \"wipnote 9.9.90 (go)\"\n") // stamp still says 9.9.9
	reinstalled("binary reports another version")
	if err := os.RemoveAll(plugin); err != nil {
		t.Fatal(err)
	}
	reinstalled("plugin tree missing")
	out, err := runCloudSetup(t, home, base, "WIPNOTE_VERSION=9.9.9")
	if err != nil || !strings.Contains(out, "already installed") {
		t.Fatalf("all good: %v\n%s", err, out)
	}
}
