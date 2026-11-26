package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/core/paths"
	"github.com/shakestzd/wipnote/internal/delegate/oneshot"
)

// A launch sets the store's own flag once, says so, and is silent afterwards.
func TestStoreTrustAtLaunch_SetsTheFlagOnceAndSaysSo(t *testing.T) {
	owner, cfgPath := trustStore(t)
	line := storeTrustAtLaunch(owner, "")
	if !strings.Contains(line, "marked the store folder "+owner) || !strings.Contains(line, "launch.claude.trustStore") {
		t.Fatalf("no notice for the first launch: %q", line)
	}
	if ok, err := oneshot.TrustAccepted(owner); !ok || err != nil {
		t.Fatalf("the store's flag was not set: %v %v", ok, err)
	}
	before, _ := os.ReadFile(cfgPath)
	if line := storeTrustAtLaunch(owner, ""); line != "" {
		t.Fatalf("second launch spoke: %q", line)
	}
	if after, _ := os.ReadFile(cfgPath); string(before) != string(after) {
		t.Fatal("second launch rewrote Claude Code's config")
	}
}

// launch.claude.trustStore false leaves the flag to the owner and still warns.
func TestStoreTrustAtLaunch_ConfigOffOnlyWarns(t *testing.T) {
	owner, cfgPath := trustStore(t)
	wipnoteCfg := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(wipnoteCfg, []byte(`{"launch":{"claude":{"trustStore":false}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	line := storeTrustAtLaunch(owner, wipnoteCfg)
	if !strings.Contains(line, "wipnote store trust") {
		t.Fatalf("no warning with the key off: %q", line)
	}
	if _, err := os.Stat(cfgPath); err == nil {
		t.Fatal("Claude Code's config was written with the key off")
	}
	for _, body := range []string{`{"launch":{"claude":{"trustStore":true}}}`, `{}`, `not json`} {
		if err := os.WriteFile(wipnoteCfg, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if !launchTrustsStore(wipnoteCfg) {
			t.Fatalf("config %q turned launch trust off", body)
		}
	}
}

// A store whose code root this machine does not key to it, such as one copied
// from another machine, gets no trust from a launch.
func TestStoreTrustAtLaunch_SkipsAStoreNotMadeHere(t *testing.T) {
	owner, cfgPath := trustStore(t)
	copied := filepath.Join(paths.ProjectsDir(), "-from-another-machine")
	if err := os.CopyFS(copied, os.DirFS(owner)); err != nil {
		t.Fatal(err)
	}
	if paths.StoreMadeHere(copied) || !paths.StoreMadeHere(owner) {
		t.Fatalf("StoreMadeHere: copied %v, own %v", paths.StoreMadeHere(copied), paths.StoreMadeHere(owner))
	}
	if line := storeTrustAtLaunch(copied, ""); !strings.Contains(line, "wipnote store trust") {
		t.Fatalf("no warning for a copied store: %q", line)
	}
	gone := filepath.Join(paths.ProjectsDir(), "-gone")
	if err := os.CopyFS(gone, os.DirFS(owner)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gone, paths.ProjectFileName),
		[]byte(`{"kind":"wipnote-external-store","code_root":"/no/such/project"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if line := storeTrustAtLaunch(gone, ""); !strings.Contains(line, "wipnote store trust") {
		t.Fatalf("no warning for a store with a missing code root: %q", line)
	}
	if _, err := os.Stat(cfgPath); err == nil {
		t.Fatal("Claude Code's config was written for a store not made here")
	}
}

// A config wipnote cannot write is left alone, and the launch warns with the reason.
func TestStoreTrustAtLaunch_UnwritableConfigWarns(t *testing.T) {
	owner, cfgPath := trustStore(t)
	dir := filepath.Dir(cfgPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil { // no lock and no file can be made here
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	line := storeTrustAtLaunch(owner, "")
	if !strings.Contains(line, "wipnote store trust") || !strings.Contains(line, "could not set it") {
		t.Fatalf("no warning for a read-only config folder: %q", line)
	}
	if ok, _ := oneshot.TrustAccepted(owner); ok {
		t.Fatal("trust was set in a read-only folder")
	}
}

// An in-repo store needs nothing and gets nothing.
func TestStoreTrustAtLaunch_InRepoStoreIsSilent(t *testing.T) {
	_, cfgPath := trustStore(t)
	if line := storeTrustAtLaunch(t.TempDir(), ""); line != "" {
		t.Fatalf("launch spoke for an in-repo store: %q", line)
	}
	if _, err := os.Stat(cfgPath); err == nil {
		t.Fatal("Claude Code's config was written for an in-repo store")
	}
}
